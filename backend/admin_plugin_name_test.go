package main

// admin_plugin_name_test.go —— 插件名字校验与官方 bundle 启停的回归测试。
//
// 名字校验（审查 C5）：
//   - handleTogglePlugin 会把它拼进
//     .../node_modules/<name>/package.json —— `../../../etc` 之类可穿越读取任意
//     目录下的 package.json；
//   - handleRemovePlugin 把名字原样交给 `dsh plugin --profile web remove <name>`，
//     `--foo` 这种以 `-` 开头的名字会被 dsh 当成选项（argv 注入）。
//
// 现在两个入口都先过 packageNameRe（profile.go），不合法直接 400，不产生任何
// 文件操作、也不执行任何命令。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pluginGuardFixture 造一个只碰临时目录的 AdminMux：假 HOME 里有 profiles/web、
// 一个合法的 profile 清单（两个 bundle + 一个普通依赖）、市场 state.json 哨兵。
func pluginGuardFixture(t *testing.T) (*AdminMux, string, string, string) {
	t.Helper()
	home := t.TempDir()
	profileWeb := filepath.Join(home, ".dsh", "profiles", "web")
	if err := os.MkdirAll(filepath.Join(profileWeb, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
  "name": "dsh-profile-web",
  "private": true,
  "dependencies": {
    "@linxin666/dsh-client-ui-git-graph": "^0.1.0",
    "@scope/my-plugin.v2": "^1.0.0",
    "plain-dep": "^2.0.0"
  },
  "dsh": {
    "profile": {
      "bundles": [
        "@linxin666/dsh-client-ui-git-graph",
        "@scope/my-plugin.v2"
      ],
      "patchReload": "live"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(profileWeb, "package.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []string{"@linxin666/dsh-client-ui-git-graph", "@scope/my-plugin.v2"} {
		dir := filepath.Join(profileWeb, "node_modules", pkg)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := `{"name":"` + pkg + `","dsh":{"bundle":{"patch":"./cordis.patch.yml"}}}`
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// 市场自己的状态文件：控制台不再读它、更不会写它，这里作为哨兵。
	statePath := filepath.Join(profileWeb, ".dsh-market", "state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	const stateBody = `{"disabled":["keep-me"]}`
	if err := os.WriteFile(statePath, []byte(stateBody), 0o600); err != nil {
		t.Fatal(err)
	}

	// 造一个「已安装并选中」的假 dsh 版本：控制台按选中版本的**绝对路径**执行
	// `dsh plugin …`（见 server.go / dshBinPath），不依赖 PATH 里恰好有一份 dsh。
	dataDir := t.TempDir()
	binDir := filepath.Join(dataDir, "server", "1.2.3", "node_modules", ".bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "dsh-ran")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + marker + "\n"
	if err := os.WriteFile(filepath.Join(binDir, "dsh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })
	cfg := defaultConfig()
	// 端口取一个不会命中本机真实 dsh 进程的值（Stop/扫描 /proc 时会用到）。
	cfg.DshPort = 65535
	cfg.DshVersion = "1.2.3"
	initConfig(&cfg)

	renv := &RuntimeEnv{Home: home, Path: binDir, DataDir: dataDir, ConfigFile: filepath.Join(t.TempDir(), "config.json")}
	dsh := newTestDshManager(home, "")
	dsh.renv = renv
	m := &AdminMux{
		renv:   renv,
		dsh:    dsh,
		update: &UpdateManager{renv: renv, dsh: dsh},
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

// 穿越型插件名：立刻 400，且 profile 清单、市场 state.json 一个字节都不变，
// 也不会凭空生成 cordis.patch.yml。
func TestTogglePluginRejectsTraversalName(t *testing.T) {
	m, statePath, _, profileWeb := pluginGuardFixture(t)
	manifestPath := filepath.Join(profileWeb, "package.json")
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	manifestBefore, err := os.ReadFile(manifestPath)
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
	stateAfter, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(stateAfter) != string(stateBefore) {
		t.Fatalf("被拒时不得改动 state.json: %q -> %q", stateBefore, stateAfter)
	}
	manifestAfter, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(manifestAfter) != string(manifestBefore) {
		t.Fatalf("被拒时不得改动 profile 清单: %q -> %q", manifestBefore, manifestAfter)
	}
	if _, err := os.Stat(filepath.Join(profileWeb, "cordis.patch.yml")); err == nil {
		t.Fatal("被拒时不应产生 cordis.patch.yml")
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

// 官方 bundle 开关：停用=从 dsh.profile.bundles 移除（依赖保留），启用=追加到末尾。
func TestTogglePluginFollowsOfficialBundleSelection(t *testing.T) {
	m, statePath, _, profileWeb := pluginGuardFixture(t)
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	// 停用第一个 bundle：只从 bundles 移除，dependencies 原样保留。
	if rec := postPlugin(t, m, "/api/plugins/toggle", map[string]interface{}{"name": "@linxin666/dsh-client-ui-git-graph", "enabled": false}); rec.Code != http.StatusOK {
		t.Fatalf("停用应成功，实际 %d (%s)", rec.Code, rec.Body.String())
	}
	bundles, err := readProfileBundles(profileWeb)
	if err != nil {
		t.Fatal(err)
	}
	if containsString(bundles, "@linxin666/dsh-client-ui-git-graph") {
		t.Fatalf("停用后不该再出现在 bundles: %v", bundles)
	}
	if !containsString(bundles, "@scope/my-plugin.v2") {
		t.Fatalf("其它 bundle 不应被带动: %v", bundles)
	}
	var manifest struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	raw, err := os.ReadFile(filepath.Join(profileWeb, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if _, ok := manifest.Dependencies["@linxin666/dsh-client-ui-git-graph"]; !ok {
		t.Fatal("停用只应取消 bundle 选择，依赖必须保留")
	}

	// 重新启用：追加到末尾（官方行为，可能改变配置优先级）。
	if rec := postPlugin(t, m, "/api/plugins/toggle", map[string]interface{}{"name": "@linxin666/dsh-client-ui-git-graph", "enabled": true}); rec.Code != http.StatusOK {
		t.Fatalf("启用应成功，实际 %d (%s)", rec.Code, rec.Body.String())
	}
	bundles, err = readProfileBundles(profileWeb)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(bundles), 2; got != want {
		t.Fatalf("启用后 bundles 数量 = %d, want %d (%v)", got, want, bundles)
	}
	if bundles[len(bundles)-1] != "@linxin666/dsh-client-ui-git-graph" {
		t.Fatalf("启用应追加到末尾，实际 %v", bundles)
	}

	// 控制台不再碰插件市场的 state.json，也不写 cordis.patch.yml。
	stateAfter, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(stateAfter) != string(stateBefore) {
		t.Fatalf("启停不得改动市场 state.json: %q -> %q", stateBefore, stateAfter)
	}
	if _, err := os.Stat(filepath.Join(profileWeb, "cordis.patch.yml")); err == nil {
		t.Fatal("官方 bundle 开关不应写 cordis.patch.yml")
	}
}

// 没有声明 dsh.bundle 的普通依赖不是官方插件页里的 bundle：拒绝启停且不改盘。
func TestTogglePluginRejectsNonBundleDependency(t *testing.T) {
	m, _, _, profileWeb := pluginGuardFixture(t)
	manifestPath := filepath.Join(profileWeb, "package.json")
	before, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}

	rec := postPlugin(t, m, "/api/plugins/toggle", map[string]interface{}{"name": "plain-dep", "enabled": false})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("普通依赖应 400，实际 %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "err_plugin_not_bundle" {
		t.Fatalf("错误 code = %q, want err_plugin_not_bundle (%s)", body.Code, rec.Body.String())
	}
	after, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("被拒时不得改动 profile 清单: %q -> %q", before, after)
	}
}
