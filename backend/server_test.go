package main

// server_test.go —— dsh 多版本管理（列表 / 安装 / 取消 / 删除 / 切换）的回归测试。
//
// 这里钉住的都是「改一处就会静默出错」的契约：
//   - 版本列表必须过滤掉 0.1.7-alpha.1 之前的版本（更早的 dsh 在子路径挂载下必然 404）；
//   - 版本号会变成目录名、也会拼进 npm 参数，必须拒绝路径穿越字符；
//   - 「有更新」的红点基准是镜像源的 dist-tags.latest，不是列表里的最高版本；
//   - 安装失败/取消都必须把半成品目录与 npm 缓存清干净（否则下次「重新下载」会命中坏树）；
//   - 当前正在使用的版本不允许删除（否则控制台会显示「未安装」而 dsh 还在跑）。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- 测试脚手架 ---

// newServerTestManager 造一个只碰临时目录的 UpdateManager + ServerManager。
func newServerTestManager(t *testing.T, dataDir string) (*UpdateManager, *ServerManager) {
	t.Helper()
	prevCfg := GetConfig()
	initConfig(&AppConfig{})
	t.Cleanup(func() { initConfig(&prevCfg) })

	renv := &RuntimeEnv{DataDir: dataDir, ConfigFile: filepath.Join(dataDir, "config.json")}
	dsh := &DshManager{renv: renv, logf: func(logLevel, string, ...interface{}) {}}
	upd := &UpdateManager{
		renv:     renv,
		dsh:      dsh,
		statuses: map[updateKind]*UpdateStatus{updateKindDsh: {Kind: updateKindDsh}},
	}
	srv := newServerManager(renv, dsh, upd)
	upd.server = srv
	return upd, srv
}

// fakeInstalledVersion 在版本根目录下造一份「看起来装好了」的版本：一个可执行的
// `node_modules/.bin/dsh`（shell 脚本，回显版本号）。
func fakeInstalledVersion(t *testing.T, dataDir, version string) string {
	t.Helper()
	bin := filepath.Join(serverRootFor(&RuntimeEnv{DataDir: dataDir}), version, "node_modules", ".bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho " + version + "\n"
	if err := os.WriteFile(filepath.Join(bin, "dsh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(bin, "dsh")
}

// fakeNpmInstall 造一个「安装成功」的假 npm 钩子：写出 dsh 可执行文件与
// attachment-local 的编译产物（供 fsync 补丁目标存在）。
func fakeNpmInstall(t *testing.T, attempts *[]string, failMirrors map[string]bool) func(*ServerManager, context.Context, string, string, npmMirror) error {
	return func(m *ServerManager, ctx context.Context, dir, version string, mirror npmMirror) error {
		if attempts != nil {
			*attempts = append(*attempts, mirror.Name)
		}
		if failMirrors != nil && failMirrors[mirror.Name] {
			return &os.PathError{Op: "npm", Path: dir, Err: os.ErrInvalid}
		}
		bin := filepath.Join(dir, "node_modules", ".bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			return err
		}
		script := "#!/bin/sh\necho " + version + "\n"
		if err := os.WriteFile(filepath.Join(bin, "dsh"), []byte(script), 0o755); err != nil {
			return err
		}
		att := filepath.Join(dir, "node_modules", "@deepseek-ai", "dsh-attachment-local", "lib")
		if err := os.MkdirAll(att, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(att, "index.js"), []byte(sampleAttachmentStoreJS), 0o644)
	}
}

// waitInstallPhase 等安装状态进入终态（done / error / 被取消）。
func waitInstallPhase(t *testing.T, srv *ServerManager, want ...string) *DshInstallState {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		srv.mu.Lock()
		st := srv.install
		srv.mu.Unlock()
		if st != nil {
			for _, w := range want {
				if st.Phase == w {
					return st
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	t.Fatalf("安装状态未进入 %v，当前: %+v", want, srv.install)
	return nil
}

// --- 版本列表 ---

func TestFilterDshVersions(t *testing.T) {
	raw := map[string]json.RawMessage{}
	for _, v := range []string{
		"0.1.5-rc.3", "0.1.6-alpha.2", "0.1.7-alpha.1", "0.1.7-rc.1",
		"0.2.0-rc.2", "0.2.1-alpha.1", "garbage!!",
	} {
		raw[v] = json.RawMessage(`{}`)
	}
	tags := map[string]string{"latest": "0.2.0-rc.2", "alpha": "0.2.1-alpha.1"}

	got := filterDshVersions(raw, tags)
	var versions []string
	for _, e := range got {
		versions = append(versions, e.Version)
	}
	want := []string{"0.2.1-alpha.1", "0.2.0-rc.2", "0.1.7-rc.1", "0.1.7-alpha.1"}
	if strings.Join(versions, ",") != strings.Join(want, ",") {
		t.Fatalf("版本列表 = %v, want %v（必须降序且隐藏 0.1.7-alpha.1 之前）", versions, want)
	}
	// dist-tag 要挂在对应版本上（前端据此显示 latest/alpha/next 标注）。
	tagOf := map[string][]string{}
	for _, e := range got {
		tagOf[e.Version] = e.Tags
	}
	if strings.Join(tagOf["0.2.0-rc.2"], ",") != "latest" {
		t.Errorf("0.2.0-rc.2 的标签 = %v, want [latest]", tagOf["0.2.0-rc.2"])
	}
	if strings.Join(tagOf["0.2.1-alpha.1"], ",") != "alpha" {
		t.Errorf("0.2.1-alpha.1 的标签 = %v, want [alpha]", tagOf["0.2.1-alpha.1"])
	}
	if len(tagOf["0.1.7-alpha.1"]) != 0 {
		t.Errorf("无 dist-tag 的版本不应带标签，实得 %v", tagOf["0.1.7-alpha.1"])
	}
}

func TestValidVersionArgRejectsPathTraversal(t *testing.T) {
	for _, ok := range []string{"0.2.0-rc.2", "0.1.7-alpha.1", "1.0.0+build.1"} {
		if !validVersionArg(ok) {
			t.Errorf("%q 应当被接受", ok)
		}
	}
	for _, bad := range []string{"", "../etc", "a/b", "..", "-flag", "0.1.0 beta", strings.Repeat("1", 65)} {
		if validVersionArg(bad) {
			t.Errorf("%q 必须被拒绝（会成为目录名与 npm 参数）", bad)
		}
	}
}

// 版本列表缓存：非强制调用吃缓存（不联网），只有「刷新列表 / 自动检测」才重拉；
// 缓存为空时（控制台刚启动）必须联网，否则弹窗会是空的。
func TestVersionListCacheServedUntilForced(t *testing.T) {
	var reqs int32
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&reqs, 1)
		_, _ = w.Write([]byte(`{"name":"@deepseek-ai/dsh","dist-tags":{"latest":"0.2.0-rc.2"},` +
			`"versions":{"0.2.0-rc.2":{},"0.2.1-alpha.1":{}}}`))
	}))
	defer mirror.Close()
	withMirrors(t, []npmMirror{{Name: "镜像一", URL: mirror.URL}})

	_, srv := newServerTestManager(t, t.TempDir())

	// 1) 空缓存 + 非强制：必须联网（第一次打开弹窗的场景）。
	if err := srv.refreshVersions(false); err != nil {
		t.Fatalf("首次拉取失败: %v", err)
	}
	if got := atomic.LoadInt32(&reqs); got != 1 {
		t.Fatalf("首次应联网 1 次，实际 %d", got)
	}
	if len(srv.snapshot().Versions) != 2 {
		t.Fatalf("列表应有两个可用版本，实际 %+v", srv.snapshot().Versions)
	}

	// 2) 有缓存后：非强制调用一律吃缓存（打开弹窗不应打镜像源）。
	for i := 0; i < 3; i++ {
		if err := srv.refreshVersions(false); err != nil {
			t.Fatalf("吃缓存不应报错: %v", err)
		}
	}
	if got := atomic.LoadInt32(&reqs); got != 1 {
		t.Fatalf("有缓存时非强制调用不应联网，实际请求 %d 次", got)
	}

	// 3) 强制（弹窗「刷新」/ 自动检测）才重拉。
	if err := srv.refreshVersions(true); err != nil {
		t.Fatalf("强制刷新失败: %v", err)
	}
	if got := atomic.LoadInt32(&reqs); got != 2 {
		t.Fatalf("强制刷新应再联网 1 次，实际请求 %d 次", got)
	}
}

// --- 本地已装版本与选中版本 ---

func TestSelectedVersionRequiresInstalledDir(t *testing.T) {
	dataDir := t.TempDir()
	upd, srv := newServerTestManager(t, dataDir)
	_ = upd

	fakeInstalledVersion(t, dataDir, "0.2.0-rc.2")
	fakeInstalledVersion(t, dataDir, "0.1.7-alpha.1")
	// 空目录（下载到一半被杀）不算已安装。
	if err := os.MkdirAll(filepath.Join(serverRootFor(srv.renv), "9.9.9", "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got := srv.installedVersions(); strings.Join(got, ",") != "0.2.0-rc.2,0.1.7-alpha.1" {
		t.Fatalf("已安装版本 = %v, want 降序的两份", got)
	}

	// 配置里写着某个版本，但目录不存在 → 视为未安装（控制台显示「未安装」）。
	cfg := GetConfig()
	cfg.DshVersion = "9.9.9"
	if err := SaveConfig(srv.renv, &cfg, false); err != nil {
		t.Fatal(err)
	}
	if got := srv.selectedVersion(); got != "" {
		t.Fatalf("未真正安装的版本不应被选中，实得 %q", got)
	}

	cfg.DshVersion = "0.1.7-alpha.1"
	if err := SaveConfig(srv.renv, &cfg, false); err != nil {
		t.Fatal(err)
	}
	if got := srv.selectedVersion(); got != "0.1.7-alpha.1" {
		t.Fatalf("选中版本 = %q, want 0.1.7-alpha.1", got)
	}
}

// 快照要把「镜像上已下架、本地还装着」的版本也列出来，否则用户既看不到也删不掉它。
func TestSnapshotIncludesUnlistedInstalledVersion(t *testing.T) {
	dataDir := t.TempDir()
	_, srv := newServerTestManager(t, dataDir)
	fakeInstalledVersion(t, dataDir, "0.1.9-rc.1")

	srv.mu.Lock()
	srv.versions = []DshVersionEntry{{Version: "0.2.0-rc.2", Tags: []string{"latest"}}}
	srv.tags = map[string]string{"latest": "0.2.0-rc.2"}
	srv.latest = "0.2.0-rc.2"
	srv.mu.Unlock()

	snap := srv.snapshot()
	if len(snap.Versions) != 2 {
		t.Fatalf("版本列表应含本地独有版本，实得 %+v", snap.Versions)
	}
	if snap.Versions[0].Version != "0.2.0-rc.2" || snap.Versions[1].Version != "0.1.9-rc.1" {
		t.Fatalf("顺序应为版本降序，实得 %+v", snap.Versions)
	}
	if !snap.Versions[1].Installed || snap.Versions[1].Active {
		t.Fatalf("本地独有版本应标记为已安装且未选中，实得 %+v", snap.Versions[1])
	}
	if snap.Versions[0].Installed {
		t.Fatalf("镜像上的版本不该被标成已安装: %+v", snap.Versions[0])
	}
}

// --- 「有更新」的红点：基准是 dist-tags.latest，而不是列表最高版本 ---

func TestRefreshDshStatusUsesLatestTag(t *testing.T) {
	dataDir := t.TempDir()
	upd, srv := newServerTestManager(t, dataDir)
	fakeInstalledVersion(t, dataDir, "0.2.0-rc.2")

	cfg := GetConfig()
	cfg.DshVersion = "0.2.0-rc.2"
	if err := SaveConfig(srv.renv, &cfg, false); err != nil {
		t.Fatal(err)
	}

	// 1) 选中的就是 latest：即使列表里存在更新的 alpha，也不该亮红点。
	srv.mu.Lock()
	srv.versions = []DshVersionEntry{{Version: "0.2.1-alpha.1"}, {Version: "0.2.0-rc.2"}}
	srv.latest = "0.2.0-rc.2"
	srv.mu.Unlock()
	srv.refreshDshStatus()
	if st := upd.getStatus(updateKindDsh); st.HasUpdate || st.LocalVersion != "0.2.0-rc.2" {
		t.Fatalf("选中 latest 时不应报有更新: %+v", st)
	}

	// 2) latest 走高了 → 亮红点。
	srv.mu.Lock()
	srv.latest = "0.3.0"
	srv.mu.Unlock()
	srv.refreshDshStatus()
	if st := upd.getStatus(updateKindDsh); !st.HasUpdate || st.LatestVersion != "0.3.0" {
		t.Fatalf("latest 升高后应报有更新: %+v", st)
	}

	// 3) 未安装任何版本（选中为空）时不臆造「有更新」结论。
	cfg = GetConfig()
	cfg.DshVersion = ""
	if err := SaveConfig(srv.renv, &cfg, false); err != nil {
		t.Fatal(err)
	}
	srv.refreshDshStatus()
	if st := upd.getStatus(updateKindDsh); st.HasUpdate || st.LocalVersion != "" {
		t.Fatalf("未安装时不应报有更新: %+v", st)
	}

	// 4) 预发布按 compareVersion 语义比较：alpha.2 落后于 alpha.5。
	fakeInstalledVersion(t, dataDir, "0.1.7-alpha.2")
	cfg = GetConfig()
	cfg.DshVersion = "0.1.7-alpha.2"
	if err := SaveConfig(srv.renv, &cfg, false); err != nil {
		t.Fatal(err)
	}
	srv.mu.Lock()
	srv.latest = "0.1.7-alpha.5"
	srv.mu.Unlock()
	srv.refreshDshStatus()
	if st := upd.getStatus(updateKindDsh); !st.HasUpdate {
		t.Fatalf("0.1.7-alpha.2 相对 latest 0.1.7-alpha.5 应判为有更新: %+v", st)
	}
}

// --- 安装：镜像源逐个尝试 + 成功后打补丁 + 失败清理 ---

func TestInstallFallsBackToNextMirror(t *testing.T) {
	dataDir := t.TempDir()
	_, srv := newServerTestManager(t, dataDir)

	var attempts []string
	var mu sync.Mutex
	prev := npmInstallFn
	npmInstallFn = func(m *ServerManager, ctx context.Context, dir, version string, mirror npmMirror) error {
		mu.Lock()
		attempts = append(attempts, mirror.Name)
		mu.Unlock()
		if mirror.Name == npmMirrors[0].Name {
			return &os.PathError{Op: "npm", Path: dir, Err: os.ErrInvalid}
		}
		return fakeNpmInstall(t, nil, nil)(m, ctx, dir, version, mirror)
	}
	t.Cleanup(func() { npmInstallFn = prev })

	if err := srv.Install("0.3.0"); err != nil {
		t.Fatalf("启动安装失败: %v", err)
	}
	st := waitInstallPhase(t, srv, "done")
	if st.Version != "0.3.0" || st.Error != "" {
		t.Fatalf("安装状态 = %+v, want done/0.3.0", st)
	}
	mu.Lock()
	got := strings.Join(attempts, ",")
	mu.Unlock()
	want := npmMirrors[0].Name + "," + npmMirrors[1].Name
	if got != want {
		t.Fatalf("镜像源尝试顺序 = %q, want %q（不重试、按阿里→腾讯→华为顺序）", got, want)
	}
	if !dshVersionInstalled(srv.versionDir("0.3.0")) {
		t.Fatal("安装完成后版本目录里应有可执行的 dsh")
	}
	// 安装成功的版本可以被选中。
	cfg := GetConfig()
	cfg.DshVersion = "0.3.0"
	if err := SaveConfig(srv.renv, &cfg, false); err != nil {
		t.Fatal(err)
	}
	if got := srv.selectedVersion(); got != "0.3.0" {
		t.Fatalf("选中版本 = %q, want 0.3.0", got)
	}
}

func TestInstallAllMirrorsFailedCleansUp(t *testing.T) {
	dataDir := t.TempDir()
	_, srv := newServerTestManager(t, dataDir)

	prev := npmInstallFn
	npmInstallFn = func(m *ServerManager, ctx context.Context, dir, version string, mirror npmMirror) error {
		return &os.PathError{Op: "npm", Path: dir, Err: os.ErrInvalid}
	}
	t.Cleanup(func() { npmInstallFn = prev })

	if err := srv.Install("0.4.0"); err != nil {
		t.Fatalf("启动安装失败: %v", err)
	}
	st := waitInstallPhase(t, srv, "error")
	if !strings.Contains(st.Error, mirrorNames()) {
		t.Fatalf("错误信息应说明三个镜像源都失败、不试官方源，实际: %v", st.Error)
	}
	if _, err := os.Stat(srv.versionDir("0.4.0")); !os.IsNotExist(err) {
		t.Fatal("全部失败后不应留下半成品目录")
	}
}

// 回归：装完一个版本之后必须还能装别的版本。
//
// 早期实现用「安装状态不为空」当「正在安装」，而安装成功后的终态（done）会一直留在
// 状态里给前端显示结果，于是**装好第一个版本后再也装不了第二个**，一直提示
// 「已有 dsh 版本正在安装（X），请等它结束或先取消」。正确判据只有三个「进行中」阶段
// （installInProgress）。
func TestInstallAfterTerminalStatesIsAllowed(t *testing.T) {
	dataDir := t.TempDir()
	_, srv := newServerTestManager(t, dataDir)
	prev := npmInstallFn
	npmInstallFn = fakeNpmInstall(t, nil, nil)
	t.Cleanup(func() { npmInstallFn = prev })

	// 1) 第一个版本装成功（终态 done 留在状态里）。
	if err := srv.Install("0.3.0"); err != nil {
		t.Fatalf("启动安装失败: %v", err)
	}
	if st := waitInstallPhase(t, srv, "done"); st.Version != "0.3.0" {
		t.Fatalf("安装状态版本 = %q, want 0.3.0", st.Version)
	}

	// 2) 关键回归点：还要能装下一个版本。
	if err := srv.Install("0.4.0"); err != nil {
		t.Fatalf("上一个版本已装完，不应再被当成「正在安装」: %v", err)
	}
	if st := waitInstallPhase(t, srv, "done"); st.Version != "0.4.0" {
		t.Fatalf("第二次安装状态版本 = %q, want 0.4.0", st.Version)
	}
	if !dshVersionInstalled(srv.versionDir("0.4.0")) {
		t.Fatal("第二个版本应已装好")
	}

	// 3) 「已安装」这条规则不能因为本次修复被放开。
	if err := srv.Install("0.4.0"); err == nil || !strings.Contains(err.Error(), "已安装") {
		t.Fatalf("重复下载已安装版本应被拒绝，实际: %v", err)
	}

	// 4) 真在进行中时仍要拒绝 —— 以及失败终态之后同样要能重新装。
	//    注意：一次安装会按镜像源逐个尝试，所以这个假 npm 会被调用多次，
	//    只关闭 started 一次（sync.Once），每次都等 release。
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	npmInstallFn = func(m *ServerManager, ctx context.Context, dir, version string, mirror npmMirror) error {
		once.Do(func() { close(started) })
		<-release
		return errors.New("boom")
	}
	if err := srv.Install("0.5.0"); err != nil {
		t.Fatalf("启动安装失败: %v", err)
	}
	<-started
	if err := srv.Install("0.6.0"); err == nil || !strings.Contains(err.Error(), "正在安装") {
		t.Fatalf("安装进行中时应拒绝，实际: %v", err)
	}
	close(release)
	if st := waitInstallPhase(t, srv, "error"); !strings.Contains(st.Error, "boom") {
		t.Fatalf("失败终态应带错误信息，实际: %+v", st)
	}

	npmInstallFn = fakeNpmInstall(t, nil, nil)
	if err := srv.Install("0.6.0"); err != nil {
		t.Fatalf("失败终态之后应能重新安装: %v", err)
	}
	if st := waitInstallPhase(t, srv, "done"); st.Version != "0.6.0" {
		t.Fatalf("重新安装状态版本 = %q, want 0.6.0", st.Version)
	}
}

// 取消：终止安装、删除版本目录与 npm 下载缓存，状态复位为「已取消」。
func TestCancelInstallClearsDirAndNpmCache(t *testing.T) {
	dataDir := t.TempDir()
	_, srv := newServerTestManager(t, dataDir)

	started := make(chan struct{})
	prev := npmInstallFn
	npmInstallFn = func(m *ServerManager, ctx context.Context, dir, version string, mirror npmMirror) error {
		// 造出「npm 缓存 + 半成品」现场后挂住，等取消信号。
		if err := os.MkdirAll(m.npmCacheDir(), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(m.npmCacheDir(), "blob"), []byte("cached"), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "partial"), []byte("half"), 0o644); err != nil {
			return err
		}
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	t.Cleanup(func() { npmInstallFn = prev })

	if err := srv.Install("0.5.0"); err != nil {
		t.Fatalf("启动安装失败: %v", err)
	}
	<-started
	if !srv.CancelInstall() {
		t.Fatal("CancelInstall 应报告取消成功")
	}
	waitInstallPhase(t, srv, "")

	if _, err := os.Stat(srv.versionDir("0.5.0")); !os.IsNotExist(err) {
		t.Fatal("取消后应删除该版本的安装目录")
	}
	if _, err := os.Stat(srv.npmCacheDir()); !os.IsNotExist(err) {
		t.Fatal("取消后应清除 npm 下载缓存")
	}
	srv.mu.Lock()
	cancelled := srv.install != nil && srv.install.Cancelled
	srv.mu.Unlock()
	if !cancelled {
		t.Fatal("状态应标记为「已取消」（前端显示中性提示而非错误）")
	}
}

// --- 删除与切换 ---

func TestDeleteVersionRules(t *testing.T) {
	dataDir := t.TempDir()
	_, srv := newServerTestManager(t, dataDir)
	fakeInstalledVersion(t, dataDir, "0.2.0-rc.2")
	fakeInstalledVersion(t, dataDir, "0.1.7-alpha.1")

	cfg := GetConfig()
	cfg.DshVersion = "0.2.0-rc.2"
	if err := SaveConfig(srv.renv, &cfg, false); err != nil {
		t.Fatal(err)
	}

	// 正在使用的版本不允许删除（否则控制台会显示「未安装」而 dsh 还在跑）。
	if err := srv.Delete("0.2.0-rc.2"); err == nil || !strings.Contains(err.Error(), "当前正在使用") {
		t.Fatalf("删除当前版本应被拒绝，实际: %v", err)
	}
	if _, err := os.Stat(srv.versionDir("0.2.0-rc.2")); err != nil {
		t.Fatalf("被拒绝时不应改动目录: %v", err)
	}

	// 非法版本号一律拒绝（会成为相对路径）。
	if err := srv.Delete("../0.2.0-rc.2"); err == nil {
		t.Fatal("含路径分隔符的版本号必须被拒绝")
	}

	// 其它版本可以删除；未安装的版本报「未安装」。
	if err := srv.Delete("0.1.7-alpha.1"); err != nil {
		t.Fatalf("删除非当前版本应成功: %v", err)
	}
	if _, err := os.Stat(srv.versionDir("0.1.7-alpha.1")); !os.IsNotExist(err) {
		t.Fatal("删除后目录应消失")
	}
	if err := srv.Delete("9.9.9"); err == nil || !strings.Contains(err.Error(), "未安装") {
		t.Fatalf("删除未安装版本应报错，实际: %v", err)
	}
}

func TestSwitchVersion(t *testing.T) {
	dataDir := t.TempDir()
	_, srv := newServerTestManager(t, dataDir)
	fakeInstalledVersion(t, dataDir, "0.2.0-rc.2")

	// 调用序列要跨 goroutine 记录（启动 dsh 是异步的），故加锁。
	var mu sync.Mutex
	var calls []string
	record := func(s string) { mu.Lock(); calls = append(calls, s); mu.Unlock() }
	snapshot := func() string { mu.Lock(); defer mu.Unlock(); return strings.Join(calls, ",") }

	prevStop, prevFree, prevStart, prevBusy := dshStopFn, dshPortFreeFn, startDshCapturedFn, marketBusyFn
	t.Cleanup(func() {
		dshStopFn, dshPortFreeFn, startDshCapturedFn, marketBusyFn = prevStop, prevFree, prevStart, prevBusy
	})
	dshStopFn = func(*UpdateManager) error { record("stop"); return nil }
	dshPortFreeFn = func(*UpdateManager, time.Duration) { record("portfree") }
	startDone := make(chan struct{})
	startDshCapturedFn = func(*UpdateManager) error { record("capture"); close(startDone); return nil }
	marketBusyFn = func(*UpdateManager) (bool, string) { return false, "" }

	// 未安装的版本不能切换。
	if err := srv.Switch("9.9.9"); err == nil || !strings.Contains(err.Error(), "尚未安装") {
		t.Fatalf("切换到未安装版本应被拒绝，实际: %v", err)
	}
	if got := snapshot(); got != "" {
		t.Fatalf("被拒绝时不应动 dsh: %q", got)
	}

	if err := srv.Switch("0.2.0-rc.2"); err != nil {
		t.Fatalf("切换失败: %v", err)
	}
	if got := GetConfig().DshVersion; got != "0.2.0-rc.2" {
		t.Fatalf("配置里的选中版本 = %q, want 0.2.0-rc.2", got)
	}
	select {
	case <-startDone:
	case <-time.After(3 * time.Second):
		t.Fatal("切换后应异步启动新版本的 dsh")
	}
	if got := snapshot(); got != "stop,portfree,capture" {
		t.Fatalf("切换序列 = %q, want stop,portfree,capture（先停 dsh 再起新版本）", got)
	}

	// 已经是当前版本时不再重复停机。
	mu.Lock()
	calls = nil
	mu.Unlock()
	if err := srv.Switch("0.2.0-rc.2"); err == nil || !strings.Contains(err.Error(), "已经是当前版本") {
		t.Fatalf("重复切换应被拒绝，实际: %v", err)
	}
	if got := snapshot(); got != "" {
		t.Fatalf("重复切换不应动 dsh: %q", got)
	}
}

// --- 附件 fsync 补丁 ---

// sampleAttachmentStoreJS 是 dsh-attachment-local 编译产物里那段「逐级上溯目录 fsync」
// 的最小复刻（含 CI 补丁依赖的锚点），用于验证补丁能被正确注入。
const sampleAttachmentStoreJS = `"use strict";
async function ensureDurableDirectory(path, boundary) {
	const target = resolve(path);
	const stop = resolve(boundary);
	await mkdir(target, { recursive: true, mode: 0o700 });
	let level = target;
	while (level !== stop) {
		const parent = dirname(level);
		await syncDirectory(parent);
		if (parent === level) return;
		level = parent;
	}
}
async function ensureDurableHome(path) {
	await ensureDurableDirectory(path, parse(home).root);
}
`

func TestPatchAttachmentFsync(t *testing.T) {
	dir := t.TempDir()
	lib := filepath.Join(dir, "node_modules", "@deepseek-ai", "dsh-attachment-local", "lib")
	if err := os.MkdirAll(lib, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(lib, "index.js")
	if err := os.WriteFile(path, []byte(sampleAttachmentStoreJS), 0o644); err != nil {
		t.Fatal(err)
	}
	_, srv := newServerTestManager(t, t.TempDir())

	if err := srv.patchAttachmentFsync(dir); err != nil {
		t.Fatalf("打补丁失败: %v", err)
	}
	patched, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(patched), fsyncPatchMarker) {
		t.Fatal("补丁标记未写入")
	}
	if !strings.Contains(string(patched), `error.code === "EACCES"`) {
		t.Fatalf("补丁体不完整:\n%s", patched)
	}
	// 幂等：再打一次不出错、也不重复注入。
	if err := srv.patchAttachmentFsync(dir); err != nil {
		t.Fatalf("重复打补丁应幂等: %v", err)
	}
	again, _ := os.ReadFile(path)
	if strings.Count(string(again), fsyncPatchMarker) != 1 {
		t.Fatal("补丁被重复注入")
	}

	// 锚点缺失 + 仍存在「上溯到根」的逻辑 → 必须报错（提示人工核对上游改动）。
	broken := filepath.Join(t.TempDir(), "node_modules", "@deepseek-ai", "dsh-attachment-local", "lib")
	if err := os.MkdirAll(broken, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "async function x(home){ await ensureDurableDirectory(home, parse(home).root); }\n"
	if err := os.WriteFile(filepath.Join(broken, "index.js"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := srv.patchAttachmentFsync(filepath.Dir(filepath.Dir(broken))); err == nil {
		t.Fatal("锚点缺失时应报错")
	}

	// 目录里没有 attachment-local（布局变化）→ 也要报错，而不是静默通过。
	if err := srv.patchAttachmentFsync(t.TempDir()); err == nil {
		t.Fatal("找不到 attachment-local 时应报错")
	}
}
