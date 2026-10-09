package main

// release_notes_test.go —— 「按版本查更新日志」的回归测试（见 update.go 的
// notesIndex / splitBilingualNotes 与 server.go 的 LookupVersionNotes）。
//
// 三条要钉住的语义：
//  1. **中英分栏按锚点切，不按标题文字**：上游 release 正文用 `<h3 id="cn-…">` /
//     `<h3 id="en-…">` 分栏，顶部的「[中文] | [English]」导航行必须被排除；
//  2. **取不到 ≠ 没有日志**：拉取失败（断网/限流）返回 available=false 且**不写缓存**
//     （下次点还能重试）；成功但该版本没有 release 返回 available=true + 空正文，
//     界面显示「该版本没有更新日志」；
//  3. **未知版本号被记成「没有日志」**，不会被反复重查（限流下这是关键）。
//
// 正文样本取自 dsh 上游真实 release 的形态（见 deepseek-ai/deepseek-harness 的
// dsh-v0.2.1-alpha.2），并覆盖两种退化形态：只有中文锚点、两个锚点都没有。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const notesBodyBilingual = `[中文](#cn-0.2.1-alpha.2-community) | [English](#en-0.2.1-alpha.2-community)

<h3 id="cn-0.2.1-alpha.2-community">新增功能</h3>

- 新增思考过程机器翻译实验插件。 @tianyicui

### 问题修复

- 修复模型响应流停止输出的问题。 @tianyicui

<h3 id="en-0.2.1-alpha.2-community">New Features</h3>

- Add an experimental machine-translation plugin. @tianyicui

### Bug Fixes

- Fix the response stream hanging on stop. @tianyicui
`

const notesBodyCNOnly = `<h3 id="cn-0.1.3-alpha.2-community">新增功能</h3>

- 仅中文段落（该版本没有英文分栏）。
`

const notesBodyNoAnchor = `[中文](#chinese) | [English](#english)

✨ 新增功能

- 早期版本：正文里没有语言锚点（id 是 chinese/english），只能整段展示。
`

func TestSplitBilingualNotes(t *testing.T) {
	cn, en := splitBilingualNotes(notesBodyBilingual)
	// 导航行在 cn 锚点之前，必须被排除
	if strings.Contains(cn, "[中文]") || strings.Contains(cn, "English](#en-") {
		t.Fatalf("中文块不得包含顶部的语言导航行, got:\n%s", cn)
	}
	// 中文块从 cn 锚点起、到 en 锚点止
	if !strings.HasPrefix(cn, "新增功能") {
		t.Fatalf("中文块应从 cn 锚点内容起（HTML 压平为首行「新增功能」）, got:\n%s", cn)
	}
	if strings.Contains(cn, "New Features") || strings.Contains(cn, "Bug Fixes") {
		t.Fatalf("中文块不得串入英文段落, got:\n%s", cn)
	}
	if !strings.Contains(cn, "新增思考过程机器翻译实验插件") || !strings.Contains(cn, "修复模型响应流停止输出") {
		t.Fatalf("中文块缺少正文条目, got:\n%s", cn)
	}
	// 英文块从 en 锚点起
	if !strings.HasPrefix(en, "New Features") {
		t.Fatalf("英文块应从 en 锚点内容起, got:\n%s", en)
	}
	if strings.Contains(en, "新增功能") {
		t.Fatalf("英文块不得串入中文段落, got:\n%s", en)
	}
	if !strings.Contains(en, "Add an experimental machine-translation plugin") {
		t.Fatalf("英文块缺少正文条目, got:\n%s", en)
	}
	// HTML 标签必须被压平成纯文本（前端按纯文本展示，不解析 HTML）
	for _, block := range []string{cn, en} {
		if strings.Contains(block, "<h3") || strings.Contains(block, "</") {
			t.Fatalf("正文里不应残留 HTML 标签, got:\n%s", block)
		}
	}

	// 只有中文锚点：中文 = 全文，英文留空（前端按界面语言回退中文）
	cn2, en2 := splitBilingualNotes(notesBodyCNOnly)
	if !strings.Contains(cn2, "仅中文段落") || en2 != "" {
		t.Fatalf("只有中文锚点时应中文有内容、英文为空, got cn=%q en=%q", cn2, en2)
	}

	// 两个锚点都没有（极早期版本）：整段当中文，且开头的语言导航行必须被去掉
	cn3, en3 := splitBilingualNotes(notesBodyNoAnchor)
	if !strings.Contains(cn3, "早期版本") || en3 != "" {
		t.Fatalf("无锚点时应整段归中文, got cn=%q en=%q", cn3, en3)
	}
	if strings.HasPrefix(cn3, "[中文]") || strings.Contains(cn3, "[English](#english)") {
		t.Fatalf("无锚点正文里的语言导航行应被去掉, got:\n%s", cn3)
	}

	// 空正文
	if cn4, en4 := splitBilingualNotes("   "); cn4 != "" || en4 != "" {
		t.Fatalf("空正文应返回空串, got cn=%q en=%q", cn4, en4)
	}
}

// newTestNotesIndex 起一个假的 GitHub Releases API，返回给定的 releases JSON。
// hits 记录请求次数，用来断言「未知版本不会反复重查」。
func newTestNotesIndex(t *testing.T, handler http.HandlerFunc, hits *int32) (*notesIndex, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		handler(w, r)
	}))
	// 把 API 根从 api.github.com 改到测试服务器（保留 releases 路径）。
	restore := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = restore })
	ix := newNotesIndex(releaseRepo{owner: "o", name: "r"}, func(v string) string { return "dsh-v" + v },
		dshNotesReleases, func() *http.Client { return srv.Client() }, 5*time.Second)
	return ix, srv
}

func TestNotesIndexLookup(t *testing.T) {
	var hits int32
	ix, srv := newTestNotesIndex(t, func(w http.ResponseWriter, r *http.Request) {
		// 断言请求的是 releases 列表接口，而不是逐个 tag 查
		if !strings.HasSuffix(r.URL.Path, "/releases") {
			t.Errorf("应以 releases 列表建索引, got path=%s", r.URL.Path)
		}
		// 只拉最近 10 个 release（见 dshNotesReleases）：整仓拉取会被未认证 API 限流惩罚
		if got := r.URL.Query().Get("per_page"); got != "10" {
			t.Errorf("per_page 应为 10, got %q", got)
		}
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{"tag_name": "dsh-v0.2.1-alpha.2", "body": notesBodyBilingual, "draft": false},
			{"tag_name": "dsh-v0.1.3-alpha.2", "body": notesBodyCNOnly, "draft": false},
			{"tag_name": "dsh-v9.9.9-draft", "body": "draft 不该进索引", "draft": true},
		})
	}, &hits)
	defer srv.Close()

	// 命中已知版本：中英都拿到
	got, ok := ix.Lookup("0.2.1-alpha.2")
	if !ok || !strings.Contains(got.Note, "新增功能") || !strings.Contains(got.NoteEn, "New Features") {
		t.Fatalf("0.2.1-alpha.2 应中英齐备, ok=%v note=%q noteEn=%q", ok, got.Note, got.NoteEn)
	}
	// 只有中文的版本：英文留空（前端回退中文）
	gotCN, ok := ix.Lookup("0.1.3-alpha.2")
	if !ok || gotCN.Note == "" || gotCN.NoteEn != "" {
		t.Fatalf("0.1.3-alpha.2 应只有中文, ok=%v note=%q noteEn=%q", ok, gotCN.Note, gotCN.NoteEn)
	}
	// 未知版本：available=true（真的问过、确实没有）+ 空正文
	unknown, ok := ix.Lookup("0.9.9-zzz")
	if !ok || unknown.Note != "" {
		t.Fatalf("未知版本应 available=true 且正文为空, ok=%v note=%q", ok, unknown.Note)
	}
	// draft 不进索引（tag 会被剥前缀后当作版本）
	if d, _ := ix.Lookup("9.9.9-draft"); d.Note != "" {
		t.Fatalf("draft release 不该进索引, got %q", d.Note)
	}
	// 前面几次查询（含未知版本）只应触发一次真正的网络拉取
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("索引应只拉一次, got %d 次", n)
	}
	// 再查一次仍然不重拉
	if _, _ = ix.Lookup("0.2.1-alpha.2"); atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("命中缓存后不该再拉, got %d 次", atomic.LoadInt32(&hits))
	}
}

// 拉取失败必须是 available=false，且**不写缓存** —— 否则一次断网会把「暂时取不到」
// 固化成「该版本没有日志」，用户重试也永远看不到内容。
func TestNotesIndexFetchFailureNotCached(t *testing.T) {
	var hits int32
	var fail atomic.Bool
	fail.Store(true)
	ix, srv := newTestNotesIndex(t, func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			// 模拟未认证 API 限流
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{"tag_name": "dsh-v0.2.1-alpha.2", "body": notesBodyBilingual, "draft": false},
		})
	}, &hits)
	defer srv.Close()

	if _, ok := ix.Lookup("0.2.1-alpha.2"); ok {
		t.Fatal("限流时应 available=false（前端显示「暂时取不到」）")
	}
	// 失败不写缓存：网络恢复后再点必须能拿到
	fail.Store(false)
	got, ok := ix.Lookup("0.2.1-alpha.2")
	if !ok || !strings.Contains(got.Note, "新增功能") {
		t.Fatalf("失败后重试应能拉到内容, ok=%v note=%q", ok, got.Note)
	}
	if n := atomic.LoadInt32(&hits); n != 2 {
		t.Fatalf("失败那次 + 重试那次共应请求 2 次, got %d", n)
	}
}

// 插件市场的更新日志走的是**harness 那一套**：只取「要装的那一版」的 tag，一次单 tag
// 请求，不做整仓索引（见 market.go 的 refreshMarketStatus）。这里钉住两件事：
//   - tag 拼法是 `v<版本>`（`dsh-market/dsh-market` 的 release tag 形态）；
//   - 正文是**原样 Markdown**（与 harness 完全一致，前端 MarkdownText 渲染）——
//     市场没有中文锚点，不做中英切分那套。
func TestMarketReleaseNotesFromLatestTag(t *testing.T) {
	const body = "## What's Changed\n* fix(deps): bump undici by @someone in https://github.com/dsh-market/dsh-market/pull/812\n"
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]string{"body": body})
	}))
	defer srv.Close()
	restore := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = restore })

	notes := fetchReleaseNotes(srv.Client(), marketReleaseRepo, marketReleaseTag("1.66.14"))
	wantPath := "/repos/dsh-market/dsh-market/releases/tags/v1.66.14"
	if gotPath != wantPath {
		t.Fatalf("应只按最新版 tag 单次查询, got path=%q want %q", gotPath, wantPath)
	}
	// 原样 Markdown 透传（保留 `##` 标题与 `*` 列表，MarkdownText 才能渲染成标题/列表）
	if strings.TrimSpace(notes) != strings.TrimSpace(body) {
		t.Fatalf("正文应原样返回, got %q", notes)
	}
}
