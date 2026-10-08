package main

// 本文件覆盖「反代之后 dshmarket 的一键重启」：dshmarket 从 1.66.x 起，除了重启
// 路由本身的栅栏（trustedRestartRequest），还让状态轮询回答「从当前这个页面发起的
// 重启能不能过栅栏」（restartReachableFrom → /dsh-market/status 的 restartReachable
// 字段，见 dshmarket/src/restart.ts 的 #782/#678）——前端拿到 false 就把「立即重启」
// 按钮藏起来。判据是「回环对端 + 无转发标记头 + Host 是回环 authority」。
//
// 反代本来就会把重启请求伪装成同源回环直连（否则 POST 一律 403），但只对
// /dsh-market/restart 做了伪装、给其它路径追加 x-forwarded-* —— 于是出现自相矛盾的
// 状态：POST /dsh-market/restart 能成功，前端却因为状态轮询被藏了按钮。
//
// 这里断言两条路径都伪装成「浏览器在回环上直连 dsh」，并保留反例：普通业务路径必须
// 照旧带转发标记头（伪装是按路径开的，不是全局关掉）。
//
// 全部测试用真实监听端口模拟 dsh（httptest），不启动任何 dsh 进程。

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// upstreamSeen 是上游（模拟 dsh）实际收到的请求现场。
type upstreamSeen struct {
	remoteAddr string
	host       string
	header     http.Header
}

// recordUpstream 起一个记录请求现场的上游。
func recordUpstream(t *testing.T) (*httptest.Server, *upstreamSeen) {
	t.Helper()
	seen := &upstreamSeen{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.remoteAddr = r.RemoteAddr
		seen.host = r.Host
		seen.header = r.Header.Clone()
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(upstream.Close)
	return upstream, seen
}

// --- 复刻 dshmarket/src/restart.ts 的判据（只用于断言，不参与生产逻辑） ---

// hasHeader 对应 Node 里 `request.headers[name] !== undefined`：**存在即命中**，
// 空值也算（反代自己写的是非空值，但判据要按上游实现来）。
func hasHeader(h http.Header, name string) bool {
	_, ok := h[http.CanonicalHeaderKey(name)]
	return ok
}

// jsLoopbackAuthority 复刻 dshmarket 的 loopbackAuthority：只认 127.0.0.1 /
// localhost / [::1]，带端口先截掉。
func jsLoopbackAuthority(authority string) bool {
	lower := strings.ToLower(authority)
	var name string
	if strings.HasPrefix(lower, "[") {
		if end := strings.Index(lower, "]"); end >= 0 {
			name = lower[:end+1]
		}
	} else {
		name = strings.SplitN(lower, ":", 2)[0]
	}
	return name == "127.0.0.1" || name == "localhost" || name == "[::1]"
}

// jsLoopbackPeer 对应 dshmarket 对 socket.remoteAddress 的三选一（含 IPv4-mapped）。
func jsLoopbackPeer(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	switch host {
	case "127.0.0.1", "::1":
		return true
	}
	return strings.TrimPrefix(host, "::ffff:") == "127.0.0.1"
}

// marketForwardingTrace 复刻 directLoopbackRequest 里与请求头有关的那半（对端 +
// 转发标记头 + Host），也就是 restartReachableFrom 的全部判据。
func marketForwardingTrace(seen *upstreamSeen) bool {
	if !jsLoopbackPeer(seen.remoteAddr) {
		return false
	}
	for _, name := range []string{"Forwarded", "X-Forwarded-For", "X-Real-Ip"} {
		if hasHeader(seen.header, name) {
			return false
		}
	}
	return jsLoopbackAuthority(seen.host)
}

// marketTrustedRestart 复刻 trustedRestartRequest（在 directLoopbackRequest 之上
// 再要求 Origin 存在且与 Host 同源）—— 重启 POST 真正要过的那道栅栏。
func marketTrustedRestart(seen *upstreamSeen) bool {
	if !marketForwardingTrace(seen) {
		return false
	}
	origin := seen.header.Get("Origin")
	if origin == "" {
		return false
	}
	rest := strings.TrimPrefix(origin, "http://")
	if rest == origin {
		rest = strings.TrimPrefix(origin, "https://")
		if rest == origin {
			return false
		}
	}
	return rest == seen.host
}

// --- 回环栅栏路径：伪装成同源回环直连 ---

func TestMarketFencedRoutesLookLikeDirectLoopback(t *testing.T) {
	cases := []struct {
		name    string
		method  string
		target  string
		headers map[string]string
	}{
		{
			name:   "status 轮询（GET，浏览器不发 Origin）",
			method: http.MethodGet,
			target: proxyMountTestBase + marketStatusRoute,
		},
		{
			name:   "restart（POST，带浏览器 Origin）",
			method: http.MethodPost,
			target: proxyMountTestBase + marketRestartRoute,
			headers: map[string]string{
				"Origin": "http://192.168.1.9:3079",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream, seen := recordUpstream(t)
			f := newMountedFixture(t, portOfURL(t, upstream.URL), proxyMountTestBase)
			f.boot.set(phaseReady, "")

			req := httptest.NewRequest(tc.method, tc.target, nil)
			req.Header.Set("Cookie", sessionCookieHeader())
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			f.proxy.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("应被转发到 dsh 上游: code=%d body=%q", rec.Code, rec.Body.String())
			}
			if seen.header == nil {
				t.Fatal("上游没有收到请求")
			}
			// 上游对端必须是回环：反代只 dial 127.0.0.1:<dshPort>。
			if !jsLoopbackPeer(seen.remoteAddr) {
				t.Fatalf("上游看到的对端 = %q，应为回环（栅栏要求 remoteAddress 是回环）", seen.remoteAddr)
			}
			// 任何一个转发标记头都会让 restartReachable 变 false / 重启 POST 吃 403。
			for _, name := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-Ip"} {
				if hasHeader(seen.header, name) {
					t.Fatalf("回环栅栏路径不应携带 %s（实际 %q）", name, seen.header.Get(name))
				}
			}
			// Host 必须是回环 authority：栅栏里唯一无法被伪造的判据。
			if want := "127.0.0.1:" + strconv.Itoa(portOfURL(t, upstream.URL)); seen.host != want {
				t.Fatalf("上游看到的 Host = %q, want %q", seen.host, want)
			}
			if !jsLoopbackAuthority(seen.host) {
				t.Fatalf("Host %q 不是回环 authority", seen.host)
			}
			// 状态轮询的「预测」与重启 POST 的栅栏必须同时成立 —— 否则就是
			// 「按钮点不到、接口却能通」的自相矛盾。
			if !marketForwardingTrace(seen) {
				t.Fatal("dshmarket 会判定 restartReachable=false，前端仍会藏起按钮")
			}
			if tc.method == http.MethodPost && !marketTrustedRestart(seen) {
				t.Fatalf("POST 过不了 trustedRestartRequest: origin=%q host=%q",
					seen.header.Get("Origin"), seen.host)
			}
		})
	}
}

// --- 反例：普通业务路径照旧带转发标记头 ---

func TestNormalRouteStillCarriesForwardingTrace(t *testing.T) {
	upstream, seen := recordUpstream(t)
	f := newMountedFixture(t, portOfURL(t, upstream.URL), proxyMountTestBase)
	f.boot.set(phaseReady, "")

	rec := proxyGet(t, f.proxy, proxyMountTestBase+"/api/rpc", sessionCookieHeader())
	if rec.Code != http.StatusOK {
		t.Fatalf("应被转发到 dsh 上游: code=%d body=%q", rec.Code, rec.Body.String())
	}
	if !hasHeader(seen.header, "X-Forwarded-For") || !hasHeader(seen.header, "X-Forwarded-Host") {
		t.Fatalf("普通路径应保留转发标记头，实际 %v", seen.header)
	}
	// 其余市场路径没有被伪装：只有重启栅栏那两条。
	if marketForwardingTrace(seen) {
		t.Fatal("普通路径不应被误判成回环直连")
	}
	if isLoopbackFencedRoute("/dsh-market/installed") || isLoopbackFencedRoute("/api/rpc") {
		t.Fatal("伪装范围只应是 /dsh-market/restart 与 /dsh-market/status")
	}
}

// 路径集合本身钉住：别在整理代码时把 status 从伪装名单里拿掉。
func TestIsLoopbackFencedRouteCoversBothMarketPaths(t *testing.T) {
	for _, path := range []string{marketRestartRoute, marketStatusRoute} {
		if !isLoopbackFencedRoute(path) {
			t.Fatalf("%s 必须走回环栅栏伪装", path)
		}
	}
	for _, path := range []string{"/dsh-market/installed", "/dsh-market/update", "/api/rpc", "/"} {
		if isLoopbackFencedRoute(path) {
			t.Fatalf("%s 不该被伪装成回环直连", path)
		}
	}
}
