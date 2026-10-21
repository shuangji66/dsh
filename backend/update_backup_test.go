package main

// update_backup_test.go —— dsh 数据备份测试的公共夹具。
//
// 规则：**只有用户主动的 dsh-data-* 要留存**（见 BackupDshData）；备份目录里的
// .part 半成品会被清理（见 update_backup_progress_test.go）。

import (
	"os"
	"os/exec"
	"testing"
)

// listBackupNames 列出 TRIM_PKGVAR/backup 下匹配 prefix 的备份包名。
func listBackupNames(t *testing.T, m *UpdateManager, prefix string) []string {
	t.Helper()
	entries, err := os.ReadDir(m.backupDir())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if isBackupFile(e.Name(), prefix) {
			names = append(names, e.Name())
		}
	}
	return names
}

// newBackupTestManager 造一个只碰临时目录的 UpdateManager。
//
// dsh 被伪装成「看起来已在运行」（进程 pid 记 0）：测试不该真的去 exec 一个 dsh 进程
// （那会受宿主机 PATH 影响，可能真起一个进程）。这个伪装是无副作用的：Start 立刻返回
// "already running"；万一走到 Stop，pid 0 会让它直接判定「没有可停的 dsh」而返回。
// PATH 同样指向空目录，避免任何外部命令去跑宿主机上真实的那份。
func newBackupTestManager(t *testing.T, home, appDest string) *UpdateManager {
	t.Helper()
	renv := &RuntimeEnv{Home: home, TRIMAppDest: appDest, Path: t.TempDir()}
	return &UpdateManager{
		renv:     renv,
		dsh:      &DshManager{renv: renv, logf: logAt, cmd: &exec.Cmd{Process: &os.Process{Pid: 0}}},
		statuses: map[updateKind]*UpdateStatus{updateKindDsh: {Kind: updateKindDsh}},
	}
}
