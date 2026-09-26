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
	"path/filepath"
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
	logf        logFunc // 带等级的日志出口（测试注入静默实现）
	statsMu     sync.Mutex
	lastCpu     float64 // 最近一次采样的 CPU 使用率（%）
	lastMemory  int64   // 最近一次采样的常驻内存（MB）
	lastStatsAt time.Time
	// procStartMu 保护下面这组「PID → 进程启动时刻」缓存。同一个 PID 的启动时刻不会变，
	// 而 Status() 每秒都被 SSE 推一次（概览页 CPU/内存要实时刷新），没必要每次都去读
	// /proc；PID 变了（启停 / 装插件自重启）自然会重读。与上面 lastStatsAt 那套秒级
	// 缓存同一个思路。
	procStartMu  sync.Mutex
	procStartPid int
	procStartAt  time.Time
	procStartOK  bool
	tokenMu      sync.RWMutex
	token        string // 新版 dsh 启动时在日志输出的一次性访问 token
	authMu       sync.RWMutex
	authCookie   string // 用 token 换取到的 dsh 会话 cookie（形如 "dsh-auth-xxx=yyy"）
	// sessionMu 保护下面两个「启动代号」：每代 dsh 启动都会生成新的 token，需要用
	// 它异步换取会话凭据（见 captureDshSession），换取完成之前反代不应放行转发
	// （否则转发到 dsh 必然被拒）。用独立互斥锁：Start 会长时间持有 m.mu，而标记
	// 来自异步的 captureDshSession。
	sessionMu       sync.Mutex
	sessionGen      int // dsh 启动代号：每次 Start 递增
	sessionReadyGen int // 已完成凭据换取（或确认无需凭据）的启动代号
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

	// pluginCmdMu 保护 pluginCmdN：正在执行的 `dsh plugin …` 命令计数。
	// 这些命令会在 dsh 进程内持有 profile 写锁（plugin-manager 的
	// package.json.lock），所以「更新 dsh 服务 / 更新市场 / 回滚 server 目录」之前
	// 必须先确认没有正在跑的命令，否则杀掉它们就会留下陈旧锁。用独立互斥锁：
	// Stop 会长时间持有 m.mu，不能共用。
	pluginCmdMu sync.Mutex
	pluginCmdN  int
}

// beginPluginCmd / endPluginCmd 维护「正在执行插件命令」的计数。
func (m *DshManager) beginPluginCmd() {
	m.pluginCmdMu.Lock()
	m.pluginCmdN++
	m.pluginCmdMu.Unlock()
}

func (m *DshManager) endPluginCmd() {
	m.pluginCmdMu.Lock()
	if m.pluginCmdN > 0 {
		m.pluginCmdN--
	}
	m.pluginCmdMu.Unlock()
}

// PluginCmdRunning 报告是否正在执行 `dsh plugin …`（控制台侧发起的插件操作）。
// 市场面板内的安装走它自己 spawn 的子进程，由 marketBusyFn 覆盖，两者互补。
func (m *DshManager) PluginCmdRunning() bool {
	m.pluginCmdMu.Lock()
	defer m.pluginCmdMu.Unlock()
	return m.pluginCmdN > 0
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

// bumpSessionGen 在每次 dsh 启动时递增启动代号，使此前那代的凭据不再算「已落定」。
func (m *DshManager) bumpSessionGen() {
	m.sessionMu.Lock()
	m.sessionGen++
	m.sessionMu.Unlock()
}

// SessionSettled 报告本代 dsh 的访问凭据是否已尘埃落定。
//
// dsh 每次启动都会打印一次性 token，harness 需要用它换取 dsh-auth-* 会话 cookie
// （见 captureDshSession，由 Start 之后的调用方异步执行）。在换取完成之前反代
// 转发必然拿不到凭据、只会收到 dsh 的未授权响应，因此反代据此决定「继续显示
// 等待页」还是「放行」（见 reverseProxy.state）。
//
// 旧版 dsh 不打印 token 时，等待会在 15 秒后超时并同样标记为已落定，所以最坏
// 情况只是多显示 15 秒等待页，不会永远卡住。
func (m *DshManager) SessionSettled() bool {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	return m.sessionReadyGen >= m.sessionGen
}

// markSessionSettled 由 captureDshSession 在等待 token（拿到或超时）结束后调用。
func (m *DshManager) markSessionSettled() {
	m.sessionMu.Lock()
	m.sessionReadyGen = m.sessionGen
	m.sessionMu.Unlock()
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
		logf:       logAt,
	}
}

// logInfo / logWarn / logError 是 DshManager 的日志出口包装（等级见 logging.go）。
func (m *DshManager) logInfo(format string, a ...interface{}) {
	m.logf(levelInfo, format, a...)
}
func (m *DshManager) logWarn(format string, a ...interface{}) {
	m.logf(levelWarn, format, a...)
}
func (m *DshManager) logError(format string, a ...interface{}) {
	m.logf(levelError, format, a...)
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
	prevLive := m.livePid
	found := m.findDshPid()
	m.livePid = found
	m.pidCheckedAt = time.Now()
	// 只在实时 PID 真正变化时记一行：本函数由状态轮询高频调用，而 dsh 自重启后
	// m.cmd 里的旧 PID 永远是「已死」，按 found != tracked 判定会每次调用都刷一行
	// （历史上曾把同一行刷出 3500+ 条）。
	if found > 0 && found != prevLive {
		m.logInfo("dsh pid changed: tracked=%d -> live=%d", tracked, found)
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
		m.logError("failed to write dsh pid file %s: %v", m.dshPidFile, err)
		return
	}
	// 写入成功是常规操作，不记日志。
	m.dshPidFilePid = pid
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
		m.logError("failed to remove dsh pid file %s: %v", m.dshPidFile, err)
	}
}

// readDshPidFile 读取 dsh 服务 PID 文件（HARNESS_DSH_PID_FILE，即「dsh.pid」）
// 中记录的 dsh 进程 PID。文件内容为纯数字（可能带首尾空白），返回记录的正整数；
// 文件不存在、内容非法或未配置路径时返回 0。
func (m *DshManager) readDshPidFile() int {
	if m.dshPidFile == "" {
		return 0
	}
	data, err := os.ReadFile(m.dshPidFile)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

// killPidGracefully 对单个 PID 做优雅终止：先发 SIGTERM 等待退出，短暂超时后
// 若仍未退出则补发 SIGKILL。它只针对该 PID 本身（不杀进程组），用于「dsh.pid」
// 精确保底——避免进程组 kill 误伤同进程组内其它无关进程。
func (m *DshManager) killPidGracefully(pid int) {
	if pid <= 0 || !processAlive(pid) {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
	time.Sleep(2 * time.Second)
	if processAlive(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
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
		// 回收成功是常规收尾，不记日志。
	case <-time.After(3 * time.Second):
		m.logWarn("reap of dsh child pid %d deferred (process still shutting down)", cmd.Process.Pid)
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
	m.logInfo("dsh self-restart requested (old pid=%d), watching for the replacement", tracked)
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
			m.logWarn("dsh self-restart: tracked pid %d still alive after 30s, restart likely rejected", oldPID)
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
			m.logInfo("dsh self-restart detected: tracked=%d -> live=%d", oldPID, found)
			return
		}
		if time.Now().After(deadline) {
			m.logError("dsh self-restart: no replacement found within 90s (old pid=%d)", oldPID)
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

// UptimeSeconds 返回 dsh 当前 PID **该进程**已运行的秒数（进程不在 / 读不到时返回 false）。
//
// 按 PID 缓存进程启动时刻：同一个 PID 的启动时刻是常量，而 Status() 每秒都会被 SSE 推
// 一次，没必要每秒读两遍 /proc（概览页的 CPU/内存也有同样的秒级缓存，见 Stats）。
// PID 变化（用户启停、装插件自重启）会让缓存失效并重新读取 —— 因此自重启后运行时间
// 会从新进程的启动时刻重新算起，这正是概览页要展示的语义。
//
// 注意：PID 复用（同一 PID 被另一个进程占用）理论上会让缓存偏旧，但这里只在
// 「PID 没变」时才复用缓存，而 dsh 的 PID 来自 effectivePID()/PID 文件（进程一换就是
// 新 PID）；Linux 的 PID 回绕需要几万个进程才能撞上，实际不会影响。
func (m *DshManager) UptimeSeconds(pid int) (int64, bool) {
	if pid <= 0 {
		return 0, false
	}
	m.procStartMu.Lock()
	if m.procStartPid != pid || !m.procStartOK {
		start, ok := procStartTime(pid)
		m.procStartPid, m.procStartAt, m.procStartOK = pid, start, ok
	}
	start, ok := m.procStartAt, m.procStartOK
	m.procStartMu.Unlock()
	if !ok {
		return 0, false
	}
	d := time.Since(start)
	if d < 0 {
		return 0, true
	}
	return int64(d / time.Second), true
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

	// 代理只作为环境变量交给 dsh。dsh ≥0.1.5 的 @deepseek-ai/dsh-http-proxy 会在加载任何
	// 插件前从启动环境解析代理策略（小写优先、大写兜底，ALL_PROXY 兜底两种 scheme）并安装
	// 为全局 dispatcher；该包还会把解析结果同时写回小写+大写发给自己的子进程，并由
	// dsh-subprocess 给自建环境的子进程补上 NODE_USE_ENV_PROXY。
	//
	// 因此这里不再设置 NODE_USE_ENV_PROXY：对 dsh 自身的 fetch 它已无作用（dispatcher 已
	// 覆盖），却会让 node 在启动时解析 HTTP_PROXY/HTTPS_PROXY —— 遇到无法解析的值（漏写
	// scheme 的 127.0.0.1:7890、空白等）node 直接以 ERR_INVALID_URL 退出，dsh 随之完全起
	// 不来；而 dsh 自身对这类值只是报告并跳过，继续直连。
	// 注意：这里是「代理dsh」开关（ProxyEnabled），只影响 dsh 进程自身的出网；
	// harness 自己的更新是否走代理由另一个开关（ProxyUpdate / 设置页「代理更新」）决定，
	// 见 update.go 的 updateClients。
	if cfg.ProxyEnabled && cfg.ProxyAddr != "" {
		set("http_proxy=", cfg.ProxyAddr)
		set("https_proxy=", cfg.ProxyAddr)
		set("HTTP_PROXY=", cfg.ProxyAddr)
		set("HTTPS_PROXY=", cfg.ProxyAddr)
		set("all_proxy=", cfg.ProxyAddr)
		set("ALL_PROXY=", cfg.ProxyAddr)
	} else {
		// 关闭代理时清掉启动环境里的代理变量，否则 dsh 仍会从环境解析出策略。
		// NODE_USE_ENV_PROXY 不在此列：它不由 harness 定义，交给 dsh 按自己的策略决定。
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
	// 启动前自愈：清掉持有者已不存在的 profile 写锁。上一次 dsh 被停掉/被杀死时，
	// plugin-manager 可能来不及删除它的 `package.json.lock`，那份陈旧锁会让之后
	// 所有插件操作白等 120 秒再失败（见 cleanStaleProfileLocks）。
	m.cleanStaleProfileLocks()
	// 每次启动都重置 token 与会话 cookie，避免复用上一次启动的旧凭据；同时递增
	// 启动代号，让反代判定「本代凭据尚未落定」而继续显示等待页（见 SessionSettled）。
	m.tokenMu.Lock()
	m.token = ""
	m.tokenMu.Unlock()
	m.setAuthCookie("")
	m.bumpSessionGen()

	cmd := exec.Command(bin, "web", "--no-open", "--port", fmt.Sprintf("%d", cfg.DshPort))
	cmd.Dir = m.renv.TRIMAppDest
	cmd.Env = m.buildEnv()
	// 拦截 dsh 子进程的 stdout/stderr：既原样写到全局日志（dshLogWriter，
	// 不改格式、固定黄色），又扫描其中的一次性访问 token（新版 dsh 启动时会打印
	// "dsh web: http://...?token=XXX"）。
	scanner := &tokenScanner{
		dst: dshLogWriter{},
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
	m.logInfo("dsh started pid=%d port=%d", cmd.Process.Pid, cfg.DshPort)
	return nil
}

// Stop terminates the dsh process. 主路径用 pkill 按主线程进程名匹配并终止
// （node24 为 "MainThread"、node26 为 "node-MainThread"，用正则同时覆盖）；
// 未命中/超时时进入精确保底：先按「dsh.pid」文件记录的 PID 精确杀进程，
// 若仍存活再退回进程组 kill。
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

	// 停止方式：**只按 PID / 进程组精准终止**，不再用
	// `pkill -TERM -u <user> -x "MainThread|node-MainThread"`。
	//
	// 为什么去掉按进程名杀：node 进程的 comm 就是 "MainThread"/"node-MainThread"，
	// 该 pkill 会杀掉该用户下**所有** Node 进程 —— 包括插件市场正在跑的
	// `dsh plugin --profile web add`（dsh-plugin-manager 正持有 profile 写锁
	// `profiles/web/package.json.lock`）以及它的 pnpm 子进程。杀掉锁持有者会留下
	// 陈旧锁文件，而该锁的等待上限是 120 秒且实现上不清理陈旧锁，于是之后每一次
	// 「插件列表 / 安装 / 更新」都会白等 120 秒再失败（线上现象：市场内无法更新、
	// 控制台插件列表空白，且浏览器轮询不断堆积挂起的 dsh 进程）。
	// 精准路径本来就已经写好（pid 文件 → 进程组兜底），这里改为始终走它。
	if pidFilePid := m.readDshPidFile(); pidFilePid > 0 && processAlive(pidFilePid) {
		m.logInfo("dsh stop: killing by pid-file pid %d", pidFilePid)
		m.killPidGracefully(pidFilePid)
	}
	if live := m.findDshPid(); live > 0 {
		m.logInfo("dsh stop: killing process group of dsh pid %d", live)
		m.fallbackKill(live)
	}
	// 受管子进程仍在（例如 pid 文件没跟上）时再补一刀。
	if tracked > 0 && processAlive(tracked) && tracked != target {
		m.logInfo("dsh stop: killing tracked pid %d", tracked)
		m.killPidGracefully(tracked)
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
	st := map[string]interface{}{
		"running":    running,
		"pid":        pid,
		"startedAt":  startedAt.Format(time.RFC3339),
		"dshPort":    cfg.DshPort,
		"proxyPort":  cfg.ProxyPort,
		"locked":     running,
		"cpuPercent": cpu,
		"memoryMB":   mem,
	}
	// 进程运行时间（秒）：按 /proc/<pid>/stat 里**该 PID** 的真实启动时刻算（按 PID 缓存，
	// 不每秒读 /proc），而不是 m.startedAt —— 装插件会触发 dsh 自重启换 PID，m.startedAt
	// 可能还停在前一代的时刻，照它算会把新一代的运行时间多加一截。读不到（进程刚消失等）
	// 就**不带这个字段**，前端显示「—」；进程刚起来不足 1 秒时是真实的 0，前端显示「0 秒」。
	if up, ok := m.UptimeSeconds(pid); ok {
		st["uptimeSeconds"] = up
	}
	return st
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
//
// 执行前会先清掉「持有者已死」的 profile 写锁：dsh 的 plugin-manager 在插件列表 /
// 安装 / 卸载时都会先拿 `profiles/web/package.json.lock`，而陈旧锁会让这条命令
// 白等 120 秒再失败（见 cleanStaleProfileLocks）。顺手在这里自愈，控制台就不会
// 出现「插件列表空白 + 请求挂起」。
func (m *DshManager) runPluginCmd(args ...string) (string, error) {
	m.cleanStaleProfileLocks()
	// 登记「插件命令进行中」：更新 dsh 服务 / 更新市场 / 回滚都要先看这个计数，
	// 否则会把这批命令连同它持有的 profile 写锁一起带走（见 PluginCmdRunning）。
	m.beginPluginCmd()
	defer m.endPluginCmd()
	// 注意：Go 不允许向变参函数混合传字面量与 slice...，需先拼成一个切片再一次性展开。
	all := append([]string{"plugin", "--profile", "web"}, args...)
	// 带超时：即使遇到无法自愈的挂起（例如持有者还活着但在等网络），也不能让
	// 控制台的 HTTP 请求无限挂住、并不断堆积 dsh 子进程。
	return m.runDshCmdTimeout(pluginCmdTimeout, all...)
}

// --- 陈旧的 dsh 写锁清理 ---

// pluginCmdTimeout 是控制台侧插件命令（list / remove）的上限。dsh 的 profile 写锁
// 等待上限是 120 秒，这里留出余量让「确实在等锁」的命令自己报错，只拦真正的挂死。
const pluginCmdTimeout = 180 * time.Second

// profileLockCandidates 返回会顺带清理的 dsh 写锁路径。
// 刻意不递归 .dsh：sessions/**/session.lock 是长生命周期会话锁，语义不同。
func (m *DshManager) profileLockCandidates() []string {
	home := m.effectiveHome()
	if home == "" {
		return nil
	}
	dshHome := filepath.Join(home, ".dsh")
	return []string{
		// dsh-plugin-manager：插件列表 / 安装 / 卸载都会先拿它（等待上限 120 秒）
		filepath.Join(dshHome, "profiles", "web", "package.json.lock"),
		// dsh-app-boot：模块 fallback 目录的写锁
		filepath.Join(dshHome, "profiles", "node_modules.lock"),
		// 设置与凭据文件的写锁（dsh-settings-file / dsh-credentials-local）
		filepath.Join(dshHome, "settings.yaml.lock"),
		filepath.Join(dshHome, ".credentials.yaml.lock"),
	}
}

// staleLockWatchInterval 是陈旧写锁巡检周期。
const staleLockWatchInterval = 30 * time.Second

// startStaleLockWatch 常驻巡检并清理陈旧的 dsh 写锁。
//
// 为什么需要常驻：锁是在 dsh 进程内创建的，而 harness 只在「启动 dsh」与「执行
// dsh plugin …」时顺带清理。如果锁是在控制台空闲时被留下的，用户下一次在**市场面板里**
// 操作仍会白等 120 秒再失败。30 秒一次的巡检把这个窗口压到可忽略，代价只是每周期
// 四次 stat/read。
func (m *DshManager) startStaleLockWatch() {
	go func() {
		for {
			time.Sleep(staleLockWatchInterval)
			m.cleanStaleProfileLocks()
		}
	}()
}

// cleanStaleProfileLocks 删除「持有者进程已不存在」的 dsh 写锁文件，返回清理条数。
//
// 为什么 harness 要管这件事：dsh 的跨进程写锁（@deepseek-ai/dsh-atomic-write 的
// withFileLock）是 `<文件>.lock` + `wx` 独占创建，只在 finally 里删除。持有者被杀死
// （用户取消安装、进程被重启带走等）就会永久残留；而实现的争用判定只看 EEXIST、
// 不清理陈旧锁，等待上限又可以是 120 秒（plugin-manager 的 lockWaitMs 默认值），
// 于是之后每一次插件操作都会白等 120 秒再失败 —— 线上表现为「市场内无法更新、
// 控制台插件列表空白」，且浏览器轮询会不断堆积挂起的 dsh 进程。
//
// 判活依据就是锁文件里写的持有者 PID（writeFile(lockPath, `${process.pid}\n`)），
// 因此只有该 PID 确实不存在时才删；读不出 PID（写了一半就死 / 格式变化）时不动它。
func (m *DshManager) cleanStaleProfileLocks() int {
	cleaned := 0
	for _, path := range m.profileLockCandidates() {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue // 文件不存在 = 本来就没有锁，正常
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		if err != nil || pid <= 0 {
			m.logWarn("stale lock: unparsable owner pid, skipping %s", path)
			continue
		}
		if processAlive(pid) {
			continue // 真有人在用，绝不删
		}
		if err := os.Remove(path); err != nil {
			m.logError("stale lock: failed to remove %s: %v", path, err)
			continue
		}
		cleaned++
		m.logWarn("stale lock: removed %s (owner pid %d is gone)", path, pid)
	}
	return cleaned
}

// runDshCmd 以 dsh 的运行环境执行 `dsh <args...>`（如 `dsh -V` 获取版本号），
// 返回合并后的 stdout/stderr 输出。dsh 可执行文件统一按 PATH 解析。
func (m *DshManager) runDshCmd(args ...string) (string, error) {
	return m.runDshCmdTimeout(0, args...)
}

// runDshCmdTimeout 与 runDshCmd 相同，但 timeout > 0 时到点终止子进程并返回错误，
// 避免命令挂死时把调用方（控制台 HTTP 请求）一起拖住、并不断堆积 dsh 子进程。
func (m *DshManager) runDshCmdTimeout(timeout time.Duration, args ...string) (string, error) {
	cmd := exec.Command("dsh", args...)
	cmd.Env = m.buildEnv()
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if timeout <= 0 {
		if err := cmd.Run(); err != nil {
			return strings.TrimSpace(out.String()), err
		}
		return strings.TrimSpace(out.String()), nil
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return strings.TrimSpace(out.String()), err
		}
		return strings.TrimSpace(out.String()), nil
	case <-time.After(timeout):
		// 先 TERM 让它有机会走收尾（例如释放 profile 写锁），给 3 秒再强杀。
		// 注意只杀这个子进程本身，不做进程组 kill：这些 CLI 子进程没有独立进程组，
		// `kill(-pid)` 会命中 harness 自己所在的进程组。
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				_ = cmd.Process.Kill()
				<-done
			}
		} else {
			<-done
		}
		return strings.TrimSpace(out.String()),
			fmt.Errorf("执行 dsh %s 超时（%s）", strings.Join(args, " "), timeout)
	}
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
