package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"regexp"
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
	tokenMu     sync.RWMutex
	token       string // 新版 dsh 启动时在日志输出的一次性访问 token
	authMu      sync.RWMutex
	authCookie  string // 用 token 换取到的 dsh 会话 cookie（形如 "dsh-auth-xxx=yyy"）
}

// clkTCK 为 Linux 的时钟频率（每秒时钟滴答数，通常为 100）。
const clkTCK = 100.0

// tokenRe 匹配新版 dsh 启动日志中的一次性访问 token，例如：
//
//	dsh web: http://127.0.0.1:3080/?token=EGnsMjoK9i596LEuPZYn-KguZxCD6B7blhdfp2KHotU
var tokenRe = regexp.MustCompile(`[?&]token=([A-Za-z0-9_-]{8,})`)

// tokenScanner 是一个 io.Writer：它把 dsh 子进程的输出原样转发到下游
// （logOut），同时扫描其中的 token，命中后通过回调上报给 DshManager。
type tokenScanner struct {
	dst io.Writer
	mu  sync.Mutex
	buf bytes.Buffer
	cb  func(token string)
}

func (s *tokenScanner) Write(p []byte) (int, error) {
	s.mu.Lock()
	if s.cb != nil {
		s.buf.Write(p)
		// 限制缓冲大小，避免 token 尚未出现时无限增长
		if s.buf.Len() > 8192 {
			excess := s.buf.Len() - 4096
			s.buf.Next(excess)
		}
		if m := tokenRe.FindSubmatch(s.buf.Bytes()); m != nil {
			s.cb(string(m[1]))
			s.cb = nil // 只上报一次
		}
	}
	n, err := s.dst.Write(p)
	s.mu.Unlock()
	return n, err
}

// Token returns the latest one-shot access token captured from the dsh startup
// log, or "" when none has been observed yet (e.g. dsh not started / old dsh).
func (m *DshManager) Token() string {
	m.tokenMu.RLock()
	defer m.tokenMu.RUnlock()
	return m.token
}

// WaitToken blocks until a token is captured from the dsh startup log or the
// timeout elapses. It returns the token (possibly empty on timeout). Old dsh
// versions that never print a token cause this to wait out the full timeout
// (or return early once dsh has exited) and return "".
func (m *DshManager) WaitToken(timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if tok := m.Token(); tok != "" {
			return tok
		}
		// 若 dsh 进程已退出，不再等待。
		if !m.Running() {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return m.Token()
}

// AuthCookie returns the dsh session cookie (e.g. "dsh-auth-xxx=yyy") that the
// proxy carries when forwarding to dsh. Empty until ExchangeToken succeeds or
// for old dsh versions that don't use a token/cookie flow.
func (m *DshManager) AuthCookie() string {
	m.authMu.RLock()
	defer m.authMu.RUnlock()
	return m.authCookie
}

func (m *DshManager) setAuthCookie(ck string) {
	m.authMu.Lock()
	m.authCookie = ck
	m.authMu.Unlock()
}

// ExchangeToken 用启动日志中捕获的一次性 token 访问一次带 token 的 dsh 地址
// （http://127.0.0.1:<dshPort>/?token=XXX），从响应头的 Set-Cookie 中提取
// dsh 会话 cookie（dsh-auth-*）并保存。此后反代访问 dsh 时携带该 cookie、
// 访问不带 token 的地址即可。旧版 dsh（无 token）时此方法直接返回。
func (m *DshManager) ExchangeToken() error {
	tok := m.Token()
	if tok == "" {
		return nil
	}
	port := GetConfig().DshPort
	target := fmt.Sprintf("http://127.0.0.1:%d/?token=%s", port, url.QueryEscape(tok))
	// 用 CookieJar 自动收集访问链路中所有 Set-Cookie（含重定向响应），
	// 确保能取到 dsh 下发的认证 cookie（dsh-auth-*）。
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		Jar:     jar,
	}
	resp, err := client.Get(target)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	cookies := jar.Cookies(&url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", port)})
	for _, c := range cookies {
		if strings.HasPrefix(c.Name, "dsh-auth-") && c.Value != "" {
			m.setAuthCookie(c.Name + "=" + c.Value)
			m.logf("dsh auth cookie captured: %s", c.Name)
			return nil
		}
	}
	return nil
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
	// dsh 可执行文件统一按 PATH 解析（node_modules/.bin/dsh），不再用额外覆盖。
	bin := "dsh"
	// 每次启动都重置 token 与会话 cookie，避免复用上一次启动的旧凭据。
	m.tokenMu.Lock()
	m.token = ""
	m.tokenMu.Unlock()
	m.setAuthCookie("")

	cmd := exec.Command(bin, "web", "--no-open", "--port", fmt.Sprintf("%d", cfg.DshPort))
	cmd.Dir = m.renv.TRIMAppDest
	cmd.Env = m.buildEnv()
	// 拦截 dsh 子进程的 stdout/stderr：既照常写到全局日志，又扫描其中的
	// 一次性访问 token（新版 dsh 启动时会打印 "dsh web: http://...?token=XXX"）。
	scanner := &tokenScanner{
		dst: logOut,
		cb: func(tok string) {
			m.tokenMu.Lock()
			m.token = tok
			m.tokenMu.Unlock()
			m.logf("dsh access token captured: %s", tok)
		},
	}
	cmd.Stdout = scanner
	cmd.Stderr = scanner
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start dsh: %w", err)
	}
	m.cmd = cmd
	m.startedAt = time.Now()
	m.logf("dsh started pid=%d port=%d", cmd.Process.Pid, cfg.DshPort)
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

    // 停止后清空访问 token 与会话 cookie，避免把已失效的旧凭据继续用于反代转发。
    m.tokenMu.Lock()
    m.token = ""
    m.tokenMu.Unlock()
    m.setAuthCookie("")

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

// PluginInfo 描述一个 dsh 插件依赖条目。
type PluginInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Resolved string `json:"resolved"`
}

// runPluginCmd 以 dsh 的运行环境执行 `dsh plugin --profile web <args...>`，
// 返回合并后的 stdout/stderr 输出。
func (m *DshManager) runPluginCmd(args ...string) (string, error) {
	// dsh 可执行文件统一按 PATH 解析（node_modules/.bin/dsh）。
	bin := "dsh"
	cmdArgs := append([]string{"plugin", "--profile", "web"}, args...)
	cmd := exec.Command(bin, cmdArgs...)
	cmd.Env = m.buildEnv()
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return strings.TrimSpace(out.String()), err
	}
	return strings.TrimSpace(out.String()), nil
}

// parsePluginList 解析 `dsh plugin --profile web list` 的输出，返回
// "dependencies:" 区块下的插件列表（插件名、版本），跳过 node-pty。
// 实际输出形如：
//
//	Legend: production dependency, optional only, dev only
//	dsh-profile-web /vol1/@appshare/... (PRIVATE)
//	│
//	│ dependencies:
//	├── dsh-mobile-hanui@0.2.5
//	├── dsh-vision-router@2.0.1
//	└── node-pty@1.1.0
//	3 packages
func parsePluginList(output string) []PluginInfo {
	var plugins []PluginInfo
	lines := strings.Split(output, "\n")
	inDeps := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "dependencies:") {
			inDeps = true
			continue
		}
		if !inDeps {
			continue
		}
		if trimmed == "" {
			continue
		}
		// 汇总行，如 "(3 packages)" 或 "3 packages"
		if strings.HasSuffix(trimmed, "packages)") || strings.HasSuffix(trimmed, " packages") {
			break
		}
		// 去除树形前缀（├── └── │ 等），取实际条目内容
		entry := stripTreePrefix(trimmed)
		if entry == "" {
			continue
		}
		// 用最后一个 @ 分割 name 和 version（scoped 包名可能含 @）
		at := strings.LastIndex(entry, "@")
		if at <= 0 {
			continue
		}
		name := entry[:at]
		ver := entry[at+1:]
		if name == "node-pty" {
			continue
		}
		plugins = append(plugins, PluginInfo{Name: name, Version: ver})
	}
	return plugins
}

// stripTreePrefix 移除行首的树形符号（空格、├、└、│、─ 等），返回实际内容。
func stripTreePrefix(s string) string {
	for {
		t := strings.TrimLeft(s, " ├└│─")
		if t == s {
			break
		}
		s = t
	}
	return strings.TrimSpace(s)
}