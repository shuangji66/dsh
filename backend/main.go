package main

import (
	"context"
	"embed"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
)

//go:embed all:embed
var embeddedFS embed.FS

// embeddedFrontend returns the embedded SPA assets.
func embeddedFrontend() embed.FS {
	return embeddedFS
}

// stopCh is closed when the process should shut down gracefully.
var stopCh = make(chan struct{})

// logOut 是全局日志输出目标；默认写到 stdout，配置了 HARNESS_LOG_FILE 时
// 同时写入日志文件（主进程与 dsh 子进程的日志都会经由此处落盘）。
var logOut io.Writer = os.Stdout

func logger() *log.Logger {
	return log.New(logOut, "[Harness] ", log.LstdFlags)
}

// setupLogFile 根据运行时日志路径（RuntimeEnv.LogFile，含默认值）打开日志文件；
// 路径为空则返回 nil，表示不落盘。返回的清理函数负责关闭并删除日志文件。
func setupLogFile(path string) func() {
	if path == "" {
		return func() {}
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			logger().Printf("failed to create log dir %s: %v", dir, err)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		logger().Printf("failed to open log file %s: %v", path, err)
		return func() {}
	}
	logOut = io.MultiWriter(os.Stdout, f)
	logger().Printf("logging to file %s", path)
	return func() {
		if err := f.Close(); err != nil {
			logger().Printf("failed to close log file %s: %v", path, err)
		}
		// 主进程停止时删除日志文件
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			logger().Printf("failed to remove log file %s: %v", path, err)
		}
	}
}

// writePidFile writes the current process ID to the specified file.
// Returns an error if the file cannot be written.
func writePidFile(path string, pid int) error {
	if path == "" {
		return nil // no pid file requested
	}
	// Ensure the directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(pid)), 0644)
}

// removePidFile removes the pid file if it exists.
func removePidFile(path string) {
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		logger().Printf("failed to remove pid file %s: %v", path, err)
	}
}

func main() {
	renv := loadRuntimeEnv()

	cleanupLog := setupLogFile(renv.LogFile)
	defer cleanupLog()

	// HARNESS_PID_FILE 记录的是 harness 控制台自身 PID（用于平台识别控制台
	// 进程），dsh 服务 PID 单独由 HARNESS_DSH_PID_FILE 记录（见 DshManager，
	// 随 dsh 启动/自重启/停止实时刷新），二者不要混用。
	pidFile := renv.PidFile
	if pidFile != "" {
		if err := writePidFile(pidFile, os.Getpid()); err != nil {
			logger().Printf("failed to write pid file %s: %v", pidFile, err)
		} else {
			logger().Printf("pid written to %s", pidFile)
		}
		defer removePidFile(pidFile)
	}
	// 退出时顺带清理 dsh 服务 PID 文件（正常退出前 dsh.Stop() 已移除，
	// 这里兜底处理异常退出路径）。
	defer removePidFile(renv.DshPidFile)

	if _, err := net.Dial("unix", renv.AdminSock); err == nil {
		logger().Printf("Admin socket %s is already in use, another instance is running. Exiting.", renv.AdminSock)
		os.Exit(1)
	}
	os.Remove(renv.AdminSock)
	logger().Printf("Harness backend starting (pid=%d)", os.Getpid())

	cfg := LoadConfig(&renv)
	initConfig(&cfg)

	// 启动时检测 node 版本：若此前选了 node26 但 node v26 已被卸载/不存在，
	// 主动回退到 node24 并改写持久化配置，避免用失效版本启动 dsh。
	if ensureValidNodeVersion(&renv) {
		logger().Printf("[node] node26 已不存在，版本已回退到 node24 并持久化")
	}

	auth := NewAuth()
	if cfg.AuthEnabled {
		if cfg.Password == "" {
			logger().Printf("[Auth] 未设置密码 —— 鉴权未启用，任何人都可访问。")
		} else if v := validatePassword(cfg.Password); v != "" {
			logger().Printf("[Auth] 密码校验失败: %s —— 鉴权未启用。", v)
		} else {
			logger().Printf("[Auth] 密码校验通过，登录鉴权已启用。")
		}
	} else {
		logger().Printf("[Auth] 鉴权已禁用（AuthEnabled=false）。")
	}

	dsh := NewDshManager(&renv)
	upd := newUpdateManager(&renv, dsh)
	upd.startAutoCheck()
	upd.startDailyCleanup()
	// 巡检并清理陈旧的 dsh 写锁（持有者已死）：这类锁会让插件列表/安装白等 120 秒
	// 再失败，见 cleanStaleProfileLocks。
	dsh.startStaleLockWatch()
	// 启动阶段状态机在此创建：反代（两条监听）与设置页的端口切换都要用它。
	boot := newBootState()
	admin := newAdminMux(&renv, dsh, auth, upd, boot)
	admin.SetSPA(embeddedFrontend())

	// 准备终端会话临时镜像目录（进程停止时整目录清除）
	createdSessionDir := false
	if renv.SessionDir != "" && renv.SessionDir != "/" && renv.SessionDir != "." {
		if err := os.MkdirAll(renv.SessionDir, 0o700); err != nil {
			logger().Printf("failed to create session dir %s: %v", renv.SessionDir, err)
		} else {
			createdSessionDir = true
		}
	}

	// 启动 admin socket（非阻塞）
	go func() {
		if err := serveAdminSocket(admin); err != nil {
			logger().Printf("admin socket server: %v", err)
		}
	}()
	logger().Printf("admin console on unix socket %s baseurl %q", renv.AdminSock, renv.AdminBaseURL)

	// 反代在 dsh 启动之前就开始监听：控制台启动期间访问反代端口不再是“无响应”，
	// 而是先走登录鉴权、再看带阶段的等待页，dsh 完成启动（含换取凭据、安装依赖）
	// 后等待页自动跳转。放行门禁见 reverseProxy.state()。
	// 监听端口取自持久化配置（AppConfig.ProxyPort，默认 3079），绑定失败不致命：
	// 控制台（admin socket）与 Unix Socket 挂载照常提供，仅这条 TCP 监听缺席。
	if err := startProxy(cfg.ProxyPort, auth, dsh, boot); err != nil {
		logger().Printf("reverse proxy listen on :%d failed: %v", cfg.ProxyPort, err)
	}
	// 第二条监听：unix socket + 子路径挂载（fnOS 网关把 /app/Harness/dsh 转发到这里）。
	startProxySocket(renv.ProxySock, renv.ProxyBaseURL, auth, dsh, boot)

	// 所有核心服务已启动，现在处理 dsh 和 node-pty 安装
	if os.Getenv("HARNESS_AUTOSTART") != "0" {
		boot.set(phaseStarting, "")
		if err := dsh.Start(); err != nil {
			boot.set(phaseFailed, err.Error())
			logger().Printf("autostart dsh: %v", err)
		} else {
			// dsh 启动成功，等待并捕获其一次性访问 token（新版 dsh 会打印
			// "dsh web: http://127.0.0.1:<port>/?token=XXX"），并从 Set-Cookie
			// 换取 dsh 会话 cookie，供反代转发时携带。
			boot.set(phaseAuth, "")
			captureDshSession(dsh)
			// 执行 node-pty 固定版本与清理（会等待目录生成）。传入空 home 由函数内部
			// 优先从 config.json 的 homeDir 解析 dsh 实际使用的 HOME。若返回需要重启，
			// 则在 pnpm install 完成后重启 dsh 使 node-pty 1.2.0-beta.15 生效。
			boot.set(phaseDeps, "")
			restartNeeded, err := ensureNodePty(&renv, "")
			if err != nil {
				logger().Printf("Warning: node-pty setup failed: %v, dsh may not work", err)
			}
			if restartNeeded {
				logger().Printf("node-pty setup changed workspace, restarting dsh")
				boot.set(phaseDeps, "依赖已更新，正在重启 dsh")
				if serr := dsh.Stop(); serr != nil {
					logger().Printf("restart dsh (stop) after node-pty setup failed: %v", serr)
				} else if serr := dsh.Start(); serr != nil {
					logger().Printf("restart dsh (start) after node-pty setup failed: %v", serr)
				} else {
					// 重启后 dsh 会生成新的访问 token，需重新捕获会话。
					boot.set(phaseAuth, "")
					captureDshSession(dsh)
				}
			}
			// 启动流水线收尾：之后能否放行只取决于 dsh 端口与凭据（dsh 运行期的
			// 重启窗口由 reverseProxy.state() 自行推导成“启动中/已停止”）。
			boot.set(phaseReady, "")
		}
	} else {
		boot.set(phaseDisabled, "")
		logger().Printf("HARNESS_AUTOSTART=0, dsh not auto-started, skipping node-pty installation")
	}

	// 等待退出信号
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	select {
	case s := <-sig:
		logger().Printf("received signal %v, shutting down", s)
	case <-stopCh:
		logger().Printf("shutdown requested, stopping")
	}

	dsh.Stop()
	// 停止控制台：终止全部终端会话（进程 + 历史文件），并删除临时会话目录
	admin.sessions.CloseAll()
	if createdSessionDir {
		os.RemoveAll(renv.SessionDir)
	}
	os.Remove(renv.AdminSock)
	stopProxySocket()
	logger().Printf("backend stopped")
}

// captureDshSession 等待并捕获 dsh 的一次性访问 token，并用 token 换取 dsh
// 会话 cookie，供反向代理转发时携带。每次 dsh 启动都会生成新的 token，因此
// dsh 重启后需重新调用本函数。
//
// 无论是否拿到 token 都要标记本代凭据「已落定」（SessionSettled）：反代据此
// 决定放行还是继续显示等待页，漏标会让反代一直停在等待页。旧版 dsh 不打印
// token 时等待会超时返回空串，同样算落定，避免永久卡住。
func captureDshSession(dsh *DshManager) {
	defer dsh.markSessionSettled()
	if tok := dsh.WaitToken(15 * time.Second); tok != "" {
		// 用 token 访问一次带 token 的地址，从 Set-Cookie 换取 dsh 会话 cookie，
		// 供反代转发时携带（访问不带 token 的 dsh 地址）。成功时不输出日志。
		if err := dsh.ExchangeToken(); err != nil {
			logger().Printf("dsh token exchange failed: %v", err)
		}
	}
}

// initConfig applies a config as the process-wide singleton.
func initConfig(c *AppConfig) {
	cfgLock.Lock()
	cfg = *c
	cfgLock.Unlock()
}

// netListen is a thin wrapper so adminmux.go can reference it.
func netListen(network, addr string) (net.Listener, error) {
	return net.Listen(network, addr)
}

var (
	proxyServer *http.Server
	proxyLn     net.Listener
	proxyMu     sync.Mutex
)

// startProxy 绑定并启动反代的 TCP 监听（根挂载，端口见 AppConfig.ProxyPort）。
//
// 与旧实现的区别：监听在这里**同步绑定**，绑定失败（端口被占用、无权限）直接返回
// 错误而不是只在 goroutine 里记日志 —— 设置页切换端口时需要先确认新端口可用，
// 失败就必须保持旧监听不动（见 admin.go 的 applyProxyPortChange）。
// 切换成功后旧监听随即关闭，端口改动无需重启控制台（进程退出时由操作系统回收）。
func startProxy(port int, auth *Auth, dsh *DshManager, boot *bootState) error {
	addr := ":" + strconv.Itoa(port)
	ln, err := netListen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: addr, Handler: newReverseProxy(auth, dsh, boot)}

	proxyMu.Lock()
	oldSrv, oldLn := proxyServer, proxyLn
	proxyServer, proxyLn = srv, ln
	proxyMu.Unlock()

	go func() {
		logger().Printf("reverse proxy listening on %s", addr)
		// 端口切换（或进程退出）时监听会被主动关闭，Serve 返回的 ErrServerClosed /
		// net.ErrClosed 属正常收尾，不记为错误。
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			logger().Printf("proxy server error: %v", err)
		}
	}()
	closeProxyListener(oldSrv, oldLn)
	return nil
}

// closeProxyListener 关闭一条已被替换下来的反代监听（端口切换时由 startProxy
// 传入旧值）。
func closeProxyListener(srv *http.Server, ln net.Listener) {
	if srv == nil && ln == nil {
		return
	}
	// 先关监听让 Serve 立刻返回，再优雅关闭在途请求（等待页是短连接，不会久留）。
	if ln != nil {
		if err := ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			logger().Printf("close old proxy listener: %v", err)
		}
	}
	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, context.DeadlineExceeded) {
			logger().Printf("shutdown old proxy server: %v", err)
		}
	}
}

var (
	proxySockServer *http.Server
	proxySockPath   string
)

// startProxySocket 在 unix socket 上再开一条反代监听，并把它挂在 baseURL 前缀下
// （见 proxyMount）。这是 fnOS 部署形态：平台网关把
// http://<fnip>:<port>/app/Harness/dsh 整段转发到本 socket，反代剥掉该前缀
// 再转发给 dsh，于是 dsh 前端（0.1.7-alpha.1 起全部使用文档相对路径）自动发起的
// "<prefix>/api"、"<prefix>/plugins/..." 都落回这里。
//
// socket 路径取自 HARNESS_PROXY_SOCK（默认 $TRIM_APPDEST/dsh.sock，即
// /var/apps/Harness/target/dsh.sock）；取值为空或 "off"、目录不可创建、监听失败
// 时只记日志并跳过——这条监听是可选的部署形态，不能因此让控制台起不来。
func startProxySocket(sockPath, baseURL string, auth *Auth, dsh *DshManager, boot *bootState) {
	if sockPath == "" || sockPath == "off" {
		logger().Printf("proxy unix socket disabled (HARNESS_PROXY_SOCK=%q)", sockPath)
		return
	}
	if dir := filepath.Dir(sockPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			logger().Printf("proxy unix socket dir %s unavailable: %v — skipping", dir, err)
			return
		}
	}
	// socket 存在且能连上，说明已有活着的持有者（正常路径下 main 的 admin socket
	// 占用检查会先退出，这里只是兜底）：让它继续服务，自己不再抢。
	if _, err := net.Dial("unix", sockPath); err == nil {
		logger().Printf("proxy unix socket %s already in use, skipping", sockPath)
		return
	}
	os.Remove(sockPath)
	ln, err := netListen("unix", sockPath)
	if err != nil {
		logger().Printf("proxy unix socket listen %s failed: %v", sockPath, err)
		return
	}
	if err := os.Chmod(sockPath, 0o660); err != nil {
		logger().Printf("chmod proxy unix socket %s: %v", sockPath, err)
	}
	proxySockServer = &http.Server{Handler: newReverseProxyAt(auth, dsh, boot, baseURL)}
	proxySockPath = sockPath
	go func() {
		logger().Printf("reverse proxy listening on unix socket %s (baseurl %q)", sockPath, newProxyMount(baseURL).dir())
		if err := proxySockServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			logger().Printf("proxy unix socket server error: %v", err)
		}
	}()
}

// stopProxySocket 关闭子路径挂载的监听并删除 socket 文件（退出时清理，避免留下
// 只能靠 dial 失败才发现的陈旧 socket）。
func stopProxySocket() {
	if proxySockServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = proxySockServer.Shutdown(ctx)
		cancel()
		proxySockServer = nil
	}
	if proxySockPath != "" {
		os.Remove(proxySockPath)
		proxySockPath = ""
	}
}
