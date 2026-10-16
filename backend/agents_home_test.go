package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// envValue 从 buildEnv 返回的 "k=v" 列表里取某个环境变量的值（key 精确匹配，
// 不会把 DSH_AGENTS_HOME 当成 HOME）。
func envValue(env []string, name string) (string, bool) {
	prefix := name + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return e[len(prefix):], true
		}
	}
	return "", false
}

// dsh 进程必须拿到 DSH_AGENTS_HOME = $HOME/.agents（与下发的 HOME 同一个主目录），
// 而不是让 dsh 自己用 os.homedir() 兜底 —— 后者在 HOME 缺失时会落到 /etc/passwd
// 里的主目录，跑到平台共享目录之外。
func TestBuildEnvSetsAgentsHome(t *testing.T) {
	home := t.TempDir()
	m := newTestDshManager(home, "")

	env := m.buildEnv()
	gotHome, ok := envValue(env, "HOME")
	if !ok || gotHome != home {
		t.Fatalf("HOME = %q (存在=%v), want %q", gotHome, ok, home)
	}
	got, ok := envValue(env, "DSH_AGENTS_HOME")
	if !ok {
		t.Fatalf("buildEnv 未下发 DSH_AGENTS_HOME")
	}
	if want := filepath.Join(home, ".agents"); got != want {
		t.Fatalf("DSH_AGENTS_HOME = %q, want %q", got, want)
	}
}

// 共享 agent 配置根随主目录走：换一个主目录，下发的值跟着换（不是写死的路径）。
func TestBuildEnvAgentsHomeFollowsHome(t *testing.T) {
	for _, home := range []string{t.TempDir(), t.TempDir()} {
		m := newTestDshManager(home, "")
		got, ok := envValue(m.buildEnv(), "DSH_AGENTS_HOME")
		if !ok || got != filepath.Join(home, ".agents") {
			t.Fatalf("home=%q 时 DSH_AGENTS_HOME = %q (存在=%v)", home, got, ok)
		}
	}
}

// 没有主目录（开发环境没给 HOME）时既不下发 DSH_AGENTS_HOME，也不去创建任何东西。
func TestAgentsHomeWithoutHome(t *testing.T) {
	if dir := agentsHomeDir(""); dir != "" {
		t.Fatalf("agentsHomeDir(\"\") = %q, want 空串", dir)
	}
	out, _ := captureLogs(t)
	m := &DshManager{renv: &RuntimeEnv{}, logf: func(logLevel, string, ...interface{}) {}}
	ensureAgentsHome("")
	if got := out.String(); got != "" {
		t.Fatalf("无主目录时不应输出日志: %q", got)
	}
	// 不传主目录时不应下发（只在 HOME 非空的那条分支里设置）。
	if v, ok := envValue(m.buildEnv(), "DSH_AGENTS_HOME"); ok && v != "" {
		t.Fatalf("无主目录时下发了 DSH_AGENTS_HOME=%q", v)
	}
}

// 控制台启动时的「检测并创建」：目录不存在就建出来，已存在时是空操作；
// 两种情况都**不输出日志**（基础成功操作不记日志，见 AGENTS 规则 7）。
func TestEnsureAgentsHomeCreatesDirQuietly(t *testing.T) {
	out, _ := captureLogs(t)
	home := t.TempDir()
	want := filepath.Join(home, ".agents")

	ensureAgentsHome(home)
	fi, err := os.Stat(want)
	if err != nil {
		t.Fatalf("未创建 %s: %v", want, err)
	}
	if !fi.IsDir() {
		t.Fatalf("%s 不是目录", want)
	}
	if got := out.String(); got != "" {
		t.Fatalf("创建成功不应输出日志: %q", got)
	}

	// 已存在时幂等，且同样安静。
	ensureAgentsHome(home)
	if fi2, err := os.Stat(want); err != nil || !fi2.IsDir() {
		t.Fatalf("二次调用破坏了 %s: %v", want, err)
	}
	if got := out.String(); got != "" {
		t.Fatalf("目录已存在时不应输出日志: %q", got)
	}
}

// 建不出来时只记一条英文 ERROR（不 panic、不拦住启动）。
func TestEnsureAgentsHomeLogsFailure(t *testing.T) {
	out, _ := captureLogs(t)
	// 主目录位置上放一个普通文件：$HOME/.agents 必然 ENOTDIR。
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("准备测试文件失败: %v", err)
	}
	ensureAgentsHome(file)
	got := out.String()
	if !strings.Contains(got, "[ERROR]") || !strings.Contains(got, "agents home") {
		t.Fatalf("创建失败应记一条 ERROR: %q", got)
	}
}
