package main

// update_backup_test.go —— 「哪些备份要留、哪些收尾即删」的回归测试。
//
// 规则（见 update.go 的 removeUnusedBackup）：**只有用户主动的 dsh-data-* 要留存**。
// harness 控制台、dsh 服务版本、插件市场都没有「回滚/回退」入口（换版本 = 换一个已
// 安装的版本目录，见 server.go），因此这些路径产生的备份包收尾时必须删掉，否则只会
// 在 backup/ 里堆积。

import (
	"os"
	"os/exec"
	"path/filepath"
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

// 更新 harness 成功后：新二进制就位，且 backup/ 里不留 harness- 备份包。
func TestApplyHarnessLeavesNoBackup(t *testing.T) {
	t.Setenv("TRIM_PKGVAR", t.TempDir())

	appDest := t.TempDir()
	binDir := filepath.Join(appDest, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "harness"), []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	extractDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(extractDir, "harness"), []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	prevCfg := GetConfig()
	// DshPort 取一个不会出现在任何进程 argv 里的端口：applyHarness 会调 dsh.Stop()，
	// 而 Stop 在「没有受管进程」时按 `--port <DshPort>` 扫 /proc 找 dsh。
	initConfig(&AppConfig{DshPort: 65535})
	prevBinDir := harnessBinDirFn
	harnessBinDirFn = func(*UpdateManager) string { return binDir }
	t.Cleanup(func() {
		initConfig(&prevCfg)
		harnessBinDirFn = prevBinDir
	})

	m := newBackupTestManager(t, t.TempDir(), appDest)
	got, err := m.applyHarness(extractDir)
	if err != nil {
		t.Fatalf("applyHarness 失败: %v", err)
	}
	if want := filepath.Join(binDir, "harness"); got != want {
		t.Fatalf("返回的新二进制路径 = %q, want %q", got, want)
	}
	if raw, err := os.ReadFile(got); err != nil || string(raw) != "new-binary" {
		t.Fatalf("替换后的二进制内容 = %q err=%v, want new-binary", raw, err)
	}
	if names := listBackupNames(t, m, "harness-"); len(names) != 0 {
		t.Fatalf("harness 备份应当被删除（没有回滚入口），实得 %v", names)
	}
}
