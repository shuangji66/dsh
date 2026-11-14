package main

// plugin_detail.go —— 插件卡片的信息按钮：读插件自己的详情文档（对齐官方插件中心）。
//
// 官方（@deepseek-ai/dsh-plugin-manager 的 readBundleDetail → dsh-plugin-detail）的选择顺序：
//   1. **优先** `locale/<语言>.json` 里的 `meta.detail`（相对包根的 Markdown 路径）；
//      按语言链 zh-CN → zh → en 找第一个声明它的 locale；
//   2. **兜底** 包根目录的 README（README.md / readme.md / Readme.md / README.markdown / README）。
// 控制台的插件页列的全是本机已安装的包，所以只读本地包目录，不用走 npm CDN。
//
// 与官方一致的几条细节（别改）：
//   - 声明了 `meta.detail` 但文件不存在是**错误**，不回落 README；
//   - `meta.detail` 必须是包内相对路径、以 .md/.markdown 结尾，不能含 scheme / 反斜杠 / `..`；
//   - 文档超过 1 MiB 视为不可读；
//   - 正文去掉 BOM 与开头的 YAML front matter（那是文件元数据，不是正文）。

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	// pluginDetailMaxBytes 与官方的 pluginManager.detailMaxBytes 默认值一致（1 MiB）。
	pluginDetailMaxBytes = 1 << 20
)

// pluginDetail 是 GET /api/plugins/detail 的成功载荷。
type pluginDetail struct {
	// Status：found（有内容）/ none（既没有 meta.detail 也没有 README）。
	Status string `json:"status"`
	// Origin：locale（meta.detail 指定的文档）/ readme（兜底读的 README）。
	Origin string `json:"origin,omitempty"`
	// Path 是文档在包内的 / 分隔相对路径（前端只用于展示，不参与拼接）。
	Path string `json:"path,omitempty"`
	// Markdown 是正文（已去掉 BOM 与 YAML front matter）。
	Markdown string `json:"markdown,omitempty"`
}

var (
	// detailPathSchemeRe 对应官方 detailPath 的 scheme 检查：`C:` / `https:` 之类一律拒绝。
	detailPathSchemeRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
	// detailDocumentRe 只接受 Markdown 文档。
	detailDocumentRe = regexp.MustCompile(`(?i)\.(?:md|markdown)$`)
	// detailLanguageRe 与官方一样只接受 BCP 47 形状的语言 id（小写）。
	detailLanguageRe = regexp.MustCompile(`^[a-z]{2,8}(?:-[a-z0-9]{1,8})*$`)
	// detailFrontMatterRe 匹配文档开头的 YAML front matter（官方 markdownOf 的等价实现）。
	detailFrontMatterRe = regexp.MustCompile(`(?s)^---\r?\n.*?\r?\n---[ \t]*(?:\r?\n|$)`)
	// pluginReadmeRe 是 README 的宽松匹配（目录列举时用；大小写不敏感）。
	pluginReadmeRe = regexp.MustCompile(`(?i)^readme(?:\.md|\.markdown)?$`)
)

// pluginReadmeNames 是 README 的尝试顺序（与官方 README_NAMES 一致）。
var pluginReadmeNames = []string{"README.md", "readme.md", "Readme.md", "README.markdown", "README"}

// handlePluginDetail 读取某个已安装插件的详情文档。
//
// 参数：name（插件名，必填）、lang（界面语言，如 zh / en；非法或缺省按 en 处理）。
func (m *AdminMux) handlePluginDetail(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		writeErrU(w, http.StatusBadRequest, "err_need_plugin_name", "缺少插件名")
		return
	}
	// 名字会拼进 node_modules/<name>，必须先过 npm 包名校验（与卸载/启停同一条规则）。
	if !validPluginName(name) {
		writeErrU(w, http.StatusBadRequest, "err_plugin_name_invalid", "插件名不合法（仅允许 npm 包名）")
		return
	}
	profileWebDir := filepath.Join(m.dsh.effectiveHome(), ".dsh", "profiles", "web")
	detail, err := readPluginDetail(profileWebDir, name, r.URL.Query().Get("lang"))
	if err != nil {
		// 诊断文本保持英文（会进日志，也作为 detail 参数给到界面）。
		logWarn("[plugin] read detail of %s failed: %v", name, err)
		writeErrU(w, http.StatusInternalServerError, "err_plugin_detail_failed",
			"读取插件详情失败：%s", "detail", err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "name": name, "status": detail.Status,
		"origin": detail.Origin, "path": detail.Path, "markdown": detail.Markdown})
}

// readPluginDetail 按官方顺序读一个已安装插件的详情文档。
func readPluginDetail(profileWebDir, name, language string) (pluginDetail, error) {
	pkgDir := filepath.Join(profileWebDir, "node_modules", name)
	info, err := os.Stat(pkgDir)
	if err != nil {
		return pluginDetail{}, fmt.Errorf("read package directory: %w", err)
	}
	if !info.IsDir() {
		return pluginDetail{}, fmt.Errorf("package path is not a directory")
	}

	// 1) 优先：locale/<语言>.json 的 meta.detail。
	detailPath, declared, err := localeDetailPath(pkgDir, detailLanguageChain(language))
	if err != nil {
		return pluginDetail{}, err
	}
	if declared {
		markdown, err := readDetailDocument(filepath.Join(pkgDir, filepath.FromSlash(detailPath)))
		if err != nil {
			// 官方语义：声明了却读不到是错误，不回落到 README（否则会悄悄显示成另一份内容）。
			return pluginDetail{}, fmt.Errorf("%s (named by meta.detail): %w", detailPath, err)
		}
		return pluginDetail{Status: "found", Origin: "locale", Path: detailPath, Markdown: markdown}, nil
	}

	// 2) 兜底：包根目录的 README。
	readme, found, err := findPluginReadme(pkgDir)
	if err != nil {
		return pluginDetail{}, err
	}
	if !found {
		return pluginDetail{Status: "none"}, nil
	}
	markdown, err := readDetailDocument(filepath.Join(pkgDir, readme))
	if err != nil {
		return pluginDetail{}, fmt.Errorf("%s: %w", readme, err)
	}
	return pluginDetail{Status: "found", Origin: "readme", Path: readme, Markdown: markdown}, nil
}

// detailLanguageChain 复刻官方的 languageChain：语言本身 → 逐级短前缀 → en，全部小写。
// 非法或空的语言 id 按 `en` 处理（控制台只会传自己的界面语言，这里不制造额外错误）。
func detailLanguageChain(language string) []string {
	id := strings.ToLower(strings.TrimSpace(language))
	if !detailLanguageRe.MatchString(id) {
		return []string{"en"}
	}
	parts := strings.Split(id, "-")
	chain := make([]string, 0, len(parts)+1)
	seen := map[string]bool{}
	for i := len(parts); i > 0; i-- {
		item := strings.Join(parts[:i], "-")
		if seen[item] {
			continue
		}
		seen[item] = true
		chain = append(chain, item)
	}
	if !seen["en"] {
		chain = append(chain, "en")
	}
	return chain
}

// localeFileNames 返回某个语言对应的 locale 文件名：语言本身，以及两字母区域的大写写法
// （官方同时认 zh-cn.json 与 zh-CN.json）。
func localeFileNames(language string) []string {
	names := []string{language}
	base, region, ok := strings.Cut(language, "-")
	if ok && len(base) >= 2 && len(base) <= 3 && len(region) == 2 {
		upper := base + "-" + strings.ToUpper(region)
		if upper != language {
			names = append(names, upper)
		}
	}
	return names
}

// localeDetailPath 按语言链找第一个声明了 meta.detail 的 locale 文件。
//
// 与官方 localeDetail 的细节一致：某个语言的 locale 文件存在但没声明 meta.detail 时，
// 该语言到此为止，继续看链上的下一个语言（不会因为同名文件里没有 detail 就放弃兜底链）。
func localeDetailPath(pkgDir string, languages []string) (string, bool, error) {
	for _, language := range languages {
		for _, file := range localeFileNames(language) {
			rel := path.Join("locale", file+".json")
			raw, err := os.ReadFile(filepath.Join(pkgDir, filepath.FromSlash(rel)))
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					continue
				}
				return "", false, fmt.Errorf("read %s: %w", rel, err)
			}
			var parsed struct {
				Meta *struct {
					Detail *string `json:"detail"`
				} `json:"meta"`
			}
			if err := json.Unmarshal(raw, &parsed); err != nil {
				return "", false, fmt.Errorf("%s: %w", rel, err)
			}
			if parsed.Meta != nil && parsed.Meta.Detail != nil {
				declared := strings.TrimSpace(*parsed.Meta.Detail)
				if !validDetailDocumentPath(declared) {
					return "", false, fmt.Errorf("%s: meta.detail must be a Markdown path inside the package", rel)
				}
				return declared, true, nil
			}
			break
		}
	}
	return "", false, nil
}

// validDetailDocumentPath 复刻官方 detailPath 的校验：包内相对路径、/ 分隔、无 scheme、
// 无空段/./..、以 .md/.markdown 结尾。
func validDetailDocumentPath(p string) bool {
	if p == "" || strings.Contains(p, `\`) || detailPathSchemeRe.MatchString(p) {
		return false
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return detailDocumentRe.MatchString(p)
}

// findPluginReadme 找包根目录的 README：先按官方名单逐个精确查找，再兜底列目录按
// README 名单的优先序（大小写不敏感）挑一个。
func findPluginReadme(pkgDir string) (string, bool, error) {
	for _, name := range pluginReadmeNames {
		info, err := os.Stat(filepath.Join(pkgDir, name))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return "", false, fmt.Errorf("stat %s: %w", name, err)
		}
		if info.Mode().IsRegular() {
			return name, true, nil
		}
	}
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return "", false, fmt.Errorf("read package directory: %w", err)
	}
	var matches []string
	for _, entry := range entries {
		if entry.IsDir() || !pluginReadmeRe.MatchString(entry.Name()) {
			continue
		}
		matches = append(matches, entry.Name())
	}
	if len(matches) == 0 {
		return "", false, nil
	}
	rank := func(name string) int {
		for i, candidate := range pluginReadmeNames {
			if strings.EqualFold(name, candidate) {
				return i
			}
		}
		return len(pluginReadmeNames)
	}
	sort.Slice(matches, func(i, j int) bool {
		if ri, rj := rank(matches[i]), rank(matches[j]); ri != rj {
			return ri < rj
		}
		return matches[i] < matches[j]
	})
	return matches[0], true, nil
}

// readDetailDocument 读一份详情文档：只接受普通文件，超过 1 MiB 视为不可读，
// 正文去掉 BOM 与开头的 YAML front matter。
func readDetailDocument(filename string) (string, error) {
	info, err := os.Stat(filename)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular file")
	}
	if info.Size() > pluginDetailMaxBytes {
		return "", fmt.Errorf("larger than %d bytes", pluginDetailMaxBytes)
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	return stripDetailFrontMatter(string(raw)), nil
}

// stripDetailFrontMatter 去掉开头的 BOM 与 YAML front matter（官方 markdownOf 的等价实现）。
func stripDetailFrontMatter(text string) string {
	text = strings.TrimPrefix(text, "\uFEFF")
	return detailFrontMatterRe.ReplaceAllString(text, "")
}
