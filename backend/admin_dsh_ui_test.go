package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 控制台入口下的 dsh 反代（/app/Harness/gui/）：请求必须由 AdminMux 自己接住，
// 剥掉 /app/Harness/gui 后转发给 dsh，而不是落到 SPA 的 404/index.html。
//
// 这条路径存在的意义见 fnosEntryURL：App 的网关鉴权按入口注入，把 dsh 挂在控制台
// 入口之下才能复用控制台的鉴权，避免跨到独立的 /app/Harness/dsh 入口。
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
	m.dshUI = newReverseProxyMount(m.auth, dsh, boot, "/app/Harness/gui", true)
	m.dshUI.trusted = true

	h := m.buildHandler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app/Harness/gui/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "dsh-ui-marker") {
		t.Fatalf("body 未包含 dsh 内容: %q", rec.Body.String())
	}
}
