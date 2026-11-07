package main

import (
	"strings"
	"testing"
)

// dsh 进程的 SHELL 必须固定成 /bin/bash：平台以服务账号启动控制台时
// SHELL=/usr/sbin/nologin，dsh 的内置终端会按 process.env.SHELL 解析默认
// shell（dsh-subprocess-local 的 terminalEnvironment），结果跑成 nologin。
func TestBuildEnvPinsShell(t *testing.T) {
	t.Setenv("SHELL", "/usr/sbin/nologin")

	m := newTestDshManager(t.TempDir(), "")
	env := m.buildEnv()

	got, ok := envValue(env, "SHELL")
	if !ok {
		t.Fatalf("buildEnv 未下发 SHELL")
	}
	if got != "/bin/bash" {
		t.Fatalf("SHELL = %q, want /bin/bash", got)
	}
	// set() 是原位替换：父进程已经带着 SHELL 时不能追加出第二个同名项。
	count := 0
	for _, e := range env {
		if strings.HasPrefix(e, "SHELL=") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("SHELL 出现 %d 次, want 1 次", count)
	}
}

// 父进程没有 SHELL（开发环境直接跑控制台）时同样补上，dsh 不靠
// /etc/passwd 里的登录 shell 兜底。
func TestBuildEnvPinsShellWhenUnset(t *testing.T) {
	t.Setenv("SHELL", "")

	m := newTestDshManager(t.TempDir(), "")
	if got, ok := envValue(m.buildEnv(), "SHELL"); !ok || got != "/bin/bash" {
		t.Fatalf("SHELL = %q (存在=%v), want /bin/bash", got, ok)
	}
}
