package main

// update_backup_progress_test.go —— dsh 数据备份「进度 + 取消」的回归测试。
//
// 三条必须成立的约束：
//  1. 打包过程中按块汇报进度（原始字节 + 文件数），总量已知时百分比才有意义；
//  2. 取消能落在**单个大文件的中间**（自写的 tgzCopy 每块检查一次），并且
//     不完整的 .tar.gz 必须被删掉 —— 半个包留在备份列表里比没有更糟；
//  3. 备份与「更新/恢复」双向互斥：备份持锁期间恢复被拒，恢复/更新进行中备份被拒。

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// makeSrcTree 造一个 srcDir：n 个 1MB 的文件（内容不同，避免压缩过度）。
func makeSrcTree(t *testing.T, srcDir string, n int) int64 {
	t.Helper()
	var total int64
	for i := 0; i < n; i++ {
		data := bytes.Repeat([]byte{byte('a' + i)}, 1<<20)
		if err := os.WriteFile(filepath.Join(srcDir, fmt.Sprintf("f%d.bin", i)), data, 0o644); err != nil {
			t.Fatal(err)
		}
		total += int64(len(data))
	}
	return total
}

// 成功路径：字节数累加到总量、文件数正确、产物存在。
func TestTgzDirAsProgressReportsFullProgress(t *testing.T) {
	src := t.TempDir()
	total := makeSrcTree(t, src, 3)
	dest := filepath.Join(t.TempDir(), "out.tar.gz")

	var got int64
	var files int
	err := tgzDirAsProgress(src, dest, ".dsh", &tgzProgress{
		onBytes: func(n int64) { got += n },
		onFile:  func() { files++ },
	})
	if err != nil {
		t.Fatalf("tgzDirAsProgress: %v", err)
	}
	if got != total {
		t.Fatalf("进度汇报的原始字节数 = %d, 期望 %d", got, total)
	}
	if files != 3 {
		t.Fatalf("进度汇报的文件数 = %d, 期望 3", files)
	}
	if fi, serr := os.Stat(dest); serr != nil || fi.Size() == 0 {
		t.Fatalf("备份产物应存在且非空: %v", serr)
	}
}

// 取消路径：错误是 errBackupCancelled（不是失败），产物文件被删除。
// 取消阈值设在 1MB 之后，而总量是 8MB —— 取消必然落在打包中间。
func TestTgzDirAsProgressCancelRemovesPartialFile(t *testing.T) {
	src := t.TempDir()
	makeSrcTree(t, src, 8)
	dest := filepath.Join(t.TempDir(), "out.tar.gz")

	var got int64
	err := tgzDirAsProgress(src, dest, ".dsh", &tgzProgress{
		onBytes:   func(n int64) { got += n },
		cancelled: func() bool { return got >= 1<<20 },
	})
	if !errors.Is(err, errBackupCancelled) {
		t.Fatalf("期望 errBackupCancelled，实际 %v", err)
	}
	if got < 1<<20 || got >= 8<<20 {
		t.Fatalf("取消应落在打包中间，实际只处理了 %d 字节", got)
	}
	if _, serr := os.Stat(dest); !os.IsNotExist(serr) {
		t.Fatalf("取消后必须删掉不完整的备份文件, stat err=%v", serr)
	}
}

// scanDirSize 的总量必须等于实际打包的原始字节数（否则百分比会算错），
// 且与打包同一规则跳过产物自身。
func TestScanDirSizeMatchesPackedBytes(t *testing.T) {
	src := t.TempDir()
	total := makeSrcTree(t, src, 4)
	// 产物落在 src 内：必须被跳过（防御性规则，见 tgzDirAs 注释）。
	dest := filepath.Join(src, "dsh-data-x-20250101000000.tar.gz")
	if err := os.WriteFile(dest, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, files := scanDirSize(src, dest)
	if got != total {
		t.Fatalf("scanDirSize 字节数 = %d, 期望 %d", got, total)
	}
	if files != 4 {
		t.Fatalf("scanDirSize 文件数 = %d, 期望 4", files)
	}
}

// 管理层：异步备份跑完后状态落定、产物按 dsh-data-<版本>-<时间戳>.tar.gz 命名并出现在列表里。
// 进度字段的最终值也必须自洽（Bytes == TotalBytes）。
func TestBackupDshDataAsync(t *testing.T) {
	t.Setenv("TRIM_PKGVAR", t.TempDir())
	home := t.TempDir()
	m := newBackupTestManager(t, home, t.TempDir())
	resetDshBackupTracker(t)

	dshDir := filepath.Join(home, ".dsh")
	if err := os.MkdirAll(filepath.Join(dshDir, "profiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	total := makeSrcTree(t, dshDir, 2)

	if err := m.BackupDshData(); err != nil {
		t.Fatalf("BackupDshData: %v", err)
	}
	st := waitDshBackupDone(t, m, 30*time.Second)
	if !st.Ok || st.Cancelled || st.Error != "" {
		t.Fatalf("备份应成功，实际 %+v", st)
	}
	if st.TotalBytes != total || st.Bytes != total {
		t.Fatalf("进度字段不自洽: bytes=%d total=%d 期望 %d", st.Bytes, st.TotalBytes, total)
	}
	if st.Files != 2 || st.TotalFiles != 2 {
		t.Fatalf("文件数 = %d/%d, 期望 2/2", st.Files, st.TotalFiles)
	}
	if !isBackupFile(st.Name, "dsh-data-") {
		t.Fatalf("备份名不符合规则: %q", st.Name)
	}
	fi, serr := os.Stat(st.Path)
	if serr != nil {
		t.Fatalf("产物文件应存在: %v", serr)
	}
	if fi.Size() != st.Size {
		t.Fatalf("产物大小与状态不一致: %d != %d", fi.Size(), st.Size)
	}
	backups, err := m.ListDshDataBackups()
	if err != nil || len(backups) != 1 {
		t.Fatalf("备份列表应有一条, 实际 %d (err=%v)", len(backups), err)
	}
}

// 取消管理层：取消后状态标记为 cancelled、backup 目录里不留任何半成品。
// 这里靠「循环请求取消直到运行结束」保证必然命中一次运行中的备份（样本足够大）。
func TestBackupDshDataCancel(t *testing.T) {
	t.Setenv("TRIM_PKGVAR", t.TempDir())
	home := t.TempDir()
	m := newBackupTestManager(t, home, t.TempDir())
	resetDshBackupTracker(t)

	dshDir := filepath.Join(home, ".dsh")
	if err := os.MkdirAll(dshDir, 0o755); err != nil {
		t.Fatal(err)
	}
	makeSrcTree(t, dshDir, 24) // 24MB：足够让取消落在打包中间

	if err := m.BackupDshData(); err != nil {
		t.Fatalf("BackupDshData: %v", err)
	}
	// 反复请求取消（幂等）直到这次运行结束：备份一旦开始，取消一定会在下一块边界生效。
	deadline := time.Now().Add(30 * time.Second)
	var st DshBackupStatus
	for time.Now().Before(deadline) {
		st = m.CancelDshBackup()
		if !st.Running {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if st.Running {
		t.Fatal("超时：备份既没结束、取消也没生效")
	}
	if !st.Cancelled || st.Ok {
		t.Fatalf("取消后状态应为 cancelled, 实际 %+v", st)
	}
	names := listBackupNames(t, m, "dsh-data-")
	if len(names) != 0 {
		t.Fatalf("取消后不应留下任何备份文件, 实际 %v", names)
	}
}

// 互斥（备份方向）：更新/恢复持锁期间，备份直接被拒且不产生副作用。
func TestBackupRefusedWhileOtherUpdateRunning(t *testing.T) {
	t.Setenv("TRIM_PKGVAR", t.TempDir())
	home := t.TempDir()
	m := newBackupTestManager(t, home, t.TempDir())
	resetDshBackupTracker(t)

	if err := os.MkdirAll(filepath.Join(home, ".dsh"), 0o755); err != nil {
		t.Fatal(err)
	}
	m.applying.Lock()
	err := m.BackupDshData()
	m.applying.Unlock()

	if err == nil || !strings.Contains(err.Error(), "其它更新/恢复") {
		t.Fatalf("其它更新进行中时备份应被拒绝，实际 err=%v", err)
	}
	if st := m.GetDshBackupStatus(); st.Running || st.Done {
		t.Fatalf("被拒绝时不应改动备份状态: %+v", st)
	}
	if names := listBackupNames(t, m, "dsh-data-"); len(names) != 0 {
		t.Fatalf("被拒绝时不应产生文件: %v", names)
	}
}

// 互斥（备份自身）：一次只允许一个备份，第二个请求被拒。
func TestBackupRefusedWhileBackupRunning(t *testing.T) {
	t.Setenv("TRIM_PKGVAR", t.TempDir())
	home := t.TempDir()
	m := newBackupTestManager(t, home, t.TempDir())

	if err := os.MkdirAll(filepath.Join(home, ".dsh"), 0o755); err != nil {
		t.Fatal(err)
	}
	m.backupMu.Lock()
	err := m.BackupDshData()
	m.backupMu.Unlock()

	if err == nil || !strings.Contains(err.Error(), "已有备份任务") {
		t.Fatalf("已有备份在跑时应被拒绝，实际 err=%v", err)
	}
}

// 互斥（恢复方向）：备份正在跑时恢复被拒（删 ~/.dsh 会让那次备份变成半成品）。
func TestRestoreRefusedWhileBackupRunning(t *testing.T) {
	t.Setenv("TRIM_PKGVAR", t.TempDir())
	m := newBackupTestManager(t, t.TempDir(), t.TempDir())
	resetDshBackupTracker(t)
	path := makeBackupFile(t, m, "dsh-data-1.0.0-20250101000000.tar.gz")

	// 直接占住跟踪器模拟「备份进行中」：RestoreDshData 的第一道判定就是它。
	dshBackup.begin("dsh-data-1.0.0-20250101000000.tar.gz", path)
	dshBackup.setTotals(1, 1)
	err := m.RestoreDshData(path)

	if err == nil || !strings.Contains(err.Error(), "正在备份") {
		t.Fatalf("备份进行中时恢复应被拒绝，实际 err=%v", err)
	}
}

// --- 测试辅助 ---

// resetDshBackupTracker 把包级跟踪器复位，避免用例之间互相污染（它是进程级单例）：
// 用例结束后再复位一次，免得把「已完成」的状态留给下一个用例。
func resetDshBackupTracker(t *testing.T) {
	t.Helper()
	resetDshBackupState()
	t.Cleanup(resetDshBackupState)
}

func resetDshBackupState() {
	dshBackup.mu.Lock()
	dshBackup.st = DshBackupStatus{}
	dshBackup.active = false
	dshBackup.wantCancel = false
	dshBackup.mu.Unlock()
}

// waitDshBackupDone 轮询备份状态直到落定。
func waitDshBackupDone(t *testing.T, m *UpdateManager, timeout time.Duration) DshBackupStatus {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st := m.GetDshBackupStatus()
		if st.Done {
			return st
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待备份结束超时, 最后状态 %+v", m.GetDshBackupStatus())
	return DshBackupStatus{}
}

// --- 半成品（.part）与完成包的区分 ---

// 打包先写 <名称>.part、成功后改名：留下的半成品不能被当成一份可用备份。
// 恢复是「先删 ~/.dsh 再解压」，拿半个包恢复就是把数据删了却解不出内容。
func TestBackupWritesPartFileThenRenames(t *testing.T) {
	t.Setenv("TRIM_PKGVAR", t.TempDir())
	home := t.TempDir()
	m := newBackupTestManager(t, home, t.TempDir())
	resetDshBackupTracker(t)

	dshDir := filepath.Join(home, ".dsh")
	if err := os.MkdirAll(dshDir, 0o755); err != nil {
		t.Fatal(err)
	}
	makeSrcTree(t, dshDir, 2)

	if err := m.BackupDshData(); err != nil {
		t.Fatalf("BackupDshData: %v", err)
	}
	st := waitDshBackupDone(t, m, 30*time.Second)
	if !st.Ok {
		t.Fatalf("备份应成功，实际 %+v", st)
	}
	// 完成包在，半成品不在。
	if _, err := os.Stat(st.Path); err != nil {
		t.Fatalf("完成包应存在: %v", err)
	}
	if _, err := os.Stat(st.Path + backupPartSuffix); !os.IsNotExist(err) {
		t.Fatalf("收尾后不该留下 .part 半成品")
	}
	// 半成品也不该出现在备份列表里（名字不匹配 -<14位时间戳>.tar.gz）。
	if isBackupFile(filepath.Base(st.Path)+backupPartSuffix, "dsh-data-") {
		t.Fatal(".part 半成品不该被当成合法备份文件")
	}
}

// 启动清理：上次打包被杀留下的 .part 会被扫掉（它既不在备份列表里，也不会被 30 天清理碰到）。
func TestSweepPartialBackups(t *testing.T) {
	t.Setenv("TRIM_PKGVAR", t.TempDir())
	m := newBackupTestManager(t, t.TempDir(), t.TempDir())
	resetDshBackupTracker(t)

	dir := m.backupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(dir, "dsh-data-1.0.0-20250101000000.tar.gz"+backupPartSuffix)
	keep := filepath.Join(dir, "dsh-data-1.0.0-20250101000000.tar.gz")
	for _, p := range []string{partial, keep} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	m.sweepPartialBackups()

	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Fatal("半成品应被清掉")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("完成包不能被误删: %v", err)
	}
}

// 正在写的那一份不能被删：删掉后这次备份仍会报「成功」，盘上却查无此包。
func TestDeleteDshDataBackupRefusesInFlightFile(t *testing.T) {
	t.Setenv("TRIM_PKGVAR", t.TempDir())
	m := newBackupTestManager(t, t.TempDir(), t.TempDir())
	resetDshBackupTracker(t)

	dir := m.backupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "dsh-data-1.0.0-20250101000000.tar.gz"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 模拟「这份文件正在被备份写入」。
	dshBackup.begin(name, path)
	if err := m.DeleteDshDataBackup(name); err == nil {
		t.Fatal("正在写入的备份文件不允许删除")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("被拒绝时不该删除文件: %v", err)
	}

	// 备份结束后即可删除。
	dshBackup.finish(true, false, nil, 1)
	if err := m.DeleteDshDataBackup(name); err != nil {
		t.Fatalf("备份结束后应可删除: %v", err)
	}
}
