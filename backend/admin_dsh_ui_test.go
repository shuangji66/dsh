package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 控制台入口下的 dsh 反代（/app/Harness/dsh/）：请求必须由 AdminMux 自己接住，
// 剥掉 /app/Harness/dsh 后转发给 dsh，而不是落到 SPA 的 404/index.html。
//
// 这条路径存在的意义见 fnosEntryURL：App 的网关鉴权按入口注入，把 dsh 挂在控制台
// 入口之下才能复用控制台的鉴权，避免跨到另一个网关入口（另一个 gatewayPrefix）。
func TestAdminMuxServesDshUIUnderConsoleBase(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Errorf("dsh upstream path = %q, want /", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><head></head><body>dsh-ui-marker</body></html>"))
	}))
	defer upstream.Close()

	prev := GetConfig()
	cfg := prev
	cfg.DshPort = upstream.Listener.Addr().(*net.TCPAddr).Port
	initConfig(&cfg)
	t.Cleanup(func() { initConfig(&prev) })

	dsh := newTestDshManager(t.TempDir(), "")
	dsh.markSessionSettled()
	boot := newBootState()
	boot.set(phaseReady, "")

	m := &AdminMux{
		renv: &RuntimeEnv{AdminBaseURL: "/app/Harness"},
		dsh:  dsh,
		auth: NewAuth(),
		boot: boot,
	}
	m.dshUI = newReverseProxyMount(m.auth, dsh, boot, "/app/Harness/dsh", true)
	m.dshUI.trusted = true

	h := m.buildHandler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app/Harness/dsh/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "dsh-ui-marker") {
		t.Fatalf("body 未包含 dsh 内容: %q", rec.Body.String())
	}
}

// 旧路径 /gui 会被重定向到 /dsh（GET/HEAD 301、其它 308；保留子路径与 query），
// 不会再被当成 dsh 挂载点。
func TestAdminMuxRedirectsLegacyGuiPath(t *testing.T) {
	dsh := newTestDshManager(t.TempDir(), "")
	boot := newBootState()
	m := &AdminMux{
		renv: &RuntimeEnv{AdminBaseURL: "/app/Harness"},
		dsh:  dsh,
		auth: NewAuth(),
		boot: boot,
	}
	m.dshUI = newReverseProxyMount(m.auth, dsh, boot, "/app/Harness/dsh", true)
	m.dshUI.trusted = true

	h := m.buildHandler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app/Harness/gui/api/x?y=1", nil))

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want 301", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/app/Harness/dsh/api/x?y=1" {
		t.Fatalf("Location = %q", loc)
	}
}

// 旧 /gui 的 POST 必须用 308 保留方法与请求体（dsh 的旧页面会把相对路径的 POST
// 发到 /gui/…，301 会把它们降级成 GET），转义路径也不能在重定向上被解码。
func TestAdminMuxLegacyGuiRedirectKeepsMethodAndEscapes(t *testing.T) {
	dsh := newTestDshManager(t.TempDir(), "")
	boot := newBootState()
	m := &AdminMux{
		renv: &RuntimeEnv{AdminBaseURL: "/app/Harness"},
		dsh:  dsh,
		auth: NewAuth(),
		boot: boot,
	}
	m.dshUI = newReverseProxyMount(m.auth, dsh, boot, "/app/Harness/dsh", true)
	m.dshUI.trusted = true
	h := m.buildHandler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/app/Harness/gui/api/x?y=1", strings.NewReader(`{"a":1}`)))
	if rec.Code != http.StatusPermanentRedirect {
		t.Fatalf("POST status = %d, want 308", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/app/Harness/dsh/api/x?y=1" {
		t.Fatalf("POST Location = %q", loc)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app/Harness/gui/a%2Fb", nil))
	if loc := rec.Header().Get("Location"); loc != "/app/Harness/dsh/a%2Fb" {
		t.Fatalf("转义路径 Location = %q，转义形态必须原样保留", loc)
	}
}

// 网关先剥掉控制台 baseurl 的部署（落到 /dsh/…）也要保留转义形态：否则
// /api/a%2Fb 会被重新拼成 /api/a/b，dsh 的插件 bundle 路径直接 404。
func TestAdminMuxStrippedBasePreservesEscapedPath(t *testing.T) {
	var gotURI string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.URL.RequestURI()
		_, _ = w.Write([]byte("dsh-upstream"))
	}))
	defer upstream.Close()

	prev := GetConfig()
	cfg := prev
	cfg.DshPort = upstream.Listener.Addr().(*net.TCPAddr).Port
	initConfig(&cfg)
	t.Cleanup(func() { initConfig(&prev) })

	dsh := newTestDshManager(t.TempDir(), "")
	dsh.markSessionSettled()
	boot := newBootState()
	boot.set(phaseReady, "")

	m := &AdminMux{
		renv: &RuntimeEnv{AdminBaseURL: "/app/Harness"},
		dsh:  dsh,
		auth: NewAuth(),
		boot: boot,
	}
	m.dshUI = newReverseProxyMount(m.auth, dsh, boot, "/app/Harness/dsh", true)
	m.dshUI.trusted = true

	rec := httptest.NewRecorder()
	m.buildHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dsh/api/a%2Fb?x=1", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != "dsh-upstream" {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if gotURI != "/api/a%2Fb?x=1" {
		t.Fatalf("上游 URI = %q, want %q", gotURI, "/api/a%2Fb?x=1")
	}
}
