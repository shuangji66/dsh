package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// busyMarket 把「市场正在安装/更新插件」注入 replaceBusyGuard 的探测来源，并在测试
// 结束时还原。返回的函数用于显式提前还原（也可依赖 t.Cleanup）。
func busyMarket(t *testing.T) func() {
	t.Helper()
	prev := marketBusyFn
	marketBusyFn = func(*UpdateManager) (bool, string) { return true, "安装插件" }
	restore := func() { marketBusyFn = prev }
	t.Cleanup(restore)
	return restore
}

// 插件操作在跑时「重置插件」必须被忙守卫拒绝，且不得删除 profiles 目录。
// 这条路径会删掉整个 profiles 并重启 dsh（终止进程组）—— 正是 AGENTS 第 5 节
// gotcha 2 要求先过守卫的场景。
func TestResetPluginsRefusedWhenPluginBusy(t *testing.T) {
	home := t.TempDir()
	profiles := filepath.Join(home, ".dsh", "profiles", "web")
	if err := os.MkdirAll(profiles, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(profiles, "package.json")
	if err := os.WriteFile(marker, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })
	cfg := defaultConfig()
	cfg.HomeDir = home
	initConfig(&cfg)

	dsh := newTestDshManager(home, "")
	m := &AdminMux{renv: &RuntimeEnv{}, dsh: dsh, update: &UpdateManager{dsh: dsh}}
	busyMarket(t)

	rec := httptest.NewRecorder()
	m.handleResetPlugins(rec, httptest.NewRequest(http.MethodPost, "/api/plugins/reset", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("插件操作进行中时重置插件应 409，实际 %d (%s)", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("被拒时不得删除 profiles 目录: %v", err)
	}
}

// 插件操作在跑时「切换主目录」必须被忙守卫拒绝：不改配置、不删目标 ~/.dsh、不重启 dsh。
func TestSetHomeRefusedWhenPluginBusy(t *testing.T) {
	home := t.TempDir()
	srcDsh := filepath.Join(home, ".dsh")
	if err := os.MkdirAll(srcDsh, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	// 目标目录已有一份 .dsh：migrate 分支会先整体删除它，这正是要挡住的动作。
	destDsh := filepath.Join(dest, ".dsh")
	if err := os.MkdirAll(destDsh, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(destDsh, "keep-me")
	if err := os.WriteFile(keep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })
	cfg := defaultConfig()
	cfg.HomeDir = home
	initConfig(&cfg)

	cfgFile := filepath.Join(t.TempDir(), "config.json")
	dsh := newTestDshManager(home, "")
	m := &AdminMux{renv: &RuntimeEnv{ConfigFile: cfgFile}, dsh: dsh, update: &UpdateManager{dsh: dsh}}
	busyMarket(t)

	payload, err := json.Marshal(map[string]interface{}{"path": dest, "migrate": true})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.handleSetHome(rec, httptest.NewRequest(http.MethodPost, "/api/dsh/set-home", strings.NewReader(string(payload))))
	if rec.Code != http.StatusConflict {
		t.Fatalf("插件操作进行中时切换主目录应 409，实际 %d (%s)", rec.Code, rec.Body.String())
	}
	if got := GetConfig().HomeDir; got != home {
		t.Fatalf("被拒时配置不应改变，HomeDir = %s, want %s", got, home)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("被拒时不得删除目标 ~/.dsh: %v", err)
	}
	if _, err := os.Stat(cfgFile); err == nil {
		t.Fatal("被拒时不应写配置文件")
	}
}
