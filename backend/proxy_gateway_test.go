package main

// 本文件覆盖「飞牛网关访问」这条通路：平台网关（fnOS open-gateway）在转发请求时会
// 注入 X-Trim-Username / X-Trim-Isadmin / X-Trim-Userid 三个身份头，代表飞牛 OS
// 已完成登录认证。因此网关那条线（Unix Socket 监听，见 startProxySocket）上的请求
// 跳过 harness 登录鉴权，但仍要经过就绪门禁（等待页照旧），并在登录列表里以
// 「网关访问」记录。
//
// 关键约束：只有网关线认这三个头。TCP 端口（局域网可达）上的请求即使带齐三个头也
// 必须照旧走登录鉴权 —— 否则任何人都能加三个头绕过端口鉴权。
//
// 全部测试用真实监听端口模拟 dsh（httptest / net.Listen），不启动任何 dsh 进程。

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// gatewayTestIP 是 httptest.NewRequest 默认的 RemoteAddr 主机（192.0.2.1:1234），
// 网关访客按「用户 + 客户端 IP」建键，用例里据此推算期望的记录 ID。
const gatewayTestIP = "192.0.2.1"

// gatewayHeaders 造一份网关注入的身份头。
func gatewayHeaders(uid int, username string) map[string]string {
	return map[string]string{
		headerTrimUsername: username,
		headerTrimIsAdmin:  "1",
		headerTrimUserID:   strconv.Itoa(uid),
	}
}

// proxyGetWithHeaders 发一个带指定请求头的请求。
func proxyGetWithHeaders(t *testing.T, p http.Handler, target string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	return rec
}

// --- 网关线：跳过鉴权、照旧记录 ---

// 网关线上的请求带身份头、不带任何 cookie，也应直接转发给 dsh（不再被重定向到
// 登录页），并在登录列表里记下一条「网关访问」。
func TestGatewayLineSkipsLoginAndRecordsVisitor(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "dsh-upstream")
	}))
	defer upstream.Close()

	f := newMountedFixture(t, portOfURL(t, upstream.URL), proxyMountTestBase)
	f.boot.set(phaseReady, "")

	rec := proxyGetWithHeaders(t, f.proxy, proxyMountTestBase+"/api/rpc", gatewayHeaders(1000, "alice"))
	if rec.Code != http.StatusOK || rec.Body.String() != "dsh-upstream" {
		t.Fatalf("网关访问应跳过鉴权直接转发: code=%d body=%q", rec.Code, rec.Body.String())
	}
	if sc := rec.Header().Get("Set-Cookie"); sc != "" {
		t.Fatalf("网关访问不应下发会话 cookie, 实际 %q", sc)
	}

	visitors := f.proxy.auth.Visitors()
	if len(visitors) != 1 {
		t.Fatalf("网关访问应记录 1 条登录记录, 实际 %d 条: %+v", len(visitors), visitors)
	}
	v := visitors[0]
	if v.Source != visitorSourceGateway {
		t.Fatalf("记录来源 = %q, want %q", v.Source, visitorSourceGateway)
	}
	if v.ID != gatewayVisitorID(1000, gatewayTestIP) {
		t.Fatalf("记录 ID = %q, want %q", v.ID, gatewayVisitorID(1000, gatewayTestIP))
	}
	if v.Username != "alice" {
		t.Fatalf("记录用户名 = %q, want alice", v.Username)
	}
	if !v.Admin {
		t.Fatal("X-Trim-Isadmin: 1 应记为管理员访问")
	}
	if !v.ExpiresAt.IsZero() {
		t.Fatalf("网关访问没有登录有效期，ExpiresAt 应为零值, 实际 %v", v.ExpiresAt)
	}
	if v.LastAccess.IsZero() {
		t.Fatal("网关访问应记录最近访问时间")
	}
}

// 同一飞牛用户在**同一环境（同一客户端 IP）**下的多次访问合并为一条（网关不签会话
// cookie，只能按用户 + IP 标识），不同用户各占一条。
func TestGatewayVisitorKeyedByFnOSUserAndIP(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	f := newMountedFixture(t, portOfURL(t, upstream.URL), proxyMountTestBase)
	f.boot.set(phaseReady, "")

	for i := 0; i < 3; i++ {
		proxyGetWithHeaders(t, f.proxy, proxyMountTestBase+"/api/rpc", gatewayHeaders(1000, "alice"))
	}
	proxyGetWithHeaders(t, f.proxy, proxyMountTestBase+"/api/rpc", gatewayHeaders(1001, "bob"))

	visitors := f.proxy.auth.Visitors()
	if len(visitors) != 2 {
		t.Fatalf("不同用户各占一条、同一用户同一环境合并，期望 2 条, 实际 %d 条: %+v", len(visitors), visitors)
	}
}

// 同一个人从不同环境（网络）访问飞牛时 IP 不同：各占一条记录，并各自展示来源 IP，
// 而不是反复刷新同一条。
func TestGatewayVisitorSeparatesEnvironmentsByIP(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	f := newMountedFixture(t, portOfURL(t, upstream.URL), proxyMountTestBase)
	f.boot.set(phaseReady, "")

	// 平台网关经 X-Real-Ip（或 X-Forwarded-For）告知真实客户端地址。
	visit := func(ip string) {
		hdr := gatewayHeaders(1000, "alice")
		hdr["X-Real-Ip"] = ip
		proxyGetWithHeaders(t, f.proxy, proxyMountTestBase+"/api/rpc", hdr)
	}
	visit("203.0.113.7")  // 外网环境
	visit("203.0.113.7")  // 同一环境再访问：刷新同一条
	visit("192.168.1.20") // 内网环境

	visitors := f.proxy.auth.Visitors()
	if len(visitors) != 2 {
		t.Fatalf("不同环境应各占一条、同环境刷新同一条，期望 2 条, 实际 %d 条: %+v", len(visitors), visitors)
	}
	seen := map[string]string{} // ip -> id
	for _, v := range visitors {
		if v.Source != visitorSourceGateway || v.Username != "alice" {
			t.Fatalf("记录字段不符: %+v", v)
		}
		seen[v.IP] = v.ID
	}
	for _, ip := range []string{"203.0.113.7", "192.168.1.20"} {
		if id, ok := seen[ip]; !ok {
			t.Fatalf("缺少 %s 环境的记录: %+v", ip, visitors)
		} else if id != gatewayVisitorID(1000, ip) {
			t.Fatalf("%s 的记录 ID = %q, want %q", ip, id, gatewayVisitorID(1000, ip))
		}
	}
}

// 等待界面照旧：dsh 未就绪时网关访问拿到等待页（而不是被放行或被重定向登录），
// 就绪状态端点也仍然可用（等待页脚本要靠它跳转）。
func TestGatewayLineStillWaitsForReady(t *testing.T) {
	f := newMountedFixture(t, closedPort(t), proxyMountTestBase)
	f.boot.set(phaseStarting, "")

	rec := proxyGetWithHeaders(t, f.proxy, proxyMountTestBase+"/api/rpc", gatewayHeaders(1000, "alice"))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "__DSH_WAIT__") {
		t.Fatalf("未就绪时应给等待页, code=%d body=%.120q", rec.Code, rec.Body.String())
	}
	if len(f.proxy.auth.Visitors()) != 0 {
		t.Fatal("未就绪（未真正放行）时不应记入登录列表")
	}

	rec = proxyGetWithHeaders(t, f.proxy, proxyMountTestBase+readyPath, gatewayHeaders(1000, "alice"))
	if rec.Code != http.StatusOK {
		t.Fatalf("网关访问应能轮询 %s, code=%d", readyPath, rec.Code)
	}
	var st proxyState
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("解析 /_ready 响应失败: %v", err)
	}
	if st.Ready {
		t.Fatal("dsh 未就绪时 /_ready 不应报告 ready")
	}
}

// --- 端口线（TCP）：不认网关注入的头 ---

// 局域网可达的 TCP 端口线必须照旧要求登录：伪造三个身份头不能绕过鉴权，也不能被
// 记成「网关访问」。
func TestPortLineIgnoresForgedGatewayHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "dsh-upstream")
	}))
	defer upstream.Close()

	f := newProxyFixture(t, portOfURL(t, upstream.URL)) // 根挂载 = TCP 端口线
	f.boot.set(phaseReady, "")

	rec := proxyGetWithHeaders(t, f.proxy, "/api/rpc", gatewayHeaders(1000, "alice"))
	if rec.Code != http.StatusFound {
		t.Fatalf("端口线伪造网关身份头仍应重定向到登录页, code=%d body=%.120q", rec.Code, rec.Body.String())
	}
	// next 为空查询串时带一个尾随 "?"（历史行为，见 proxy.go 的 safeNext 调用），
	// 这里只断言「落在登录页且 next 指向被拦下的路径」。
	if loc := rec.Header().Get("Location"); loc != authLogin+"?next=%2Fapi%2Frpc%3F" {
		t.Fatalf("重定向目标 = %q, want %s?next=...", loc, authLogin)
	}
	if len(f.proxy.auth.Visitors()) != 0 {
		t.Fatal("未通过鉴权的请求不应记入登录列表")
	}

	// 带有效会话 cookie 时照旧放行，且记的是端口访客（而不是网关访客）。
	rec = proxyGet(t, f.proxy, "/api/rpc", sessionCookieHeader())
	if rec.Code != http.StatusOK || rec.Body.String() != "dsh-upstream" {
		t.Fatalf("端口线带 cookie 应照旧放行, code=%d body=%q", rec.Code, rec.Body.String())
	}
	visitors := f.proxy.auth.Visitors()
	if len(visitors) != 1 || visitors[0].Source != visitorSourcePort {
		t.Fatalf("端口访问应记为 port 来源, 实际 %+v", visitors)
	}
}

// WebSocket 升级与 HTTP 共用同一份判定：网关线上带身份头时不再被 401 挡下。
// httptest.ResponseRecorder 不支持 Hijack，通过鉴权后以 500（hijack not supported）
// 结束，未通过鉴权则是 401 —— 用这两个状态码区分是否被鉴权拦下。
func TestGatewayLineUpgradeSkipsLogin(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer upstream.Close()

	f := newMountedFixture(t, portOfURL(t, upstream.URL), proxyMountTestBase)
	f.boot.set(phaseReady, "")

	wsGet := func(hdr map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, proxyMountTestBase+"/ws", nil)
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Connection", "Upgrade")
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		r := httptest.NewRecorder()
		f.proxy.ServeHTTP(r, req)
		return r
	}

	if rec := wsGet(map[string]string{}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("网关线上无身份头的升级请求应 401, code=%d", rec.Code)
	}
	if rec := wsGet(gatewayHeaders(1000, "alice")); rec.Code == http.StatusUnauthorized {
		t.Fatal("网关线上带身份头的升级请求不应被鉴权挡下")
	}
}

// 网关线上不带任何身份头（例如本机进程直连 socket）时退回原有 cookie 鉴权，
// 不因为「这条线可信」就无条件放行。
func TestGatewayLineWithoutHeadersFallsBackToLogin(t *testing.T) {
	f := newMountedFixture(t, closedPort(t), proxyMountTestBase)
	f.boot.set(phaseReady, "")

	rec := proxyGetWithHeaders(t, f.proxy, proxyMountTestBase+"/api/rpc", nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("无身份头应重定向到登录页, code=%d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != proxyMountTestBase+"/_login?next=%2Fapi%2Frpc%3F" {
		t.Fatalf("重定向目标 = %q, 应带挂载前缀", loc)
	}

	// 身份头不完整（缺用户名）同样不算网关访问。
	partial := map[string]string{headerTrimUserID: "1000"}
	if rec := proxyGetWithHeaders(t, f.proxy, proxyMountTestBase+"/api/rpc", partial); rec.Code != http.StatusFound {
		t.Fatalf("身份头不完整应重定向到登录页, code=%d", rec.Code)
	}
}

// startProxySocket 起的监听必须真的按网关线构造（信任身份头）：端到端验证
// 「socket + 三个头 + 无 cookie → 200」。
func TestGatewaySocketEndToEndSkipsLogin(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "dsh-upstream")
	}))
	defer upstream.Close()

	prev := GetConfig()
	cfg := prev
	cfg.DshPort = portOfURL(t, upstream.URL)
	cfg.AuthEnabled = true
	cfg.Password = proxyTestPassword
	initConfig(&cfg)
	t.Cleanup(func() { initConfig(&prev) })

	dsh := newTestDshManager(t.TempDir(), "")
	boot := newBootState()
	boot.set(phaseReady, "")

	sock := t.TempDir() + "/target/dsh.sock"
	startProxySocket(sock, proxyMountTestBase, NewAuth(), dsh, boot)
	t.Cleanup(stopProxySocket)

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	req, err := http.NewRequest(http.MethodGet, "http://unix"+proxyMountTestBase+"/api/rpc", nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range gatewayHeaders(1000, "alice") {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("经 socket 请求失败: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "dsh-upstream" {
		t.Fatalf("网关访问应跳过鉴权: code=%d body=%q", resp.StatusCode, string(body))
	}
}

// --- 登录列表：网关条目不可注销、按闲置清理 ---

func TestGatewayVisitorCannotBeRevoked(t *testing.T) {
	auth := NewAuth()
	auth.recordGatewayVisitor(gatewayVisitor{
		ID: gatewayVisitorID(1000, gatewayTestIP), UserID: 1000, Username: "alice", IP: gatewayTestIP,
	})

	if auth.RevokeVisitor(gatewayVisitorID(1000, gatewayTestIP)) {
		t.Fatal("网关访问没有会话凭据可吊销，注销应返回 false")
	}
	if got := len(auth.Visitors()); got != 1 {
		t.Fatalf("注销失败后网关条目应原样保留, 实际 %d 条", got)
	}
	if v := auth.Visitors()[0]; v.IP != gatewayTestIP {
		t.Fatalf("网关条目应记录来源 IP, 实际 %q", v.IP)
	}

	// 接口层同样明确拒绝（而不是假装成功）。
	mux := &AdminMux{auth: auth}
	req := httptest.NewRequest(http.MethodDelete, "/api/visitors",
		strings.NewReader(`{"id":"`+gatewayVisitorID(1000, gatewayTestIP)+`"}`))
	rec := httptest.NewRecorder()
	mux.handleDeleteVisitor(rec, req)
	var resp struct {
		OK      bool   `json:"ok"`
		Deleted bool   `json:"deleted"`
		Msg     string `json:"msg"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v (body=%q)", err, rec.Body.String())
	}
	if !resp.OK || resp.Deleted {
		t.Fatalf("网关条目不应被注销, resp=%+v", resp)
	}
}

// 网关条目没有登录有效期，靠闲置时长清理（复用登录有效期），避免列表长期堆积早已离开的用户。
func TestGatewayVisitorPurgedWhenIdle(t *testing.T) {
	auth := NewAuth()
	auth.recordGatewayVisitor(gatewayVisitor{
		ID: gatewayVisitorID(1000, gatewayTestIP), UserID: 1000, Username: "alice", IP: gatewayTestIP,
	})
	// 端口访客（带登录有效期）同时存在，验证两类条目的清理互不影响。
	auth.visitors.record("tok", "10.0.0.9", time.Now().Add(time.Hour).Unix())

	auth.visitors.mu.Lock()
	auth.visitors.byToken[gatewayVisitorID(1000, gatewayTestIP)].LastAccess = time.Now().Add(-gatewayVisitorIdleTTL() - time.Minute)
	auth.visitors.mu.Unlock()

	visitors := auth.Visitors()
	if len(visitors) != 1 || visitors[0].Source != visitorSourcePort {
		t.Fatalf("闲置超时的网关条目应被清理、端口条目应保留, 实际 %+v", visitors)
	}
}

// withAuthTTLHours 临时把配置里的登录有效期改成 hours，测试结束还原。
func withAuthTTLHours(t *testing.T, hours int) {
	t.Helper()
	cfgLock.Lock()
	old := cfg.AuthTTLHours
	cfg.AuthTTLHours = hours
	cfgLock.Unlock()
	t.Cleanup(func() {
		cfgLock.Lock()
		cfg.AuthTTLHours = old
		cfgLock.Unlock()
	})
}

// 网关条目的闲置清除时长复用登录有效期（authTTLHours），不另设固定值。
func TestGatewayVisitorIdleTTLUsesAuthTTL(t *testing.T) {
	const id = "gateway-ttl-test"
	auth := NewAuth()
	auth.visitors.recordGateway(id, gatewayTestIP, "alice", false)
	// 闲置 90 分钟：登录有效期 2 小时时仍在窗口内，1 小时时应被清除。
	auth.visitors.mu.Lock()
	auth.visitors.byToken[id].LastAccess = time.Now().Add(-90 * time.Minute)
	auth.visitors.mu.Unlock()

	withAuthTTLHours(t, 2)
	if got := auth.Visitors(); len(got) != 1 {
		t.Fatalf("闲置 90 分钟未达登录有效期（2 小时），条目应保留, 实际 %+v", got)
	}

	withAuthTTLHours(t, 1)
	if got := auth.Visitors(); len(got) != 0 {
		t.Fatalf("闲置 90 分钟超过登录有效期（1 小时），条目应被清除, 实际 %+v", got)
	}

	// 未配置（<=0）时与登录一致地回退到 4 小时。
	withAuthTTLHours(t, 0)
	if got := gatewayVisitorIdleTTL(); got != 4*time.Hour {
		t.Fatalf("未配置登录有效期时应回退 4 小时, 实际 %v", got)
	}
}
