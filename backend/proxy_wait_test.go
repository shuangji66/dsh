package main

// 本文件覆盖「反代先于 dsh 监听」这一改动带来的行为：控制台启动期间反代端口就已经
// 在监听，访客先走登录鉴权、再看带阶段的等待页，dsh 完成启动（含换取会话凭据、
// 安装运行依赖）后等待页自动跳转；在此之前不得把请求转发给 dsh（否则会带着空
// cookie 被 dsh 拒绝，正是旧实现里“访问无响应/会话失效”的根源）。
//
// 全部测试用真实监听端口模拟 dsh（httptest），不启动任何 dsh 进程。

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const proxyTestPassword = "Abcd1234!"

type proxyFixture struct {
	proxy *reverseProxy
	dsh   *DshManager
	boot  *bootState
}

// newProxyFixture 构造一个开启登录鉴权的反代：dshPort 指向真实监听端口（模拟已
// 就绪的 dsh），或一个无人监听的端口（模拟 dsh 还没起来）。
func newProxyFixture(t *testing.T, dshPort int) *proxyFixture {
	t.Helper()
	prev := GetConfig()
	cfg := prev
	cfg.DshPort = dshPort
	cfg.AuthEnabled = true
	cfg.Password = proxyTestPassword
	initConfig(&cfg)
	t.Cleanup(func() { initConfig(&prev) })

	dsh := newTestDshManager(t.TempDir(), "")
	boot := newBootState()
	return &proxyFixture{proxy: newReverseProxy(NewAuth(), dsh, boot), dsh: dsh, boot: boot}
}

// pendSession 模拟 dsh 刚启动、会话凭据还在异步换取中（captureDshSession 尚未返回）。
func (f *proxyFixture) pendSession() {
	f.dsh.sessionMu.Lock()
	f.dsh.sessionGen++
	f.dsh.sessionMu.Unlock()
}

// listenHTTP 起一个真实监听的后端（模拟已就绪的 dsh），返回其端口。
func listenHTTP(t *testing.T, h http.Handler) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("无法监听本地端口: %v", err)
	}
	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().(*net.TCPAddr).Port
}

// closedPort 返回一个当下无人监听的端口（先监听拿到空闲端口再关掉）。
func closedPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("无法获取空闲端口: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func portOfURL(t *testing.T, raw string) int {
	t.Helper()
	i := strings.LastIndexByte(raw, ':')
	if i < 0 {
		t.Fatalf("无法从 %q 解析端口", raw)
	}
	port, err := strconv.Atoi(raw[i+1:])
	if err != nil {
		t.Fatalf("无法从 %q 解析端口: %v", raw, err)
	}
	return port
}

// sessionCookieHeader 造一份有效的控制台登录凭据。
func sessionCookieHeader() string {
	expire := time.Now().Add(time.Hour).Unix()
	// 三段式：<过期秒>.<nonce>.<mac>，mac 用密码 + 进程外随机密钥签名（见 auth.go 的
	// sessionSign）。用同一个进程内的 Auth 取密钥即可 —— 密钥来自同一个密钥文件。
	nonce := newSessionNonce()
	auth := NewAuth()
	return authCookie + "=" + strconv.FormatInt(expire, 10) + "." + nonce + "." +
		sessionSign(auth.sessionKey(proxyTestPassword), expire, nonce)
}

// proxyGet 直接调用反代 handler（httptest.NewRequest 的 RemoteAddr 非回环，
// 与真实浏览器流量一致）。
func proxyGet(t *testing.T, p http.Handler, target, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	return rec
}

// rawUpgradeStatus 用裸 TCP 发起一次 WebSocket 升级请求，返回状态行。
func rawUpgradeStatus(t *testing.T, addr, cookie string) string {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	req := "GET /ws HTTP/1.1\r\nHost: " + addr + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"
	if cookie != "" {
		req += "Cookie: " + cookie + "\r\n"
	}
	req += "\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		t.Fatalf("write: %v", err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("读取状态行失败: %v", err)
	}
	return strings.TrimSpace(line)
}

// fakeUpgradeUpstream 起一个只认 WebSocket 升级的假 dsh：回 101，并记录第一份升级
// 请求的完整请求头（用于断言转发给上游的 Cookie）。端口探测连接不发任何字节就断开
// （见 BackendChecker.quick），读到 EOF 直接忽略。
func fakeUpgradeUpstream(t *testing.T) (port int, headOfUpgrade func() string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("无法监听本地端口: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	var mu sync.Mutex
	var got string
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				var head strings.Builder
				for {
					line, err := br.ReadString('\n')
					if err != nil {
						return
					}
					head.WriteString(line)
					if line == "\r\n" || line == "\n" {
						break
					}
				}
				text := head.String()
				if !strings.HasPrefix(text, "GET ") {
					return
				}
				mu.Lock()
				got = text
				mu.Unlock()
				_, _ = io.WriteString(c, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
			}(conn)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, func() string {
		mu.Lock()
		defer mu.Unlock()
		return got
	}
}

// countedUpstream 起一个极简的假 dsh（只回固定 200），只统计「真正发过请求」的连接数：
// 端口探测连接不发任何字节（直接 EOF），不计入，因此计数等于转发用的上游连接数。
func countedUpstream(t *testing.T) (port int, conns func() int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("无法监听本地端口: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	var mu sync.Mutex
	n := 0
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				first := true
				for {
					line, err := br.ReadString('\n')
					if err != nil {
						return
					}
					if first {
						first = false
						if !strings.HasPrefix(line, "GET ") {
							return
						}
						mu.Lock()
						n++
						mu.Unlock()
					}
					if line == "\r\n" {
						if _, err := io.WriteString(c, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nContent-Type: text/plain\r\n\r\nok"); err != nil {
							return
						}
					}
				}
			}(conn)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, func() int {
		mu.Lock()
		defer mu.Unlock()
		return n
	}
}

// --- 鉴权先于就绪判断 ---

func TestProxyAsksForLoginBeforeBackendReady(t *testing.T) {
	f := newProxyFixture(t, closedPort(t))
	f.boot.set(phaseStarting, "")

	rec := proxyGet(t, f.proxy, "/", "")
	if rec.Code != http.StatusFound {
		t.Fatalf("未登录且 dsh 未就绪时应 302 到登录页，实际 %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, authLogin+"?next=") {
		t.Fatalf("重定向目标 = %q, 期望 %s?next=...", loc, authLogin)
	}

	// 等待期间登录页必须可用（旧实现把就绪判断放在鉴权之前，启动期间根本无法登录）。
	rec = proxyGet(t, f.proxy, authLogin, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "欢迎回来") {
		t.Fatalf("等待期间登录页不可用: code=%d", rec.Code)
	}

	// 就绪状态端点同样要求鉴权，避免向匿名访客泄露内部阶段/错误文本。
	rec = proxyGet(t, f.proxy, readyPath, "")
	if rec.Code != http.StatusFound {
		t.Fatalf("未登录访问 %s 应重定向登录，实际 %d", readyPath, rec.Code)
	}
}

// 面板后端的内部调用（回环 + /dsh-market/ 前缀）不再免鉴权：市场探测是到 dsh 端口的
// 直连（market.go 的 marketBusyFn），不经过反代；免鉴权分支在仓库内没有使用者，却让
// 本机任意进程（或同机嵌套反代）无口令拿到市场全部控制面接口。未登录时同样 302 登录页。
func TestProxyInternalRequestStillRequiresLogin(t *testing.T) {
	f := newProxyFixture(t, closedPort(t))
	f.boot.set(phaseStarting, "")

	req := httptest.NewRequest(http.MethodGet, "/dsh-market/status", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	f.proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("回环 + /dsh-market/ 在未登录时应重定向到登录页, 实际 code=%d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, authLogin+"?next=") {
		t.Fatalf("重定向目标 = %q, 期望 %s?next=...", loc, authLogin)
	}
}

// 等待页的 Refresh 兜底跳转不得被请求行里的协议相对地址劫持：根挂载下
// `GET //evil.com/` 会被 Go 原样保留（r.URL.RequestURI() == "//evil.com/"），
// 浏览器按协议相对地址解析 → 10 秒后把已登录用户送往外站（开放重定向）。
func TestProxyWaitingPageRefreshRejectsProtocolRelativeURI(t *testing.T) {
	f := newProxyFixture(t, closedPort(t))
	f.boot.set(phaseDeps, "")

	for _, target := range []string{"//evil.com/", "//evil.com", "///evil.com/x"} {
		rec := proxyGet(t, f.proxy, target, sessionCookieHeader())
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "__DSH_WAIT__") {
			t.Fatalf("%s: 应给等待页, code=%d", target, rec.Code)
		}
		refresh := rec.Header().Get("Refresh")
		if strings.Contains(refresh, "evil.com") {
			t.Fatalf("%s: Refresh 头不得携带外站地址, 实际 %q", target, refresh)
		}
		if refresh != "10; url=/" {
			t.Fatalf("%s: Refresh 应回落到挂载目录, 实际 %q", target, refresh)
		}
	}

	// 合法路径仍按原地址兜底重载（保留 query）。
	rec := proxyGet(t, f.proxy, "/a/b?x=1", sessionCookieHeader())
	if want := "10; url=/a/b?x=1"; rec.Header().Get("Refresh") != want {
		t.Fatalf("Refresh = %q, want %q", rec.Header().Get("Refresh"), want)
	}
}

// --- 等待页 ---

func TestProxyWaitingPageCarriesPhase(t *testing.T) {
	f := newProxyFixture(t, closedPort(t))
	f.boot.set(phaseDeps, "")

	rec := proxyGet(t, f.proxy, "/session/abc", sessionCookieHeader())
	if rec.Code != http.StatusOK {
		t.Fatalf("等待页状态码 = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("等待页 Content-Type = %q", ct)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("等待页必须 no-store，否则就绪后可能命中旧页面")
	}
	if !strings.Contains(rec.Header().Get("Refresh"), "url=/session/abc") {
		t.Fatalf("无 JS 兜底的 Refresh 头应保留原始路径，实际 %q", rec.Header().Get("Refresh"))
	}

	body := rec.Body.String()
	if !strings.Contains(body, `window.__DSH_WAIT__={"phase":"deps","ready":false}`) {
		t.Fatal("等待页未注入当前启动阶段")
	}
	if !strings.Contains(body, `var READY="/_ready"`) {
		t.Fatal("等待页脚本未指向 /_ready 轮询端点（根挂载不带前缀）")
	}
}

// 启动过程只给一句「等待服务就绪」，不暴露内部阶段（换取凭据 / 安装 node-pty），
// 且黄色排查提示的阈值是 30 秒。
func TestProxyWaitingPageHidesInternalPhases(t *testing.T) {
	f := newProxyFixture(t, closedPort(t))
	f.boot.set(phaseDeps, "")

	body := proxyGet(t, f.proxy, "/", sessionCookieHeader()).Body.String()
	if !strings.Contains(body, "正在等待 DeepSeek Harness 服务就绪") {
		t.Fatal("等待页缺少「等待服务就绪」的统一提示")
	}
	for _, leak := range []string{"node-pty", "安装运行依赖", "安装依赖", "换取凭据"} {
		if strings.Contains(body, leak) {
			t.Fatalf("等待页不应展示内部阶段细节: %q", leak)
		}
	}
	if !strings.Contains(body, "HINT_AFTER=30000") {
		t.Fatal("排查提示阈值应为 30 秒")
	}
}

// 失败原因可能含任意文本（含错误路径、插件名），注入脚本时必须转义。
func TestProxyWaitingPageEscapesFailureDetail(t *testing.T) {
	f := newProxyFixture(t, closedPort(t))
	f.boot.set(phaseFailed, `</script><script>alert(1)</script>`)

	body := proxyGet(t, f.proxy, "/", sessionCookieHeader()).Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatal("失败原因未转义就注入脚本，存在脚本注入风险")
	}
	if !strings.Contains(body, `\u003c/script\u003e`) {
		t.Fatal("失败原因应经 JSON 编码（< > 转义）后注入")
	}
}

// 等待页的注入点必须唯一：模板里每个占位符都只应出现一次（__WAIT_STATE__ 注入
// 阶段、__READY_URL__ 注入带挂载前缀的轮询地址）。
func TestWaitingPageHasSinglePlaceholder(t *testing.T) {
	for _, ph := range []string{"__WAIT_STATE__", "__READY_URL__"} {
		if n := strings.Count(waitingPageHTML, ph); n != 1 {
			t.Fatalf("%s 出现 %d 次, want 1", ph, n)
		}
	}
}

// --- 就绪判定（等待页 /_ready 与放行门禁必须是同一入口） ---

func TestProxyReadyStatePhases(t *testing.T) {
	cases := []struct {
		name       string
		up         bool
		bootPhase  string
		bootDetail string
		pend       bool
		wantPhase  string
		wantReady  bool
		wantDetail string
	}{
		{name: "启动流水线进行中即使端口已通也不放行", up: true, bootPhase: phaseDeps, wantPhase: phaseDeps},
		{name: "凭据未落定", up: true, bootPhase: phaseReady, pend: true, wantPhase: phaseAuth},
		{name: "端口未监听且进程不在", up: false, bootPhase: phaseReady, wantPhase: phaseStopped},
		{name: "启动失败", up: false, bootPhase: phaseFailed, bootDetail: "boom", wantPhase: phaseFailed, wantDetail: "boom"},
		// 端口通但凭据未落定时只把「流水线已收尾」的阶段改写成 auth；failed /
		// disabled 必须原样保留（否则 dsh 启动失败而端口被旧进程占着时，等待页
		// 只剩转圈、看不到失败原因）。
		{name: "启动失败且端口被占用时保留失败原因", up: true, bootPhase: phaseFailed, bootDetail: "boom", pend: true, wantPhase: phaseFailed, wantDetail: "boom"},
		{name: "端口被占用时保留未自动启动", up: true, bootPhase: phaseDisabled, pend: true, wantPhase: phaseDisabled},
		{name: "未自动启动", up: false, bootPhase: phaseDisabled, wantPhase: phaseDisabled},
		{name: "就绪", up: true, bootPhase: phaseReady, wantPhase: phaseReady, wantReady: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			port := closedPort(t)
			if tc.up {
				port = listenHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			}
			f := newProxyFixture(t, port)
			f.boot.set(tc.bootPhase, tc.bootDetail)
			if tc.pend {
				f.pendSession()
			}

			rec := proxyGet(t, f.proxy, readyPath, sessionCookieHeader())
			if rec.Code != http.StatusOK {
				t.Fatalf("状态码 = %d, want 200", rec.Code)
			}
			var st proxyState
			if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
				t.Fatalf("解析 %s 响应失败: %v (body=%s)", readyPath, err, rec.Body.String())
			}
			if st.Phase != tc.wantPhase || st.Ready != tc.wantReady {
				t.Fatalf("state = %+v, want phase=%s ready=%v", st, tc.wantPhase, tc.wantReady)
			}
			if st.Detail != tc.wantDetail {
				t.Fatalf("detail = %q, want %q", st.Detail, tc.wantDetail)
			}
		})
	}
}

// --- 转发 ---

// 全仓没有跨源调用方（后端只有反代这一处 CORS、前端也没有跨源请求）：不得无条件给
// 所有转发响应加上 access-control-allow-origin: *，否则任意站点都能读取已登录用户的
// 转发响应（dsh 页面与 API 内容）。
func TestProxyForwardAddsNoCORSHeader(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	f := newProxyFixture(t, portOfURL(t, upstream.URL))
	f.boot.set(phaseReady, "")

	rec := proxyGet(t, f.proxy, "/", sessionCookieHeader())
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("转发失败: code=%d body=%q", rec.Code, rec.Body.String())
	}
	if acao := rec.Header().Get("Access-Control-Allow-Origin"); acao != "" {
		t.Fatalf("反代不得自行追加 CORS 头, 实际 %q", acao)
	}
}

// 核心回归：dsh 端口已通但凭据还没落定时不得转发；凭据落定后立刻放行。
func TestProxyDoesNotForwardBeforeCredentialsSettled(t *testing.T) {
	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = io.WriteString(w, "dsh-upstream")
	}))
	defer upstream.Close()

	f := newProxyFixture(t, portOfURL(t, upstream.URL))
	f.boot.set(phaseReady, "")
	f.pendSession()

	rec := proxyGet(t, f.proxy, "/", sessionCookieHeader())
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "__DSH_WAIT__") {
		t.Fatalf("凭据未落定时应给等待页，实际 code=%d", rec.Code)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("凭据未落定时不应转发到 dsh，实际转发 %d 次", got)
	}

	// captureDshSession 完成后（标记落定）立即放行。
	f.dsh.markSessionSettled()
	rec = proxyGet(t, f.proxy, "/", sessionCookieHeader())
	if rec.Code != http.StatusOK || rec.Body.String() != "dsh-upstream" {
		t.Fatalf("凭据落定后应转发到 dsh，实际 code=%d body=%q", rec.Code, rec.Body.String())
	}
}

// 转发时必须携带 dsh 换来的会话 cookie（不是浏览器的控制台 cookie）。
func TestProxyForwardCarriesDshAuthCookie(t *testing.T) {
	var got string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Cookie")
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	f := newProxyFixture(t, portOfURL(t, upstream.URL))
	f.boot.set(phaseReady, "")
	f.dsh.setAuthCookie("dsh-auth-x=secret")

	if rec := proxyGet(t, f.proxy, "/", sessionCookieHeader()); rec.Body.String() != "ok" {
		t.Fatalf("转发失败: code=%d body=%q", rec.Code, rec.Body.String())
	}
	if got != "dsh-auth-x=secret" {
		t.Fatalf("上游收到的 Cookie = %q, want dsh-auth-x=secret", got)
	}
}

// --- WebSocket ---

// WS 升级与 HTTP 共用 state() 这一份放行判定：启动流水线还处于 deps（内部阶段，
// 收尾时还会重启一次 dsh）时，即使 dsh 端口已经能连通也必须 503，否则刚建立的连接
// 会在流水线收尾重启时失效。
func TestProxyWebSocketRejectedDuringBootPipeline(t *testing.T) {
	f := newProxyFixture(t, listenHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})))
	f.boot.set(phaseDeps, "")

	srv := httptest.NewServer(f.proxy)
	defer srv.Close()

	status := rawUpgradeStatus(t, srv.Listener.Addr().String(), sessionCookieHeader())
	if !strings.Contains(status, "503") {
		t.Fatalf("boot 流水线未收尾时的 WebSocket 状态行 = %q, want 503", status)
	}
}

// 流水线收尾 + 凭据落定 + 端口通（state() 报告 ready）：升级照旧放行。
func TestProxyWebSocketAllowedWhenStateReady(t *testing.T) {
	port, _ := fakeUpgradeUpstream(t)
	f := newProxyFixture(t, port)
	f.boot.set(phaseReady, "")

	srv := httptest.NewServer(f.proxy)
	defer srv.Close()

	status := rawUpgradeStatus(t, srv.Listener.Addr().String(), sessionCookieHeader())
	if !strings.Contains(status, "101") {
		t.Fatalf("就绪时的 WebSocket 状态行 = %q, want 101", status)
	}
}

// dsh 凭据为空是可达状态（旧版 dsh 不打印 token → 等待超时后仍算落定；ExchangeToken
// 失败），此时不得把控制台的 harness_session 交给 dsh 及其插件。
func TestProxyForwardDropsConsoleCookieWhenDshCredentialsMissing(t *testing.T) {
	var mu sync.Mutex
	var got []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, r.Header.Values("Cookie")...)
		mu.Unlock()
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	f := newProxyFixture(t, portOfURL(t, upstream.URL))
	f.boot.set(phaseReady, "")

	if rec := proxyGet(t, f.proxy, "/", sessionCookieHeader()); rec.Body.String() != "ok" {
		t.Fatalf("转发失败: code=%d body=%q", rec.Code, rec.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	for _, c := range got {
		if strings.Contains(c, authCookie) {
			t.Fatalf("dsh 凭据为空时不得转发控制台 cookie, 上游收到 %q", c)
		}
	}
	if len(got) != 0 {
		t.Fatalf("dsh 凭据为空时上游不应收到任何 Cookie, 实际 %q", got)
	}
}

// WS 升级重建头时同样要先清掉控制台 cookie。
func TestProxyUpgradeDropsConsoleCookieWhenDshCredentialsMissing(t *testing.T) {
	port, headOfUpgrade := fakeUpgradeUpstream(t)
	f := newProxyFixture(t, port)
	f.boot.set(phaseReady, "")

	srv := httptest.NewServer(f.proxy)
	defer srv.Close()

	status := rawUpgradeStatus(t, srv.Listener.Addr().String(), sessionCookieHeader())
	if !strings.Contains(status, "101") {
		t.Fatalf("WebSocket 状态行 = %q, want 101", status)
	}
	head := headOfUpgrade()
	if head == "" {
		t.Fatal("上游未收到升级请求")
	}
	if strings.Contains(head, authCookie) {
		t.Fatalf("dsh 凭据为空时升级请求不得携带控制台 cookie:\n%s", head)
	}
	for _, line := range strings.Split(head, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "cookie:") {
			t.Fatalf("dsh 凭据为空时升级请求不应带 Cookie 行: %q", line)
		}
	}
}

// 共享 transport 的核心收益：上游连接复用。旧实现每个请求新建 &http.Transport{}，
// 连接永不复用（每请求一次新 TCP 握手 + 一批 TIME_WAIT）。
func TestProxyForwardReusesUpstreamConnection(t *testing.T) {
	port, conns := countedUpstream(t)
	f := newProxyFixture(t, port)
	f.boot.set(phaseReady, "")

	for i := 1; i <= 3; i++ {
		rec := proxyGet(t, f.proxy, "/api/rpc?i="+strconv.Itoa(i), sessionCookieHeader())
		if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
			t.Fatalf("第 %d 次转发失败: code=%d body=%q", i, rec.Code, rec.Body.String())
		}
		if n := conns(); n != 1 {
			t.Fatalf("第 %d 次转发时上游已建立 %d 条连接, 期望始终复用同一条", i, n)
		}
	}
}

func TestProxyWebSocketRejectsBeforeCredentialsSettled(t *testing.T) {
	f := newProxyFixture(t, listenHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})))
	f.boot.set(phaseReady, "")
	f.pendSession()

	srv := httptest.NewServer(f.proxy)
	defer srv.Close()

	status := rawUpgradeStatus(t, srv.Listener.Addr().String(), sessionCookieHeader())
	if !strings.Contains(status, "503") {
		t.Fatalf("凭据未落定时的 WebSocket 状态行 = %q, want 503", status)
	}
}

func TestProxyWebSocketRequiresLogin(t *testing.T) {
	f := newProxyFixture(t, listenHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})))
	f.boot.set(phaseReady, "")

	srv := httptest.NewServer(f.proxy)
	defer srv.Close()

	status := rawUpgradeStatus(t, srv.Listener.Addr().String(), "")
	if !strings.Contains(status, "401") {
		t.Fatalf("未登录时的 WebSocket 状态行 = %q, want 401", status)
	}
}
