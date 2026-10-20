package main

// npm_registry_test.go —— 设置页「npm 镜像源」的配置语义：
//   - 默认阿里云；旧配置缺字段 / 值非法时回退默认；
//   - 选中的源随 config.json 持久化，重载后原样保留；
//   - dsh 安装与两个受管插件都取 selectedNpmMirror()，即「选哪个就用哪个」；
//   - 保存设置时非法值被拒绝，旧版前端不带该字段时沿用已保存的选择。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNpmRegistryConfigDefaultsAndPersistence(t *testing.T) {
	prevCfg := GetConfig()
	t.Cleanup(func() { initConfig(&prevCfg) })
	dir := t.TempDir()

	// 旧配置没有 npmRegistry 字段 → 默认阿里云。
	legacy := filepath.Join(dir, "legacy.json")
	if err := os.WriteFile(legacy, []byte(`{"dshPort":13080}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadConfig(&RuntimeEnv{ConfigFile: legacy}).NpmRegistry; got != npmRegistryDefault {
		t.Fatalf("旧配置缺字段时应回退 %q，实得 %q", npmRegistryDefault, got)
	}

	// 存了未知值 → 回退默认（不能把未知 slug 传给 npm/日志）。
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"dshPort":13080,"npmRegistry":"nowhere"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadConfig(&RuntimeEnv{ConfigFile: bad}).NpmRegistry; got != npmRegistryDefault {
		t.Fatalf("非法值应回退 %q，实得 %q", npmRegistryDefault, got)
	}

	// 选中的源随配置落盘，重载后原样保留。
	renv := &RuntimeEnv{ConfigFile: filepath.Join(dir, "config.json")}
	cfg := defaultConfig()
	cfg.NpmRegistry = "huawei"
	cfg.DshMemLimit = 1024
	if err := SaveConfig(renv, &cfg, false); err != nil {
		t.Fatalf("保存配置失败: %v", err)
	}
	if got := LoadConfig(renv).NpmRegistry; got != "huawei" {
		t.Fatalf("重载后的镜像源 = %q, want huawei", got)
	}
}

// selectedNpmMirror 是 dsh 安装、版本列表与两个受管插件共用的唯一取用入口。
func TestSelectedNpmMirrorFollowsConfig(t *testing.T) {
	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })

	for _, tc := range []struct {
		slug, name, url string
	}{
		{"aliyun", "阿里云", "https://registry.npmmirror.com"},
		{"tencent", "腾讯云", "https://mirrors.cloud.tencent.com/npm"},
		{"huawei", "华为云", "https://repo.huaweicloud.com/repository/npm"},
		{"npmjs", "官方源", "https://registry.npmjs.org"},
		// 非法值 / 空串按默认阿里云处理（保存时已拒绝，这里只是防御）。
		{"nowhere", "阿里云", "https://registry.npmmirror.com"},
		{"", "阿里云", "https://registry.npmmirror.com"},
	} {
		cfg := GetConfig()
		cfg.NpmRegistry = tc.slug
		initConfig(&cfg)
		got := selectedNpmMirror()
		if got.Name != tc.name || got.URL != tc.url {
			t.Fatalf("npmRegistry=%q 时选中 (%q, %q), want (%q, %q)",
				tc.slug, got.Name, got.URL, tc.name, tc.url)
		}
	}
}

// handleSaveSettings 保存时的镜像源校验：非法值 400、旧前端空字段沿用旧选择。
func TestSaveSettingsValidatesNpmRegistry(t *testing.T) {
	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })

	cfg := defaultConfig()
	cfg.DshMemLimit = 2048
	cfg.NpmRegistry = "tencent"
	initConfig(&cfg)

	renv := &RuntimeEnv{ConfigFile: filepath.Join(t.TempDir(), "config.json")}
	m := &AdminMux{renv: renv, dsh: newTestDshManager(t.TempDir(), ""), boot: newBootState()}

	save := func(mutate func(*AppConfig)) int {
		t.Helper()
		next := GetConfig()
		mutate(&next)
		body, err := json.Marshal(map[string]interface{}{"config": next})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(string(body)))
		rec := httptest.NewRecorder()
		m.handleSaveSettings(rec, req)
		return rec.Code
	}

	// 非法值：拒绝保存，配置保持原样。
	if code := save(func(c *AppConfig) { c.NpmRegistry = "nowhere" }); code != http.StatusBadRequest {
		t.Fatalf("非法镜像源的状态 = %d, want 400", code)
	}
	if got := GetConfig().NpmRegistry; got != "tencent" {
		t.Fatalf("被拒后镜像源 = %q, want tencent", got)
	}

	// 旧版前端不带该字段（空串）：按「未修改」处理，不能重置用户的选择。
	if code := save(func(c *AppConfig) { c.NpmRegistry = "" }); code != http.StatusOK {
		t.Fatalf("空字段（旧前端）的状态 = %d, want 200", code)
	}
	if got := GetConfig().NpmRegistry; got != "tencent" {
		t.Fatalf("空字段保存后镜像源 = %q, want tencent", got)
	}

	// 合法值：保存并落盘。
	if code := save(func(c *AppConfig) { c.NpmRegistry = "npmjs" }); code != http.StatusOK {
		t.Fatalf("官方源的保存状态 = %d, want 200", code)
	}
	onDisk, err := os.ReadFile(renv.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(onDisk), `"npmRegistry": "npmjs"`) {
		t.Fatalf("落盘配置里应有 npmRegistry=npmjs:\n%s", onDisk)
	}
}
