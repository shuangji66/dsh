package main

// 本文件覆盖「技能中心插件（@linxin666/dsh-client-ui-skill-explorer）经反代访问」
// 这条链路：它的九条宿主路由（list / read / set-enabled / set-provider-enabled /
// create / update / delete / import / health）共用同一个 guard —— loopback 直连，
// 或 remoteWebUiPairing 提供的「配对设备」cookie；本部署没有那个配对服务，所以
// 任何非 loopback 直连的请求都吃 403 `forbidden: loopback-only`。
//
// 反代现在对**所有** dsh 流量都不再追加转发标记头（见 proxy.go 的
// forwardingTraceHeaders）：dsh 只监听 127.0.0.1、控制台又是唯一入口且已鉴权，
// 逐路径伪装只会让新插件持续踩同一个 403。本文件从插件侧做对照断言：九条路由
// （两种挂载形态、含 POST 的 Origin 改写）转发到上游后都要满足插件的
// isLoopbackRequest，否则界面拿到的仍是 403。
//
// 全部测试用真实监听端口模拟 dsh（httptest），不启动任何 dsh 进程。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

// skillExplorerRoutes 与插件 ROUTES 常量一一对应（lib/types/routes.d.ts）。
var skillExplorerRoutes = []string{
	"/api/dsh-skill-explorer/list",
	"/api/dsh-skill-explorer/read",
	"/api/dsh-skill-explorer/set-enabled",
	"/api/dsh-skill-explorer/set-provider-enabled",
	"/api/dsh-skill-explorer/create",
	"/api/dsh-skill-explorer/update",
	"/api/dsh-skill-explorer/delete",
	"/api/dsh-skill-explorer/import",
	"/api/dsh-skill-explorer/health",
}

// skillExplorerLoopback 复刻插件的 isLoopbackRequest（lib/index.js 的 loopback.ts）：
// socket 对端回环 + 无 cf-connecting-ip / x-forwarded-for + Host 是回环 authority +
// 不是 cross-site，且 Origin（若存在）与 Host 同源。配对设备那条允许路径在本部署
// 不成立（没有 remoteWebUiPairing 服务），所以这里只断言回环这半边。
func skillExplorerLoopback(seen *upstreamSeen) bool {
	if !jsLoopbackPeer(seen.remoteAddr) {
		return false
	}
	if hasHeader(seen.header, "Cf-Connecting-Ip") || hasHeader(seen.header, "X-Forwarded-For") {
		return false
	}
	if !jsLoopbackAuthority(seen.host) {
		return false
	}
	if seen.header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := seen.header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Host == seen.host
}

// 经网关（子路径挂载）与经端口（根挂载）两种形态都要过栅栏；入站请求刻意带上
// CDN / 上一跳塞进来的转发标记头与跨站标记，断言反代把它们全部清掉并改写同源。
func TestSkillExplorerRoutesLookLikeDirectLoopback(t *testing.T) {
	type fenceCase struct {
		name    string
		method  string
		path    string
		root    bool // true = 端口根挂载（无子路径前缀）；false = 网关子路径挂载
		headers map[string]string
	}

	cases := make([]fenceCase, 0, len(skillExplorerRoutes)+2)
	// 九条路由逐个过一遍：插件侧 guard 虽然共用，但反代的挂载剥离/请求头处理
	// 是按路径做的，漏一条就是那个接口 403。
	for _, route := range skillExplorerRoutes {
		cases = append(cases, fenceCase{
			name:   "网关 " + route,
			method: http.MethodGet,
			path:   proxyMountTestBase + route,
			headers: map[string]string{
				"Cf-Connecting-Ip": "203.0.113.9",
				"X-Forwarded-For":  "203.0.113.9",
				"X-Real-Ip":        "203.0.113.9",
				"Forwarded":        "for=203.0.113.9",
			},
		})
	}

	cases = append(cases,
		fenceCase{
			name:   "set-enabled（POST，公网 Origin + 跨站标记）",
			method: http.MethodPost,
			path:   proxyMountTestBase + "/api/dsh-skill-explorer/set-enabled",
			headers: map[string]string{
				"Origin":           "https://fn.example.com:3333",
				"Sec-Fetch-Site":   "cross-site",
				"X-Forwarded-For":  "203.0.113.9",
				"X-Forwarded-Host": "fn.example.com:3333",
			},
		},
		fenceCase{
			name:   "list（GET，端口根挂载）",
			method: http.MethodGet,
			path:   "/api/dsh-skill-explorer/list",
			root:   true,
			headers: map[string]string{
				"X-Forwarded-Proto": "https",
			},
		},
	)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream, seen := recordUpstream(t)
			port := portOfURL(t, upstream.URL)
			var proxy http.Handler
			if tc.root {
				f := newProxyFixture(t, port)
				f.boot.set(phaseReady, "")
				proxy = f.proxy
			} else {
				f := newMountedFixture(t, port, proxyMountTestBase)
				f.boot.set(phaseReady, "")
				proxy = f.proxy
			}

			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Cookie", sessionCookieHeader())
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			proxy.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("应被转发到 dsh 上游: code=%d body=%q", rec.Code, rec.Body.String())
			}
			if seen.header == nil {
				t.Fatal("上游没有收到请求")
			}
			// 转发标记头一个都不能留：插件看 cf-connecting-ip / x-forwarded-for 就拒。
			for _, name := range forwardingTraceHeaders {
				if hasHeader(seen.header, name) {
					t.Fatalf("回环栅栏路径不应携带 %s（实际 %q）", name, seen.header.Get(name))
				}
			}
			// Host 是栅栏里唯一无法被伪造的判据，必须是回环 authority。
			if want := "127.0.0.1:" + strconv.Itoa(port); seen.host != want {
				t.Fatalf("上游看到的 Host = %q, want %q", seen.host, want)
			}
			// 插件的 isLoopbackRequest 必须判真，否则界面拿到的仍是 403。
			if !skillExplorerLoopback(seen) {
				t.Fatalf("插件会判成远端请求（403 forbidden: loopback-only）: host=%q origin=%q sec-fetch-site=%q headers=%v",
					seen.host, seen.header.Get("Origin"), seen.header.Get("Sec-Fetch-Site"), seen.header)
			}
		})
	}
}
