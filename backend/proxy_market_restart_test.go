package main

// 本文件覆盖「反代之后 dshmarket 的一键重启」：dshmarket 从 1.66.x 起，除了重启
// 路由本身的栅栏（trustedRestartRequest），还让状态轮询回答「从当前这个页面发起的
// 重启能不能过栅栏」（restartReachableFrom → /dsh-market/status 的 restartReachable
// 字段，见 dshmarket/src/restart.ts 的 #782/#678）——前端拿到 false 就把「立即重启」
// 按钮藏起来。判据是「回环对端 + 无转发标记头 + Host 是回环 authority」。
//
// 反代现在对**所有** dsh 流量都不再追加转发标记头（见 proxy.go 的
// forwardingTraceHeaders）：dsh 只监听 127.0.0.1、控制台又是唯一入口且已鉴权，
// 逐路径伪装只会让新插件持续踩同一个 403。这里断言市场的重启/状态两条路确实像
// 「浏览器在回环上直连 dsh」，并顺手钉住普通业务路径也满足同一判据。
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

// marketStatusPath 是 dshmarket 的状态轮询路由（restartReachable 字段的来源）。
const marketStatusPath = "/dsh-market/status"

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

// --- 市场的回环栅栏：反代转发后看起来像同源回环直连 ---

func TestMarketRestartAndStatusLookLikeDirectLoopback(t *testing.T) {
	cases := []struct {
		name    string
		method  string
		target  string
		headers map[string]string
	}{
		{
			name:   "status 轮询（GET，浏览器不发 Origin）",
			method: http.MethodGet,
			target: proxyMountTestBase + marketStatusPath,
			// 上一跳/CDN 塞进来的转发标记头也必须被清掉。
			headers: map[string]string{
				"X-Forwarded-For":  "203.0.113.9",
				"Cf-Connecting-Ip": "203.0.113.9",
			},
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
			for _, name := range forwardingTraceHeaders {
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

// 全局策略：普通业务路径也满足「回环直连」判据 —— 转发头在 forward 里被整份删掉，
// 入站带来的那几份也一并清掉。这条曾经是反例（只有栅栏路径才伪装），改成全局后
// 它就是回归守卫：谁把 x-forwarded-* 加回来，插件栅栏就会重新 403。
func TestNormalRouteAlsoLooksLikeDirectLoopback(t *testing.T) {
	upstream, seen := recordUpstream(t)
	f := newMountedFixture(t, portOfURL(t, upstream.URL), proxyMountTestBase)
	f.boot.set(phaseReady, "")

	req := httptest.NewRequest(http.MethodGet, proxyMountTestBase+"/api/rpc", nil)
	req.Header.Set("Cookie", sessionCookieHeader())
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	req.Header.Set("X-Real-Ip", "203.0.113.9")
	req.Header.Set("Forwarded", "for=203.0.113.9")
	req.Header.Set("Cf-Connecting-Ip", "203.0.113.9")
	rec := httptest.NewRecorder()
	f.proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("应被转发到 dsh 上游: code=%d body=%q", rec.Code, rec.Body.String())
	}
	for _, name := range forwardingTraceHeaders {
		if hasHeader(seen.header, name) {
			t.Fatalf("普通路径也不应携带 %s（实际 %q）", name, seen.header.Get(name))
		}
	}
	if !marketForwardingTrace(seen) {
		t.Fatalf("普通路径也该被判定成回环直连: host=%q headers=%v", seen.host, seen.header)
	}
}

// 删除清单本身钉住：两个已知栅栏的并集一个都不能少（少一个就是那族插件 403）。
func TestForwardingTraceHeadersCoverKnownFences(t *testing.T) {
	want := []string{
		"Forwarded",         // dshmarket directLoopbackRequest
		"X-Forwarded-For",   // dshmarket + skill-explorer isProxiedRequest
		"X-Real-Ip",         // dshmarket directLoopbackRequest
		"Cf-Connecting-Ip",  // skill-explorer isProxiedRequest
		"X-Forwarded-Host",  // 反代自己以前追加的那两个
		"X-Forwarded-Proto", // （同上）
	}
	got := make(map[string]bool, len(forwardingTraceHeaders))
	for _, name := range forwardingTraceHeaders {
		got[http.CanonicalHeaderKey(name)] = true
	}
	for _, name := range want {
		if !got[http.CanonicalHeaderKey(name)] {
			t.Fatalf("forwardingTraceHeaders 少了 %s", name)
		}
	}
}
