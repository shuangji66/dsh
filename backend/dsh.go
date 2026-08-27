package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// DshManager owns the dsh process lifecycle. Started dsh keeps the ports that
// were active at launch-time; changing them requires a full stop first.
type DshManager struct {
	mu          sync.Mutex
	cmd         *exec.Cmd
	startedAt   time.Time
	renv        *RuntimeEnv
	logf        func(string, ...interface{})
	statsMu     sync.Mutex
	lastCpu     float64 // 最近一次采样的 CPU 使用率（%）
	lastMemory  int64   // 最近一次采样的常驻内存（MB）
	lastStatsAt time.Time
}

// clkTCK 为 Linux 的时钟频率（每秒时钟滴答数，通常为 100）。
const clkTCK = 100.0

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

// procCPUTicks reads a process's utime and stime (clock ticks) from /proc/<pid>/stat.
// It locates the comm field by scanning past the last ')' so a name containing
// spaces or parens does not confuse the parse.
func procCPUTicks(pid int) (uint64, uint64, bool) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, 0, false
	}
	s := string(data)
	idx := strings.LastIndexByte(s, ')')
	if idx == -1 || idx+2 > len(s) {
		return 0, 0, false
	}
	fields := strings.Fields(s[idx+2:])
	// After state: fields[0]=state, [11]=utime, [12]=stime
	if len(fields) < 13 {
		return 0, 0, false
	}
	utime, err1 := strconv.ParseUint(fields[11], 10, 64)
	stime, err2 := strconv.ParseUint(fields[12], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return utime, stime, true
}

// procRSSMB reads a process's resident set size (MB) from /proc/<pid>/status.
func procRSSMB(pid int) int64 {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, err := strconv.ParseInt(fields[1], 10, 64)
				if err == nil {
					return kb / 1024
				}
			}
			return 0
		}
	}
	return 0
}

// Stats returns the dsh process CPU usage (%) and resident memory (MB). The
// CPU% is computed from a short sampling window and cached for ~1 second so
// frequent callers (SSE heartbeat / settings poll) do not each block on a sleep.
func (m *DshManager) Stats() (float64, int64) {
	m.statsMu.Lock()
	defer m.statsMu.Unlock()
	if time.Since(m.lastStatsAt) < time.Second {
		return m.lastCpu, m.lastMemory
	}
	if m.cmd == nil || m.cmd.Process == nil {
		m.lastCpu, m.lastMemory, m.lastStatsAt = 0, 0, time.Now()
		return 0, 0
	}
	pid := m.cmd.Process.Pid
	u1, s1, ok1 := procCPUTicks(pid)
	time.Sleep(300 * time.Millisecond)
	u2, s2, ok2 := procCPUTicks(pid)
	if !ok1 || !ok2 {
		m.lastMemory = procRSSMB(pid)
		m.lastStatsAt = time.Now()
		return m.lastCpu, m.lastMemory
	}
	deltaTicks := float64(int64(u2-u1) + int64(s2-s1))
	// 若 delta 为负（进程在采样窗口内被替换 / PID 复用，utime 回退），按 0 处理。
	if deltaTicks < 0 {
		deltaTicks = 0
	}
	// /proc/<pid>/stat 的 utime/stime 聚合了进程所有线程的 CPU 时间，因此
	// 多线程进程可得 > 100%（每核 100%）。除以 CPU 核心数归一到“占整机 CPU
	// 的百分比”，并 clamp 到 [0, 100]，避免概览页显示超过 100% 的使用率。
	numCPU := float64(runtime.NumCPU())
	if numCPU <= 0 {
		numCPU = 1
	}
	cpu := deltaTicks / clkTCK / 0.3 * 100 / numCPU
	if cpu < 0 {
		cpu = 0
	} else if cpu > 100 {
		cpu = 100
	}
	m.lastCpu = cpu
	m.lastMemory = procRSSMB(pid)
	m.lastStatsAt = time.Now()
	return m.lastCpu, m.lastMemory
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
		set("all_proxy=", cfg.ProxyAddr)
		set("ALL_PROXY=", cfg.ProxyAddr)
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
			switch key {
			case "http_proxy", "https_proxy", "HTTP_PROXY", "HTTPS_PROXY",
				"all_proxy", "ALL_PROXY", "NO_PROXY", "no_proxy":
				continue
			}
			out = append(out, e)
		}
		env = out
	}

	set("DSH_WEB_URL=", fmt.Sprintf("http://127.0.0.1:%d", cfg.DshPort))
	// 通过 NODE_OPTIONS 设置 dsh 进程的内存上限（--max-old-space-size）
	if cfg.DshMemLimit > 0 {
		set("NODE_OPTIONS=", "--max-old-space-size="+strconv.Itoa(cfg.DshMemLimit))
	}
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
	// 将 dsh 子进程的 stdout/stderr 接到全局日志输出（stdout + 可选日志文件）
	cmd.Stdout = logOut
	cmd.Stderr = logOut
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
        user = "Harness"
    }

    // 使用 pkill，但设置超时防止卡住
    done := make(chan struct{})
    var pkillErr error
    go func() {
        pkillCmd := exec.Command("pkill", "-TERM", "-u", user, "-x", "MainThread")
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
	cpu, mem := m.Stats()
	return map[string]interface{}{
		"running":    running,
		"pid":        pid,
		"startedAt":  m.startedAt.Format(time.RFC3339),
		"dshPort":    cfg.DshPort,
		"proxyPort":  m.renv.ProxyPort,
		"locked":     running,
		"cpuPercent": cpu,
		"memoryMB":   mem,
	}
}

func (m *DshManager) setStarted(t time.Time) { m.startedAt = t }