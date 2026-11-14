package main

// plugin_install_test.go —— 插件页「安装插件」的两个关键面：
//  1. 规格归一化：包名 / 包名@版本 / 仓库地址 / 本地目录 / 完整 `dsh plugin add …`
//     命令都要能认出来；相对路径、选项注入（`-` 开头）、多目标、非白名单 URL 一律在
//     执行任何命令之前拒掉（并带可翻译的 code）；
//  2. 安装任务：命令参数正确（`--profile web add <规格> --registry=<选中源>`）、
//     成功后能从 profile 清单里认出包名/版本并给出「需重启」、失败时错误带 code，
//     且失败/被拒都不会把 applying 锁留在手里。
//
// 这些用例只碰临时目录与假 dsh 脚本，不联网、不动真实 ~/.dsh。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestNormalizePluginInstallInput 覆盖用户可能粘贴的全部写法与常见误输入。
func TestNormalizePluginInstallInput(t *testing.T) {
	dir := t.TempDir()
	tarball := filepath.Join(dir, "plugin.tgz")
	if err := os.WriteFile(tarball, []byte("tgz"), 0o644); err != nil {
		t.Fatal(err)
	}
	plainFile := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(plainFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		in   string
		spec string
		pkg  string
		code string // 期望的错误 code；空 = 成功
	}{
		{name: "包名", in: "dsh-better-sidebar", spec: "dsh-better-sidebar", pkg: "dsh-better-sidebar"},
		{name: "包名@版本", in: " dsh-better-sidebar@0.32.1 ", spec: "dsh-better-sidebar@0.32.1", pkg: "dsh-better-sidebar"},
		{name: "scope 包名", in: "@linxin666/dsh-client-ui-skill-explorer", spec: "@linxin666/dsh-client-ui-skill-explorer", pkg: "@linxin666/dsh-client-ui-skill-explorer"},
		{name: "scope 包名@版本", in: "@linxin666/dsh-client-ui-skill-explorer@0.4.18", spec: "@linxin666/dsh-client-ui-skill-explorer@0.4.18", pkg: "@linxin666/dsh-client-ui-skill-explorer"},
		{name: "git 简写", in: "github:owner/repo", spec: "github:owner/repo"},
		{name: "托管仓库", in: "https://github.com/owner/repo", spec: "https://github.com/owner/repo"},
		{name: "托管仓库 + .git + 片段", in: "https://github.com/owner/repo.git#main", spec: "https://github.com/owner/repo.git#main"},
		{name: "git+https", in: "git+https://github.com/owner/repo.git", spec: "git+https://github.com/owner/repo.git"},
		{name: "scp 形式", in: "git@github.com:owner/repo.git", spec: "git@github.com:owner/repo.git"},
		{name: "tarball URL", in: "https://example.com/foo/bar.tgz", spec: "https://example.com/foo/bar.tgz"},
		{name: "本地目录", in: dir, spec: dir},
		{name: "本地 tarball", in: tarball, spec: tarball},
		{name: "file: 本地目录", in: "file:" + dir, spec: "file:" + dir},
		{name: "整段命令", in: "dsh plugin add dsh-better-sidebar", spec: "dsh-better-sidebar", pkg: "dsh-better-sidebar"},
		{name: "整段命令带 profile", in: "dsh plugin --profile web add github:owner/repo", spec: "github:owner/repo"},
		{name: "整段命令 --profile=", in: "dsh plugin --profile=web install foo@1.2.3", spec: "foo@1.2.3", pkg: "foo"},
		{name: "绝对路径的 dsh", in: "/usr/local/bin/dsh plugin add foo", spec: "foo", pkg: "foo"},
		{name: "带引号的路径", in: `dsh plugin add "` + dir + `"`, spec: dir},

		{name: "空输入", in: "   ", code: "err_plugin_spec_empty"},
		{name: "选项注入", in: "--registry=http://evil", code: "err_plugin_spec_option"},
		{name: "相对路径", in: "./plugins/my-plugin", code: "err_plugin_spec_relative"},
		{name: "上级相对路径", in: "../my-plugin", code: "err_plugin_spec_relative"},
		{name: "波浪号路径", in: "~/plugins/my-plugin", code: "err_plugin_spec_relative"},
		{name: "含空格的未识别规格", in: "plugin dir/foo", code: "err_plugin_spec_name"},
		{name: "版本号为空", in: "foo@", code: "err_plugin_spec_name"},
		{name: "不是仓库也不是 tarball 的 URL", in: "https://example.com/foo", code: "err_plugin_spec_url"},
		{name: "路径不存在", in: filepath.Join(dir, "missing"), code: "err_plugin_path_missing"},
		{name: "普通文件不是安装包", in: plainFile, code: "err_plugin_path_not_dir"},
		{name: "多行输入", in: "foo\nbar", code: "err_plugin_spec_invalid"},
		{name: "过长", in: strings.Repeat("a", pluginInstallSpecMaxLen+1), code: "err_plugin_spec_too_long"},
		{name: "只写 dsh plugin", in: "dsh plugin", code: "err_plugin_cmd_only_add"},
		{name: "非 add 子命令", in: "dsh plugin remove foo", code: "err_plugin_cmd_only_add_got"},
		{name: "add 但没有规格", in: "dsh plugin add", code: "err_plugin_spec_empty"},
		{name: "一次两个目标", in: "dsh plugin add foo bar", code: "err_plugin_cmd_target"},
		{name: "命令里带选项", in: "dsh plugin add foo --registry=https://evil", code: "err_plugin_cmd_option"},
		{name: "别的 profile", in: "dsh plugin --profile desktop add foo", code: "err_plugin_cmd_profile"},
		{name: "--profile 缺值", in: "dsh plugin --profile", code: "err_plugin_cmd_profile_missing"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec, pkg, err := normalizePluginInstallInput(tc.in)
			if tc.code == "" {
				if err != nil {
					t.Fatalf("应接受 %q，却报错: %v", tc.in, err)
				}
				if spec != tc.spec || pkg != tc.pkg {
					t.Fatalf("归一化结果 = (%q, %q)，期望 (%q, %q)", spec, pkg, tc.spec, tc.pkg)
				}
				return
			}
			if err == nil {
				t.Fatalf("%q 应被拒绝，实际归一化成 %q", tc.in, spec)
			}
			if spec != "" || pkg != "" {
				t.Fatalf("被拒时不应返回规格: (%q, %q)", spec, pkg)
			}
			msg, ok := uiMsgOf(err)
			if !ok || msg.Code != tc.code {
				t.Fatalf("%q 的错误 code = %q（带 code=%v），期望 %q；错误: %v",
					tc.in, msg.Code, ok, tc.code, err)
			}
		})
	}
}

// --- 安装任务（异步）---

type pluginInstallFixture struct {
	m       *AdminMux
	marker  string // 假 dsh 记下的 argv
	profile string // profile 目录（假 dsh 往这里写清单模拟 pnpm 安装）
}

// newPluginInstallFixture 造一个只碰临时目录的 AdminMux：假 HOME + profile 清单 +
// 「已安装并选中」的假 dsh 版本（脚本由调用方给，必须把 argv 写进 $HARNESS_TEST_OUT）。
func newPluginInstallFixture(t *testing.T, script string) *pluginInstallFixture {
	t.Helper()
	pluginInstall.reset()
	t.Cleanup(pluginInstall.reset)

	home := t.TempDir()
	profile := filepath.Join(home, ".dsh", "profiles", "web")
	if err := os.MkdirAll(filepath.Join(profile, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 装前的清单：一个旧依赖，用来验证「装前快照」能读、且不会被当成新装的包。
	const manifest = `{
  "name": "dsh-profile-web",
  "private": true,
  "dependencies": {
    "old-dep": "^1.0.0"
  },
  "dsh": {
    "profile": {
      "bundles": []
    }
  }
}`
	if err := os.WriteFile(filepath.Join(profile, "package.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	dataDir := t.TempDir()
	const version = "1.2.3"
	bin := versionDshBinFor(&RuntimeEnv{DataDir: dataDir}, version)
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}

	prevCfg := GetConfig()
	t.Cleanup(func() { initConfig(&prevCfg) })
	cfg := defaultConfig()
	// 端口取一个不会命中本机真实 dsh 进程的值；市场忙碌探测在下面直接固定成「不忙」。
	cfg.DshPort = 65535
	cfg.DshVersion = version
	initConfig(&cfg)

	prevBusy := marketBusyFn
	marketBusyFn = func(*UpdateManager) (bool, string) { return false, "" }
	t.Cleanup(func() { marketBusyFn = prevBusy })

	renv := &RuntimeEnv{Home: home, DataDir: dataDir, ConfigFile: filepath.Join(t.TempDir(), "config.json")}
	dsh := newTestDshManager(home, "")
	dsh.renv = renv
	m := &AdminMux{
		renv:   renv,
		dsh:    dsh,
		update: &UpdateManager{renv: renv, dsh: dsh},
	}
	marker := filepath.Join(t.TempDir(), "calls.txt")
	t.Setenv("HARNESS_TEST_OUT", marker)
	t.Setenv("HARNESS_TEST_PROFILE", profile)
	return &pluginInstallFixture{m: m, marker: marker, profile: profile}
}

func (f *pluginInstallFixture) calls(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(f.marker)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(raw)
}

func postInstallPlugin(t *testing.T, m *AdminMux, spec string) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"spec": spec})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/plugins/install", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	m.handleInstallPlugin(rec, req)
	return rec
}

// waitPluginInstallIdle 等后台任务落定（测试里的假命令都是毫秒级）。
func waitPluginInstallIdle(t *testing.T) PluginInstallStatus {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st := pluginInstall.status(); !st.Running {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("安装任务没有在 10 秒内结束")
	return PluginInstallStatus{}
}

// 成功安装：命令参数正确、能从清单里认出包名/版本、bundle 判为「需重启」，并释放锁。
func TestInstallPluginRunsAddAndReportsResult(t *testing.T) {
	script := `
printf '%s\n' "$*" >> "$HARNESS_TEST_OUT"
cat > "$HARNESS_TEST_PROFILE/package.json" <<'JSON'
{
  "name": "dsh-profile-web",
  "private": true,
  "dependencies": {
    "old-dep": "^1.0.0",
    "fake-plugin": "9.9.9"
  },
  "dsh": {
    "profile": {
      "bundles": ["fake-plugin"]
    }
  }
}
JSON
mkdir -p "$HARNESS_TEST_PROFILE/node_modules/fake-plugin"
cat > "$HARNESS_TEST_PROFILE/node_modules/fake-plugin/package.json" <<'JSON'
{"name":"fake-plugin","version":"9.9.9","dsh":{"bundle":{"patch":"./cordis.patch.yml"},"client":true}}
JSON
`
	f := newPluginInstallFixture(t, script)

	rec := postInstallPlugin(t, f.m, "fake-plugin@9.9.9")
	if rec.Code != http.StatusOK {
		t.Fatalf("启动安装应 200，实际 %d (%s)", rec.Code, rec.Body.String())
	}

	st := waitPluginInstallIdle(t)
	if st.Phase != "done" || st.Error != "" {
		t.Fatalf("安装应成功，实际 phase=%q error=%q", st.Phase, st.Error)
	}
	if st.Name != "fake-plugin" || st.Version != "9.9.9" {
		t.Fatalf("应认出装进来的包名/版本，实际 name=%q version=%q", st.Name, st.Version)
	}
	if !st.Restart {
		t.Fatal("声明了 dsh.bundle 的新插件应提示「需重启 dsh」")
	}
	if st.MessageRef == nil || st.MessageRef.Code != "msg_plugin_install_done" {
		t.Fatalf("成功文案应带 code，实际 %+v", st.MessageRef)
	}

	// 命令：profile 固定 web，规格原样（精确版本），镜像源用设置页选中的那一个。
	calls := f.calls(t)
	want := "plugin --profile web add fake-plugin@9.9.9 --registry=https://registry.npmmirror.com"
	if !strings.Contains(calls, want) {
		t.Fatalf("安装命令应为 %q，实际记录:\n%s", want, calls)
	}

	// 状态接口要能把同一份结果回给（刷新过页面的）前端。
	statusRec := httptest.NewRecorder()
	f.m.handlePluginInstallStatus(statusRec, httptest.NewRequest(http.MethodGet, "/api/plugins/install/status", nil))
	if statusRec.Code != http.StatusOK {
		t.Fatalf("状态接口应 200，实际 %d (%s)", statusRec.Code, statusRec.Body.String())
	}
	var payload struct {
		OK bool `json:"ok"`
		PluginInstallStatus
	}
	if err := json.Unmarshal(statusRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析状态响应失败: %v", err)
	}
	if !payload.OK || payload.Running || payload.Phase != "done" || payload.Name != "fake-plugin" {
		t.Fatalf("状态响应不对: %+v (raw %s)", payload, statusRec.Body.String())
	}

	// applying 锁必须已经释放，否则后续的更新/备份全会被挡。
	if !f.m.update.applying.TryLock() {
		t.Fatal("安装结束后 applying 锁没有释放")
	}
	f.m.update.applying.Unlock()
}

// 没选中 dsh 版本：给出「去概览页下载并切换」的可操作提示，而不是 pnpm 的 "(no output)"。
func TestInstallPluginReportsMissingDshVersion(t *testing.T) {
	f := newPluginInstallFixture(t, `printf '%s\n' "$*" >> "$HARNESS_TEST_OUT"`)
	cfg := GetConfig()
	cfg.DshVersion = ""
	initConfig(&cfg)

	rec := postInstallPlugin(t, f.m, "fake-plugin")
	if rec.Code != http.StatusOK {
		t.Fatalf("启动安装应 200，实际 %d (%s)", rec.Code, rec.Body.String())
	}
	st := waitPluginInstallIdle(t)
	if st.Phase != "failed" || st.ErrorRef == nil || st.ErrorRef.Code != "err_no_dsh_version" {
		t.Fatalf("应报 err_no_dsh_version，实际 phase=%q error=%q ref=%+v", st.Phase, st.Error, st.ErrorRef)
	}
	if calls := f.calls(t); calls != "" {
		t.Fatalf("没有 dsh 版本时不应执行任何命令，实际记录:\n%s", calls)
	}
}

// 失败安装：错误带 code（镜像源 + 诊断尾行），锁同样要释放。
func TestInstallPluginReportsFailure(t *testing.T) {
	script := `
printf '%s\n' "$*" >> "$HARNESS_TEST_OUT"
echo "ERR_PNPM_FETCH_404  fake-plugin is not in the registry" >&2
exit 1
`
	f := newPluginInstallFixture(t, script)

	rec := postInstallPlugin(t, f.m, "fake-plugin")
	if rec.Code != http.StatusOK {
		t.Fatalf("启动安装应 200，实际 %d (%s)", rec.Code, rec.Body.String())
	}
	st := waitPluginInstallIdle(t)
	if st.Phase != "failed" || st.Error == "" {
		t.Fatalf("安装应失败，实际 phase=%q error=%q", st.Phase, st.Error)
	}
	if st.ErrorRef == nil || st.ErrorRef.Code != "err_plugin_install_failed" {
		t.Fatalf("失败文案应带 err_plugin_install_failed，实际 %+v", st.ErrorRef)
	}
	if st.ErrorRef.Params["mirror"] != "aliyun" {
		t.Fatalf("失败文案应带镜像源 slug，实际 %+v", st.ErrorRef.Params)
	}
	if !strings.Contains(st.ErrorRef.Params["detail"], "ERR_PNPM_FETCH_404") {
		t.Fatalf("失败文案应带 pnpm 诊断尾行，实际 %q", st.ErrorRef.Params["detail"])
	}

	if !f.m.update.applying.TryLock() {
		t.Fatal("安装失败后 applying 锁没有释放")
	}
	f.m.update.applying.Unlock()
}

// 非法规格：400 + code，且绝不执行 dsh 命令。
func TestInstallPluginRejectsInvalidSpecBeforeRunning(t *testing.T) {
	f := newPluginInstallFixture(t, `printf '%s\n' "$*" >> "$HARNESS_TEST_OUT"`)

	for _, in := range []string{"--registry=http://evil", "./relative-plugin", "dsh plugin remove foo"} {
		rec := postInstallPlugin(t, f.m, in)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("spec=%q 应 400，实际 %d (%s)", in, rec.Code, rec.Body.String())
		}
		var body struct {
			OK    bool   `json:"ok"`
			Code  string `json:"code"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("解析响应失败: %v", err)
		}
		if body.OK || body.Code == "" || body.Error == "" {
			t.Fatalf("被拒响应应带 ok=false、code 与原始文案，实际 %+v", body)
		}
	}
	if calls := f.calls(t); calls != "" {
		t.Fatalf("被拒时不得执行 dsh 命令，实际记录:\n%s", calls)
	}
}

// 已有安装任务在跑：409 忙拒绝，且不会把 applying 锁漏在手里。
func TestInstallPluginRefusesWhenAlreadyRunning(t *testing.T) {
	f := newPluginInstallFixture(t, `printf '%s\n' "$*" >> "$HARNESS_TEST_OUT"`)
	if err := pluginInstall.begin("dsh-x", "dsh-x"); err != nil {
		t.Fatal(err)
	}

	rec := postInstallPlugin(t, f.m, "dsh-x")
	if rec.Code != http.StatusConflict {
		t.Fatalf("已有安装任务时应 409，实际 %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "err_plugin_install_running") {
		t.Fatalf("忙拒绝应带 err_plugin_install_running，实际 %s", rec.Body.String())
	}
	if !f.m.update.applying.TryLock() {
		t.Fatal("被拒时不应留下 applying 锁")
	}
	f.m.update.applying.Unlock()
	if calls := f.calls(t); calls != "" {
		t.Fatalf("被拒时不得执行 dsh 命令，实际记录:\n%s", calls)
	}
}
