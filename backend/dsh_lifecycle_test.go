package main

import (
	"os/exec"
	"testing"
	"time"
)

// startDeadChild 启动一个立刻退出的子进程，并等它进入僵尸态（我们不 Wait 它，
// 因此 ProcessState 仍为空 —— 这正是「dsh 自行退出」在控制台侧的真实样子）。
func startDeadChild(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Skipf("无法启动子进程: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for processAlive(cmd.Process.Pid) {
		if time.Now().After(deadline) {
			t.Fatalf("子进程 %d 未在预期时间内退出", cmd.Process.Pid)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Cleanup(func() { _ = cmd.Wait() })
	return cmd
}

// 回归（审查第 1 条）：受管子进程自行退出后，不能再被当成「仍在运行」——
// 旧实现只看 ProcessState（只有 Wait 会写），于是 Status 报 running、Start 报
// already running、Stop 直接返回，用户只能重启控制台。
func TestStoppedDetectsDeadTrackedProcess(t *testing.T) {
	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })
	// DshPort=0：findDshPid 不会命中本机任何真实 dsh 进程。
	initConfig(&AppConfig{DshPort: 0})

	dsh := newTestDshManager(t.TempDir(), "")
	cmd := startDeadChild(t)
	dsh.mu.Lock()
	dsh.cmd = cmd
	dsh.mu.Unlock()

	if !dsh.stopped() {
		t.Fatal("PID 已不存在的受管进程必须判为 stopped")
	}
	if dsh.Running() {
		t.Fatal("Running() 不应把已退出的受管进程算作运行中")
	}
	if st := dsh.Status(); st["running"] != false {
		t.Fatalf("Status().running = %v, want false", st["running"])
	}
}

// 回归（审查第 1 条）：受管进程已死且 /proc 里没有实时 dsh 时，Stop 必须收回
// 受管状态（m.cmd 清空、一次性 token 清空）并回收僵尸，否则 Start() 会一直被
// 「already running」挡住；会话 cookie 则**刻意保留**，供下次启动的凭据探测使用。
func TestStopClearsDeadTrackedProcess(t *testing.T) {
	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })
	initConfig(&AppConfig{DshPort: 0})

	dsh := newTestDshManager(t.TempDir(), "")
	cmd := startDeadChild(t)
	dsh.mu.Lock()
	dsh.cmd = cmd
	dsh.mu.Unlock()
	dsh.setAuthCookie("dsh-auth-stale=1")

	if err := dsh.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	dsh.mu.Lock()
	tracked := dsh.cmd
	dsh.mu.Unlock()
	if tracked != nil {
		t.Fatalf("Stop 后 m.cmd 应被清空，实际仍指向 pid %d", tracked.Process.Pid)
	}
	if ck := dsh.AuthCookie(); ck != "dsh-auth-stale=1" {
		t.Fatalf("Stop 不应清空会话 cookie（下次启动靠它探测，见 forgetDshProcessLocked），实际 %q", ck)
	}
	if tok := dsh.Token(); tok != "" {
		t.Fatalf("Stop 后应清空一次性 token，实际 %q", tok)
	}

	// Start 的前置检查同样不应再被「已死的受管 cmd」挡住（这里只验证判定，不去真的
	// 拉起 dsh：开发机上 PATH 里就有一份真实 dsh，测试绝不能顺手启动一个真进程）。
	dsh.mu.Lock()
	blocked := dsh.alreadyRunningLocked()
	dsh.mu.Unlock()
	if blocked {
		t.Fatal("alreadyRunningLocked() 不应把已退出且未回收的受管 cmd 算作运行中")
	}
}

// Stop 必须结束当前这一代：仍在飞行中的 captureDshSession / repairSession 可能在
// Stop 之后才拿到探测/兑换结果，若不加代号栅栏就会把已停止的一代标成「凭据已落定」。
// 会话 cookie 本身保留（探测靠它），但「已落定」必须失效。
func TestStopInvalidatesSessionSettled(t *testing.T) {
	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })
	initConfig(&AppConfig{DshPort: 0})

	dsh := newTestDshManager(t.TempDir(), "")
	dsh.bumpSessionGen()
	gen := dsh.SessionGen()
	dsh.markSessionSettledGen(gen)
	dsh.setAuthCookie("dsh-auth-keep=1")
	if !dsh.SessionSettled() {
		t.Fatal("前置：同一代标记后应落定")
	}

	if err := dsh.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if dsh.SessionSettled() {
		t.Fatal("Stop 后旧代不得再算「凭据已落定」（否则 Stop 期间/之后仍可能放行）")
	}
	if got := dsh.AuthCookie(); got != "dsh-auth-keep=1" {
		t.Fatalf("Stop 应保留会话 cookie 供下次启动探测，实际 %q", got)
	}
}
