package main

import (
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

// 新的接口（dsh 版本 / 插件市场 / 数据备份）在「有别的操作正在进行」时也要回 409，
// 而不是 400 —— 409 表示「现在不行、稍后重试就行」，400 表示「这次请求本身有问题」。
//
// 注意这里有两把锁，别混：插件市场/备份/恢复/更新用 UpdateManager.applying，
// dsh 版本安装/删除/切换用 ServerManager.applying（它们改的是不同的目录）。
func newBusyAPIFixture(t *testing.T) (*AdminMux, *UpdateManager, string) {
	t.Helper()
	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })
	cfg := defaultConfig()
	initConfig(&cfg)

	dataDir := t.TempDir()
	home := t.TempDir()
	// 备份要求 HOME 下的 ~/.dsh 存在（这条检查在互斥之前）。
	if err := os.MkdirAll(filepath.Join(home, ".dsh"), 0o755); err != nil {
		t.Fatal(err)
	}
	dsh := newTestDshManager(home, "")
	upd := &UpdateManager{
		renv:     &RuntimeEnv{DataDir: dataDir, Home: home},
		dsh:      dsh,
		statuses: map[updateKind]*UpdateStatus{updateKindMarket: {Kind: updateKindMarket}},
	}
	dsh.renv = upd.renv
	upd.server = newServerManager(upd.renv, dsh, upd)
	return &AdminMux{renv: upd.renv, dsh: dsh, update: upd}, upd, dataDir
}

func TestDshVersionAPIsReturn409WhenBusy(t *testing.T) {
	m, upd, dataDir := newBusyAPIFixture(t)
	// 切换/删除要用一个「已安装」的版本，否则会先因为「尚未安装」被挡成 400；
	// 安装则要挑一个还没装的版本（已装的会先被「无需重复下载」挡成 400）。
	fakeInstalledVersion(t, dataDir, "0.2.0")

	upd.server.applying.Lock()
	defer upd.server.applying.Unlock()

	cases := []struct {
		name    string
		handler http.HandlerFunc
		target  string
		version string
	}{
		{"安装", m.handleDshVersionInstall, "/api/dsh/versions/install", "0.3.0"},
		{"切换", m.handleDshVersionSwitch, "/api/dsh/versions/switch", "0.2.0"},
		{"删除", m.handleDshVersionDelete, "/api/dsh/versions/delete", "0.2.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tc.target,
				strings.NewReader(`{"version":"`+tc.version+`"}`))
			tc.handler(rec, req)
			if rec.Code != http.StatusConflict {
				t.Fatalf("忙时应回 409，实际 %d (%s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestMarketAndBackupAPIsReturn409WhenBusy(t *testing.T) {
	m, upd, _ := newBusyAPIFixture(t)
	upd.applying.Lock()
	defer upd.applying.Unlock()

	cases := []struct {
		name    string
		handler http.HandlerFunc
		target  string
	}{
		{"市场安装", m.handleMarketInstall, "/api/market/install"},
		{"市场更新", m.handleMarketUpdate, "/api/market/update"},
		{"市场卸载", m.handleMarketRemove, "/api/market/remove"},
		{"数据备份", m.handleDshBackup, "/api/dsh/backup"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.handler(rec, httptest.NewRequest(http.MethodPost, tc.target, strings.NewReader("{}")))
			if rec.Code != http.StatusConflict {
				t.Fatalf("忙时应回 409，实际 %d (%s)", rec.Code, rec.Body.String())
			}
		})
	}
}
