package main

// plugin_detail_test.go —— 插件详情文档（信息按钮弹窗）的读取规则。
//
// 参考官方 @deepseek-ai/dsh-plugin-manager 的 readBundleDetail：
//   1. 优先 locale/<语言>.json 的 meta.detail（语言链 zh-CN → zh → en）；
//   2. 兜底包根目录的 README（README.md / readme.md / Readme.md / README.markdown / README）；
//   3. 声明了 meta.detail 却读不到是错误，不回落 README；
//   4. 只接受包内相对路径（无 scheme、无反斜杠、无 ..），且必须以 .md/.markdown 结尾；
//   5. 正文去掉 BOM 与 YAML front matter。
//
// 这些用例只碰临时目录，不起 dsh、不联网。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeDetailPackage 在 <profileWeb>/node_modules/<name> 下写一个包的若干文件。
func writeDetailPackage(t *testing.T, profileWeb, name string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(profileWeb, "node_modules", name)
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestReadPluginDetailPrefersLocaleDocument(t *testing.T) {
	profileWeb := t.TempDir()
	writeDetailPackage(t, profileWeb, "demo-plugin", map[string]string{
		"package.json":      `{"name":"demo-plugin","version":"1.0.0"}`,
		"locale/zh.json":    `{"meta":{"detail":"docs/DETAIL.zh.md"}}`,
		"docs/DETAIL.zh.md": "# 中文详情\n\n这是详情正文。",
		"README.md":         "# English README",
		"docs/DETAIL.en.md": "# English detail",
		"locale/en.json":    `{"meta":{"detail":"docs/DETAIL.en.md"}}`,
	})

	zh, err := readPluginDetail(profileWeb, "demo-plugin", "zh")
	if err != nil {
		t.Fatalf("中文详情应可读: %v", err)
	}
	if zh.Status != "found" || zh.Origin != "locale" || zh.Path != "docs/DETAIL.zh.md" {
		t.Fatalf("中文应命中 locale 文档，实际 %+v", zh)
	}
	if !strings.Contains(zh.Markdown, "中文详情") {
		t.Fatalf("正文不对: %q", zh.Markdown)
	}

	en, err := readPluginDetail(profileWeb, "demo-plugin", "en")
	if err != nil {
		t.Fatalf("英文详情应可读: %v", err)
	}
	if en.Origin != "locale" || en.Path != "docs/DETAIL.en.md" {
		t.Fatalf("英文应命中 locale 文档，实际 %+v", en)
	}
}

// 没有 locale 文档时回落 README；locale 存在但没声明 detail 也继续走链。
func TestReadPluginDetailFallsBackToReadme(t *testing.T) {
	profileWeb := t.TempDir()
	writeDetailPackage(t, profileWeb, "fallback-plugin", map[string]string{
		"package.json":   `{"name":"fallback-plugin","version":"1.0.0"}`,
		"locale/zh.json": `{"meta":{"title":"插件"}}`,
		"README.md":      "# 兜底 README",
	})

	doc, err := readPluginDetail(profileWeb, "fallback-plugin", "zh")
	if err != nil {
		t.Fatalf("应回落到 README: %v", err)
	}
	if doc.Status != "found" || doc.Origin != "readme" || doc.Path != "README.md" {
		t.Fatalf("应命中 README，实际 %+v", doc)
	}
	if !strings.Contains(doc.Markdown, "兜底 README") {
		t.Fatalf("正文不对: %q", doc.Markdown)
	}
}

// README 的其它大小写/后缀写法也要能认出来（官方按名单 + 目录列举兜底）。
func TestReadPluginDetailReadmeVariants(t *testing.T) {
	cases := map[string]string{
		"README.MD":       "# 大写后缀",
		"readme.md":       "# 小写",
		"README.markdown": "# 另一种后缀",
	}
	for filename, body := range cases {
		t.Run(filename, func(t *testing.T) {
			profileWeb := t.TempDir()
			writeDetailPackage(t, profileWeb, "readme-plugin", map[string]string{
				"package.json": `{"name":"readme-plugin","version":"1.0.0"}`,
				filename:       body,
			})
			doc, err := readPluginDetail(profileWeb, "readme-plugin", "zh")
			if err != nil {
				t.Fatalf("应找到 %s: %v", filename, err)
			}
			if doc.Status != "found" || doc.Path != filename {
				t.Fatalf("应命中 %s，实际 %+v", filename, doc)
			}
		})
	}
}

func TestReadPluginDetailNone(t *testing.T) {
	profileWeb := t.TempDir()
	writeDetailPackage(t, profileWeb, "empty-plugin", map[string]string{
		"package.json": `{"name":"empty-plugin","version":"1.0.0"}`,
	})
	doc, err := readPluginDetail(profileWeb, "empty-plugin", "zh")
	if err != nil {
		t.Fatalf("无内容不应报错: %v", err)
	}
	if doc.Status != "none" {
		t.Fatalf("应返回 none，实际 %+v", doc)
	}
}

// 声明的 meta.detail 文件不存在：报错，不回落 README（官方语义）。
func TestReadPluginDetailDeclaredFileMissingIsError(t *testing.T) {
	profileWeb := t.TempDir()
	writeDetailPackage(t, profileWeb, "broken-plugin", map[string]string{
		"package.json":   `{"name":"broken-plugin","version":"1.0.0"}`,
		"locale/zh.json": `{"meta":{"detail":"docs/MISSING.md"}}`,
		"README.md":      "# 不应显示",
	})

	doc, err := readPluginDetail(profileWeb, "broken-plugin", "zh")
	if err == nil {
		t.Fatalf("声明文件缺失应报错，实际 %+v", doc)
	}
	if !strings.Contains(err.Error(), "MISSING.md") {
		t.Fatalf("错误应点名缺失文件，实际 %v", err)
	}
}

// meta.detail 的非法写法一律拒绝（不读包外文件）。
func TestReadPluginDetailRejectsInvalidMetaDetail(t *testing.T) {
	for _, detail := range []string{
		"../outside.md",
		"/etc/passwd.md",
		"docs\\DETAIL.md",
		"https://example.com/DETAIL.md",
		"docs/DETAIL.txt",
		"docs//DETAIL.md",
		"docs/./DETAIL.md",
		"",
	} {
		t.Run(detail, func(t *testing.T) {
			profileWeb := t.TempDir()
			writeDetailPackage(t, profileWeb, "bad-plugin", map[string]string{
				"package.json":   `{"name":"bad-plugin","version":"1.0.0"}`,
				"locale/zh.json": `{"meta":{"detail":` + jsonString(t, detail) + `}}`,
				"README.md":      "# 不应显示",
			})
			if _, err := readPluginDetail(profileWeb, "bad-plugin", "zh"); err == nil {
				t.Fatalf("meta.detail=%q 应被拒绝", detail)
			}
		})
	}
}

// 语言链：zh-CN → zh → en；locale 文件名同时认 zh-CN.json 与 zh-cn.json。
func TestReadPluginDetailLanguageChain(t *testing.T) {
	profileWeb := t.TempDir()
	writeDetailPackage(t, profileWeb, "chain-plugin", map[string]string{
		"package.json":      `{"name":"chain-plugin","version":"1.0.0"}`,
		"locale/zh.json":    `{"meta":{"detail":"docs/ZH.md"}}`,
		"docs/ZH.md":        "# 简体",
		"locale/zh-CN.json": `{"meta":{"detail":"docs/ZH-CN.md"}}`,
		"docs/ZH-CN.md":     "# 大陆",
	})

	// zh-CN 精确命中 zh-CN.json
	doc, err := readPluginDetail(profileWeb, "chain-plugin", "zh-CN")
	if err != nil || doc.Path != "docs/ZH-CN.md" {
		t.Fatalf("zh-CN 应命中 zh-CN.json，实际 %+v (%v)", doc, err)
	}
	// zh-cn 先试 zh-cn.json（不存在），再试大写的 zh-CN.json
	doc, err = readPluginDetail(profileWeb, "chain-plugin", "zh-cn")
	if err != nil || doc.Path != "docs/ZH-CN.md" {
		t.Fatalf("zh-cn 应命中 zh-CN.json，实际 %+v (%v)", doc, err)
	}
	// 未知语言走 en 链；没有 locale/en.json 时回落 README —— 本包没有 README，得到 none
	doc, err = readPluginDetail(profileWeb, "chain-plugin", "fr")
	if err != nil || doc.Status != "none" {
		t.Fatalf("fr 应无内容（不误用中文），实际 %+v (%v)", doc, err)
	}
}

func TestReadPluginDetailStripsFrontMatterAndBOM(t *testing.T) {
	profileWeb := t.TempDir()
	writeDetailPackage(t, profileWeb, "bom-plugin", map[string]string{
		"package.json": `{"name":"bom-plugin","version":"1.0.0"}`,
		"README.md":    "\uFEFF---\ntitle: demo\n---\n# 正文\n",
	})
	doc, err := readPluginDetail(profileWeb, "bom-plugin", "zh")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc.Markdown, "title: demo") || strings.HasPrefix(doc.Markdown, "---") {
		t.Fatalf("front matter 应被去掉，实际 %q", doc.Markdown)
	}
	if !strings.Contains(doc.Markdown, "# 正文") {
		t.Fatalf("正文丢了: %q", doc.Markdown)
	}
}

func TestReadPluginDetailTooLarge(t *testing.T) {
	profileWeb := t.TempDir()
	writeDetailPackage(t, profileWeb, "huge-plugin", map[string]string{
		"package.json": `{"name":"huge-plugin","version":"1.0.0"}`,
		"README.md":    strings.Repeat("x", pluginDetailMaxBytes+1),
	})
	if _, err := readPluginDetail(profileWeb, "huge-plugin", "zh"); err == nil {
		t.Fatal("超过 1 MiB 的文档应报错")
	}
}

// --- HTTP 入口 ---

func detailMux(t *testing.T) (*AdminMux, string) {
	t.Helper()
	home := t.TempDir()
	renv := &RuntimeEnv{Home: home}
	dsh := newTestDshManager(home, "")
	dsh.renv = renv
	return &AdminMux{renv: renv, dsh: dsh}, filepath.Join(home, ".dsh", "profiles", "web")
}

func getPluginDetail(t *testing.T, m *AdminMux, query string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/plugins/detail?"+query, nil)
	m.handlePluginDetail(rec, req)
	return rec
}

func TestHandlePluginDetailValidation(t *testing.T) {
	m, _ := detailMux(t)

	if rec := getPluginDetail(t, m, "lang=zh"); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "err_need_plugin_name") {
		t.Fatalf("缺少 name 应 400，实际 %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := getPluginDetail(t, m, "name=../../etc&lang=zh"); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "err_plugin_name_invalid") {
		t.Fatalf("非法 name 应 400，实际 %d (%s)", rec.Code, rec.Body.String())
	}
}

// 没有 dsh manager（测试或未启用 dsh 的实例）：回 500 的 code，不要解引用 nil 崩掉连接。
func TestHandlePluginDetailWithoutDshManager(t *testing.T) {
	rec := getPluginDetail(t, &AdminMux{}, "name=demo&lang=zh")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("没有 dsh manager 应 500，实际 %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "err_plugin_detail_failed") {
		t.Fatalf("应带 err_plugin_detail_failed，实际 %s", rec.Body.String())
	}
}

func TestHandlePluginDetailSuccess(t *testing.T) {
	m, profileWeb := detailMux(t)
	writeDetailPackage(t, profileWeb, "demo-plugin", map[string]string{
		"package.json": `{"name":"demo-plugin","version":"1.0.0"}`,
		"README.md":    "# 详情正文",
	})

	rec := getPluginDetail(t, m, "name=demo-plugin&lang=zh")
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200，实际 %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		OK       bool   `json:"ok"`
		Status   string `json:"status"`
		Origin   string `json:"origin"`
		Path     string `json:"path"`
		Markdown string `json:"markdown"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.OK || body.Status != "found" || body.Origin != "readme" || body.Path != "README.md" || !strings.Contains(body.Markdown, "详情正文") {
		t.Fatalf("响应不对: %+v", body)
	}
}

func TestHandlePluginDetailMissingPackage(t *testing.T) {
	m, _ := detailMux(t)
	rec := getPluginDetail(t, m, "name=not-installed&lang=zh")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("未安装的包应 500，实际 %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "err_plugin_detail_failed") {
		t.Fatalf("应带 err_plugin_detail_failed，实际 %s", rec.Body.String())
	}
}

// jsonString 把字符串编码成 JSON 字面量（测试里拼 locale 文件用）。
func jsonString(t *testing.T, value string) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
