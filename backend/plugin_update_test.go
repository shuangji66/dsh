package main

// plugin_update_test.go —— 插件卡片「检查更新」的判定与入口。
//
// 规则对齐官方插件中心（dsh-plugin-manager 的 newerVersions / updateCandidate）：
//   - 只有比已装版本新的版本才可能是更新；
//   - 预发布版不算更新候选（要尝鲜可在安装弹窗里指定精确版本）；
//   - 废弃（deprecated）版本不算；
//   - 与当前 dsh 版本不兼容（engines.dsh / dsh peer 范围）的不算；
//   - 已装的是 bundle 时，候选也必须仍然声明 dsh.bundle；
//   - 取满足条件的**最高版本**。
// 另外，只有 profile 里记录为 registry 版本范围/标签的依赖才检查 —— git/本地路径装的包
// 在镜像源上可能同名，不能拿它比版本。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func manifestFromJSON(t *testing.T, raw string) *pluginManifest {
	t.Helper()
	var meta pluginManifest
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		t.Fatalf("解析 manifest 失败 (%s): %v", raw, err)
	}
	return &meta
}

func rawVersion(t *testing.T, body string) json.RawMessage {
	t.Helper()
	if !json.Valid([]byte(body)) {
		t.Fatalf("不是合法 JSON: %s", body)
	}
	return json.RawMessage(body)
}

func TestNewestCompatiblePluginVersion(t *testing.T) {
	installedPlain := manifestFromJSON(t, `{"name":"demo","version":"1.0.0"}`)
	installedBundle := manifestFromJSON(t, `{"name":"demo","version":"1.0.0","dsh":{"bundle":{"patch":"./cordis.patch.yml"}}}`)
	bundleBody := `"dsh":{"bundle":{"patch":"./cordis.patch.yml"}}`

	cases := []struct {
		name      string
		installed *pluginManifest
		runtime   string
		versions  map[string]json.RawMessage
		want      string
	}{
		{
			name:      "取比当前高的最高稳定版",
			installed: installedPlain,
			versions: map[string]json.RawMessage{
				"1.0.0": rawVersion(t, `{"name":"demo","version":"1.0.0"}`),
				"1.0.5": rawVersion(t, `{"name":"demo","version":"1.0.5"}`),
				"1.1.0": rawVersion(t, `{"name":"demo","version":"1.1.0"}`),
			},
			want: "1.1.0",
		},
		{
			name:      "预发布版不算更新",
			installed: installedPlain,
			versions: map[string]json.RawMessage{
				"1.1.0-beta.1": rawVersion(t, `{"name":"demo","version":"1.1.0-beta.1"}`),
			},
			want: "",
		},
		{
			name:      "稳定版优先于更高的预发布版",
			installed: installedPlain,
			versions: map[string]json.RawMessage{
				"1.1.0":         rawVersion(t, `{"name":"demo","version":"1.1.0"}`),
				"2.0.0-alpha.1": rawVersion(t, `{"name":"demo","version":"2.0.0-alpha.1"}`),
			},
			want: "1.1.0",
		},
		{
			name:      "废弃版本不算",
			installed: installedPlain,
			versions: map[string]json.RawMessage{
				"1.1.0": rawVersion(t, `{"name":"demo","version":"1.1.0"}`),
				"1.2.0": rawVersion(t, `{"name":"demo","version":"1.2.0","deprecated":"use 1.1"}`),
			},
			want: "1.1.0",
		},
		{
			name:      "与当前 dsh 不兼容的版本不算",
			installed: installedPlain,
			runtime:   "0.2.2",
			versions: map[string]json.RawMessage{
				"1.1.0": rawVersion(t, `{"name":"demo","version":"1.1.0","engines":{"dsh":">=0.2.1 <0.3.0"}}`),
				"1.2.0": rawVersion(t, `{"name":"demo","version":"1.2.0","engines":{"dsh":">=0.3.0"}}`),
			},
			want: "1.1.0",
		},
		{
			name:      "bundle 的候选必须仍是 bundle",
			installed: installedBundle,
			versions: map[string]json.RawMessage{
				"1.1.0": rawVersion(t, `{"name":"demo","version":"1.1.0"}`),
			},
			want: "",
		},
		{
			name:      "bundle 候选仍是 bundle 时可用",
			installed: installedBundle,
			versions: map[string]json.RawMessage{
				"1.1.0": rawVersion(t, `{"name":"demo","version":"1.1.0",`+bundleBody+`}`),
			},
			want: "1.1.0",
		},
		{
			name:      "普通依赖不要求 bundle",
			installed: installedPlain,
			versions: map[string]json.RawMessage{
				"1.1.0": rawVersion(t, `{"name":"demo","version":"1.1.0"}`),
			},
			want: "1.1.0",
		},
		{
			name:      "没有更高版本",
			installed: installedPlain,
			versions: map[string]json.RawMessage{
				"1.0.0": rawVersion(t, `{"name":"demo","version":"1.0.0"}`),
				"0.9.9": rawVersion(t, `{"name":"demo","version":"0.9.9"}`),
			},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := newestCompatiblePluginVersion(tc.versions, tc.installed, tc.runtime); got != tc.want {
				t.Fatalf("最新可用版本 = %q，期望 %q", got, tc.want)
			}
		})
	}
}

func TestPluginDependencyFromRegistry(t *testing.T) {
	registry := []string{"^1.2.3", "~1.2.3", "1.2.3", "latest", "next", "npm:other@1.0.0", "workspace:^"}
	notRegistry := []string{"github:owner/repo", "git+https://github.com/o/r.git", "https://example.com/x.tgz", "file:../plugin", "link:../plugin", "/vol1/1000/plugins/x", "../plugin"}
	for _, spec := range registry {
		if !pluginDependencyFromRegistry(spec) {
			t.Fatalf("%q 应按 registry 依赖处理", spec)
		}
	}
	for _, spec := range notRegistry {
		if pluginDependencyFromRegistry(spec) {
			t.Fatalf("%q 不应按 registry 依赖处理", spec)
		}
	}
}

// --- HTTP 入口（假镜像源）---

// pluginUpdateFixture 造一个临时 profile + 假 registry，返回可直接调 handler 的 mux。
type pluginUpdateFixture struct {
	m          *AdminMux
	profileWeb string
	manifest   string
}

func newPluginUpdateFixture(t *testing.T, packument string) *pluginUpdateFixture {
	t.Helper()
	home := t.TempDir()
	profileWeb := filepath.Join(home, ".dsh", "profiles", "web")
	if err := os.MkdirAll(filepath.Join(profileWeb, "node_modules", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := &pluginUpdateFixture{m: &AdminMux{}, profileWeb: profileWeb}
	f.manifest = `{
  "name": "dsh-profile-web",
  "private": true,
  "dependencies": { "demo": "^1.0.0" },
  "dsh": { "profile": { "bundles": [] } }
}`
	if err := os.WriteFile(filepath.Join(profileWeb, "package.json"), []byte(f.manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profileWeb, "node_modules", "demo", "package.json"),
		[]byte(`{"name":"demo","version":"1.0.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/demo" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(packument))
	}))
	t.Cleanup(srv.Close)

	prevMirrors := npmMirrors
	prevCfg := GetConfig()
	t.Cleanup(func() {
		npmMirrors = prevMirrors
		initConfig(&prevCfg)
	})
	npmMirrors = []npmMirror{{Name: "测试源", Slug: "test", URL: srv.URL}}
	cfg := defaultConfig()
	cfg.NpmRegistry = "test"
	initConfig(&cfg)

	dsh := newTestDshManager(home, "")
	dsh.renv = &RuntimeEnv{Home: home}
	f.m.dsh = dsh
	return f
}

func (f *pluginUpdateFixture) setDependency(t *testing.T, spec string) {
	t.Helper()
	body := strings.Replace(f.manifest, `"demo": "^1.0.0"`, `"demo": "`+spec+`"`, 1)
	if err := os.WriteFile(filepath.Join(f.profileWeb, "package.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func getPluginUpdate(t *testing.T, m *AdminMux, query string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	m.handlePluginUpdateCheck(rec, httptest.NewRequest(http.MethodGet, "/api/plugins/update-check?"+query, nil))
	return rec
}

func TestHandlePluginUpdateCheck(t *testing.T) {
	packument := `{
  "name": "demo",
  "versions": {
    "1.0.0": {"name": "demo", "version": "1.0.0"},
    "1.0.1": {"name": "demo", "version": "1.0.1"},
    "1.1.0": {"name": "demo", "version": "1.1.0"},
    "1.2.0-beta.1": {"name": "demo", "version": "1.2.0-beta.1"},
    "1.1.5": {"name": "demo", "version": "1.1.5", "deprecated": "old"}
  },
  "dist-tags": {"latest": "1.1.0"}
}`
	f := newPluginUpdateFixture(t, packument)

	rec := getPluginUpdate(t, f.m, "name=demo")
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200，实际 %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		OK        bool   `json:"ok"`
		Name      string `json:"name"`
		Current   string `json:"current"`
		Latest    string `json:"latest"`
		HasUpdate bool   `json:"hasUpdate"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	// 1.2.0-beta.1 是预发布、1.1.5 已废弃，最高可用是 1.1.0。
	if !body.OK || body.Current != "1.0.0" || body.Latest != "1.1.0" || !body.HasUpdate {
		t.Fatalf("检查结果不对: %+v", body)
	}
}

func TestHandlePluginUpdateCheckNoUpdate(t *testing.T) {
	f := newPluginUpdateFixture(t, `{"name":"demo","versions":{"1.0.0":{"name":"demo","version":"1.0.0"}}}`)
	rec := getPluginUpdate(t, f.m, "name=demo")
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200，实际 %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Latest    string `json:"latest"`
		HasUpdate bool   `json:"hasUpdate"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.HasUpdate || body.Latest != "" {
		t.Fatalf("已是最新版本时不应报更新: %+v", body)
	}
}

func TestHandlePluginUpdateCheckNonRegistryInstall(t *testing.T) {
	f := newPluginUpdateFixture(t, `{"name":"demo","versions":{"9.9.9":{"name":"demo","version":"9.9.9"}}}`)
	f.setDependency(t, "github:owner/repo")

	rec := getPluginUpdate(t, f.m, "name=demo")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("git 安装应 400，实际 %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "err_plugin_update_not_registry") {
		t.Fatalf("应带 err_plugin_update_not_registry，实际 %s", rec.Body.String())
	}
}

func TestHandlePluginUpdateCheckValidation(t *testing.T) {
	f := newPluginUpdateFixture(t, `{"name":"demo","versions":{}}`)
	if rec := getPluginUpdate(t, f.m, "name="); rec.Code != http.StatusBadRequest {
		t.Fatalf("缺少 name 应 400，实际 %d", rec.Code)
	}
	if rec := getPluginUpdate(t, f.m, "name=../../etc"); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 name 应 400，实际 %d", rec.Code)
	}
}

func TestProfileDependencySpec(t *testing.T) {
	home := t.TempDir()
	profileWeb := filepath.Join(home, ".dsh", "profiles", "web")
	if err := os.MkdirAll(profileWeb, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"web","dependencies":{"demo":"^1.2.3"}}`
	if err := os.WriteFile(filepath.Join(profileWeb, "package.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := profileDependencySpec(profileWeb, "demo")
	if err != nil || spec != "^1.2.3" {
		t.Fatalf("应读到依赖值，实际 %q (%v)", spec, err)
	}
	if _, err := profileDependencySpec(profileWeb, "absent"); err == nil {
		t.Fatal("缺失的依赖应报错")
	}
}

// --- 插件页其它插件的定时检查（后台）---

// pageUpdatePlugin 描述一个测试插件：已装版本、profile 里的依赖写法、镜像源上是否有更高版本。
type pageUpdatePlugin struct {
	name    string
	version string
	spec    string
	latest  string // 空 = 镜像源上没有更高版本
}

type pageUpdateFixture struct {
	um          *UpdateManager
	profileWeb  string
	versions    map[string]string
	delay       time.Duration
	mu          sync.Mutex
	requests    []string
	inFlight    int
	maxInFlight int
}

func newPageUpdateFixture(t *testing.T, plugins []pageUpdatePlugin, delay time.Duration) *pageUpdateFixture {
	t.Helper()
	home := t.TempDir()
	profileWeb := filepath.Join(home, ".dsh", "profiles", "web")
	deps := make(map[string]string, len(plugins))
	byName := make(map[string]pageUpdatePlugin, len(plugins))
	versions := make(map[string]string, len(plugins))
	for _, p := range plugins {
		dir := filepath.Join(profileWeb, "node_modules", p.name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		manifest := map[string]string{"name": p.name, "version": p.version}
		raw, _ := json.Marshal(manifest)
		if err := os.WriteFile(filepath.Join(dir, "package.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		spec := p.spec
		if spec == "" {
			spec = "^" + p.version
		}
		deps[p.name] = spec
		byName[p.name] = p
		versions[p.name] = p.version
	}
	profile, _ := json.Marshal(map[string]interface{}{
		"name":         "dsh-profile-web",
		"private":      true,
		"dependencies": deps,
		"dsh":          map[string]interface{}{"profile": map[string]interface{}{"bundles": []string{}}},
	})
	if err := os.WriteFile(filepath.Join(profileWeb, "package.json"), profile, 0o600); err != nil {
		t.Fatal(err)
	}

	f := &pageUpdateFixture{profileWeb: profileWeb, versions: versions, delay: delay}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		f.mu.Lock()
		f.requests = append(f.requests, name)
		f.inFlight++
		if f.inFlight > f.maxInFlight {
			f.maxInFlight = f.inFlight
		}
		f.mu.Unlock()
		defer func() {
			f.mu.Lock()
			f.inFlight--
			f.mu.Unlock()
		}()
		if f.delay > 0 {
			time.Sleep(f.delay)
		}
		p, ok := byName[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		vs := map[string]interface{}{
			p.version: map[string]string{"name": p.name, "version": p.version},
		}
		if p.latest != "" {
			vs[p.latest] = map[string]string{"name": p.name, "version": p.latest}
		}
		raw, _ := json.Marshal(map[string]interface{}{"name": p.name, "versions": vs})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
	t.Cleanup(srv.Close)

	prevMirrors := npmMirrors
	prevCfg := GetConfig()
	t.Cleanup(func() {
		npmMirrors = prevMirrors
		initConfig(&prevCfg)
	})
	npmMirrors = []npmMirror{{Name: "测试源", Slug: "test", URL: srv.URL}}
	cfg := defaultConfig()
	cfg.NpmRegistry = "test"
	initConfig(&cfg)

	dsh := newTestDshManager(home, "")
	dsh.renv = &RuntimeEnv{Home: home}
	f.um = &UpdateManager{dsh: dsh}
	return f
}

func (f *pageUpdateFixture) list() pluginList {
	return pluginList{versions: f.versions}
}

func (f *pageUpdateFixture) requestNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func TestCheckPagePluginsCachesOnlyUpdates(t *testing.T) {
	f := newPageUpdateFixture(t, []pageUpdatePlugin{
		{name: "demo-update", version: "1.0.0", latest: "1.1.0"},
		{name: "demo-current", version: "2.0.0"},
		{name: "demo-git", version: "0.1.0", spec: "github:owner/repo"},
		{name: "dshmarket", version: "1.0.0", latest: "2.0.0"}, // 受管插件：不归这一轮检查
	}, 0)

	f.um.checkPagePluginsWith(f.list())

	updates := f.um.pagePluginUpdates()
	if len(updates) != 1 {
		t.Fatalf("应只有 demo-update 进缓存，实际 %+v", updates)
	}
	if got := updates["demo-update"]; got.LatestVersion != "1.1.0" || !got.HasUpdate {
		t.Fatalf("demo-update 的结论不对: %+v", got)
	}
	for _, name := range []string{"demo-current", "demo-git", "dshmarket"} {
		if _, ok := updates[name]; ok {
			t.Fatalf("%s 不应进缓存", name)
		}
	}
	for _, name := range f.requestNames() {
		if name == "demo-git" || name == "dshmarket" {
			t.Fatalf("不应该请求 %s（非 registry 安装 / 受管插件）", name)
		}
	}
}

func TestCheckPagePluginsLogsOnlyUpdatesAndDedupes(t *testing.T) {
	f := newPageUpdateFixture(t, []pageUpdatePlugin{
		{name: "demo-update", version: "1.0.0", latest: "1.1.0"},
		{name: "demo-current", version: "2.0.0"},
	}, 0)
	out, _ := captureLogs(t)

	f.um.checkPagePluginsWith(f.list())
	logs := out.String()
	if !strings.Contains(logs, "update available: demo-update 1.0.0 -> 1.1.0") {
		t.Fatalf("有更新时应记一行，实际:\n%s", logs)
	}
	if strings.Contains(logs, "demo-current") {
		t.Fatalf("没更新不应记日志，实际:\n%s", logs)
	}

	// 同一结论的下一轮（每小时）不再重复打同一行
	out.Reset()
	f.um.checkPagePluginsWith(f.list())
	if strings.Contains(out.String(), "update available") {
		t.Fatalf("同一更新不应重复记日志，实际:\n%s", out.String())
	}

	// 更新消失（例如已升级）后清掉日志去重记录；再出现时重新记
	out.Reset()
	f.versions["demo-update"] = "1.1.0"
	if err := os.WriteFile(
		filepath.Join(f.profileWeb, "node_modules", "demo-update", "package.json"),
		[]byte(`{"name":"demo-update","version":"1.1.0"}`), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	f.um.checkPagePluginsWith(f.list())
	if strings.Contains(out.String(), "update available") {
		t.Fatalf("升级后不应再报更新，实际:\n%s", out.String())
	}
	if _, ok := f.um.pagePluginUpdates()["demo-update"]; ok {
		t.Fatal("升级后缓存里的更新结论应被清掉")
	}
}

func TestCheckPagePluginsBoundsConcurrency(t *testing.T) {
	var plugins []pageUpdatePlugin
	for i := 0; i < 6; i++ {
		plugins = append(plugins, pageUpdatePlugin{
			name:    fmt.Sprintf("demo-%d", i),
			version: "1.0.0",
			latest:  "1.1.0",
		})
	}
	f := newPageUpdateFixture(t, plugins, 40*time.Millisecond)
	f.um.checkPagePluginsWith(f.list())

	if got := len(f.um.pagePluginUpdates()); got != len(plugins) {
		t.Fatalf("6 个插件都应有更新，实际缓存 %d 个", got)
	}
	f.mu.Lock()
	max := f.maxInFlight
	f.mu.Unlock()
	if max > pagePluginCheckConcurrency {
		t.Fatalf("并发上限应为 %d，实际峰值 %d", pagePluginCheckConcurrency, max)
	}
}

func TestHandlePluginUpdatesEndpoint(t *testing.T) {
	f := newPageUpdateFixture(t, []pageUpdatePlugin{
		{name: "demo-update", version: "1.0.0", latest: "1.1.0"},
	}, 0)
	f.um.checkPagePluginsWith(f.list())

	mux := &AdminMux{update: f.um}
	rec := httptest.NewRecorder()
	mux.handlePluginUpdates(rec, httptest.NewRequest(http.MethodGet, "/api/plugins/updates", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200，实际 %d", rec.Code)
	}
	var body struct {
		OK      bool                        `json:"ok"`
		Updates map[string]PluginUpdateInfo `json:"updates"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.OK || body.Updates["demo-update"].LatestVersion != "1.1.0" {
		t.Fatalf("响应不对: %s", rec.Body.String())
	}
}

// 单卡检查（手动）同样只在有更新时记日志：没更新的点击不该刷日志。
func TestHandlePluginUpdateCheckLogsOnlyOnUpdate(t *testing.T) {
	t.Run("无更新不记日志", func(t *testing.T) {
		f := newPluginUpdateFixture(t, `{"name":"demo","versions":{"1.0.0":{"name":"demo","version":"1.0.0"}}}`)
		out, _ := captureLogs(t)
		if rec := getPluginUpdate(t, f.m, "name=demo"); rec.Code != http.StatusOK {
			t.Fatalf("应 200，实际 %d (%s)", rec.Code, rec.Body.String())
		}
		if strings.Contains(out.String(), "update available") {
			t.Fatalf("没更新不应记日志，实际:\n%s", out.String())
		}
	})

	t.Run("有更新记一行", func(t *testing.T) {
		f := newPluginUpdateFixture(t, `{"name":"demo","versions":{
			"1.0.0":{"name":"demo","version":"1.0.0"},
			"1.1.0":{"name":"demo","version":"1.1.0"}
		}}`)
		out, _ := captureLogs(t)
		if rec := getPluginUpdate(t, f.m, "name=demo"); rec.Code != http.StatusOK {
			t.Fatalf("应 200，实际 %d (%s)", rec.Code, rec.Body.String())
		}
		if !strings.Contains(out.String(), "update available: demo 1.0.0 -> 1.1.0") {
			t.Fatalf("有更新应记一行，实际:\n%s", out.String())
		}
	})
}
