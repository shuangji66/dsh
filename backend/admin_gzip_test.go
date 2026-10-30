package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// 预压缩资产（vite.config.ts 的 precompressAssets 生成 .gz 兄弟文件）：
// 只有客户端明确接受 gzip 时才发压缩字节，且必须带 Vary: Accept-Encoding。
func TestSPAPrecompressedAssets(t *testing.T) {
	const index = `<!doctype html><html><head></head><body></body></html>`
	fsys := fstest.MapFS{
		"embed/index.html":              &fstest.MapFile{Data: []byte(index)},
		"embed/assets/app-abc123.js":    &fstest.MapFile{Data: []byte("console.log(1)\n")},
		"embed/assets/app-abc123.js.gz": &fstest.MapFile{Data: []byte("GZIPPED-BYTES")},
		"embed/assets/only-identity.js": &fstest.MapFile{Data: []byte("identity\n")},
	}
	h := spaHandler(fsys, "/app/Harness")
	get := func(target, accept string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if accept != "" {
			req.Header.Set("Accept-Encoding", accept)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}

	// 1) 接受 gzip → 压缩字节 + Content-Encoding + Vary（Content-Type 仍按原文件名）
	w := get("/app/Harness/assets/app-abc123.js", "gzip, deflate, br")
	if w.Code != http.StatusOK {
		t.Fatalf("gzip asset status = %d", w.Code)
	}
	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Errorf("Content-Encoding = %q, want gzip", got)
	}
	if got := w.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Errorf("Vary = %q, want Accept-Encoding", got)
	}
	if got := w.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got, want := w.Body.String(), "GZIPPED-BYTES"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	if got, want := w.Header().Get("Content-Length"), "13"; got != want {
		t.Errorf("Content-Length = %q, want %q", got, want)
	}

	// 2) 不接受 gzip → 原始字节，且仍然带 Vary（同一 URL 有两个表示）
	w = get("/app/Harness/assets/app-abc123.js", "")
	if w.Header().Get("Content-Encoding") != "" {
		t.Errorf("identity response must not be gzip-encoded")
	}
	if got, want := w.Body.String(), "console.log(1)\n"; got != want {
		t.Errorf("identity body = %q, want %q", got, want)
	}
	if got := w.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Errorf("identity Vary = %q, want Accept-Encoding", got)
	}

	// 3) gzip;q=0 是显式拒绝
	w = get("/app/Harness/assets/app-abc123.js", "gzip;q=0, identity")
	if w.Header().Get("Content-Encoding") != "" || w.Body.String() != "console.log(1)\n" {
		t.Errorf("q=0 must not receive gzip")
	}

	// 4) 没有 .gz 兄弟文件时退回原始字节
	w = get("/app/Harness/assets/only-identity.js", "gzip")
	if w.Header().Get("Content-Encoding") != "" || w.Body.String() != "identity\n" {
		t.Errorf("asset without .gz must be served identity")
	}

	// 5) index.html 不预压缩（它要注入 base href）
	w = get("/app/Harness/", "gzip")
	if w.Header().Get("Content-Encoding") != "" {
		t.Errorf("index.html must not be gzip-precompressed")
	}
}

func TestAcceptsGzip(t *testing.T) {
	cases := []struct {
		header string
		want   bool
	}{
		{"", false},
		{"gzip", true},
		{"deflate, gzip", true},
		{"gzip;q=0", false},
		{"gzip;q=0.5", true},
		{"br;q=1, gzip;q=0.001", true},
		{"br, deflate", false},
		{"GZIP", true},
		// `*` 通配符：没有显式 gzip 条目时才生效；显式条目优先（含 q=0 的拒绝）。
		{"*", true},
		{"*;q=0", false},
		{"*;q=0.5, br", true},
		{"gzip;q=0, *", false},
		{"GZIP;Q=0.5", true},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if c.header != "" {
			req.Header.Set("Accept-Encoding", c.header)
		}
		if got := acceptsGzip(req); got != c.want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", c.header, got, c.want)
		}
	}
}
