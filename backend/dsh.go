package main

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// DshManager owns the dsh process lifecycle. Started dsh keeps the ports that
// were active at launch-time; changing them requires a full stop first.
type DshManager struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	startedAt time.Time
	renv      *RuntimeEnv
	logf      func(string, ...interface{})
}

func NewDshManager(renv *RuntimeEnv) *DshManager {
	return &DshManager{
		renv: renv,
		logf: func(f string, a ...interface{}) {
			logger().Printf(f, a...)
		},
	}
}

// Running reports whether dsh is currently active.
func (m *DshManager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cmd != nil && m.cmd.Process != nil && !m.stopped()
}

func (m *DshManager) stopped() bool {
	if m.cmd == nil || m.cmd.Process == nil {
		return true
	}
	if m.cmd.ProcessState != nil {
		return true
	}
	return false
}

// PID returns the current dsh pid or 0.
func (m *DshManager) PID() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != nil && m.cmd.Process != nil {
		return m.cmd.Process.Pid
	}
	return 0
}

// buildEnv constructs the child environment, capturing PATH/HOME/PNPM_HOME plus
// proxy and dsh-specific variables.
func (m *DshManager) buildEnv() []string {
	cfg := GetConfig()
	env := os.Environ()

	set := func(k, v string) {
		found := false
		for i, e := range env {
			if len(e) > len(k) && e[:len(k)] == k {
				env[i] = k + v
				found = true
				break
			}
			if e == k {
				env[i] = k + v
				found = true
				break
			}
		}
		if !found {
			env = append(env, k+v)
		}
	}

	if m.renv.Path != "" {
		set("PATH=", m.renv.Path)
	}
	if m.renv.Home != "" {
		set("HOME=", m.renv.Home)
	}
	if m.renv.PnpmHome != "" {
		set("PNPM_HOME=", m.renv.PnpmHome)
	}

	if cfg.ProxyEnabled && cfg.ProxyAddr != "" {
		set("http_proxy=", cfg.ProxyAddr)
		set("https_proxy=", cfg.ProxyAddr)
		set("HTTP_PROXY=", cfg.ProxyAddr)
		set("HTTPS_PROXY=", cfg.ProxyAddr)
	} else {
		out := env[:0]
		for _, e := range env {
			key := e
			idx := len(e)
			for i := 0; i < len(e); i++ {
				if e[i] == '=' {
					idx = i
					break
				}
			}
			if idx > len(e) {
				idx = len(e)
			}
			key = e[:idx]
			if key == "http_proxy" || key == "https_proxy" || key == "HTTP_PROXY" || key == "HTTPS_PROXY" || key == "NO_PROXY" || key == "no_proxy" {
				continue
			}
			out = append(out, e)
		}
		env = out
	}

	set("DSH_WEB_URL=", fmt.Sprintf("http://127.0.0.1:%d", cfg.DshPort))
	return env
}

// Start launches `dsh web --no-open --port <port>` in the app server dir.
func (m *DshManager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != nil && m.cmd.Process != nil && !m.stopped() {
		return fmt.Errorf("dsh is already running (pid %d)", m.cmd.Process.Pid)
	}

	cfg := GetConfig()
	bin := m.renv.DshBin
	if bin == "" {
		bin = "dsh"
	}
	cmd := exec.Command(bin, "web", "--no-open", "--port", fmt.Sprintf("%d", cfg.DshPort))
	cmd.Dir = m.renv.DshWorkDir
	cmd.Env = m.buildEnv()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start dsh: %w", err)
	}
	m.cmd = cmd
	m.startedAt = time.Now()
	m.logf("dsh started pid=%d port=%d workdir=%s", cmd.Process.Pid, cfg.DshPort, m.renv.DshWorkDir)
	return nil
}

// Stop terminates the dsh process by killing all "MainThread" processes
// belonging to the current user using pkill, with a fallback to process group kill.
// Stop terminates the dsh process by killing all "MainThread" processes
// belonging to the current user using pkill, with a fallback to process group kill.
func (m *DshManager) Stop() error {
    m.mu.Lock()
    defer m.mu.Unlock()

    if m.cmd == nil || m.cmd.Process == nil {
        return nil
    }

    if m.cmd.ProcessState != nil {
        m.cmd = nil
        return nil
    }

    user := os.Getenv("USER")
    if user == "" {
        user = "root"
    }

    // 使用 pkill，但设置超时防止卡住
    done := make(chan struct{})
    var pkillErr error
    go func() {
        pkillCmd := exec.Command("pkill", "-u", user, "-f", "MainThread")
        pkillErr = pkillCmd.Run()
        close(done)
    }()

    select {
    case <-done:
        if pkillErr != nil {
            m.logf("pkill MainThread failed: %v, falling back to process group kill", pkillErr)
            // 回退到进程组 kill
            m.fallbackKill()
        } else {
            m.logf("pkill MainThread succeeded")
        }
    case <-time.After(3 * time.Second):
        m.logf("pkill MainThread timed out, falling back to process group kill")
        // 超时则使用 fallback
        m.fallbackKill()
    }

    m.cmd = nil
    return nil
}

// fallbackKill 是回退的进程组终止逻辑（原 fallback 部分提取为独立方法）
func (m *DshManager) fallbackKill() {
    pid := m.cmd.Process.Pid
    pgid, err := syscall.Getpgid(pid)
    if err == nil {
        _ = syscall.Kill(-pgid, syscall.SIGTERM)
    } else {
        _ = m.cmd.Process.Signal(syscall.SIGTERM)
    }
    time.Sleep(2 * time.Second)
    if pgid, err := syscall.Getpgid(pid); err == nil {
        _ = syscall.Kill(-pgid, syscall.SIGKILL)
    } else {
        _ = m.cmd.Process.Kill()
    }
    time.Sleep(500 * time.Millisecond)
}

// Status summarizes lifecycle state for the API.
func (m *DshManager) Status() map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	running := m.cmd != nil && m.cmd.Process != nil && !m.stopped()
	pid := 0
	if running {
		pid = m.cmd.Process.Pid
	}
	cfg := GetConfig()
	return map[string]interface{}{
		"running":   running,
		"pid":       pid,
		"startedAt": m.startedAt.Format(time.RFC3339),
		"dshPort":   cfg.DshPort,
		"proxyPort": m.renv.ProxyPort,
		"locked":    running,
	}
}

func (m *DshManager) setStarted(t time.Time) { m.startedAt = t }