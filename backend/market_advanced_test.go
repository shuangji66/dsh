package main

// market_advanced_test.go —— 增强插件（dsh-fnos-advanced）与插件市场共用同一套受管插件
// 实现（market.go 的 managedPlugin）的回归测试。
//
// 这里钉住的契约：
//   - 两个插件的检测/安装/卸载互不串台：各写各的状态槽位（kind=market / advanced），
//     命令里用的也是各自的 npm 包名；
//   - 本地检测缓存**按 kind 分开**（一个插件刚查过，不能让另一个插件吃它的缓存）；
//   - 一次刷新只跑一条 `dsh plugin list`（两个插件共用输出），但缓存命中时一条都不跑；
//   - 安装仍带精确版本号、仍按镜像源回退；卸载仍不带 `--registry`。

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// pluginListWithPkgs 造一份 `dsh plugin --profile web list` 输出，按 name → version
// 列出依赖（空版本号表示不列出）。
func pluginListWithPkgs(pkgs map[string]string) string {
	var names []string
	for name, version := range pkgs {
		if version != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return samplePluginListNoMarket
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("Legend: production dependency, optional only, dev only\n\n")
	b.WriteString("dsh-profile-web /vol1/@appshare/Harness/.dsh/profiles/web (PRIVATE)\n\n")
	b.WriteString("dependencies:\n")
	for i, name := range names {
		prefix := "├── "
		if i == len(names)-1 {
			prefix = "└── "
		}
		b.WriteString(prefix + name + "@" + pkgs[name] + "\n")
	}
	b.WriteString("\n" + fmt.Sprintf("%d", len(names)) + " packages\n")
	return b.String()
}

// newManagedPluginTestManager 造一个同时带 market / advanced 两个状态槽位的管理器。
func newManagedPluginTestManager(t *testing.T, dataDir, version string) *UpdateManager {
	t.Helper()
	upd := newMarketTestManager(t, dataDir, version)
	upd.mu.Lock()
	upd.statuses[updateKindAdvanced] = &UpdateStatus{Kind: updateKindAdvanced}
	upd.mu.Unlock()
	return upd
}

func waitPluginPhase(t *testing.T, upd *UpdateManager, kind updateKind, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if st := upd.getStatus(kind); st.Phase == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s 状态未进入 %q，当前: %+v", kind, want, upd.getStatus(kind))
}

// stubPluginRestart 把「重启 dsh」与忙守卫都打桩（本文件不验证那两条链路）。
func stubPluginRestart(t *testing.T) {
	t.Helper()
	prevStop, prevFree, prevStart, prevBusy, prevReady :=
		dshStopFn, dshPortFreeFn, startDshCapturedFn, marketBusyFn, marketReadyFn
	t.Cleanup(func() {
		dshStopFn, dshPortFreeFn, startDshCapturedFn, marketBusyFn, marketReadyFn =
			prevStop, prevFree, prevStart, prevBusy, prevReady
	})
	dshStopFn = func(*UpdateManager) error { return nil }
	dshPortFreeFn = func(*UpdateManager, time.Duration) {}
	startDshCapturedFn = func(*UpdateManager) error { return nil }
	marketBusyFn = func(*UpdateManager) (bool, string) { return false, "" }
	marketReadyFn = func(*UpdateManager, time.Duration) marketWaitResult { return marketWaitReady }
}

// 检测：输出里有 dsh-fnos-advanced 时能拿到版本号，且不误认成市场。
func TestAdvancedInstalledFromPluginList(t *testing.T) {
	dataDir := t.TempDir()
	upd := newManagedPluginTestManager(t, dataDir, "0.2.0")
	fakeDshPlugin(t, dataDir, "0.2.0", pluginListWithPkgs(map[string]string{
		marketPackageName:   "1.66.11",
		advancedPackageName: "0.2.3",
	}))

	v, ok, err := upd.pluginInstalled(advancedPlugin)
	if err != nil {
		t.Fatalf("检测不应报错: %v", err)
	}
	if !ok || v != "0.2.3" {
		t.Fatalf("增强插件检测 = (%q, %v), want (0.2.3, true)", v, ok)
	}
	// 市场那一路不受影响。
	if mv, mok, err := upd.marketInstalled(); err != nil || !mok || mv != "1.66.11" {
		t.Fatalf("市场检测 = (%q, %v, %v), want (1.66.11, true)", mv, mok, err)
	}
}

// 本地检测缓存按 kind 分开：市场刚查过不会让增强插件拿到市场那一份缓存。
func TestManagedPluginLocalCacheIsPerKind(t *testing.T) {
	dataDir := t.TempDir()
	upd := newManagedPluginTestManager(t, dataDir, "0.2.0")
	fakeDshPlugin(t, dataDir, "0.2.0", pluginListWithPkgs(map[string]string{
		marketPackageName:   "1.0.0",
		advancedPackageName: "0.2.1",
	}))

	if snap := upd.detectPluginLocal(marketPlugin, false); snap.Version != "1.0.0" {
		t.Fatalf("市场本地检测 = %q, want 1.0.0", snap.Version)
	}
	// 改盘上的结论：增强插件此时还没查过，非强制调用也必须真查一次。
	fakeDshPlugin(t, dataDir, "0.2.0", pluginListWithPkgs(map[string]string{
		marketPackageName:   "9.9.9",
		advancedPackageName: "0.2.2",
	}))
	if snap := upd.detectPluginLocal(advancedPlugin, false); snap.Version != "0.2.2" {
		t.Fatalf("增强插件首次检测 = %q, want 0.2.2（不能吃市场的缓存）", snap.Version)
	}
	// 各自吃各自的缓存。
	if snap := upd.detectPluginLocal(marketPlugin, false); snap.Version != "1.0.0" {
		t.Fatalf("市场应吃自己的缓存 1.0.0，实得 %q", snap.Version)
	}
	if snap := upd.detectPluginLocal(advancedPlugin, false); snap.Version != "0.2.2" {
		t.Fatalf("增强插件应吃自己的缓存 0.2.2，实得 %q", snap.Version)
	}
	// force=true 各自重查。
	if snap := upd.detectPluginLocal(advancedPlugin, true); snap.Version != "0.2.2" {
		t.Fatalf("强制检测应拿到 0.2.2，实得 %q", snap.Version)
	}
}

// 安装：装的是增强插件的**精确最新版本号**，状态写进 advanced，market 不受影响。
func TestInstallAdvancedUsesExactNewestVersion(t *testing.T) {
	dataDir := t.TempDir()
	upd := newManagedPluginTestManager(t, dataDir, "0.2.0")
	marker := fakeDshPlugin(t, dataDir, "0.2.0", pluginListWithPkgs(map[string]string{
		marketPackageName: "1.66.11",
	}))
	stubPluginRestart(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(marketPackumentMulti(t,
			[]string{"0.2.2", "0.2.3", "0.3.0-beta.1"}, "0.2.3")))
	}))
	defer srv.Close()
	withMirrors(t, []npmMirror{{Name: "镜像一", URL: srv.URL}})

	if err := upd.InstallAdvanced(); err != nil {
		t.Fatalf("启动安装失败: %v", err)
	}
	waitPluginPhase(t, upd, updateKindAdvanced, "done")

	raw, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	calls := string(raw)
	// 版本号最高的一版是 0.3.0-beta.1（不是 dist-tags.latest 指的 0.2.3）。
	if !strings.Contains(calls, "plugin --profile web add "+advancedPackageName+"@0.3.0-beta.1") {
		t.Fatalf("安装命令应带增强插件的精确版本号，实际: %q", calls)
	}
	if strings.Contains(calls, advancedPackageName+"@latest") {
		t.Fatalf("安装命令不应使用 @latest 标签，实际: %q", calls)
	}
	if strings.Contains(calls, marketPackageName+"@0.3.0-beta.1") {
		t.Fatalf("不应把增强插件的版本装到市场包上，实际: %q", calls)
	}

	st := upd.getStatus(updateKindAdvanced)
	if st.Phase != "done" || st.Error != "" {
		t.Fatalf("增强插件状态 = %+v, want done 且无错误", st)
	}
	// 市场状态没被这次操作动过（不会因为共用 applying/seq 而误写）。
	if ms := upd.getStatus(updateKindMarket); ms.Phase != "" || ms.Seq != 0 {
		t.Fatalf("市场状态不应被增强插件操作改写: %+v", ms)
	}
}

// 卸载：remove dsh-fnos-advanced，不带 --registry，只执行一次。
func TestRemoveAdvancedUsesRemoveCommand(t *testing.T) {
	dataDir := t.TempDir()
	upd := newManagedPluginTestManager(t, dataDir, "0.2.0")
	marker := fakeDshPlugin(t, dataDir, "0.2.0", pluginListWithPkgs(map[string]string{
		advancedPackageName: "0.2.3",
	}))
	stubPluginRestart(t)

	if err := upd.RemoveAdvanced(); err != nil {
		t.Fatalf("启动卸载失败: %v", err)
	}
	waitPluginPhase(t, upd, updateKindAdvanced, "done")

	raw, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	calls := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(calls) == 0 || !strings.Contains(calls[0], "plugin --profile web remove "+advancedPackageName) {
		t.Fatalf("卸载命令不符: %v", calls)
	}
	for _, c := range calls {
		if strings.Contains(c, "--registry") {
			t.Fatalf("卸载命令不能带 --registry（pnpm remove 不支持）: %q", c)
		}
	}
}

// 完整检测：本地版本 + 镜像源最新版都写进 advanced，红点按版本号比较得出。
func TestRefreshAdvancedStatusSetsUpdateFlag(t *testing.T) {
	dataDir := t.TempDir()
	upd := newManagedPluginTestManager(t, dataDir, "0.2.0")
	fakeDshPlugin(t, dataDir, "0.2.0", pluginListWithPkgs(map[string]string{
		advancedPackageName: "0.2.1",
	}))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(marketPackument(t, "0.2.3")))
	}))
	defer srv.Close()
	withMirrors(t, []npmMirror{{Name: "镜像一", URL: srv.URL}})

	upd.refreshPluginStatus(advancedPlugin)
	st := upd.getStatus(updateKindAdvanced)
	if st.LocalVersion != "0.2.1" || st.LatestVersion != "0.2.3" || !st.HasUpdate {
		t.Fatalf("增强插件状态 = %+v, want 本地 0.2.1 / 最新 0.2.3 / 有更新", st)
	}
	if st.Error != "" {
		t.Fatalf("不应有错误: %v", st.Error)
	}
	// 市场状态没被顺带写脏（它的本地检测仍为「查不了」：这份输出里没有市场）。
	if ms := upd.getStatus(updateKindMarket); ms.LatestVersion != "" {
		t.Fatalf("市场状态不应被增强插件的检测改写: %+v", ms)
	}
}

// 镜像源全部失败时，错误要带上**增强插件的包名**（而不是笼统说「插件市场」）。
func TestAdvancedMirrorFailureNamesThePackage(t *testing.T) {
	dataDir := t.TempDir()
	upd := newManagedPluginTestManager(t, dataDir, "0.2.0")
	fakeDshPlugin(t, dataDir, "0.2.0", samplePluginListNoMarket)
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer bad.Close()
	withMirrors(t, []npmMirror{{Name: "镜像一", URL: bad.URL}})

	_, _, err := upd.pluginLatest(advancedPlugin)
	if err == nil {
		t.Fatal("镜像源全部失败时应报错")
	}
	if !bytes.Contains([]byte(err.Error()), []byte(advancedPackageName)) {
		t.Fatalf("错误信息应点名 %s，实得: %v", advancedPackageName, err)
	}
}

// 一次强制刷新两个插件只跑一条 `dsh plugin list`；随后非强制刷新命中缓存，一条都不跑。
func TestRefreshManagedPluginsSharesOneListCommand(t *testing.T) {
	dataDir := t.TempDir()
	upd := newManagedPluginTestManager(t, dataDir, "0.2.0")
	marker := fakeDshPlugin(t, dataDir, "0.2.0", pluginListWithPkgs(map[string]string{
		marketPackageName:   "1.0.0",
		advancedPackageName: "0.2.1",
	}))

	upd.refreshManagedPluginsLocal(true)

	listCalls := func() int {
		raw, err := os.ReadFile(marker)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Count(string(raw), "plugin --profile web list")
	}
	if got := listCalls(); got != 1 {
		t.Fatalf("两个插件应共用一次 list 命令，实际 %d 次", got)
	}
	if st := upd.getStatus(updateKindMarket); st.LocalVersion != "1.0.0" || st.Unavailable {
		t.Fatalf("市场本地版本 = %+v, want 1.0.0 且可用", st)
	}
	if st := upd.getStatus(updateKindAdvanced); st.LocalVersion != "0.2.1" || st.Unavailable {
		t.Fatalf("增强插件本地版本 = %+v, want 0.2.1 且可用", st)
	}

	// 两个 kind 都有缓存：非强制刷新不该再起子进程。
	upd.refreshManagedPluginsLocal(false)
	if got := listCalls(); got != 1 {
		t.Fatalf("缓存命中时不应再跑 list，实际共 %d 次", got)
	}
}

// 插件操作失败进日志的文本必须是英文（规则 7）：面向用户的错误优先取 detail / cmd，
// 没有英文参数时只记 code，不能把中文兜底文案打进日志。
func TestPluginOpLogTextStaysEnglish(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "detail 优先",
			err:  uiErr("err_plugin_op_failed", "插件 %s 操作失败: %s", "pkg", "dshmarket", "detail", "pnpm ERR_PNPM_FETCH_404"),
			want: "pnpm ERR_PNPM_FETCH_404",
		},
		{
			name: "没有 detail 时取 cmd",
			err:  uiErr("err_plugin_cmd_failed", "dsh %s 失败", "cmd", "plugin --profile web remove dshmarket"),
			want: "plugin --profile web remove dshmarket",
		},
		{
			name: "只有中文文案时记 code",
			err:  uiErr("err_plugin_restart_timeout", "dsh 服务重启后 30 秒内没有就绪，请查看控制台日志"),
			want: "[err_plugin_restart_timeout]",
		},
		{
			name: "普通错误原样",
			err:  errors.New("exec: exit status 1"),
			want: "exec: exit status 1",
		},
	}
	for _, tc := range cases {
		got := pluginOpLogText(tc.err)
		if got != tc.want {
			t.Fatalf("%s: pluginOpLogText = %q, want %q", tc.name, got, tc.want)
		}
		if !isASCII(got) {
			t.Fatalf("%s: 日志文本必须是英文，实得 %q", tc.name, got)
		}
	}
}
