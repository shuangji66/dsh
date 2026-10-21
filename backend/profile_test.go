package main

// profile_test.go —— 官方 bundle 层启停的回归测试。
//
// 控制台的插件开关对齐 @deepseek-ai/dsh-plugin-manager 的 setBundleEnabled：
//   - 只改 profile 清单的 dsh.profile.bundles；
//   - 停用保留 dependencies，启用追加到末尾；
//   - 不写 cordis.patch.yml，不碰 .dsh-market/state.json。

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeProfileFixture(t *testing.T, manifest string, bundles map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, pkgManifest := range bundles {
		pkgDir := filepath.Join(dir, "node_modules", name)
		if err := os.MkdirAll(pkgDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(pkgManifest), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestSetPluginBundleEnabledPreservesManifest(t *testing.T) {
	manifest := `{
  "name": "dsh-profile-web",
  "private": true,
  "custom": {
    "keep": "me"
  },
  "dependencies": {
    "a-plugin": "1.0.0",
    "b-plugin": "2.0.0"
  },
  "dsh": {
    "profile": {
      "bundles": [
        "a-plugin",
        "b-plugin"
      ],
      "patchReload": "live"
    }
  }
}`
	dir := writeProfileFixture(t, manifest, map[string]string{
		"a-plugin": `{"name":"a-plugin","dsh":{"bundle":{"patch":"./cordis.patch.yml"}}}`,
		"b-plugin": `{"name":"b-plugin","dsh":{"bundle":{"patch":"./cordis.patch.yml"}}}`,
	})

	changed, err := setPluginBundleEnabled(dir, "a-plugin", false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("停用应报告清单发生变化")
	}
	bundles, err := readProfileBundles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundles) != 1 || bundles[0] != "b-plugin" {
		t.Fatalf("停用后 bundles = %v, want [b-plugin]", bundles)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("回写的清单必须是合法 JSON: %v\n%s", err, raw)
	}
	if _, ok := parsed["custom"]; !ok {
		t.Fatalf("无关字段被丢掉: %s", raw)
	}
	deps, _ := parsed["dependencies"].(map[string]interface{})
	if _, ok := deps["a-plugin"]; !ok {
		t.Fatalf("停用只应取消 bundle 选择，依赖必须保留: %s", raw)
	}
	dsh, _ := parsed["dsh"].(map[string]interface{})
	profile, _ := dsh["profile"].(map[string]interface{})
	if profile["patchReload"] != "live" {
		t.Fatalf("dsh.profile 的其它字段被丢掉: %s", raw)
	}

	// 启用：追加到末尾。
	changed, err = setPluginBundleEnabled(dir, "a-plugin", true)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("启用应报告清单发生变化")
	}
	bundles, _ = readProfileBundles(dir)
	if got, want := strings.Join(bundles, ","), "b-plugin,a-plugin"; got != want {
		t.Fatalf("启用应追加到末尾: got %q, want %q", got, want)
	}

	// 幂等：已启用再启用不写盘。
	changed, err = setPluginBundleEnabled(dir, "a-plugin", true)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("重复启用不应改动清单")
	}

	// 权限与官方 saveManifest 一致（0600）。
	info, err := os.Stat(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("清单权限 = %o, want 600", got)
	}
}

func TestSetPluginBundleEnabledRejectsNonBundle(t *testing.T) {
	manifest := `{"name":"x","dsh":{"profile":{"bundles":[]}}}`
	dir := writeProfileFixture(t, manifest, map[string]string{
		"plain-dep":    `{"name":"plain-dep","version":"1.0.0"}`,
		"empty-bundle": `{"name":"empty-bundle","dsh":{"bundle":{}}}`,
	})
	before, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"plain-dep", "empty-bundle"} {
		changed, err := setPluginBundleEnabled(dir, name, true)
		if !errors.Is(err, errNotBundle) {
			t.Fatalf("%s 应返回 errNotBundle，实际 %v", name, err)
		}
		if changed {
			t.Fatalf("%s 被拒时不应报告清单变化", name)
		}
	}
	after, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("被拒时不得改动清单: %q -> %q", before, after)
	}
}

func TestSetPluginBundleEnabledCreatesProfileLayers(t *testing.T) {
	dir := writeProfileFixture(t, `{"name":"x"}`, map[string]string{
		"a-plugin": `{"name":"a-plugin","dsh":{"bundle":{"patch":"./cordis.patch.yml"}}}`,
	})
	if _, err := setPluginBundleEnabled(dir, "a-plugin", true); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"name": "x"`) {
		t.Fatalf("原有字段应保留: %s", raw)
	}
	bundles, err := readProfileBundles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundles) != 1 || bundles[0] != "a-plugin" {
		t.Fatalf("应创建 dsh.profile.bundles: %v (%s)", bundles, raw)
	}
}

func TestReadProfileBundlesToleratesMissingLayers(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	bundles, err := readProfileBundles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundles) != 0 {
		t.Fatalf("缺少 dsh/profile/bundles 时应为空，实际 %v", bundles)
	}
}
