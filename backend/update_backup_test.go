package main

// update_backup_test.go —— 「哪些备份要留、哪些收尾即删」的回归测试。
//
// 规则（见 update.go 的 removeUnusedBackup）：**只有 dsh server 的备份要留存** ——
// 概览页有「dsh 服务回滚」，回滚之后还要能再回滚到别的版本；dsh-data-* 是用户主动
// 备份的数据，同样保留。harness 控制台与插件市场都没有回滚入口，安装收尾时必须把
// 备份包删掉，否则只会在 backup/ 里堆积。

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
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

// writeServerFixture 造一个带 package.json 的假 dsh server 目录。
func writeServerFixture(t *testing.T, dir, version string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"@deepseek-ai/dsh","version":"` + version + `"}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newBackupTestManager 造一个只碰临时目录的 UpdateManager。
//
// dsh 被伪装成「看起来已在运行」（进程 pid 记 0）：applyServer 收尾会异步拉起 dsh，
// 测试不该真的去 exec 一个 dsh 进程（那会受宿主机 PATH 影响，可能真起一个进程）。
// 这个伪装是无副作用的：Start 立刻返回 "already running"；万一走到 Stop，pid 0 会让它
// 直接判定「没有可停的 dsh」而返回。PATH 同样指向空目录，避免 `dsh -V`（备份文件名里的
// 版本号）去跑宿主机上真实的那份。
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
	initConfig(&AppConfig{HomeDir: t.TempDir(), DshPort: 65535})
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

// 更新 dsh server 成功后：server- 备份必须留存（概览页回滚要用）。
func TestApplyServerKeepsBackup(t *testing.T) {
	tmp := t.TempDir()
	home := t.TempDir()
	t.Setenv("TRIM_PKGVAR", tmp)

	serverDir := filepath.Join(tmp, "target", "server")
	writeServerFixture(t, serverDir, "1.0.0")
	extractDir := t.TempDir()
	writeServerFixture(t, filepath.Join(extractDir, "server"), "2.0.0")

	prevCfg := GetConfig()
	initConfig(&AppConfig{HomeDir: home, DshPort: 65535})
	prevServerDir, prevBusy, prevStop, prevPortFree := serverDirFn, marketBusyFn, dshStopFn, dshPortFreeFn
	serverDirFn = func(*UpdateManager) string { return serverDir }
	marketBusyFn = func(*UpdateManager) (bool, string) { return false, "" }
	dshStopFn = func(*UpdateManager) error { return nil }
	dshPortFreeFn = func(*UpdateManager, time.Duration) {}
	t.Cleanup(func() {
		initConfig(&prevCfg)
		serverDirFn, marketBusyFn, dshStopFn, dshPortFreeFn = prevServerDir, prevBusy, prevStop, prevPortFree
	})

	m := newBackupTestManager(t, home, filepath.Dir(serverDir))
	if err := m.applyServer(extractDir); err != nil {
		t.Fatalf("applyServer 失败: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(serverDir, "package.json")); err != nil ||
		string(raw) != `{"name":"@deepseek-ai/dsh","version":"2.0.0"}` {
		t.Fatalf("server 目录未替换成新版本: %q err=%v", raw, err)
	}
	names := listBackupNames(t, m, "server-")
	if len(names) != 1 {
		t.Fatalf("server 备份应保留 1 份（回滚要用），实得 %v", names)
	}
	// 留存的备份要能被回滚列表认出来。
	backups, err := m.ListServerBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 || backups[0].Name != names[0] {
		t.Fatalf("回滚列表 = %+v, want [%s]", backups, names[0])
	}
}
