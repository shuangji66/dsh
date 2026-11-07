package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
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

// setupLogFile 根据运行时日志路径（RuntimeEnv.LogFile，含默认值）打开日志文件；
// 路径为空则不落盘。日志出口见 logging.go（等级 / 终端着色 / 重复抑制 /
// dsh 输出原样透传）。返回的清理函数负责关闭并删除日志文件。
func setupLogFile(path string) func() {
	if path == "" {
		return func() {}
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			logError("failed to create log dir %s: %v", dir, err)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		logError("failed to open log file %s: %v", path, err)
		return func() {}
	}
	logs.mu.Lock()
	logs.file = f
	logs.mu.Unlock()
	return func() {
		if err := f.Close(); err != nil {
			logError("failed to close log file %s: %v", path, err)
		}
		// 主进程停止时删除日志文件
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			logError("failed to remove log file %s: %v", path, err)
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
		logError("failed to remove pid file %s: %v", path, err)
	}
}

func main() {
	renv := loadRuntimeEnv()

	cleanupLog := setupLogFile(renv.LogFile)
	defer cleanupLog()

	// 数据目录下的 harness.pid 记录的是 harness 控制台自身 PID（用于平台识别
	// 控制台进程），dsh 服务 PID 单独由 dsh.pid 记录（见 DshManager，随 dsh
	// 启动/自重启/停止实时刷新），二者不要混用。
	pidFile := renv.PidFile
	if pidFile != "" {
		if err := writePidFile(pidFile, os.Getpid()); err != nil {
			logError("failed to write pid file %s: %v", pidFile, err)
		}
		defer removePidFile(pidFile)
	}
	// 退出时顺带清理 dsh 服务 PID 文件（正常退出前 dsh.Stop() 已移除，
	// 这里兜底处理异常退出路径）。
	defer removePidFile(renv.DshPidFile)

	if _, err := net.Dial("unix", renv.AdminSock); err == nil {
		logError("admin socket %s already in use, another instance is running - exiting", renv.AdminSock)
		os.Exit(1)
	}
	os.Remove(renv.AdminSock)

	// 共享 agent 配置根（$HOME/.agents）：dsh 从这里读技能与全局指令等共享配置，
	// 但启动时不会自己创建它，所以在控制台启动时检测并创建；同一个路径以
	// DSH_AGENTS_HOME 下发给 dsh 进程（见 DshManager.buildEnv）。创建成功不输出日志。
	//
	// 放在「另一个实例已在运行」检查之后：那种情况下本进程马上就要退出，
	// 不该先去动用户主目录里的东西。
	ensureAgentsHome(renv.Home)

	cfg := LoadConfig(&renv)
	initConfig(&cfg)

	// 校准终端里的 dsh 稳定入口（${数据目录}/server/.bin/dsh → 当前选中版本）。
	// 启动时对账一次，覆盖「版本目录被外部删掉 / 配置里选中的版本已不存在」；
	// 之后每次切换版本由 ServerManager.Switch 更新同一个链接。
	if err := syncDshShim(&renv); err != nil {
		logWarn("[dsh] failed to sync terminal dsh shim: %v", err)
	}

	auth := NewAuth()
	if cfg.AuthEnabled {
		if cfg.Password == "" {
			logWarn("[auth] no password configured - login auth disabled, console is open to anyone")
		} else if v := validatePassword(cfg.Password); v != "" {
			// 强度不合规**不**等于关闭鉴权：这条密码照旧参与会话 cookie 的校验与
			// 签发（见 isAuthed），只是提醒用户换一个更安全的。措辞必须与真实
			// 行为一致 —— 旧文案写成 "login auth disabled"，会把「鉴权其实开着」
			// 说成「已关闭」。
			logError("[auth] password strength is weak (%s) - still used for login auth, please change it", v)
		} else {
			logInfo("[auth] login auth enabled")
		}
	} else {
		logWarn("[auth] login auth disabled by config (authEnabled=false)")
	}

	dsh := NewDshManager(&renv)
	upd := newUpdateManager(&renv, dsh)
	upd.startAutoCheck()
	upd.startDailyCleanup()
	// 巡检并清理陈旧的 dsh 写锁（持有者已死）：这类锁会让插件列表/安装白等 120 秒
	// 再失败，见 cleanStaleProfileLocks。
	dsh.startStaleLockWatch()

	// 快捷指令命令内容与**账号库 token** 的落盘加密（主密钥缺失时自动生成 0600 的
	// master.key，见 secret.go）。失败**不退出**：控制台是这个 NAS 的带外访问入口，为一把
	// 主密钥把整个控制台停掉不划算——照常启动，但快捷指令的保存与账号库的读写（含自愈导入）
	// 都会明确报错，绝不退化成写明文（见 quickcmds.go / accountvault.go）。
	crypt, cryptErr := loadCryptor(masterKeyPath(&renv), renv.MasterKey)
	if cryptErr != nil {
		logError("[secrets] master key unavailable: %v - saving quick commands, custom dsh environment variables, and the account vault will fail", cryptErr)
	}
	// 自定义环境变量（dsh-env.json）与快捷指令/账号库共用同一把主密钥；crypt 可能为 nil，
	// buildEnv 会跳过注入并记 WARN（见 dsh_env.go / dsh.go）。
	dsh.crypt = crypt
	// 清掉上次「写一半」留下的 dsh-env.json.*.tmp（进程被杀/掉电才会有，见 sweepDshEnvTemps）。
	// 放在任何保存之前：运行期不能扫，否则可能删掉并发保存正在写的临时文件。
	sweepDshEnvTemps(dshEnvPath(&renv))
	// 一次性迁移：把旧版本留在磁盘上的**明文**命令内容加密（见 migrateQuickCmdsAtStartup）。
	// 放在这里而不是等用户打开弹窗：前端只在弹窗打开时才加载列表，用户几个月不点它，
	// 明文就会一直躺在磁盘上。失败只记日志、不拦住启动。
	if merr := migrateQuickCmdsAtStartup(&renv, crypt); merr != nil {
		logWarn("[quickcmds] migrating plaintext command contents failed (will retry when the dialog is opened or on the next start): %v", merr)
	}

	// 启动阶段状态机在此创建：反代（TCP 根挂载 + admin socket 上的 /dsh 挂载）与设置页的
	// 端口切换都要用它。
	boot := newBootState()
	admin := newAdminMux(&renv, dsh, auth, upd, boot, crypt)
	admin.SetSPA(embeddedFrontend())

	// 准备终端会话临时镜像目录（进程停止时整目录清除）
	createdSessionDir := false
	if renv.SessionDir != "" && renv.SessionDir != "/" && renv.SessionDir != "." {
		if err := os.MkdirAll(renv.SessionDir, 0o700); err != nil {
			logError("failed to create session dir %s: %v", renv.SessionDir, err)
		} else {
			createdSessionDir = true
		}
	}

	// 启动 admin socket（非阻塞）
	go func() {
		if err := serveAdminSocket(admin); err != nil {
			logError("admin console server stopped: %v", err)
		}
	}()

	// 反代在 dsh 启动之前就开始监听：控制台启动期间访问反代端口不再是“无响应”，
	// 而是先走登录鉴权、再看带阶段的等待页，dsh 完成启动（含换取凭据、安装依赖）
	// 后等待页自动跳转。放行门禁见 reverseProxy.state()。
	// 监听端口取自持久化配置（AppConfig.ProxyPort，默认 3079），绑定失败不致命：
	// 控制台（admin socket）与 Unix Socket 挂载照常提供，仅这条 TCP 监听缺席。
	if err := startProxy(cfg.ProxyPort, auth, dsh, boot, admin); err != nil {
		logError("reverse proxy listen on :%d failed: %v", cfg.ProxyPort, err)
	}

	// 所有核心服务已启动，现在处理 dsh 和 node-pty 安装
	if os.Getenv("HARNESS_AUTOSTART") != "0" {
		boot.set(phaseStarting, "")
		// 先判断「有没有装 dsh 版本」：没装时不是故障，而是还没装（控制台概览页要
		// 用户去版本列表里下载 + 切换），因此用专门的 not-installed 阶段 —— 反代
		// 的等待页会显示「尚未安装 dsh 服务」并给出下一步，而不是一条启动失败报错。
		if dsh.selectedDshVersion() == "" {
			// 等待页是双语页（中英并排），因此 detail 也给中英各一行（该页的 CSS 是
			// white-space:pre-line）。这一页是后端渲染的独立页面，用不了前端的 i18n。
			boot.set(phaseNotInstalled,
				fmt.Sprintf("请在控制台「概览」页的 dsh 版本列表里下载一个版本，再点「切换」（安装目录 %s）\n"+
					"Download a version from the console version list and press Switch (install dir %s)",
					serverRootFor(&renv), serverRootFor(&renv)))
			logWarn("dsh is not installed: no version selected under %s", serverRootFor(&renv))
		} else if err := dsh.Start(); err != nil {
			boot.set(phaseFailed, err.Error())
			logError("autostart dsh failed: %v", err)
		} else {
			// dsh 启动成功，等待并捕获其一次性访问 token（新版 dsh 会打印
			// "dsh web: http://127.0.0.1:<port>/?token=XXX"），并从 Set-Cookie
			// 换取 dsh 会话 cookie，供反代转发时携带。
			boot.set(phaseAuth, "")
			captureDshSession(dsh)
			// 执行 node-pty 固定版本与清理（会等待目录生成）。传入空 home 由函数内部
			// 回退到启动时解析出的主目录（主目录不可切换，见 DshManager.effectiveHome）。若返回需要重启，
			// 则在 pnpm install 完成后重启 dsh 使 node-pty 1.2.0-beta.15 生效。
			boot.set(phaseDeps, "")
			restartNeeded, err := ensureNodePty(&renv, "")
			if err != nil {
				logWarn("[node-pty] setup failed: %v, dsh may not work", err)
			}
			if restartNeeded {
				logInfo("[node-pty] workspace changed by setup, restarting dsh")
				boot.set(phaseDeps, "依赖已更新，正在重启 dsh")
				if serr := dsh.Stop(); serr != nil {
					logError("[node-pty] restart dsh (stop) failed: %v", serr)
				} else if serr := dsh.Start(); serr != nil {
					logError("[node-pty] restart dsh (start) failed: %v", serr)
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
		logInfo("HARNESS_AUTOSTART=0, dsh not auto-started, skipping node-pty installation")
	}

	// 启动流水线收尾（或明确不自动启动）之后，重查一次插件市场版本：市场版本只能靠
	// `dsh plugin --profile web list` 查，而启动初期那次探测（见 newUpdateManager）很可能
	// 撞上「dsh 还没起来」而以「查不了」收场（界面显示「—」）。这里才是能查的时刻
	// （dsh 已启动 + 依赖已装好）；异步执行，不拖慢启动。
	upd.RefreshMarketAfterDshStart()

	// 等待退出信号
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	select {
	case s := <-sig:
		logInfo("received signal %v, shutting down", s)
	case <-stopCh:
		logInfo("shutdown requested from console, stopping")
	}

	dsh.Stop()
	// 停止控制台：终止全部终端会话（进程 + 历史文件），并删除临时会话目录
	admin.sessions.CloseAll()
	if createdSessionDir {
		os.RemoveAll(renv.SessionDir)
	}
	os.Remove(renv.AdminSock)
	// 退出前补出被抑制重复的汇总行，否则计数丢失。
	flushLog()
	logInfo("backend stopped")
}

// dshTokenWait 是等待 dsh 启动日志里那一行带 token 的访问地址的上限。dsh 要等
// 插件加载完成（loader.await 之后）才打印它，慢设备上会比端口监听晚很多，所以这个
// 值不能太短；超过它也不算彻底放弃 —— 后台补偿会继续等（见 StartAuthRepair）。
const dshTokenWait = 20 * time.Second

// captureDshSession 等待并捕获 dsh 的一次性访问 token，并用 token 换取 dsh
// 会话 cookie，供反向代理转发时携带。每次 dsh 启动都会生成新的 token，因此
// dsh 重启后需重新调用本函数。
//
// 落定（SessionSettled）的语义是「凭据确实可用」，不是「等待结束」：
//  1. 等待期间反代已用保留的 cookie 探测成功（EnsureAuthProbe / WaitToken 提前
//     结束）→ 直接返回；
//  2. 拿到 token 且换取成功 → 落定；
//  3. 没拿到 token（或换取失败）→ 先用上一次留下的 cookie 探测一次：dsh 的
//     dsh-auth-* 由持久签名密钥签发，跨重启仍然有效（Stop 也不清，见
//     forgetDshProcessLocked），插件市场自重启、node-pty 重启都属于这一类；
//  4. 没有可用凭据（明确 401、从未存过 cookie、或探测无结论）→ 标记本代
//     「拿不到凭据」（等待页显示可操作提示）并启动后台补偿（继续等 token，最长
//     5 分钟）；
//  5. 进程已退出 → 什么都不标，state() 会按 stopped 呈现。
//
// 旧实现无论拿没拿到 token 都在 defer 里落定，慢启动时会把用户放行到一个没有
// cookie 的 dsh，只见 401，而且不会自愈。
func captureDshSession(dsh *DshManager) {
	captureDshSessionWithin(dsh, dshTokenWait)
}

// captureDshSessionWithin 是 captureDshSession 的实现体，等待时长可注入（测试用
// 短窗口；生产固定 dshTokenWait）。
func captureDshSessionWithin(dsh *DshManager, wait time.Duration) {
	gen := dsh.SessionGen()
	tok := dsh.WaitToken(wait)
	// 反代的 EnsureAuthProbe 可能在等待期间用保留的旧 cookie 探测成功并落了定
	// （见 WaitToken）：本代凭据已可用，不必再兑换 token / 打「没等到 token」的警告。
	if dsh.SessionSettled() {
		return
	}
	if tok != "" {
		// 用 token 访问一次带 token 的地址，从 Set-Cookie 换取 dsh 会话 cookie，
		// 供反代转发时携带（访问不带 token 的 dsh 地址）。成功时不输出日志。
		if err := dsh.ExchangeToken(); err != nil {
			logWarn("[dsh] token exchange failed: %v", err)
		} else {
			dsh.markSessionSettledGen(gen)
			return
		}
	} else {
		logWarn("[dsh] no access token captured within %s", wait)
	}

	// 没有可用 token：先看上一代留下的 cookie 是否仍然有效（dsh 每次启动会重新
	// 打印 token，但签名密钥是持久的，cookie 不随重启失效）。
	// 从来没有存过 cookie 时不能走「探测」—— ProbeAuth 会直接判无效而不发请求，
	// 日志里若写成「dsh 拒绝了旧凭据」会误导排查（第一次启动本来就没有）。
	// 探测结果是三态：只有明确的 401 才算「凭据不可用」；404/5xx/连接错误说明 dsh
	// 还没服务到这条路由（插件还在加载），不能据此判定失败。
	if dsh.AuthCookie() == "" {
		logWarn("[dsh] no stored dsh session cookie; waiting for the access token")
	} else {
		switch dsh.ProbeAuth(dshAuthProbeTimeout) {
		case authProbeValid:
			logInfo("[dsh] existing dsh session cookie is still valid")
			dsh.markSessionSettledGen(gen)
			return
		case authProbeInvalid:
			logWarn("[dsh] dsh rejected the stored session cookie (http 401)")
		default:
			logWarn("[dsh] dsh session probe was inconclusive, will retry in the background")
		}
	}

	if !dsh.Running() {
		// 进程已经退出：这不是凭据问题，交给 state() 按 stopped 呈现。
		return
	}
	// 端口通了但凭据拿不到：停在等待页（phaseAuthFailed），后台继续等 token。
	// 慢设备上 token 可能晚于 dshTokenWait 才打印，补偿不是可选项。
	dsh.markAuthFailedGen(gen)
	dsh.StartAuthRepair(gen)
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
func startProxy(port int, auth *Auth, dsh *DshManager, boot *bootState, account *AdminMux) error {
	addr := ":" + strconv.Itoa(port)
	ln, err := netListen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: addr, Handler: newReverseProxy(auth, dsh, boot).withAccount(account)}

	proxyMu.Lock()
	oldSrv, oldLn := proxyServer, proxyLn
	proxyServer, proxyLn = srv, ln
	proxyMu.Unlock()

	go func() {
		// 端口切换（或进程退出）时监听会被主动关闭，Serve 返回的 ErrServerClosed /
		// net.ErrClosed 属正常收尾，不记为错误。
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			logError("proxy server error: %v", err)
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
			logError("close old proxy listener: %v", err)
		}
	}
	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, context.DeadlineExceeded) {
			logError("shutdown old proxy server: %v", err)
		}
	}
}
