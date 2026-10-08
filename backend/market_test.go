package main

// market_test.go —— 插件市场（dshmarket）检测 / 安装 / 更新 / 卸载的回归测试。
//
// 形态变化（见 market.go 文件头）：市场不再是 dsh server 包自带的 bundle，而是一个
// 普通的 profile 插件 —— 检测靠 `dsh plugin --profile web list`，安装/更新/卸载靠
// `dsh plugin --profile web add|remove`，镜像源与 dsh 版本安装共用同一套
// 「阿里云 → 腾讯云 → 华为云，不重试」的回退顺序。
//
// 这里钉住的契约：
//   - `dsh plugin list` 的输出里没有 dshmarket 时版本号必须为空（前端显示「未安装」）；
//   - 最新版来自镜像源的 dist-tags.latest，**不试官方源**；
//   - 命令按镜像源顺序逐个尝试：失败的镜像不再重试，成功即返回；
//   - 命令确实带上了 `--profile web` 与 `--registry=<镜像>`（否则等于没用镜像源）。

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

// fakeDshPlugin 造一个假的已安装 dsh 版本：其 `dsh` 可执行文件把收到的参数追加到
// marker 文件、输出给定的 `dsh plugin list` 文本，并以 0 退出。返回 marker 路径。
func fakeDshPlugin(t *testing.T, dataDir, version, output string) string {
	t.Helper()
	return writeFakeDsh(t, dataDir, version, output, "exit 0\n")
}

// writeFakeDsh 在某个版本目录里写一个假 dsh 脚本（自定义脚本尾部，用于模拟失败）。
func writeFakeDsh(t *testing.T, dataDir, version, output, tail string) string {
	t.Helper()
	bin := filepath.Join(serverRootFor(&RuntimeEnv{DataDir: dataDir}), version, "node_modules", ".bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "dsh-args")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + marker + "\n" +
		"cat <<'OUT'\n" + output + "OUT\n" +
		tail
	if err := os.WriteFile(filepath.Join(bin, "dsh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return marker
}

// newMarketTestManager 造一个只碰临时目录、且「已选中某个版本」的 UpdateManager。
func newMarketTestManager(t *testing.T, dataDir, version string) *UpdateManager {
	t.Helper()
	prevCfg := GetConfig()
	initConfig(&AppConfig{DshPort: 0, DshVersion: version})
	t.Cleanup(func() { initConfig(&prevCfg) })

	renv := &RuntimeEnv{DataDir: dataDir, ConfigFile: filepath.Join(dataDir, "config.json")}
	dsh := &DshManager{renv: renv, logf: func(logLevel, string, ...interface{}) {}}
	upd := &UpdateManager{
		renv:     renv,
		dsh:      dsh,
		statuses: map[updateKind]*UpdateStatus{updateKindMarket: {Kind: updateKindMarket}},
	}
	upd.server = newServerManager(renv, dsh, upd)
	return upd
}

// pluginListWithMarket 造一份 `dsh plugin --profile web list` 的真实输出形态（含指定版本的
// dshmarket；版本号为空则不含它）。
func pluginListWithMarket(version string) string {
	dep := "└── node-pty@1.2.0-beta.15"
	count := "1 package"
	if version != "" {
		dep = "├── dshmarket@" + version + "\n└── node-pty@1.2.0-beta.15"
		count = "2 packages"
	}
	return "Legend: production dependency, optional only, dev only\n\n" +
		"dsh-profile-web /vol1/@appshare/Harness/.dsh/profiles/web (PRIVATE)\n\n" +
		"dependencies:\n" + dep + "\n\n" + count + "\n"
}

// samplePluginList 就是「装着 dshmarket 1.66.11」的那份输出。
const samplePluginList = `Legend: production dependency, optional only, dev only

dsh-profile-web /vol1/@appshare/Harness/.dsh/profiles/web (PRIVATE)

dependencies:
├── dshmarket@1.66.11
└── node-pty@1.2.0-beta.15

2 packages
`

// 没有 dshmarket 的输出（未安装形态）。
const samplePluginListNoMarket = `Legend: production dependency, optional only, dev only

dsh-profile-web /vol1/@appshare/Harness/.dsh/profiles/web (PRIVATE)

dependencies:
└── node-pty@1.2.0-beta.15

1 package
`

// --- 检测 ---

func TestMarketInstalledFromPluginList(t *testing.T) {
	dataDir := t.TempDir()
	upd := newMarketTestManager(t, dataDir, "0.2.0-rc.2")
	fakeDshPlugin(t, dataDir, "0.2.0-rc.2", samplePluginList)

	v, ok, err := upd.marketInstalled()
	if err != nil {
		t.Fatalf("检测不应报错: %v", err)
	}
	if !ok || v != "1.66.11" {
		t.Fatalf("检测结果 = (%q, %v), want (1.66.11, true)", v, ok)
	}
}

func TestMarketNotInstalledWhenListLacksIt(t *testing.T) {
	dataDir := t.TempDir()
	upd := newMarketTestManager(t, dataDir, "0.2.0-rc.2")
	fakeDshPlugin(t, dataDir, "0.2.0-rc.2", samplePluginListNoMarket)

	v, ok, err := upd.marketInstalled()
	if err != nil {
		t.Fatalf("检测不应报错: %v", err)
	}
	if ok || v != "" {
		t.Fatalf("未安装时应返回空版本，实得 (%q, %v)", v, ok)
	}

	// 本地检测写入状态：版本号为空（前端据此显示「未安装」），并带上诊断原因。
	upd.mu.Lock()
	upd.statuses[updateKindMarket] = &UpdateStatus{Kind: updateKindMarket, LatestVersion: "1.66.11"}
	upd.mu.Unlock()
	upd.refreshMarketLocal(true)
	st := upd.getStatus(updateKindMarket)
	if st.LocalVersion != "" || st.HasUpdate {
		t.Fatalf("未安装时不应有本地版本/更新标记: %+v", st)
	}
	if !strings.Contains(st.MarketDir, "未列出") {
		t.Fatalf("诊断信息应说明「列表里没有它」，实得 %q", st.MarketDir)
	}
}

// dsh 本身没装（没有选中版本）时，检测要给出可读原因，而不是 panic 或空结果。
func TestMarketInstalledWithoutDshReportsReason(t *testing.T) {
	dataDir := t.TempDir()
	upd := newMarketTestManager(t, dataDir, "")
	if _, ok, err := upd.marketInstalled(); err == nil || ok {
		t.Fatalf("未安装 dsh 时应返回错误且 ok=false，实得 ok=%v err=%v", ok, err)
	} else if !strings.Contains(err.Error(), "未安装 dsh 服务") {
		t.Fatalf("错误应说明 dsh 未安装，实得 %v", err)
	}
}

// --- 最新版：镜像源，且不试官方源 ---

// withMirrors 临时替换镜像源列表。
func withMirrors(t *testing.T, mirrors []npmMirror) {
	t.Helper()
	prev := npmMirrors
	npmMirrors = mirrors
	t.Cleanup(func() { npmMirrors = prev })
}

func marketPackument(t *testing.T, version string) string {
	t.Helper()
	return marketPackumentMulti(t, []string{version}, version)
}

// marketPackumentMulti 造一个含多个版本与 dist-tags 的 packument：用来验证「最新版本号」
// 与 `latest` 标签不是同一回事。
func marketPackumentMulti(t *testing.T, versions []string, latestTag string) string {
	t.Helper()
	vm := map[string]interface{}{}
	for _, v := range versions {
		vm[v] = map[string]string{}
	}
	tags := map[string]string{}
	if latestTag != "" {
		tags["latest"] = latestTag
	}
	doc := map[string]interface{}{
		"name":      marketPackageName,
		"dist-tags": tags,
		"versions":  vm,
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestMarketLatestFallsBackAcrossMirrors(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(marketPackument(t, "1.66.11")))
	}))
	defer good.Close()
	withMirrors(t, []npmMirror{{Name: "镜像一", URL: bad.URL}, {Name: "镜像二", URL: good.URL}})

	upd := newMarketTestManager(t, t.TempDir(), "0.2.0-rc.2")
	ver, mirror, err := upd.marketLatest()
	if err != nil {
		t.Fatalf("最新版检测不应报错: %v", err)
	}
	// 第二个返回值是**日志标识**（英文，供调用方写日志用），不是中文显示名；
	// 这里构造的假镜像没有 Slug，按 URL 主机名兜底。
	wantMirror := npmMirror{Name: "镜像二", URL: good.URL}.mirrorLogName()
	if ver != "1.66.11" || mirror != wantMirror {
		t.Fatalf("最新版 = (%q, %q), want (%q, %q)", ver, mirror, "1.66.11", wantMirror)
	}

	// 全部失败：错误信息必须点出「都不试官方源」这个语义。
	withMirrors(t, []npmMirror{{Name: "镜像一", URL: bad.URL}})
	if _, _, err := upd.marketLatest(); err == nil || !strings.Contains(err.Error(), "镜像源") {
		t.Fatalf("全部失败时应报镜像源相关错误，实得 %v", err)
	}
}

// 检测+镜像都成功时，状态里同时有本地版本、最新版与红点结论。
func TestRefreshMarketStatusSetsUpdateFlag(t *testing.T) {
	dataDir := t.TempDir()
	upd := newMarketTestManager(t, dataDir, "0.2.0-rc.2")
	fakeDshPlugin(t, dataDir, "0.2.0-rc.2", samplePluginList)
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(marketPackument(t, "1.67.0")))
	}))
	defer good.Close()
	withMirrors(t, []npmMirror{{Name: "镜像一", URL: good.URL}})

	upd.refreshMarketStatus()
	st := upd.getStatus(updateKindMarket)
	if st.LocalVersion != "1.66.11" || st.LatestVersion != "1.67.0" || !st.HasUpdate {
		t.Fatalf("市场状态 = %+v, want 本地 1.66.11 / 最新 1.67.0 / 有更新", st)
	}
	if st.Error != "" {
		t.Fatalf("不应有错误: %v", st.Error)
	}

	// 已是最新时不亮红点。
	upd.mu.Lock()
	upd.statuses[updateKindMarket] = &UpdateStatus{Kind: updateKindMarket}
	upd.mu.Unlock()
	latest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(marketPackument(t, "1.66.11")))
	}))
	defer latest.Close()
	withMirrors(t, []npmMirror{{Name: "镜像一", URL: latest.URL}})
	upd.refreshMarketStatus()
	if st := upd.getStatus(updateKindMarket); st.HasUpdate {
		t.Fatalf("本地与 latest 相同时不应报有更新: %+v", st)
	}
}

// 「最新版本」= 版本号最高的那一版，**不是** dist-tags.latest 指向的那一版：
// 标签可能滞后（发布者忘了挪）、可能指向另一条线。
func TestMarketLatestPrefersNewestVersionOverTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(marketPackumentMulti(t,
			[]string{"1.65.0", "1.66.0", "1.67.0-beta.1"}, "1.66.0")))
	}))
	defer srv.Close()
	withMirrors(t, []npmMirror{{Name: "镜像一", URL: srv.URL}})

	dataDir := t.TempDir()
	upd := newMarketTestManager(t, dataDir, "0.2.0")
	// 本机装着市场 1.60.0（旧版本），最新版本号是 1.67.0-beta.1。
	fakeDshPlugin(t, dataDir, "0.2.0", pluginListWithMarket("1.60.0"))

	ver, _, err := upd.marketLatest()
	if err != nil {
		t.Fatalf("取最新版不应报错: %v", err)
	}
	if ver != "1.67.0-beta.1" {
		t.Fatalf("最新版本号 = %q, want 1.67.0-beta.1（版本号最高的一版，而非 latest 标签指向的 1.66.0）", ver)
	}

	// 红点基准同源：本地低于最新版本号就要报有更新（哪怕 latest 标签还指着旧版本）。
	upd.refreshMarketStatus()
	if st := upd.getStatus(updateKindMarket); !st.HasUpdate || st.LatestVersion != "1.67.0-beta.1" {
		t.Fatalf("市场状态 = %+v, want 最新 1.67.0-beta.1 且有更新", st)
	}
}

// 安装命令必须带**精确版本号**（`add dshmarket@<最新版本号>`），不能是 `@latest`。
func TestInstallMarketUsesExactNewestVersion(t *testing.T) {
	dataDir := t.TempDir()
	upd := newMarketTestManager(t, dataDir, "0.2.0")
	marker := fakeDshPlugin(t, dataDir, "0.2.0", samplePluginListNoMarket)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(marketPackumentMulti(t,
			[]string{"1.66.0", "1.66.12", "1.67.0-beta.1"}, "1.66.12")))
	}))
	defer srv.Close()
	withMirrors(t, []npmMirror{{Name: "镜像一", URL: srv.URL}})

	// 重启 dsh 与就绪判定都打桩：这里只验证「装了哪一版」。
	prevStop, prevFree, prevStart, prevBusy := dshStopFn, dshPortFreeFn, startDshCapturedFn, marketBusyFn
	prevReady := marketReadyFn
	t.Cleanup(func() {
		dshStopFn, dshPortFreeFn, startDshCapturedFn, marketBusyFn = prevStop, prevFree, prevStart, prevBusy
		marketReadyFn = prevReady
	})
	dshStopFn = func(*UpdateManager) error { return nil }
	dshPortFreeFn = func(*UpdateManager, time.Duration) {}
	startDshCapturedFn = func(*UpdateManager) error { return nil }
	marketBusyFn = func(*UpdateManager) (bool, string) { return false, "" }
	marketReadyFn = func(*UpdateManager, time.Duration) marketWaitResult { return marketWaitReady }

	if err := upd.InstallMarket(); err != nil {
		t.Fatalf("启动安装失败: %v", err)
	}
	waitMarketPhase(t, upd, "done")
	raw, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	if !strings.Contains(got, "plugin --profile web add "+marketPackageName+"@1.67.0-beta.1") {
		t.Fatalf("安装命令应带精确版本号（版本号最高的一版），实际: %q", got)
	}
	if strings.Contains(got, marketPackageName+"@latest") {
		t.Fatalf("安装命令不应使用 @latest 标签，实际: %q", got)
	}
}

// 本地检测（`dsh plugin list` 子进程）也要缓存：只有显式刷新才重跑。
func TestMarketLocalDetectionIsCached(t *testing.T) {
	dataDir := t.TempDir()
	upd := newMarketTestManager(t, dataDir, "0.2.0")

	// 假 dsh 先报「装着市场 1.0.0」。
	fakeDshPlugin(t, dataDir, "0.2.0", pluginListWithMarket("1.0.0"))
	if v, ok, err := upd.marketInstalled(); err != nil || !ok || v != "1.0.0" {
		t.Fatalf("首次检测 = (%q,%v,%v), want 1.0.0", v, ok, err)
	}

	// 缓存为空 → 非强制也要真查一次；之后改成 2.0.0，非强制仍给缓存里的 1.0.0。
	if snap := upd.detectMarketLocal(false); snap.Version != "1.0.0" {
		t.Fatalf("空缓存时应真查一次，实得 %q", snap.Version)
	}
	fakeDshPlugin(t, dataDir, "0.2.0", pluginListWithMarket("2.0.0"))
	if snap := upd.detectMarketLocal(false); snap.Version != "1.0.0" {
		t.Fatalf("非强制调用应吃缓存（1.0.0），实得 %q", snap.Version)
	}
	// 强制（安装/卸载完成后、自动检测、手动检查）才重跑。
	if snap := upd.detectMarketLocal(true); snap.Version != "2.0.0" {
		t.Fatalf("强制检测应拿到 2.0.0，实得 %q", snap.Version)
	}
}

// --- 安装 / 更新 / 卸载的命令与镜像回退 ---

// 镜像源逐个尝试：第一个失败换下一个，成功即返回；三个都失败则报错。
func TestMarketPluginCmdTriesMirrorsInOrder(t *testing.T) {
	dataDir := t.TempDir()
	upd := newMarketTestManager(t, dataDir, "0.2.0-rc.2")
	// 假 dsh：第一次调用退出 1（第一个镜像源失败），之后退出 0（第二个镜像源成功）。
	stateFile := filepath.Join(t.TempDir(), "state")
	tail := "if [ -f " + stateFile + " ]; then exit 0; fi\ntouch " + stateFile + "\nexit 1\n"
	marker := writeFakeDsh(t, dataDir, "0.2.0-rc.2", "", tail)

	if err := upd.runMarketPluginCmd([]string{"add", marketPackageName + "@latest"}); err != nil {
		t.Fatalf("第二个镜像源应当成功: %v", err)
	}
	raw, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	calls := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(calls) != 2 {
		t.Fatalf("应当尝试两次（第一个镜像失败后换下一个，不重试同一个），实得 %d 次: %v", len(calls), calls)
	}
	// 命令行必须带 --profile web 与对应镜像的 --registry=。
	for i, mirror := range npmMirrors[:2] {
		if !strings.Contains(calls[i], "plugin --profile web add "+marketPackageName+"@latest") {
			t.Errorf("第 %d 次调用缺少 plugin --profile web add 形态: %q", i+1, calls[i])
		}
		if !strings.Contains(calls[i], "--registry="+mirror.URL) {
			t.Errorf("第 %d 次调用应指向 %s，实得 %q", i+1, mirror.Name, calls[i])
		}
	}

	// 三个都失败 → 报错，且错误里点明镜像源。
	//
	// 这里刻意断言「是**英文标识**、且不含中文显示名」：这条错误会被 runMarketOp 的
	// logError 原样打进日志，中文镜像名一旦混进来，日志里就又出现中文了。
	writeFakeDsh(t, dataDir, "0.2.0-rc.2", "", "exit 1\n")
	err = upd.runMarketPluginCmd([]string{"remove", marketPackageName})
	if err == nil || !strings.Contains(err.Error(), mirrorLogNames()) {
		t.Fatalf("全部镜像失败时应报错并列出镜像源（英文标识 %q），实得 %v", mirrorLogNames(), err)
	}
	for _, m := range npmMirrors {
		if strings.Contains(err.Error(), m.Name) {
			t.Fatalf("错误里不应出现中文镜像名 %q（会随日志漏出去）: %v", m.Name, err)
		}
	}
	if !strings.Contains(err.Error(), "不重试") {
		t.Fatalf("错误信息应说明不重试，实得 %v", err)
	}
}

// 卸载命令形态：remove dshmarket（同样带 --profile web 与镜像源）。
func TestRemoveMarketUsesRemoveCommand(t *testing.T) {
	dataDir := t.TempDir()
	upd := newMarketTestManager(t, dataDir, "0.2.0-rc.2")
	marker := fakeDshPlugin(t, dataDir, "0.2.0-rc.2", samplePluginListNoMarket)

	// 替换启停钩子：卸载成功后要重启 dsh，测试里不该真的去停/起进程。
	prevStop, prevFree, prevStart, prevBusy := dshStopFn, dshPortFreeFn, startDshCapturedFn, marketBusyFn
	t.Cleanup(func() {
		dshStopFn, dshPortFreeFn, startDshCapturedFn, marketBusyFn = prevStop, prevFree, prevStart, prevBusy
	})
	dshStopFn = func(*UpdateManager) error { return nil }
	dshPortFreeFn = func(*UpdateManager, time.Duration) {}
	startDshCapturedFn = func(*UpdateManager) error { return nil }
	marketBusyFn = func(*UpdateManager) (bool, string) { return false, "" }
	prevReady := marketReadyFn
	marketReadyFn = func(*UpdateManager, time.Duration) marketWaitResult { return marketWaitReady }
	t.Cleanup(func() { marketReadyFn = prevReady })

	if err := upd.RemoveMarket(); err != nil {
		t.Fatalf("启动卸载失败: %v", err)
	}
	waitMarketPhase(t, upd, "done")
	raw, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	// 第一次调用必须是 remove（随后还会跑一次 `dsh plugin list` 刷新本地版本，那是预期的）。
	calls := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if !strings.Contains(calls[0], "plugin --profile web remove "+marketPackageName) {
		t.Fatalf("卸载命令不符: %q", calls[0])
	}
	removes := 0
	for _, c := range calls {
		if strings.Contains(c, "remove "+marketPackageName) {
			removes++
		}
		// 回归点：曾经把 --registry 无差别拼给所有子命令，而 pnpm 的 remove 不认这个选项
		// （`Unknown option: 'registry'`），于是卸载必然失败还甩锅给镜像源。
		if strings.Contains(c, "--registry") {
			t.Fatalf("卸载命令不能带 --registry（pnpm remove 不支持）: %q", c)
		}
	}
	if removes != 1 {
		t.Fatalf("卸载应当只执行一次（与 registry 无关，没有「换个镜像源再试」这回事），实得 %d 次: %v", removes, calls)
	}
	st := upd.getStatus(updateKindMarket)
	if st.Phase != "done" || st.Error != "" {
		t.Fatalf("卸载后的状态 = %+v, want done 且无错误", st)
	}
}

// 卸载失败时的错误信息必须只说命令本身，不能提镜像源（与镜像源毫无关系），
// 也不能因为「三个镜像源」而重试三次。
func TestRemoveMarketFailureBlamesTheCommandNotMirrors(t *testing.T) {
	dataDir := t.TempDir()
	upd := newMarketTestManager(t, dataDir, "0.2.0-rc.2")
	marker := writeFakeDsh(t, dataDir, "0.2.0-rc.2", "", "echo 'boom' >&2\nexit 1\n")

	if err := upd.RemoveMarket(); err != nil {
		t.Fatalf("启动卸载失败: %v", err)
	}
	// 等错误落到状态里（不能只看 phase 为空 —— 那是初始值）。
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && upd.getStatus(updateKindMarket).Error == "" {
		time.Sleep(10 * time.Millisecond)
	}
	st := upd.getStatus(updateKindMarket)
	if st.Error == "" {
		t.Fatal("卸载失败应把错误写进状态")
	}
	for _, bad := range []string{"镜像源", "阿里云", "腾讯云", "华为云", "不使用官方源"} {
		if strings.Contains(st.Error, bad) {
			t.Fatalf("卸载与镜像源无关，错误信息里不该出现 %q: %s", bad, st.Error)
		}
	}
	if !strings.Contains(st.Error, "plugin --profile web remove "+marketPackageName) {
		t.Fatalf("错误信息应带上是哪条命令失败，实际: %s", st.Error)
	}
	if !strings.Contains(st.Error, "boom") {
		t.Fatalf("错误信息应带上命令输出尾行，实际: %s", st.Error)
	}
	raw, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(strings.Split(strings.TrimSpace(string(raw)), "\n")); got != 1 {
		t.Fatalf("卸载失败后不应重试（没有镜像源可换），实际执行 %d 次", got)
	}
}

// waitMarketPhase 等市场操作进入期望阶段。
func waitMarketPhase(t *testing.T, upd *UpdateManager, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if st := upd.getStatus(updateKindMarket); st.Phase == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("市场状态未进入 %q，当前: %+v", want, upd.getStatus(updateKindMarket))
}
