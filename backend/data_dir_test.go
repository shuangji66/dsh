package main

// data_dir_test.go —— 统一数据目录（HARNESS_DATA_DIR，默认 $TRIM_PKGVAR）的契约。
//
// 后端自己产生的文件（日志 / PID / 配置 / 会话密钥 / 终端会话镜像 / 更新备份与待安装
// 包）全部由这一个目录派生；单独设置某个文件路径的环境变量已移除。这组测试把这两条
// 锁死：改回「各读各的环境变量」或让某条路径漂出数据目录都会红。

import (
	"path/filepath"
	"testing"
)

// setLegacyPathEnvs 把已移除的「单独路径」环境变量全部指向独立临时目录：它们必须
// 完全不被读取（否则派生出的路径会落到这些目录里）。
func setLegacyPathEnvs(t *testing.T) {
	t.Helper()
	other := t.TempDir()
	for _, k := range []string{
		"HARNESS_CONFIG_FILE",
		"HARNESS_LOG_FILE",
		"HARNESS_PID_FILE",
		"HARNESS_DSH_PID_FILE",
		"HARNESS_QUICK_CMDS_FILE",
		"HARNESS_SESSION_DIR",
		"HARNESS_SESSION_KEY_FILE",
	} {
		t.Setenv(k, filepath.Join(other, k))
	}
}

func TestRuntimeEnvDerivesPathsFromDataDir(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("HARNESS_DATA_DIR", dataDir)
	t.Setenv("TRIM_PKGVAR", filepath.Join(t.TempDir(), "pkgvar")) // 必须被 HARNESS_DATA_DIR 盖过
	setLegacyPathEnvs(t)

	renv := loadRuntimeEnv()
	if renv.DataDir != dataDir {
		t.Fatalf("DataDir = %q, want %q", renv.DataDir, dataDir)
	}
	got := map[string]string{
		"ConfigFile":    renv.ConfigFile,
		"LogFile":       renv.LogFile,
		"PidFile":       renv.PidFile,
		"DshPidFile":    renv.DshPidFile,
		"QuickCmdsFile": renv.QuickCmdsFile,
		"SessionDir":    renv.SessionDir,
	}
	want := map[string]string{
		"ConfigFile":    filepath.Join(dataDir, "config.json"),
		"LogFile":       filepath.Join(dataDir, "harness.log"),
		"PidFile":       filepath.Join(dataDir, "harness.pid"),
		"DshPidFile":    filepath.Join(dataDir, "dsh.pid"),
		"QuickCmdsFile": filepath.Join(dataDir, "quickcmds.json"),
		"SessionDir":    filepath.Join(dataDir, "terminal-sessions"),
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}

	// 不走 RuntimeEnv 的三处解析（会话密钥、更新备份、待安装包）同样落在数据目录下。
	if p := sessionKeyFileFn(); p != filepath.Join(dataDir, "session.key") {
		t.Errorf("sessionKeyFileFn() = %q, want %q", p, filepath.Join(dataDir, "session.key"))
	}
	upd := &UpdateManager{renv: &renv}
	if p := upd.backupDir(); p != filepath.Join(dataDir, "backup") {
		t.Errorf("backupDir() = %q, want %q", p, filepath.Join(dataDir, "backup"))
	}
	if p := upd.pendingDir(); p != filepath.Join(dataDir, "backup", "pending") {
		t.Errorf("pendingDir() = %q, want %q", p, filepath.Join(dataDir, "backup", "pending"))
	}
}

// 没有 HARNESS_DATA_DIR 时默认取平台变量 TRIM_PKGVAR。
func TestDataDirDefaultsToTRIMPKGVAR(t *testing.T) {
	pkgvar := t.TempDir()
	t.Setenv("HARNESS_DATA_DIR", "")
	t.Setenv("TRIM_PKGVAR", pkgvar)

	renv := loadRuntimeEnv()
	if renv.DataDir != pkgvar {
		t.Fatalf("DataDir = %q, want %q", renv.DataDir, pkgvar)
	}
	if want := filepath.Join(pkgvar, "config.json"); renv.ConfigFile != want {
		t.Errorf("ConfigFile = %q, want %q", renv.ConfigFile, want)
	}
	if want := filepath.Join(pkgvar, "harness.log"); renv.LogFile != want {
		t.Errorf("LogFile = %q, want %q", renv.LogFile, want)
	}
}

// 「拿不到 RuntimeEnv」的兜底解析（单测构造的空 RuntimeEnv / 包级默认值）也必须走
// 同一个数据目录，不能各自退回 TRIM_PKGVAR 之外的路径。
func TestDataPathFallbacksFollowDataDir(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("HARNESS_DATA_DIR", dataDir)
	setLegacyPathEnvs(t)

	if p := quickCmdsPath(nil); p != filepath.Join(dataDir, "quickcmds.json") {
		t.Errorf("quickCmdsPath(nil) = %q", p)
	}
	if p := quickCmdsPath(&RuntimeEnv{}); p != filepath.Join(dataDir, "quickcmds.json") {
		t.Errorf("quickCmdsPath(空 renv) = %q", p)
	}
	if p := (&SessionManager{}).sessionDir(); p != filepath.Join(dataDir, "terminal-sessions") {
		t.Errorf("sessionDir(空 renv) = %q", p)
	}
	// 空 RuntimeEnv 的 UpdateManager（部分单测直接构造字面量）同理。
	if p := (&UpdateManager{}).backupDir(); p != filepath.Join(dataDir, "backup") {
		t.Errorf("backupDir(空 renv) = %q", p)
	}
}
