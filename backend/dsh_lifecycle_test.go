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
// 受管状态（m.cmd 清空、凭据清空）并回收僵尸，否则 Start() 会一直被
// 「already running」挡住。
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
	if ck := dsh.AuthCookie(); ck != "" {
		t.Fatalf("Stop 后应清空会话 cookie，实际 %q", ck)
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
