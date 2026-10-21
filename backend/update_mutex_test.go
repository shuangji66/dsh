package main

// update_mutex_test.go —— 「数据恢复必须与其它 dsh 操作互斥」的回归测试（审查第 2 条）。
//
// 背景：会停/启 dsh 或改 ~/.dsh 的操作共用 m.applying；RestoreDshData 过去只重置
// 自己的状态字段就起 goroutine，全程不持锁 —— 与别的操作并发时会出现「一边替换产物、
// 一边 RemoveAll(~/.dsh) + 解压备份」，得到半新半旧的状态，且两条路径都认为自己成功。
//
// 这里只验证互斥入口（TryLock 被拒即返回错误、不产生任何副作用），真正的恢复步骤需要
// 真实 HOME/.dsh 目录，不在单测范围内。

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
