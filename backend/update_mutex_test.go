package main

// update_mutex_test.go —— 「回滚 / 数据恢复必须与更新安装互斥」的回归测试（审查第 2 条）。
//
// 背景：安装侧用 m.applying 串行（downloadUpdate/installUpdate/DiscardUpdate），而
// RollbackServer / RestoreDshData 过去只重置自己的状态字段就起 goroutine，全程不持锁 ——
// 与安装并发时会出现「一边 copyDir 新 server、一边 RemoveAll(server) + 解压备份」，
// 得到半新半旧的目录，且两条路径都认为自己成功。
//
// 这里只验证互斥入口（TryLock 被拒即返回错误、不产生任何副作用），真正的替换步骤需要
// 真实 dsh/server 目录，不在单测范围内。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeBackupFile 在 backupDir 下造一个符合命名规则的假备份包。
func makeBackupFile(t *testing.T, m *UpdateManager, name string) string {
	t.Helper()
	path := filepath.Join(m.backupDir(), name)
	if err := os.WriteFile(path, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRollbackRefusedWhileOtherUpdateRunning(t *testing.T) {
	t.Setenv("TRIM_PKGVAR", t.TempDir())
	m := newBackupTestManager(t, t.TempDir(), t.TempDir())
	path := makeBackupFile(t, m, "server-1.0.0-20250101000000.tar.gz")

	// 模拟「另一个下载/安装正在跑」。
	m.applying.Lock()
	err := m.RollbackServer(path)
	m.applying.Unlock()

	if err == nil || !strings.Contains(err.Error(), "其它更新") {
		t.Fatalf("其它更新进行中时回滚应被拒绝，实际 err=%v", err)
	}
	if st := m.GetRollbackStatus(); st.Done {
		t.Fatal("被拒绝时不应改动回滚状态（不应报告已完成）")
	}
}

func TestRestoreDshDataRefusedWhileOtherUpdateRunning(t *testing.T) {
	t.Setenv("TRIM_PKGVAR", t.TempDir())
	m := newBackupTestManager(t, t.TempDir(), t.TempDir())
	path := makeBackupFile(t, m, "dsh-data-1.0.0-20250101000000.tar.gz")

	m.applying.Lock()
	err := m.RestoreDshData(path)
	m.applying.Unlock()

	if err == nil || !strings.Contains(err.Error(), "其它更新") {
		t.Fatalf("其它更新进行中时恢复应被拒绝，实际 err=%v", err)
	}
	if st := m.GetDshRestoreStatus(); st.Done {
		t.Fatal("被拒绝时不应改动恢复状态（不应报告已完成）")
	}
}

// 互斥必须是双向的：回滚持锁期间，新的下载/安装同样应被挡住（否则等于没互斥）。
func TestDownloadRefusedWhileRollbackRunning(t *testing.T) {
	t.Setenv("TRIM_PKGVAR", t.TempDir())
	m := newBackupTestManager(t, t.TempDir(), t.TempDir())

	// 直接占住锁模拟「回滚进行中」：downloadUpdate 用阻塞 Lock，因此放到 goroutine 里
	// 观察它是否真的等待 —— 拿不到锁就不应该产生任何下载动作。
	m.applying.Lock()
	done := make(chan struct{})
	go func() {
		// 这里不可能真的下载成功（没有版本号/网络），只要求它在锁释放前一直阻塞。
		_ = m.downloadUpdate(updateKindHarness)
		close(done)
	}()

	select {
	case <-done:
		m.applying.Unlock()
		t.Fatal("回滚持锁期间下载不应直接执行")
	default:
	}
	m.applying.Unlock()
	<-done
}
