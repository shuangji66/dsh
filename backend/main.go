package main

import (
	"context"
	"embed"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

//go:embed embed
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

// setupLogFile 根据环境变量打开日志文件；未设置则返回 nil，表示不落盘。
// 返回的清理函数负责关闭并删除日志文件（主进程停止时删除日志）。
func setupLogFile() func() {
	path := os.Getenv("HARNESS_LOG_FILE")
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

	// 配置日志文件输出（HARNESS_LOG_FILE）；主进程停止时删除日志
	cleanupLog := setupLogFile()
	defer cleanupLog()

	// Write PID file if environment variable is set
	pidFile := os.Getenv("HARNESS_PID_FILE")
	if pidFile != "" {
		if err := writePidFile(pidFile, os.Getpid()); err != nil {
			logger().Printf("failed to write pid file %s: %v", pidFile, err)
		} else {
			logger().Printf("pid written to %s", pidFile)
		}
		defer removePidFile(pidFile)
	}

	if _, err := net.Dial("unix", renv.AdminSock); err == nil {
		logger().Printf("Admin socket %s is already in use, another instance is running. Exiting.", renv.AdminSock)
		os.Exit(1)
	}
	os.Remove(renv.AdminSock)
	logger().Printf("Harness backend starting (pid=%d)", os.Getpid())

	cfg := LoadConfig(&renv)
	initConfig(&cfg)

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
	admin := newAdminMux(&renv, dsh, auth)
	admin.SetSPA(embeddedFrontend())

	go func() {
		if err := serveAdminSocket(admin); err != nil {
			logger().Printf("admin socket server: %v", err)
		}
	}()
	logger().Printf("admin console on unix socket %s baseurl %q", renv.AdminSock, renv.AdminBaseURL)

	startProxy(renv.ProxyPort, auth)

	if os.Getenv("HARNESS_AUTOSTART") != "0" {
		if err := dsh.Start(); err != nil {
			logger().Printf("autostart dsh: %v", err)
		}
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	select {
	case s := <-sig:
		logger().Printf("received signal %v, shutting down", s)
	case <-stopCh:
		logger().Printf("shutdown requested, stopping")
	}

	dsh.Stop()
	os.Remove(renv.AdminSock)
	logger().Printf("backend stopped")
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

var proxyServer *http.Server

func startProxy(port int, auth *Auth) {
	if proxyServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		proxyServer.Shutdown(ctx)
	}
	addr := ":" + strconv.Itoa(port)
	proxyServer = &http.Server{
		Addr:    addr,
		Handler: newReverseProxy(auth),
	}
	go func() {
		logger().Printf("reverse proxy listening on %s", addr)
		if err := proxyServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger().Printf("proxy server error: %v", err)
		}
	}()
}