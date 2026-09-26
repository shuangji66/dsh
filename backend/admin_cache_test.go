package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// 控制台前端静态资源的缓存策略（serveBytes）：
//   - Vite 的内容哈希产物（assets/**）→ 长期强缓存，升级后文件名会变，不会用到旧字节；
//   - 入口文件（index.html / callback.html，文件名不带哈希）→ no-cache + ETag，
//     每次回源校验，避免升级后仍用旧 index.html 指向已被替换掉的旧哈希资源。
//
// 用 fstest.MapFS 构造一个最小的 dist，不需要真实 embed 目录。
func TestSPAStaticCacheHeaders(t *testing.T) {
	const index = `<!doctype html><html><head></head><body><script src="./assets/app-abc123.js"></script></body></html>`
	// 与生产一致：embed 目录里放着整个 dist（spaHandler 会先 fs.Sub("embed")）
	fsys := fstest.MapFS{
		"embed/index.html":           &fstest.MapFile{Data: []byte(index)},
		"embed/assets/app-abc123.js": &fstest.MapFile{Data: []byte("console.log(1)\n")},
		"embed/callback.html":        &fstest.MapFile{Data: []byte("<html>cb</html>")},
	}

	h := spaHandler(fsys, "/app/Harness")
	get := func(target string, hdr map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}

	// 1) 哈希产物：强缓存 + 原样输出
	w := get("/app/Harness/assets/app-abc123.js", nil)
	if got, want := w.Header().Get("Cache-Control"), "public, max-age=31536000, immutable"; got != want {
		t.Errorf("asset Cache-Control = %q, want %q", got, want)
	}
	if w.Body.String() != "console.log(1)\n" {
		t.Errorf("asset body = %q", w.Body.String())
	}
	if w.Header().Get("ETag") != "" {
		t.Errorf("asset should not carry ETag (immutable cache)")
	}

	// 2) 首页：no-cache + ETag，且 base href 与资源前缀都被改写
	w = get("/app/Harness/", nil)
	if got, want := w.Header().Get("Cache-Control"), "no-cache"; got != want {
		t.Errorf("index Cache-Control = %q, want %q", got, want)
	}
	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatal("index ETag missing")
	}
	body := w.Body.String()
	if !strings.Contains(body, `<base href="/app/Harness/">`) {
		t.Errorf("index missing base href: %s", body)
	}
	if !strings.Contains(body, `src="/app/Harness/assets/app-abc123.js"`) {
		t.Errorf("index asset src not rewritten: %s", body)
	}

	// 3) If-None-Match 命中 → 304 且不带正文
	w = get("/app/Harness/", map[string]string{"If-None-Match": etag})
	if w.Code != http.StatusNotModified {
		t.Errorf("conditional request status = %d, want 304", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("304 body length = %d, want 0", w.Body.Len())
	}
	// 弱校验前缀 / 多值列表 / * 都应命中（反代与浏览器都可能这样发）
	for _, hdr := range []string{"W/" + etag, `"nope", ` + etag, "*"} {
		if got := get("/app/Harness/", map[string]string{"If-None-Match": hdr}); got.Code != http.StatusNotModified {
			t.Errorf("If-None-Match %q → %d, want 304", hdr, got.Code)
		}
	}
	// 4) 未命中 → 200 全量
	if w := get("/app/Harness/", map[string]string{"If-None-Match": `"stale"`}); w.Code != http.StatusOK || w.Body.Len() == 0 {
		t.Errorf("stale ETag → code %d, body %d bytes", w.Code, w.Body.Len())
	}

	// 5) SPA 前端路由回退（不存在的路径）按入口文件处理
	w = get("/app/Harness/some/route", nil)
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("spa fallback: code=%d cache-control=%q", w.Code, w.Header().Get("Cache-Control"))
	}

	// 6) callback.html 这类非哈希文件同样不强缓存
	if w := get("/app/Harness/callback.html", nil); w.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("callback Cache-Control = %q, want no-cache", w.Header().Get("Cache-Control"))
	}
}
