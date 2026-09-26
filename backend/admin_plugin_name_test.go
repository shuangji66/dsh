package main

// admin_plugin_name_test.go —— 插件名字校验的回归测试（审查 C5）。
//
// 旧实现只用 `body.Name == ""` 校验：
//   - handleTogglePlugin 随后会 readPackageRowIds(profileWebDir, name)，把名字拼进
//     .../node_modules/<name> —— `../../../etc` 之类可穿越读取任意目录下的
//     package.json / 补丁文件（其中的 `- id:` 行会被写进补丁层）；
//   - handleRemovePlugin 把名字原样交给 `dsh plugin --profile web remove <name>`，
//     `--foo` 这种以 `-` 开头的名字会被 dsh 当成选项（argv 注入）。
//
// 现在两个入口都先过 packageNameRe（与 patch.go 同一条规则），不合法直接 400，
// 不产生任何文件操作、也不执行任何命令。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pluginGuardFixture 造一个只碰临时目录的 AdminMux：假 HOME 里有 profiles/web 与
// .dsh-market/state.json（哨兵），用于断言非法名字不会改动任何东西。
func pluginGuardFixture(t *testing.T) (*AdminMux, string, string, string) {
	t.Helper()
	home := t.TempDir()
	profileWeb := filepath.Join(home, ".dsh", "profiles", "web")
	if err := os.MkdirAll(filepath.Join(profileWeb, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	statePath := marketStateFile(profileWeb)
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	const stateBody = `{"disabled":["keep-me"]}`
	if err := os.WriteFile(statePath, []byte(stateBody), 0o600); err != nil {
		t.Fatal(err)
	}

	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })
	cfg := defaultConfig()
	cfg.HomeDir = home
	// 端口取一个不会命中本机真实 dsh 进程的值（Stop/扫描 /proc 时会用到）。
	cfg.DshPort = 65535
	initConfig(&cfg)

	// 把 PATH 换成只含假 dsh 的目录：即便某个入口漏了校验（旧代码），也只会执行
	// 我们的假脚本并留下标记文件，而不会真的去动宿主机上的 dsh 安装。
	binDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "dsh-ran")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + marker + "\n"
	if err := os.WriteFile(filepath.Join(binDir, "dsh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	dsh := newTestDshManager(home, "")
	m := &AdminMux{
		renv:   &RuntimeEnv{Home: home, Path: binDir, ConfigFile: filepath.Join(t.TempDir(), "config.json")},
		dsh:    dsh,
		update: &UpdateManager{dsh: dsh},
	}
	return m, statePath, marker, profileWeb
}

func postPlugin(t *testing.T, m *AdminMux, path string, body map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	if path == "/api/plugins/toggle" {
		m.handleTogglePlugin(rec, req)
	} else {
		m.handleRemovePlugin(rec, req)
	}
	return rec
}

// 穿越型插件名：立刻 400，且不产生任何文件操作（state.json 一个字节都不变）。
func TestTogglePluginRejectsTraversalName(t *testing.T) {
	m, statePath, _, _ := pluginGuardFixture(t)
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	rec := postPlugin(t, m, "/api/plugins/toggle", map[string]interface{}{"name": "../../../etc", "enabled": false})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("穿越型插件名应 400，实际 %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "插件名不合法") {
		t.Fatalf("错误文案应说明插件名不合法: %s", rec.Body.String())
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("被拒时不得改动 state.json: %q -> %q", before, after)
	}
	// 补丁层与 package.json 也不该被触碰（旧实现会尝试读 /etc/package.json）。
	for _, p := range []string{pluginPatchPath(m.dsh.effectiveHome()), filepath.Join(m.dsh.effectiveHome(), ".dsh", "profiles", "web", "package.json")} {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("被拒时不应产生文件 %s", p)
		}
	}
}

// 以 `-` 开头的插件名（argv 注入）必须 400，且绝不执行 dsh 命令。
func TestRemovePluginRejectsOptionLikeName(t *testing.T) {
	m, _, marker, _ := pluginGuardFixture(t)

	for _, name := range []string{"--foo", "-f", "../../../etc", "a/b/../../.."} {
		rec := postPlugin(t, m, "/api/plugins/remove", map[string]interface{}{"name": name})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("name=%q 应 400，实际 %d (%s)", name, rec.Code, rec.Body.String())
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("被拒时不得执行 dsh 命令（假 dsh 被调用了）")
	}
}

// 正向对照：合法名字仍要放行并真的执行命令 —— 否则上面的「没有调用」可能只是因为
// 测试桩本身失效。合法名字包括带 @scope/ 的形态（不能过度收紧）。
func TestPluginNameValidationKeepsValidNames(t *testing.T) {
	m, _, marker, _ := pluginGuardFixture(t)

	rec := postPlugin(t, m, "/api/plugins/remove", map[string]interface{}{"name": "@scope/my-plugin.v2"})
	if rec.Code != http.StatusOK {
		t.Fatalf("合法包名应放行，实际 %d (%s)", rec.Code, rec.Body.String())
	}
	raw, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("合法名字应真正执行 dsh 命令（假 dsh 未留下标记）: %v", err)
	}
	if !strings.Contains(string(raw), "plugin --profile web remove @scope/my-plugin.v2") {
		t.Fatalf("命令参数不符: %q", raw)
	}

	if rec := postPlugin(t, m, "/api/plugins/toggle", map[string]interface{}{"name": "@linxin666/dsh-client-ui-git-graph", "enabled": false}); rec.Code != http.StatusOK {
		t.Fatalf("带 scope 的合法包名应放行，实际 %d (%s)", rec.Code, rec.Body.String())
	}
}
