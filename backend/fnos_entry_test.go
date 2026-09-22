package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// newFnOSEntryMux 构造一个只带环境（baseurl）的最小 AdminMux，用于换算飞牛入口。
func newFnOSEntryMux(adminBaseURL, proxyBaseURL string) *AdminMux {
	return &AdminMux{renv: &RuntimeEnv{AdminBaseURL: adminBaseURL, ProxyBaseURL: proxyBaseURL}}
}

// 飞牛入口：控制台当前访问地址剥离控制台 baseurl 得到飞牛 OS 访问源，
// 再拼接 dsh 服务挂载的 baseurl。
func TestFnOSEntryURL(t *testing.T) {
	m := newFnOSEntryMux("/app/Harness", "/app/Harness/dsh")

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
			want: "http://192.168.1.111:5666/app/Harness/dsh",
		},
		{
			// Referer 是控制台页面的完整地址，需剥离控制台 baseurl
			name: "Referer",
			req: func() *http.Request {
				r := httptest.NewRequest("GET", "/app/Harness/api/settings", nil)
				r.Header.Set("Referer", "http://192.168.1.111:5666/app/Harness/?view=overview")
				return r
			},
			want: "http://192.168.1.111:5666/app/Harness/dsh",
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
			want: "https://fnos.example:5667/app/Harness/dsh",
		},
		{
			// 自定义 baseurl 同样支持（网关与 dsh 挂载都不在默认位置）
			name: "CustomBaseURL",
			req: func() *http.Request {
				r := httptest.NewRequest("GET", "/foo/api/settings", nil)
				r.Header.Set("Origin", "https://10.0.0.2")
				return r
			},
			want: "https://10.0.0.2/app/custom/dsh",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mux := m
			if tc.name == "CustomBaseURL" {
				mux = newFnOSEntryMux("/foo", "/app/custom/dsh")
			}
			if got := mux.fnosEntryURL(tc.req()); got != tc.want {
				t.Fatalf("fnosEntryURL = %q, want %q", got, tc.want)
			}
		})
	}
}

// 根挂载（未启用网关子路径挂载）时不给出入口：否则会得到一个指向站点根的错地址。
func TestFnOSEntryURLRootMount(t *testing.T) {
	m := newFnOSEntryMux("/app/Harness", "")
	r := httptest.NewRequest("GET", "/app/Harness/api/settings", nil)
	r.Header.Set("Origin", "http://192.168.1.111:5666")
	if got := m.fnosEntryURL(r); got != "" {
		t.Fatalf("根挂载时 fnosEntryURL = %q, want 空串", got)
	}
}

// 拿不到任何访问地址信息时返回空串，交由前端退回浏览器自身地址。
func TestFnOSEntryURLNoAddress(t *testing.T) {
	m := newFnOSEntryMux("/app/Harness", "/app/Harness/dsh")
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
