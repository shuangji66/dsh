package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newFnOSEntryMux 构造一个只带环境（baseurl）的最小 AdminMux，用于换算飞牛入口。
// 入口现在挂在控制台 baseurl 之下的 /gui（dshUI），不再依赖 ProxyBaseURL。
func newFnOSEntryMux(adminBaseURL string) *AdminMux {
	m := &AdminMux{renv: &RuntimeEnv{AdminBaseURL: adminBaseURL, ProxyBaseURL: "/app/Harness/dsh"}}
	if base := strings.TrimRight(adminBaseURL, "/"); base != "" {
		m.dshUI = newReverseProxyMount(nil, nil, nil, base+"/gui", true)
	}
	return m
}

// 飞牛入口：控制台当前访问地址剥离控制台 baseurl 得到飞牛 OS 访问源，
// 再拼接控制台入口下的 dsh 反代路径 /gui/。
func TestFnOSEntryURL(t *testing.T) {
	m := newFnOSEntryMux("/app/Harness")

	cases := []struct {
		name string
		req  func() *http.Request
		want string
	}{
		{
			// 浏览器同源请求带的 Origin 就是控制台访问源（已含 scheme/host:port）
			name: "Origin",
			req: func() *http.Request {
				r := httptest.NewRequest("GET", "/app/Harness/api/settings", nil)
				r.Header.Set("Origin", "http://192.168.1.111:5666")
				return r
			},
			want: "http://192.168.1.111:5666/app/Harness/gui/",
		},
		{
			// Referer 是控制台页面的完整地址，需剥离控制台 baseurl
			name: "Referer",
			req: func() *http.Request {
				r := httptest.NewRequest("GET", "/app/Harness/api/settings", nil)
				r.Header.Set("Referer", "http://192.168.1.111:5666/app/Harness/?view=overview")
				return r
			},
			want: "http://192.168.1.111:5666/app/Harness/gui/",
		},
		{
			// 两个头都缺失时退回 Host，scheme 由转发头给出（网关反代 https）
			name: "HostFallback",
			req: func() *http.Request {
				r := httptest.NewRequest("GET", "/app/Harness/api/settings", nil)
				r.Host = "fnos.example:5667"
				r.Header.Set("X-Forwarded-Proto", "https")
				return r
			},
			want: "https://fnos.example:5667/app/Harness/gui/",
		},
		{
			// 自定义 baseurl 同样支持（网关与控制台 baseurl 都不在默认位置）
			name: "CustomBaseURL",
			req: func() *http.Request {
				r := httptest.NewRequest("GET", "/foo/api/settings", nil)
				r.Header.Set("Origin", "https://10.0.0.2")
				return r
			},
			want: "https://10.0.0.2/foo/gui/",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mux := m
			if tc.name == "CustomBaseURL" {
				mux = newFnOSEntryMux("/foo")
			}
			if got := mux.fnosEntryURL(tc.req()); got != tc.want {
				t.Fatalf("fnosEntryURL = %q, want %q", got, tc.want)
			}
		})
	}
}

// 没有控制台 baseurl（非网关部署）时不给出入口。
func TestFnOSEntryURLRootMount(t *testing.T) {
	m := newFnOSEntryMux("")
	r := httptest.NewRequest("GET", "/api/settings", nil)
	r.Header.Set("Origin", "http://192.168.1.111:5666")
	if got := m.fnosEntryURL(r); got != "" {
		t.Fatalf("无控制台 baseurl 时 fnosEntryURL = %q, want 空串", got)
	}
}

// 拿不到任何访问地址信息时返回空串，交由前端退回浏览器自身地址。
func TestFnOSEntryURLNoAddress(t *testing.T) {
	m := newFnOSEntryMux("/app/Harness")
	// httptest.NewRequest 默认 Host 为 "example.com"；显式清空以模拟完全没有地址。
	r := httptest.NewRequest("GET", "/app/Harness/api/settings", nil)
	r.Host = ""
	if got := m.fnosEntryURL(r); got != "" {
		t.Fatalf("无访问地址时 fnosEntryURL = %q, want 空串", got)
	}
}

// 转发头形如 "https, http" 时取最左侧（最原始）的值。
func TestFirstHeaderValue(t *testing.T) {
	if got := firstHeaderValue(" HTTPS , http "); got != "https" {
		t.Fatalf("firstHeaderValue = %q, want %q", got, "https")
	}
}
