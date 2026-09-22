package main

// 本文件覆盖「子路径挂载 + unix socket 监听」这条部署形态：平台网关（fnOS
// open-gateway）把 http://<fnip>:<port>/app/Harness/dsh 整段转发到本进程的
// dsh.sock，反代剥掉该前缀再转发给 dsh（挂载点用的是默认值，见
// HARNESS_PROXY_BASEURL）。
//
// dsh 0.1.7-alpha.1 起其前端产物完全按文档相对路径生成（index 由
// dsh-host-frontend-static 注入 <base href="./">，插件 bundle、/api、SSE、流 mux
// 都是去前导斜杠的相对形式），因此浏览器会自动把地址拼成 "<prefix>/api/..."，
// dsh 侧不需要任何 baseurl 配置——配对成立的唯一条件就是反代把前缀剥干净，
// 并把自留路径（/_login、/_logout、/_ready）与跳转目标补回前缀。
//
// 全部测试用真实监听端口模拟 dsh（httptest / net.Listen），不启动任何 dsh 进程。

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// proxyMountTestBase 取默认挂载前缀（/app/Harness/dsh，控制台 baseurl 之下的一层），
// 所有用例都按这个真实默认值换算前缀，等于顺带覆盖多层前缀的剥离与拼接。
const proxyMountTestBase = "/app/Harness/dsh"

// newMountedFixture 构造一个挂在 baseURL 下的反代（dshPort 指向真实监听端口）。
func newMountedFixture(t *testing.T, dshPort int, baseURL string) *proxyFixture {
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
	return &proxyFixture{
		proxy: newReverseProxyAt(NewAuth(), dsh, boot, baseURL),
		dsh:   dsh,
		boot:  boot,
	}
}

// --- 前缀剥离 ---

// 挂载内的请求必须按「剥掉前缀」的路径转发给 dsh：dsh 只认 /、/api、/plugins。
func TestProxyMountStripsPrefixOnForward(t *testing.T) {
	var gotURI string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.URL.RequestURI()
		_, _ = io.WriteString(w, "dsh-upstream")
	}))
	defer upstream.Close()

	f := newMountedFixture(t, portOfURL(t, upstream.URL), proxyMountTestBase)
	f.boot.set(phaseReady, "")

	for _, tc := range []struct{ target, wantURI string }{
		{proxyMountTestBase + "/api/rpc?x=1", "/api/rpc?x=1"},
		{proxyMountTestBase + "/plugins/events", "/plugins/events"},
		// 插件 bundle 的 URL 形如 /plugins/??a/client.js&rev=1：第一个 ? 开始
		// query，第二个 ? 是 query 文本的一部分，必须原样透传。
		{proxyMountTestBase + "/plugins/??a/client.js&rev=1", "/plugins/??a/client.js&rev=1"},
		{proxyMountTestBase + "/", "/"},
	} {
		rec := proxyGet(t, f.proxy, tc.target, sessionCookieHeader())
		if rec.Code != http.StatusOK || rec.Body.String() != "dsh-upstream" {
			t.Fatalf("%s: 应转发给 dsh, code=%d body=%q", tc.target, rec.Code, rec.Body.String())
		}
		if gotURI != tc.wantURI {
			t.Fatalf("%s: 上游看到的 URI = %q, want %q", tc.target, gotURI, tc.wantURI)
		}
	}
}

// 挂载点缺尾斜杠要补成目录：dsh 前端的 <base href="./"> 以目录为基准，缺斜杠
// 会让相对路径解析到站点根，资源与 API 全部跑出子路径。
func TestProxyMountRedirectsBarePrefixToDirectory(t *testing.T) {
	f := newMountedFixture(t, closedPort(t), proxyMountTestBase)

	rec := proxyGet(t, f.proxy, proxyMountTestBase+"?a=1", sessionCookieHeader())
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("GET 裸挂载点应 301, 实际 code=%d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != proxyMountTestBase+"/?a=1" {
		t.Fatalf("Location = %q, want %q", loc, proxyMountTestBase+"/?a=1")
	}

	// 非 GET 用 308 保留方法与请求体（XHR/POST 不能被降级成 GET）。
	req := httptest.NewRequest(http.MethodPost, proxyMountTestBase, strings.NewReader("x"))
	rec = httptest.NewRecorder()
	f.proxy.ServeHTTP(rec, req)
	if rec.Code != http.StatusPermanentRedirect {
		t.Fatalf("POST 裸挂载点应 308, 实际 code=%d", rec.Code)
	}
}

// 不属于本挂载的路径（网关配置错误）直接 404，而不是悄悄转发给 dsh。
func TestProxyMountRejectsForeignPath(t *testing.T) {
	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	defer upstream.Close()

	f := newMountedFixture(t, portOfURL(t, upstream.URL), proxyMountTestBase)
	f.boot.set(phaseReady, "")

	for _, target := range []string{"/", "/app/harness/", "/other/x"} {
		rec := proxyGet(t, f.proxy, target, sessionCookieHeader())
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: 应 404, 实际 code=%d", target, rec.Code)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("挂载外的路径不应转发给 dsh，实际转发 %d 次", got)
	}
}

// --- 自留路径与跳转目标都要带前缀 ---

func TestProxyMountLoginRedirectKeepsPrefix(t *testing.T) {
	f := newMountedFixture(t, closedPort(t), proxyMountTestBase)

	rec := proxyGet(t, f.proxy, proxyMountTestBase+"/api/rpc?x=1", "")
	if rec.Code != http.StatusFound {
		t.Fatalf("未登录应 302 到登录页, 实际 code=%d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != proxyMountTestBase+"/_login?next=%2Fapi%2Frpc%3Fx%3D1" {
		t.Fatalf("Location = %q, 期望带前缀且 next 为挂载内路径", loc)
	}
}

func TestProxyMountLoginRoundTrip(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "dsh-upstream")
	}))
	defer upstream.Close()

	f := newMountedFixture(t, portOfURL(t, upstream.URL), proxyMountTestBase)
	f.boot.set(phaseReady, "")

	// 登录页表单必须 POST 回带前缀的地址。
	rec := proxyGet(t, f.proxy, proxyMountTestBase+"/_login", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("登录页 code=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `action="`+proxyMountTestBase+`/_login"`) {
		t.Fatalf("登录页表单 action 缺前缀: %q", rec.Body.String())
	}

	// 登录成功后跳回 next（挂载内路径 → 补前缀）。语义与历史一致：next 取自
	// POST 的 query（登录页表单不携带 query，此时落到挂载根，对单页界面无碍）。
	form := strings.NewReader("password=" + proxyTestPassword)
	req := httptest.NewRequest(http.MethodPost, proxyMountTestBase+"/_login?next=%2Fapi%2Frpc", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	f.proxy.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("登录成功应 302, 实际 code=%d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != proxyMountTestBase+"/api/rpc" {
		t.Fatalf("登录后跳转 = %q, want %q", loc, proxyMountTestBase+"/api/rpc")
	}

	// 不带 next 时回落到挂载目录（不是站点根）。
	req = httptest.NewRequest(http.MethodPost, proxyMountTestBase+"/_login", strings.NewReader("password="+proxyTestPassword))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	f.proxy.ServeHTTP(rec, req)
	if loc := rec.Header().Get("Location"); loc != proxyMountTestBase+"/" {
		t.Fatalf("无 next 时登录跳转 = %q, want %q", loc, proxyMountTestBase+"/")
	}
}

func TestProxyMountLogoutRedirectKeepsPrefix(t *testing.T) {
	f := newMountedFixture(t, closedPort(t), proxyMountTestBase)

	rec := proxyGet(t, f.proxy, proxyMountTestBase+"/_logout", sessionCookieHeader())
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != proxyMountTestBase+"/_login" {
		t.Fatalf("登出应跳回带前缀的登录页, code=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}

// /_ready 是反代自留路径：挂载下必须命中 JSON 状态，且绝不能转发给 dsh。
func TestProxyMountReadyEndpointUnderPrefix(t *testing.T) {
	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	defer upstream.Close()

	f := newMountedFixture(t, portOfURL(t, upstream.URL), proxyMountTestBase)
	f.boot.set(phaseReady, "")

	rec := proxyGet(t, f.proxy, proxyMountTestBase+"/_ready", sessionCookieHeader())
	if rec.Code != http.StatusOK {
		t.Fatalf("带前缀的 /_ready code=%d body=%q", rec.Code, rec.Body.String())
	}
	var st proxyState
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("解析 /_ready 响应失败: %v (body=%q)", err, rec.Body.String())
	}
	if !st.Ready {
		t.Fatalf("带前缀的 /_ready 应报告就绪, body=%q", rec.Body.String())
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("/_ready 不应转发给 dsh，实际转发 %d 次", got)
	}
}

// 等待页的两处对外地址（Refresh 兜底与脚本轮询）都必须带前缀，否则浏览器会被
// 送出挂载目录（轮询 404 / 整页重载回到站点根）。
func TestProxyMountWaitingPageKeepsPrefix(t *testing.T) {
	f := newMountedFixture(t, closedPort(t), proxyMountTestBase)
	f.boot.set(phaseReady, "")
	f.pendSession()

	rec := proxyGet(t, f.proxy, proxyMountTestBase+"/api/rpc", sessionCookieHeader())
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "__DSH_WAIT__") {
		t.Fatalf("凭据未落定时应给等待页, code=%d", rec.Code)
	}
	if want := `var READY="` + proxyMountTestBase + `/_ready"`; !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("等待页轮询地址缺前缀，未找到 %s", want)
	}
	if want := "10; url=" + proxyMountTestBase + "/api/rpc"; rec.Header().Get("Refresh") != want {
		t.Fatalf("Refresh = %q, want %q", rec.Header().Get("Refresh"), want)
	}

	// 深层路径同样按完整挂载地址兜底重载。
	rec = proxyGet(t, f.proxy, proxyMountTestBase+"/a/b?x=1", sessionCookieHeader())
	if want := "10; url=" + proxyMountTestBase + "/a/b?x=1"; rec.Header().Get("Refresh") != want {
		t.Fatalf("深层路径 Refresh = %q, want %q", rec.Header().Get("Refresh"), want)
	}
}

// --- unix socket 端到端 ---

// startProxySocket 起的监听要真的能服务：建目录、建 socket、剥前缀转发，
// 并在 stopProxySocket 后删掉 socket 文件。
func TestProxySocketEndToEnd(t *testing.T) {
	var gotURI string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.URL.RequestURI()
		_, _ = io.WriteString(w, "dsh-upstream")
	}))
	defer upstream.Close()

	prev := GetConfig()
	cfg := prev
	cfg.DshPort = portOfURL(t, upstream.URL)
	cfg.AuthEnabled = false // 端到端只验证挂载与剥前缀，鉴权由上面的用例覆盖
	initConfig(&cfg)
	t.Cleanup(func() { initConfig(&prev) })

	dsh := newTestDshManager(t.TempDir(), "")
	boot := newBootState()
	boot.set(phaseReady, "")

	sock := filepath.Join(t.TempDir(), "target", "dsh.sock")
	startProxySocket(sock, proxyMountTestBase, NewAuth(), dsh, boot)
	t.Cleanup(stopProxySocket)

	if fi, err := os.Stat(sock); err != nil {
		t.Fatalf("socket 未创建: %v", err)
	} else if fi.Mode()&os.ModeSocket == 0 {
		t.Fatalf("%s 不是 socket 文件 (mode=%v)", sock, fi.Mode())
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", sock)
			},
		},
		// 重定向交给用例自己判断（裸挂载点的 301 需要看到原始响应）。
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	resp, err := client.Get("http://unix" + proxyMountTestBase + "/api/ping?x=1")
	if err != nil {
		t.Fatalf("经 socket 请求失败: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "dsh-upstream" {
		t.Fatalf("经 socket 转发失败: code=%d body=%q", resp.StatusCode, string(body))
	}
	if gotURI != "/api/ping?x=1" {
		t.Fatalf("上游 URI = %q, want /api/ping?x=1", gotURI)
	}

	// 裸挂载点重定向到目录。
	resp, err = client.Get("http://unix" + proxyMountTestBase)
	if err != nil {
		t.Fatalf("裸挂载点请求失败: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMovedPermanently || resp.Header.Get("Location") != proxyMountTestBase+"/" {
		t.Fatalf("裸挂载点 code=%d location=%q", resp.StatusCode, resp.Header.Get("Location"))
	}

	// 退出清理：socket 文件必须被删掉，避免留下陈旧 socket。
	stopProxySocket()
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Fatalf("stopProxySocket 后 socket 文件仍在: %v", err)
	}
}

// WebSocket 升级同样要按剥前缀后的路径转发：终端 / 流 mux 的 ws 请求由浏览器
// 按挂载地址发出（<prefix>/…），dsh 只认 /…，剥不干净就会连到不存在的路由。
func TestProxyMountUpgradeStripsPrefix(t *testing.T) {
	// 假 dsh：收下原始升级请求，记录请求行，回一个 101 就够验证路径。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("无法监听本地端口: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	gotLine := make(chan string, 4)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// 每个连接单独处理：反代放行前会先用一次“端口探测”连接（不发请求就
			// 关掉），不能把它当成升级请求。
			go func(c net.Conn) {
				defer c.Close()
				line, err := bufio.NewReader(c).ReadString('\n')
				if err != nil {
					return
				}
				line = strings.TrimSpace(line)
				if !strings.HasPrefix(line, "GET ") {
					return
				}
				select {
				case gotLine <- line:
				default:
				}
				_, _ = io.WriteString(c, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
			}(conn)
		}
	}()

	prev := GetConfig()
	cfg := prev
	cfg.DshPort = ln.Addr().(*net.TCPAddr).Port
	cfg.AuthEnabled = false // 鉴权分支已由其他用例覆盖，这里只看路径换算
	initConfig(&cfg)
	t.Cleanup(func() { initConfig(&prev) })

	dsh := newTestDshManager(t.TempDir(), "")
	boot := newBootState()
	boot.set(phaseReady, "")

	sock := filepath.Join(t.TempDir(), "dsh.sock")
	startProxySocket(sock, proxyMountTestBase, NewAuth(), dsh, boot)
	t.Cleanup(stopProxySocket)

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("连接 socket 失败: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	req := "GET " + proxyMountTestBase + "/api/stream HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		t.Fatalf("写升级请求失败: %v", err)
	}
	status, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("读取状态行失败: %v", err)
	}
	if !strings.Contains(status, "101") {
		t.Fatalf("升级未放行: %q", strings.TrimSpace(status))
	}

	select {
	case line := <-gotLine:
		if line != "GET /api/stream HTTP/1.1" {
			t.Fatalf("上游收到的请求行 = %q, want %q", line, "GET /api/stream HTTP/1.1")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("上游未收到升级请求")
	}
}

// 挂载前缀归一化：空/"/" 是根挂载；多余斜杠与缺前导斜杠都要被纠正，
// 多层前缀（默认值 /app/Harness/dsh）原样保留。
func TestProxyMountNormalization(t *testing.T) {
	for _, tc := range []struct {
		in       string
		wantRoot bool
		wantDir  string
	}{
		{"", true, "/"},
		{"/", true, "/"},
		{"  ", true, "/"},
		{"/app/Harness", false, "/app/Harness/"},
		{"/app/Harness/", false, "/app/Harness/"},
		{"app/Harness", false, "/app/Harness/"},
		{"app/Harness/dsh", false, "/app/Harness/dsh/"},
		{"/app/Harness/dsh/", false, "/app/Harness/dsh/"},
		{" /app/Harness/dsh ", false, "/app/Harness/dsh/"},
	} {
		m := newProxyMount(tc.in)
		if m.root() != tc.wantRoot || m.dir() != tc.wantDir {
			t.Fatalf("newProxyMount(%q) = {root:%v dir:%q}, want root=%v dir=%q", tc.in, m.root(), m.dir(), tc.wantRoot, tc.wantDir)
		}
		if got := m.join(authLogin); got != strings.TrimSuffix(tc.wantDir, "/")+authLogin {
			t.Fatalf("newProxyMount(%q).join(%s) = %q", tc.in, authLogin, got)
		}
	}

	// 多层前缀的 strip：挂载内路径剥掉整段前缀，前缀本身（含父级路径）必须区分开。
	deep := newProxyMount("/app/Harness/dsh")
	for _, tc := range []struct {
		path     string
		want     string
		wantOkay bool
	}{
		{"/app/Harness/dsh", "", true},
		{"/app/Harness/dsh/", "/", true},
		{"/app/Harness/dsh/api/x", "/api/x", true},
		// 父级路径（控制台自己的挂载点）不属于 dsh 挂载，不能放行。
		{"/app/Harness/", "", false},
		{"/app/Harness", "", false},
		{"/app/HarnessX/dsh", "", false},
	} {
		got, ok := deep.strip(tc.path)
		if got != tc.want || ok != tc.wantOkay {
			t.Fatalf("strip(%q) = (%q, %v), want (%q, %v)", tc.path, got, ok, tc.want, tc.wantOkay)
		}
	}

	// 根挂载保持历史行为：路径原样（含 query）用于转发与兜底重载。
	root := newProxyMount("")
	if got := root.joinURI("/api/x?y=1"); got != "/api/x?y=1" {
		t.Fatalf("根挂载 joinURI = %q", got)
	}
}

// 默认值守卫：dsh GUI 挂在 /app/Harness/dsh（控制台 baseurl 之下的一层），
// socket 默认落在平台应用目录下的 dsh.sock。
func TestRuntimeEnvProxyMountDefaults(t *testing.T) {
	for _, k := range []string{"TRIM_APPDEST", "HARNESS_PROXY_SOCK", "HARNESS_PROXY_BASEURL", "HARNESS_ADMIN_BASEURL"} {
		t.Setenv(k, "")
	}
	env := loadRuntimeEnv()
	if env.ProxyBaseURL != "/app/Harness/dsh" {
		t.Fatalf("ProxyBaseURL 默认 = %q, want /app/Harness/dsh", env.ProxyBaseURL)
	}
	if env.ProxySock != "/var/apps/Harness/target/dsh.sock" {
		t.Fatalf("ProxySock 默认 = %q, want /var/apps/Harness/target/dsh.sock", env.ProxySock)
	}
	// 与控制台 baseurl（/app/Harness，走 admin socket）分层：dsh 挂载在其之下一层，
	// 两者不会互相抢路径。
	m := newProxyMount(env.ProxyBaseURL)
	if want := env.AdminBaseURL + "/dsh/"; m.dir() != want {
		t.Fatalf("dsh 挂载 = %q, want 控制台 baseurl 之下一层 %q", m.dir(), want)
	}
}
