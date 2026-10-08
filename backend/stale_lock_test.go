package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// 本文件覆盖「线上故障：控制台更新过市场后，市场内无法更新、插件列表空白」的两处修复：
//  1. cleanStaleProfileLocks：清理持有者已死的 dsh 写锁（plugin-manager 的
//     profile/package.json.lock 等待上限 120 秒，陈旧锁会让一切插件操作白等后失败）；
//  2. DshManager.Stop 不再用 `pkill -x MainThread|node-MainThread` 按进程名杀
//     （那会误杀市场正在跑的 dsh plugin add 及其 pnpm 子进程 → 留下同样的陈旧锁），
//     改为始终按 PID / 进程组精准终止 —— 这里用一个「假 dsh」进程验证仍然停得掉。

func TestMain(m *testing.M) {
	// 被本文件的测试当作「假 dsh」拉起时：只挂住，等被信号杀死。
	// 用测试二进制自身充当假 dsh，可以精确构造 argv（bin.js / web / --port N），
	// 不依赖 node、也不留下孤儿进程。
	if os.Getenv("HARNESS_FAKE_DSH") == "1" {
		select {}
	}
	os.Exit(m.Run())
}

// newTestDshManager 构造测试用的 DshManager：补齐只有 NewDshManager 才会设置的字段
// （尤其 logf —— 直接字面量构造时它为 nil，调用即 panic）。
func newTestDshManager(home, pidFile string) *DshManager {
	return &DshManager{
		renv:       &RuntimeEnv{Home: home},
		dshPidFile: pidFile,
		logf:       func(logLevel, string, ...interface{}) {},
	}
}

// --- 陈旧锁清理 ---

func TestCleanStaleProfileLocks(t *testing.T) {
	home := t.TempDir()
	dshHome := filepath.Join(home, ".dsh")
	profileLock := filepath.Join(dshHome, "profiles", "web", "package.json.lock")
	fallbackLock := filepath.Join(dshHome, "profiles", "node_modules.lock")
	settingsLock := filepath.Join(dshHome, "settings.yaml.lock")
	// 一个几乎不可能存在的 PID（内核 pid_max 上限附近）
	const deadPID = 999999

	if err := os.MkdirAll(filepath.Dir(profileLock), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{profileLock, fallbackLock, settingsLock} {
		if err := os.WriteFile(p, []byte(strconv.Itoa(deadPID)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// 活锁：内容是本测试进程自己的 PID → 绝不能删。
	liveLock := filepath.Join(dshHome, ".credentials.yaml.lock")
	if err := os.WriteFile(liveLock, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 内容不可解析（写了一半就死）→ 保守不动。
	garbageLock := filepath.Join(dshHome, "profiles", "web", "cordis.patch.yml.lock")
	if err := os.WriteFile(garbageLock, []byte("not-a-pid"), 0o600); err != nil {
		t.Fatal(err)
	}

	prevCfg := GetConfig()
	initConfig(&AppConfig{})
	t.Cleanup(func() { initConfig(&prevCfg) })

	m := newTestDshManager(home, "")
	if got := m.cleanStaleProfileLocks(); got != 3 {
		t.Fatalf("清理条数 = %d, want 3", got)
	}
	for _, p := range []string{profileLock, fallbackLock, settingsLock} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("陈旧锁未被清理: %s", p)
		}
	}
	if _, err := os.Stat(liveLock); err != nil {
		t.Fatalf("持有者仍活着的锁被误删: %v", err)
	}
	if _, err := os.Stat(garbageLock); err != nil {
		t.Fatalf("无法解析的锁不应被删: %v", err)
	}
	// 幂等：再跑一次没有可清理的。
	if got := m.cleanStaleProfileLocks(); got != 0 {
		t.Fatalf("第二次清理条数 = %d, want 0", got)
	}
}

// 没有锁文件时不应报错、也不应影响其它逻辑。
func TestCleanStaleProfileLocksWithoutFiles(t *testing.T) {
	prevCfg := GetConfig()
	initConfig(&AppConfig{})
	t.Cleanup(func() { initConfig(&prevCfg) })
	m := newTestDshManager(t.TempDir(), "")
	if got := m.cleanStaleProfileLocks(); got != 0 {
		t.Fatalf("清理条数 = %d, want 0", got)
	}
}

// --- Stop() 的精准终止路径 ---

// fakeDsh 起一个「假 dsh」：argv 形如 <...>/bin.js web --port <port>，
// 这样 findDshPid 能按 /proc 扫到它，与真实 dsh 的识别条件一致。
func fakeDsh(t *testing.T, port int) *exec.Cmd {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Skipf("拿不到测试二进制路径: %v", err)
	}
	cmd := exec.Command(self)
	cmd.Path = self
	cmd.Args = []string{filepath.Join(t.TempDir(), "bin.js"), "web", "--port", strconv.Itoa(port)}
	cmd.Env = append(os.Environ(), "HARNESS_FAKE_DSH=1")
	// 独立进程组：Stop 的进程组兜底只应打死这个假 dsh，
	// 绝不能命中 go test / 调用方所在的进程组。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Skipf("无法启动假 dsh: %v", err)
	}
	return cmd
}

func waitProcessGone(cmd *exec.Cmd, d time.Duration) bool {
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

func TestStopKillsFakeDshByPidFile(t *testing.T) {
	const port = 19981
	prevCfg := GetConfig()
	initConfig(&AppConfig{DshPort: port})
	pidFile := filepath.Join(t.TempDir(), "dsh.pid")
	t.Cleanup(func() { initConfig(&prevCfg) })

	cmd := fakeDsh(t, port)
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = waitProcessGone(cmd, 3*time.Second)
		}
	}()
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newTestDshManager("", pidFile)
	if err := m.Stop(); err != nil {
		t.Fatalf("Stop 返回错误: %v", err)
	}
	if !waitProcessGone(cmd, 5*time.Second) {
		t.Fatal("按 pid 文件精准终止没有停掉假 dsh（新 Stop 路径失效）")
	}
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Fatal("Stop 后应移除 dsh PID 文件")
	}
}

func TestStopKillsFakeDshWithoutPidFile(t *testing.T) {
	const port = 19982
	prevCfg := GetConfig()
	initConfig(&AppConfig{DshPort: port})
	t.Cleanup(func() { initConfig(&prevCfg) })

	cmd := fakeDsh(t, port)
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = waitProcessGone(cmd, 3*time.Second)
		}
	}()

	// 没有 pid 文件：必须靠 /proc 发现 + 进程组兜底停掉它。
	m := newTestDshManager("", filepath.Join(t.TempDir(), "missing.pid"))
	if pid := m.findDshPid(); pid != cmd.Process.Pid {
		t.Fatalf("findDshPid = %d, want %d（假 dsh 应被 /proc 识别）", pid, cmd.Process.Pid)
	}
	if err := m.Stop(); err != nil {
		t.Fatalf("Stop 返回错误: %v", err)
	}
	if !waitProcessGone(cmd, 8*time.Second) {
		t.Fatal("没有 pid 文件时 Stop 没有停掉假 dsh")
	}
}

// Stop 在「没有 dsh 可停」时必须安全返回（不能因为改造后的路径而报错/误杀）。
func TestStopWithoutAnyDshIsSafe(t *testing.T) {
	prevCfg := GetConfig()
	initConfig(&AppConfig{DshPort: 19983})
	t.Cleanup(func() { initConfig(&prevCfg) })
	m := newTestDshManager("", filepath.Join(t.TempDir(), "missing.pid"))
	if err := m.Stop(); err != nil {
		t.Fatalf("Stop 应当安全返回，实际: %v", err)
	}
}

// --- 市场「忙」守卫 ---

// 市场操作（安装/卸载）跑完后的「重启 dsh」这一步仍要过忙守卫：重启会停 dsh，
// 若此刻市场面板正在装别的插件，那次操作会被连带杀掉并留下陈旧写锁。
// 守卫拒绝时：报错、不停 dsh；插件命令本身已经跑完（其结果保留在盘上）。
func TestMarketOpRestartRespectsBusyGuard(t *testing.T) {
	prevBusy, prevCmd := marketBusyFn, marketPluginCmdFn
	prevStop, prevFree, prevStart := dshStopFn, dshPortFreeFn, startDshCapturedFn
	t.Cleanup(func() {
		marketBusyFn, marketPluginCmdFn = prevBusy, prevCmd
		dshStopFn, dshPortFreeFn, startDshCapturedFn = prevStop, prevFree, prevStart
	})

	calls := []string{}
	marketPluginCmdFn = func(*UpdateManager, []string) error { calls = append(calls, "cmd"); return nil }
	dshStopFn = func(*UpdateManager) error { calls = append(calls, "stop"); return nil }
	dshPortFreeFn = func(*UpdateManager, time.Duration) { calls = append(calls, "portfree") }
	startDshCapturedFn = func(*UpdateManager) error { calls = append(calls, "start"); return nil }

	marketBusyFn = func(*UpdateManager) (bool, string) { return true, "dsh-mobile@0.4.4" }
	m := &UpdateManager{dsh: newTestDshManager("", "")}
	m.statuses = map[updateKind]*UpdateStatus{updateKindMarket: {Kind: updateKindMarket}}
	if err := m.InstallMarket(); err != nil {
		t.Fatalf("启动异步市场操作不应报错: %v", err)
	}
	// 等异步收尾：命令跑完了但重启被守卫拒绝。
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st := m.getStatus(updateKindMarket)
		if st.Error != "" || st.Phase == "done" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	st := m.getStatus(updateKindMarket)
	if st.Error == "" {
		t.Fatalf("市场忙时应把重启失败报给用户，实际状态 %+v", st)
	}
	if !strings.Contains(st.Error, "写锁") || !strings.Contains(st.Error, "市场") {
		t.Fatalf("错误信息应说明原因（市场忙 → 会留下陈旧写锁），实际: %v", st.Error)
	}
	if strings.Join(calls, ",") != "cmd" {
		t.Fatalf("被守卫拒绝时不应停/起 dsh，实际调用: %v", calls)
	}
}

// --- 忙守卫：更新 dsh 服务 / 回滚 server 目录同样受保护 ---

// 控制台自己在跑插件命令时也必须挡：那些命令同样持有 profile 写锁。
func TestReplaceBusyGuardBlocksWhenConsolePluginCmdRunning(t *testing.T) {
	prevBusy := marketBusyFn
	marketBusyFn = func(*UpdateManager) (bool, string) { return false, "" }
	t.Cleanup(func() { marketBusyFn = prevBusy })

	m := newTestDshManager("", "")
	upd := &UpdateManager{dsh: m}
	// 没有插件命令在跑 → 放行
	if err := upd.replaceBusyGuard("更新 dsh 服务"); err != nil {
		t.Fatalf("空闲时不应拒绝: %v", err)
	}
	// 有一个插件命令在跑（模拟控制台插件页的 list/remove）→ 拒绝
	m.beginPluginCmd()
	defer m.endPluginCmd()
	err := upd.replaceBusyGuard("更新 dsh 服务")
	if err == nil {
		t.Fatal("控制台正在执行插件命令时应拒绝")
	}
	if !strings.Contains(err.Error(), "控制台正在执行插件命令") || !strings.Contains(err.Error(), "写锁") {
		t.Fatalf("错误信息应说明原因，实际: %v", err)
	}
}

// stopDshForReplacement：忙时「拒绝且不停 dsh」，闲时「停 dsh 并等端口释放」。
func TestStopDshForReplacementRespectsGuard(t *testing.T) {
	prevStop, prevFree := dshStopFn, dshPortFreeFn
	prevBusy := marketBusyFn
	t.Cleanup(func() {
		dshStopFn, dshPortFreeFn = prevStop, prevFree
		marketBusyFn = prevBusy
	})
	calls := []string{}
	dshStopFn = func(*UpdateManager) error { calls = append(calls, "stop"); return nil }
	dshPortFreeFn = func(*UpdateManager, time.Duration) { calls = append(calls, "portfree") }

	m := newTestDshManager("", "")
	upd := &UpdateManager{dsh: m}

	marketBusyFn = func(*UpdateManager) (bool, string) { return true, "dsh-mobile@0.4.4" }
	if err := upd.stopDshForReplacement("更新 dsh 服务", updateKindDsh); err == nil {
		t.Fatal("市场忙时应拒绝")
	}
	if len(calls) != 0 {
		t.Fatalf("被拒绝时不应停 dsh: %v", calls)
	}

	marketBusyFn = func(*UpdateManager) (bool, string) { return false, "" }
	if err := upd.stopDshForReplacement("更新 dsh 服务", updateKindDsh); err != nil {
		t.Fatalf("空闲时应放行: %v", err)
	}
	if strings.Join(calls, ",") != "stop,portfree" {
		t.Fatalf("停机序列 = %v, want [stop portfree]", calls)
	}
}

// Switch 在忙时必须「拒绝且不停 dsh、不改配置」：切版本要停 dsh，会打断插件操作。
func TestSwitchVersionRefusesWhenBusy(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "server", "1.2.3", "node_modules", ".bin")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dsh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	prevBusy := marketBusyFn
	prevStop := dshStopFn
	t.Cleanup(func() { marketBusyFn, dshStopFn = prevBusy, prevStop })
	marketBusyFn = func(*UpdateManager) (bool, string) { return true, "dsh-better-sidebar@0.19.1" }
	stopped := false
	dshStopFn = func(*UpdateManager) error { stopped = true; return nil }

	prevCfg := GetConfig()
	initConfig(&AppConfig{})
	t.Cleanup(func() { initConfig(&prevCfg) })

	renv := &RuntimeEnv{DataDir: home}
	upd := &UpdateManager{renv: renv, dsh: newTestDshManager(home, "")}
	upd.server = newServerManager(renv, upd.dsh, upd)

	err := upd.server.Switch("1.2.3")
	if err == nil {
		t.Fatal("市场忙时切换 dsh 版本应当被拒绝")
	}
	if !strings.Contains(err.Error(), "插件市场") || !strings.Contains(err.Error(), "写锁") {
		t.Fatalf("错误信息应说明原因，实际: %v", err)
	}
	if stopped {
		t.Fatal("被拒绝时不应停止 dsh")
	}
	if v := GetConfig().DshVersion; v != "" {
		t.Fatalf("被拒绝时不应写入选中版本，实际 %q", v)
	}
}

// harness 自我更新与 dsh 数据恢复同样先过忙守卫，且被拒绝时不产生任何破坏。
func TestHarnessUpdateAndDataRestoreRespectBusyGuard(t *testing.T) {
	prevBusy := marketBusyFn
	t.Cleanup(func() { marketBusyFn = prevBusy })
	marketBusyFn = func(*UpdateManager) (bool, string) { return true, "dsh-free-search@0.4.32" }

	home := t.TempDir()
	dshDir := filepath.Join(home, ".dsh")
	if err := os.MkdirAll(filepath.Join(dshDir, "profiles", "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dshDir, "profiles", "web", "keep.txt")
	if err := os.WriteFile(keep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	prevCfg := GetConfig()
	initConfig(&AppConfig{})
	t.Cleanup(func() { initConfig(&prevCfg) })

	m := &UpdateManager{renv: &RuntimeEnv{Home: home}, dsh: newTestDshManager(home, "")}

	// 1) harness 自我更新：在替换自身二进制之前就被挡下（这里用一个不存在的解压目录，
	//    若守卫失效会走到 applyHarness 并报「找不到 harness 可执行文件」，错误信息不同）。
	err := m.installHarness(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "插件市场") {
		t.Fatalf("harness 更新应被忙守卫拒绝，实际: %v", err)
	}

	// 2) dsh 数据恢复：绝不能删掉 ~/.dsh
	if err := m.doRestoreDshData(filepath.Join(t.TempDir(), "dsh-data.tar.gz")); err == nil {
		t.Fatal("数据恢复应被忙守卫拒绝")
	}
	if _, statErr := os.Stat(keep); statErr != nil {
		t.Fatalf("被拒绝时不应改动 ~/.dsh: %v", statErr)
	}
}

// 停止/重启 dsh 的确认弹窗靠这个快照决定要不要多显示一条风险提示。
func TestDshBusySnapshot(t *testing.T) {
	prevBusy := marketBusyFn
	t.Cleanup(func() { marketBusyFn = prevBusy })

	dshMgr := newTestDshManager("", "")
	mux := &AdminMux{dsh: dshMgr, update: &UpdateManager{dsh: dshMgr}}

	// 1) 都空闲 → busy=false
	marketBusyFn = func(*UpdateManager) (bool, string) { return false, "" }
	snap := mux.dshBusySnapshot()
	if snap["busy"] != false {
		t.Fatalf("空闲时 busy 应为 false，实际 %+v", snap)
	}

	// 2) 市场在装插件 → 报市场 + 目标名（用户能看到"卡在哪个插件上"）
	marketBusyFn = func(*UpdateManager) (bool, string) { return true, "dsh-mobile@0.4.4" }
	snap = mux.dshBusySnapshot()
	if snap["busy"] != true || snap["source"] != "market" || snap["detail"] != "dsh-mobile@0.4.4" {
		t.Fatalf("应报市场忙及其目标，实际 %+v", snap)
	}

	// 3) 控制台自己在跑插件命令 → 优先报控制台（更准确，且不需要网络）
	dshMgr.beginPluginCmd()
	defer dshMgr.endPluginCmd()
	snap = mux.dshBusySnapshot()
	if snap["busy"] != true || snap["source"] != "console" {
		t.Fatalf("应优先报控制台插件命令，实际 %+v", snap)
	}
}
