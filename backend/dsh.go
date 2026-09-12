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
	// livePidMu 保护对 livePid 的并发读写。
	livePidMu sync.Mutex
	livePid   int // 实际在运行的 dsh 进程 pid。dsh 装插件自重启后 m.cmd 的 pid 会失效，
	// 此时用它记录在 /proc 中重新发现的实时 dsh pid（0 表示未知/未发现）。
	pidCheckedAt time.Time // 上次扫描 /proc 发现 dsh pid 的时间（避免频繁扫描）
	// dshPidFile 是 HARNESS_DSH_PID_FILE 指定的 dsh 服务 PID 文件路径（空表示不
	// 维护）。它记录 dsh 进程的实时 PID（含 dsh-market 自重启后的新 PID），dsh
	// 启动/重新发现时写入、dsh 停止或自重启窗口移除；与记录 harness 控制台自身
	// PID 的 HARNESS_PID_FILE 完全分离，互不影响。
	dshPidFile    string
	dshPidFilePid int // 最近一次写入 dsh PID 文件的 PID，用于避免重复写入（0 表示未写过）
	pidMu         sync.Mutex
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
// log, or "" when none has been observed yet (e.g. dsh not started).
func (m *DshManager) Token() string {
	m.tokenMu.RLock()
	defer m.tokenMu.RUnlock()
	return m.token
}

// WaitToken blocks until a token is captured from the dsh startup log or the
// timeout elapses. It returns the token (possibly empty on timeout or if dsh
// has exited).
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
// proxy carries when forwarding to dsh. Empty until ExchangeToken succeeds.
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
// 访问不带 token 的地址即可。
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
			return nil
		}
	}
	return nil
}

func NewDshManager(renv *RuntimeEnv) *DshManager {
	return &DshManager{
		renv:       renv,
		dshPidFile: renv.DshPidFile,
		logf: func(f string, a ...interface{}) {
			logger().Printf(f, a...)
		},
	}
}

// effectiveHome returns the HOME directory that dsh should run with. When the
// user has switched home directories via the resource page the configured value
// (AppConfig.HomeDir) wins; otherwise it falls back to the launch-time default
// HOME (the real path behind /var/apps/Harness/shares/Harness).
func (m *DshManager) effectiveHome() string {
	if h := GetConfig().HomeDir; h != "" {
		return h
	}
	return m.renv.Home
}

// Running reports whether dsh is currently active.
func (m *DshManager) Running() bool {
	m.mu.Lock()
	trackedAlive := m.cmd != nil && m.cmd.Process != nil && !m.stopped()
	m.mu.Unlock()
	if trackedAlive {
		return true
	}
	// dsh 装插件自重启：m.cmd 里的旧进程已退出，但在 /proc 中可能还有新的 dsh
	// 进程在运行，此时仍视为 running（避免概览页误判为已停止、CPU/内存读不到）。
	return m.effectivePID() > 0
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
	return m.effectivePID()
}

// isDshEntry 判断某参数的文件名（basename）是否为 dsh CLI 入口。匹配范围与
// dsh-market 的 dshArgv() 保持一致（bin.js / bin.ts / dsh），并兼容历史形态
// dsh.js / dsh.ts：dsh 被 detached helper 重新拉起后的 argv 形如
// `node <...>/bin.js web --no-open --port <port>`，入口文件名不在匹配集合内
// 就无法重新发现新 PID，导致概览 CPU/内存监控失效。
func isDshEntry(base string) bool {
	switch base {
	case "dsh", "dsh.js", "dsh.ts", "bin.js", "bin.ts":
		return true
	}
	return false
}

// findDshPid 在 /proc 中扫描当前在运行的 dsh 进程 pid。dsh 装插件自重启后，
// 新进程由 dsh 自己拉起，PID 会变化，此时 m.cmd 记录的旧 PID 已失效，需要
// 通过匹配命令行（`dsh web --no-open --port <port>`）重新发现。返回 0 表示未找到。
// 注意：僵尸进程（/proc/<pid>/cmdline 为空）不会命中。
func (m *DshManager) findDshPid() int {
	port := strconv.Itoa(GetConfig().DshPort)
	best := 0
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pidStr := e.Name()
		if pidStr[0] < '0' || pidStr[0] > '9' {
			continue
		}
		pid, err := strconv.Atoi(pidStr)
		if err != nil || pid <= 0 {
			continue
		}
		// 跳过自身（harness 是 Go 二进制，不会是 dsh，但排除自身更稳妥）。
		data, err := os.ReadFile("/proc/" + pidStr + "/cmdline")
		if err != nil {
			continue
		}
		args := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
		if len(args) < 2 {
			continue
		}
		hasDsh := false
		hasWeb := false
		hasPort := false
		for _, a := range args {
			base := a
			if i := strings.LastIndexByte(base, '/'); i >= 0 {
				base = base[i+1:]
			}
			if isDshEntry(base) {
				hasDsh = true
			}
			if a == "web" {
				hasWeb = true
			}
			// 兼容 `--port <port>` 与 `--port=<port>` 两种写法。
			if a == port || (strings.HasPrefix(a, "--port=") && strings.TrimPrefix(a, "--port=") == port) {
				hasPort = true
			}
		}
		if hasDsh && hasWeb && (hasPort) {
			// 取最大 pid，通常是最新启动的 dsh 实例。
			if pid > best {
				best = pid
			}
		}
	}
	return best
}

// processAlive reports whether /proc/<pid> still exists and the process is not
// a zombie. 判断“进程是否存活”不能只看 /proc/<pid> 目录是否存在：dsh 是 harness
// 的直接子进程，退出后若从未 Wait() 回收会长期保持僵尸态（/proc 目录仍在），
// 而僵尸进程的 cmdline 为空、CPU 时间与 RSS 均为 0。若把僵尸当作存活，
// effectivePID 会一直返回旧 PID，导致 dsh-market 自重启后概览 CPU/内存监控
// 读到 0。这里通过 /proc/<pid>/stat 的状态字段（'Z' 僵尸 / 'X' 死）识别并判死。
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	s := string(data)
	// 定位 comm 结束的 ')'（comm 可能含空格/括号），其后紧跟状态字段。
	idx := strings.LastIndexByte(s, ')')
	if idx == -1 || idx+2 >= len(s) {
		return true // 解析不出状态时按存活处理（保守）
	}
	state := s[idx+2]
	return state != 'Z' && state != 'X'
}

// effectivePID returns the dsh pid that should be used for stats/lifecycle.
// 它优先取 m.cmd 记录的 PID；若该 PID 已失效（如 dsh 装插件自重启），则尝试在
// /proc 中重新发现实时 dsh 进程（结果缓存在 livePid，避免频繁扫描 /proc）。
// 注意：这里只做只读发现，不修改 m.cmd——Stop/Start 仍以 m.cmd 为准。
func (m *DshManager) effectivePID() int {
	m.mu.Lock()
	tracked := 0
	var trackedCmd *exec.Cmd
	if m.cmd != nil && m.cmd.Process != nil {
		tracked = m.cmd.Process.Pid
		trackedCmd = m.cmd
	}
	m.mu.Unlock()

	m.livePidMu.Lock()
	defer m.livePidMu.Unlock()
	// 每 2 秒最多重新扫描一次 /proc，避免高频调用拖慢接口。
	if m.livePid > 0 && time.Since(m.pidCheckedAt) < 2*time.Second && processAlive(m.livePid) {
		return m.livePid
	}
	if tracked > 0 && processAlive(tracked) {
		m.livePid = tracked
		m.pidCheckedAt = time.Now()
		return tracked
	}
	// 被跟踪的 PID 已失效，重新发现实时 dsh 进程。
	found := m.findDshPid()
	m.livePid = found
	m.pidCheckedAt = time.Now()
	if found > 0 && found != tracked {
		m.logf("dsh pid changed: tracked=%d -> live=%d", tracked, found)
		// 旧受管进程（dsh-market 自重启前的）已退出：异步回收，避免残留僵尸。
		// 即使反代 hook 未触发（如自发重启），这里也能兜底回收。ProcessState
		// 守卫保证每个 cmd 只发起一次 Wait。
		if trackedCmd != nil && trackedCmd.ProcessState == nil {
			cp := trackedCmd
			go m.reapCmd(cp)
		}
	}
	if found > 0 {
		// 刷新 dsh 服务 PID 文件：dsh 自重启（dsh-market）后新 PID 不在 m.cmd 中，
		// 只有这里重新发现时才有机会把 dsh PID 文件更新为实时 PID。
		m.writeDshPidFile(found)
	}
	return found
}

// writeDshPidFile 把 dsh 实时 PID 写入 HARNESS_DSH_PID_FILE（仅在路径配置且
// PID 与上次写入不同时执行，避免高频调用反复写文件）。dsh 启动、自重启后重新
// 发现新 PID 时调用；文件只记录 dsh 服务进程的 PID，与 harness 控制台的
// HARNESS_PID_FILE 相互独立。
func (m *DshManager) writeDshPidFile(pid int) {
	m.pidMu.Lock()
	defer m.pidMu.Unlock()
	if m.dshPidFile == "" || pid <= 0 || pid == m.dshPidFilePid {
		return
	}
	if err := writePidFile(m.dshPidFile, pid); err != nil {
		m.logf("failed to write dsh pid file %s: %v", m.dshPidFile, err)
		return
	}
	m.dshPidFilePid = pid
	m.logf("dsh pid file %s updated -> %d", m.dshPidFile, pid)
}

// removeDshPidFile 移除 dsh 服务 PID 文件（dsh 停止或自重启窗口内）：
// dsh 进程不存在时文件不应残留旧 PID，空路径为无操作。
func (m *DshManager) removeDshPidFile() {
	m.pidMu.Lock()
	defer m.pidMu.Unlock()
	if m.dshPidFile == "" {
		return
	}
	m.dshPidFilePid = 0
	if err := os.Remove(m.dshPidFile); err != nil && !os.IsNotExist(err) {
		m.logf("failed to remove dsh pid file %s: %v", m.dshPidFile, err)
	}
}

// reapCmd 回收已退出的受管 dsh 子进程（调用 Wait），避免其残留为僵尸进程。
// 僵尸/已退出进程的 Wait 会立即返回；进程仍存活（如 Stop 后尚在优雅退出）时
// 不阻塞调用方——由后台 goroutine 内的 Wait 在进程退出后完成回收。ProcessState
// 守卫保证同一个 cmd 最多发起一次 Wait，重复调用为无操作。
func (m *DshManager) reapCmd(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil || cmd.ProcessState != nil {
		return
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
		m.logf("reaped dsh child process (was pid %d)", cmd.Process.Pid)
	case <-time.After(3 * time.Second):
		m.logf("reap of dsh child pid %d deferred (process still shutting down)", cmd.Process.Pid)
	}
}

// notifySelfRestart 由反向代理在检测到 dsh 市场（dsh-market）的一键自重启请求
// （POST /dsh-market/restart 返回 200）时调用。此时 dsh 会自行拉起重启（detached
// helper），新进程不再是本控制台的直接子进程：立即作废缓存的 PID 与 CPU/内存
// 采样，把 PID 文件回退到控制台自身（保证文件中 PID 存活），并启动后台 watcher
// 重新发现新 dsh PID、刷新 PID 文件与概览监控。
func (m *DshManager) notifySelfRestart() {
	m.mu.Lock()
	tracked := 0
	if m.cmd != nil && m.cmd.Process != nil {
		tracked = m.cmd.Process.Pid
	}
	m.mu.Unlock()

	// 作废 livePid 缓存与 CPU/内存采样缓存，迫使下一次 Status/Stats 重新发现，
	// 避免概览页在重启窗口内继续展示旧 PID 的 0 CPU / 0 内存。
	m.livePidMu.Lock()
	m.livePid = 0
	m.pidCheckedAt = time.Now()
	m.livePidMu.Unlock()
	m.statsMu.Lock()
	m.lastStatsAt = time.Time{}
	m.lastCpu, m.lastMemory = 0, 0
	m.statsMu.Unlock()

	// 自重启窗口内先移除 dsh PID 文件（旧进程即将退出，文件中不应残留旧 PID），
	// 新 dsh PID 由 watchSelfRestart / effectivePID 发现后再写回。
	m.removeDshPidFile()
	m.logf("dsh self-restart requested (old pid=%d), watching for the replacement", tracked)
	go m.watchSelfRestart(tracked)
}

// watchSelfRestart 等待 dsh 市场自重启完成并恢复监控：先等旧进程退出（含僵尸态
// ——见 processAlive），再轮询 /proc 发现新 dsh PID，成功后刷新 livePid、dsh PID
// 文件与 startedAt。任一阶段超时即放弃（如重启被 dsh-market 拒绝、新进程未能拉起）。
func (m *DshManager) watchSelfRestart(oldPID int) {
	// 阶段一：等待旧进程退出。dsh-market 在返回 HTTP 响应后约 0.5s 才对自己
	// SIGTERM，期间旧进程仍存活；若一直存活（重启被拒绝），无需继续等待。
	waitDead := time.Now().Add(30 * time.Second)
	for {
		if !processAlive(oldPID) {
			break
		}
		if time.Now().After(waitDead) {
			m.logf("dsh self-restart: tracked pid %d still alive after 30s, restart likely rejected", oldPID)
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	// 旧进程已退出（僵尸态）：回收 m.cmd 中对应的受管子进程（Wait 立即返回），
	// 清除僵尸。仅当 m.cmd 仍指向旧 PID 时回收，避免误回收期间被替换的新 cmd。
	if oldPID > 0 {
		m.mu.Lock()
		mc := m.cmd
		m.mu.Unlock()
		if mc != nil && mc.Process != nil && mc.Process.Pid == oldPID {
			m.reapCmd(mc)
		}
	}
	// 阶段二：轮询 /proc 等待新 dsh 进程出现（端口释放 + 拉起通常 1~3 秒）。
	deadline := time.Now().Add(90 * time.Second)
	for {
		if found := m.findDshPid(); found > 0 {
			m.livePidMu.Lock()
			m.livePid = found
			m.pidCheckedAt = time.Now()
			m.livePidMu.Unlock()
			m.writeDshPidFile(found)
			// startedAt 是 harness 启动 dsh 的时间，自重启后已失真；从
			// /proc 读新进程的真实启动时间，让状态接口返回准确的启动时刻。
			if start, ok := procStartTime(found); ok {
				m.mu.Lock()
				m.startedAt = start
				m.mu.Unlock()
			}
			m.logf("dsh self-restart detected: tracked=%d -> live=%d", oldPID, found)
			return
		}
		if time.Now().After(deadline) {
			m.logf("dsh self-restart: no replacement found within 90s (old pid=%d)", oldPID)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// procStartTime 从 /proc/<pid>/stat 的 starttime（字段 22，单位为时钟滴答）结合
// /proc/stat 的 btime（系统启动时刻）换算进程的真实启动时间。
func procStartTime(pid int) (time.Time, bool) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return time.Time{}, false
	}
	s := string(data)
	idx := strings.LastIndexByte(s, ')')
	if idx == -1 || idx+2 > len(s) {
		return time.Time{}, false
	}
	fields := strings.Fields(s[idx+2:])
	// 状态字段之后按 /proc/[pid]/stat 的字段号偏移：fields[0]=state(3)、
	// fields[1]=ppid(4)…… fields[19]=starttime(22)。
	if len(fields) < 20 {
		return time.Time{}, false
	}
	startTicks, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	boot, err := bootTime()
	if err != nil {
		return time.Time{}, false
	}
	secs := float64(startTicks) / clkTCK
	return boot.Add(time.Duration(secs * float64(time.Second))), true
}

// bootTime 从 /proc/stat 读取系统启动时刻（btime, Unix 秒），用于换算进程启动时间。
func bootTime() (time.Time, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "btime ") {
			sec, err := strconv.ParseInt(strings.TrimSpace(line[len("btime "):]), 10, 64)
			if err != nil {
				return time.Time{}, err
			}
			return time.Unix(sec, 0), nil
		}
	}
	return time.Time{}, fmt.Errorf("btime not found in /proc/stat")
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
	// 优先用 m.cmd 记录的 PID；若已失效（dsh 装插件自重启），自动在 /proc 中
	// 重新发现实时 dsh 进程，避免因 PID 变化导致概览页 CPU/内存读不到。
	pid := m.effectivePID()
	if pid <= 0 {
		m.lastCpu, m.lastMemory, m.lastStatsAt = 0, 0, time.Now()
		return 0, 0
	}
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
		pathVal := m.renv.Path
		// 根据配置的 node 版本，把对应版本的 bin 目录前置到 PATH。
		// node24（默认）使用系统默认 node，不额外前置；node26 在可用时前置其 bin。
		if prefix := nodeVersionBinPrefix(cfg.NodeVersion); prefix != "" {
			pathVal = prefix + ":" + pathVal
		}
		set("PATH=", pathVal)
	}
	// HOME 使用“当前主目录”（可能已在资源页被切换为某个已授权目录的实际路径），
	// 默认为主机启动时的 HOME（/var/apps/Harness/shares/Harness 的实际路径）。
	if h := m.effectiveHome(); h != "" {
		set("HOME=", h)
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
		set("NODE_USE_ENV_PROXY=", "1")
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
				"all_proxy", "ALL_PROXY", "NO_PROXY", "no_proxy", "NODE_USE_ENV_PROXY":
				continue
			}
			out = append(out, e)
		}
		env = out
	}

	set("DSH_WEB_URL=", fmt.Sprintf("http://127.0.0.1:%d", cfg.DshPort))
	// 通过 NODE_OPTIONS 设置 dsh 进程的内存上限（--max-old-space-size）。
	// 仅当“自动设置”关闭时才传递内存限制；打开时由系统 node 自动分配，不传该变量。
	if !cfg.DshMemAuto && cfg.DshMemLimit > 0 {
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
	// dsh 启动成功：把 dsh 服务 PID 文件指向新进程（harness 控制台自身的
	// HARNESS_PID_FILE 不受影响，仍记录控制台 PID）。
	m.writeDshPidFile(cmd.Process.Pid)
	m.logf("dsh started pid=%d port=%d", cmd.Process.Pid, cfg.DshPort)
	return nil
}

// Stop terminates the dsh process by killing all "MainThread" processes
// belonging to the current user using pkill, with a fallback to process group kill.
// 兼容 dsh-market 自重启：m.cmd 可能已指向退出的旧进程（僵尸或已回收），因此
// 终结目标按“受管 PID 若失效则用 /proc 中的实时 dsh PID”计算（pkill 按 comm
// 匹配也覆盖不在 m.cmd 中的新进程），并在结束后回收受管子进程，避免残留僵尸。
func (m *DshManager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var trackedCmd *exec.Cmd
	tracked := 0
	if m.cmd != nil && m.cmd.Process != nil {
		trackedCmd = m.cmd
		tracked = m.cmd.Process.Pid
	}
	// 实时 dsh PID：受管 PID 失效（如自重启）时重新在 /proc 中发现，避免 Stop
	// 只“终结”掉旧僵尸、放过自重启后的新进程。注意不调用 effectivePID——
	// Stop 已持有 m.mu，effectivePID 会重入 m.mu。
	target := tracked
	if tracked == 0 || !processAlive(tracked) {
		target = m.findDshPid()
	}
	if target <= 0 {
		// 既没有受管 dsh 也没有实时 dsh：无需停止（保持原行为，避免误杀其他进程），
		// 但 dsh 进程已不存在时仍清理可能残留的 dsh PID 文件。
		m.removeDshPidFile()
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
			// 回退到进程组 kill（按实时 pid，兼容自重启后的新进程）
			m.fallbackKill(target)
		} else {
			m.logf("pkill MainThread succeeded")
		}
	case <-time.After(3 * time.Second):
		m.logf("pkill MainThread timed out, falling back to process group kill")
		// 超时则使用 fallback
		m.fallbackKill(target)
	}

	// 停止后回收受管子进程（若已退出立即完成），避免残留僵尸。
	if trackedCmd != nil {
		m.reapCmd(trackedCmd)
	}

	// 停止后清空访问 token 与会话 cookie，避免把已失效的旧凭据继续用于反代转发。
	m.tokenMu.Lock()
	m.token = ""
	m.tokenMu.Unlock()
	m.setAuthCookie("")

	m.cmd = nil
	// dsh 已停止：移除 dsh 服务 PID 文件（进程已不存在，文件不应残留旧 PID）；
	// harness 控制台自身的 HARNESS_PID_FILE 保持不变。
	m.removeDshPidFile()
	return nil
}

// fallbackKill 是回退的进程组终止逻辑；pid 为实时 dsh PID（自重启后的新进程
// 不在 m.cmd 中，传 m.cmd 的旧僵尸 PID 会让回退终止失效）。
func (m *DshManager) fallbackKill(pid int) {
	pgid, err := syscall.Getpgid(pid)
	if err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
	} else {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
	time.Sleep(2 * time.Second)
	if pgid, err := syscall.Getpgid(pid); err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	} else {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	time.Sleep(500 * time.Millisecond)
}

// Status summarizes lifecycle state for the API.
func (m *DshManager) Status() map[string]interface{} {
	m.mu.Lock()
	trackedRunning := m.cmd != nil && m.cmd.Process != nil && !m.stopped()
	startedAt := m.startedAt
	m.mu.Unlock()
	pid := m.effectivePID()
	running := trackedRunning || pid > 0
	if !trackedRunning && pid > 0 {
		// m.cmd 旧进程已退出但 /proc 中发现新 dsh 进程（自重启），按运行中处理。
		running = true
	}
	cfg := GetConfig()
	cpu, mem := m.Stats()
	return map[string]interface{}{
		"running":    running,
		"pid":        pid,
		"startedAt":  startedAt.Format(time.RFC3339),
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
	Name     string `json:"name"`
	Version  string `json:"version"`
	Resolved string `json:"resolved"`
	// Disabled 表示该插件是否被 cordis.patch.yml 用户补丁层停用（前端据此显示启停状态）。
	Disabled bool `json:"disabled"`
	// NeedsRestart 表示该插件被启用后是否需要重启 dsh 服务才能生效（客户端插件/带原生依赖）。
	NeedsRestart bool `json:"needsRestart"`
}

// runPluginCmd 以 dsh 的运行环境执行 `dsh plugin --profile web <args...>`，
// 返回合并后的 stdout/stderr 输出。
func (m *DshManager) runPluginCmd(args ...string) (string, error) {
	// 注意：Go 不允许向变参函数混合传字面量与 slice...，需先拼成一个切片再一次性展开。
	all := append([]string{"plugin", "--profile", "web"}, args...)
	return m.runDshCmd(all...)
}

// runDshCmd 以 dsh 的运行环境执行 `dsh <args...>`（如 `dsh -V` 获取版本号），
// 返回合并后的 stdout/stderr 输出。dsh 可执行文件统一按 PATH 解析。
func (m *DshManager) runDshCmd(args ...string) (string, error) {
	cmd := exec.Command("dsh", args...)
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
