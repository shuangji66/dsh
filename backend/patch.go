// patch.go —— 通过编辑 cordis.patch.yml 实现插件的启用/停用。
//
// 机制来自 dsh-market：dsh 的 web profile 由各 bundle 层 + 用户补丁层
// （$HOME/.dsh/profiles/web/cordis.patch.yml）共同编排。补丁层采用“每 key 覆盖”
// 语义：一行 `- id: X` 加 `disabled: true` 即停用该 loader 条目，`disabled: false`
// 则强制启用下层被禁的条目。profile 的配置文件监听（HMR）会在保存后约 1 秒内
// 重新编排、无需重启；重启后也会按同一文件再次套用，因此该选择可通过官方机制
// 跨重启保留。
//
// 本模块按 dsh-market 的 dialect 逐行扫描补丁文件（刻意不做完整 YAML 解析）：
// 只需 `- id: X` + `disabled: true|false` 这一对，即可得知用户补丁层说了什么。
// 写入同样照搬 dsh-market 的安全做法：串行化写入避免并发交错，非法的条目数组
// 补丁文件拒绝追加以免越改越坏。
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// patchWriteMu 串行化补丁文件写入：并发启停不能交错一个 read-modify-write。
var patchWriteMu sync.Mutex

// rowIDRe 限定可写入补丁层的行 id（纯未加引号的 YAML 标量）。
var rowIDRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// disabledRowRe 匹配一行 `- id: X`（顶层条目）。
var disabledRowRe = regexp.MustCompile(`^- id: ([A-Za-z0-9_.-]+)\s*$`)

// disableBlockRe 匹配一个完整的禁用块 `- id: X\n  disabled: true\n`。
var disableBlockRe = regexp.MustCompile(`^- id: ['"]?[A-Za-z0-9_.-]+['"]?\r?\n  disabled: true\r?\n`)

// pluginPatchPath 返回 web profile 的 cordis.patch.yml 路径。
func pluginPatchPath(home string) string {
	return filepath.Join(home, ".dsh", "profiles", "web", "cordis.patch.yml")
}

// readDisabledPluginIds 扫描补丁层，返回被 `disabled: true` 停用的行 id。
func readDisabledPluginIds(patchPath string) []string {
	var disabled []string
	text, err := os.ReadFile(patchPath)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(text), "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		row := disabledRowRe.FindStringSubmatch(line)
		if row == nil {
			continue
		}
		var next string
		if i+1 < len(lines) {
			next = strings.TrimSpace(strings.TrimRight(lines[i+1], "\r"))
		}
		if next == "disabled: true" {
			disabled = append(disabled, row[1])
		}
	}
	return disabled
}

// setPluginDisabled 写入补丁层：disabled=true 追加/确保一个禁用块；
// disabled=false 移除该插件的禁用块（幂等）。补丁文件缺失或为空时按
// dsh-market 的 dialect 创建一个条目列表。
func setPluginDisabled(patchPath, id string, disabled bool) error {
	patchWriteMu.Lock()
	defer patchWriteMu.Unlock()

	if !rowIDRe.MatchString(id) {
		return fmt.Errorf("行 id 含特殊字符,不支持写入补丁层 / row id %s cannot be written to the patch layer", id)
	}

	if disabled {
		return appendDisableBlock(patchPath, id)
	}
	return removeDisableBlock(patchPath, id)
}

// appendDisableBlock 追加 `- id: X` + `disabled: true`；已存在则该次为 no-op。
func appendDisableBlock(patchPath, id string) error {
	for _, existing := range readDisabledPluginIds(patchPath) {
		if existing == id {
			return nil
		}
	}
	block := fmt.Sprintf("- id: %s\n  disabled: true\n", id)
	text, err := os.ReadFile(patchPath)
	if err != nil {
		// 文件不存在：直接以条目列表创建。
		if os.IsNotExist(err) {
			return os.WriteFile(patchPath, []byte(block), 0o644)
		}
		return err
	}
	core := strings.TrimSpace(stripComments(string(text)))
	if core == "" {
		// 只有注释或空：直接在末尾追加条目。
		next := string(text)
		if !strings.HasSuffix(next, "\n") {
			next += "\n"
		}
		return os.WriteFile(patchPath, []byte(next+block), 0o644)
	}
	if core == "[]" || core == "[ ]" {
		// dsh 模板自带的空 `[]` 占位：注释掉它再追加，避免一个文档里出现两个顶层元素。
		commented := regexp.MustCompile(`(?m)^[ \t]*\[[ \t]*\][ \t]*(?:#.*)?(?:\r?\n|$)`).
			ReplaceAllString(string(text), "# []\n")
		next := commented
		if !strings.HasSuffix(next, "\n") {
			next += "\n"
		}
		return os.WriteFile(patchPath, []byte(next+block), 0o644)
	}
	// 顶层以 flow 结构收尾：拒绝追加以免越改越坏。
	if lastContentLine := lastContentLine(string(text)); lastContentLine != "" &&
		(strings.HasPrefix(lastContentLine, "[") || strings.HasPrefix(lastContentLine, "{")) {
		return fmt.Errorf("补丁层以顶层流式结构结尾,不支持自动追加 / the patch layer ends in a top-level flow structure; refusing to append")
	}
	next := string(text)
	if !strings.HasSuffix(next, "\n") {
		next += "\n"
	}
	return os.WriteFile(patchPath, []byte(next+block), 0o644)
}

// removeDisableBlock 移除某插件的 `- id: X` + `disabled: true` 块（幂等）。
func removeDisableBlock(patchPath, id string) error {
	text, err := os.ReadFile(patchPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	blockRe := regexp.MustCompile(
		`(?m)^- id: ['"]?` + regexp.QuoteMeta(id) + `['"]?\r?\n  disabled: true\r?\n`,
	)
	next := blockRe.ReplaceAllString(string(text), "")
	if next == string(text) {
		return nil
	}
	// 移除最后一个块后若只剩注释/空白，补回空 `[]` 占位，避免 dsh 拒绝启动 profile。
	if strings.TrimSpace(stripComments(next)) == "" {
		restored := regexp.MustCompile(`(?m)^[ \t]*#[ \t]*\[[ \t]*\][ \t]*(?:\r?\n|$)`).
			ReplaceAllString(next, "[]\n")
		if restored != next {
			next = restored
		} else if next == "" || strings.HasSuffix(next, "\n") {
			next = next + "[]\n"
		} else {
			next = next + "\n[]\n"
		}
	}
	return os.WriteFile(patchPath, []byte(next), 0o644)
}

func stripComments(text string) string {
	return regexp.MustCompile(`(?m)^[ \t]*#.*$`).ReplaceAllString(text, "")
}

func lastContentLine(text string) string {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		return trimmed
	}
	return ""
}

// ---------------------------------------------------------------------------
// 包名 → 补丁行 id 映射（学自 dsh-market 的 rowIdsForPackage）
//
// 插件在 cordis.patch.yml 里以 loader 行 id 出现，而 dsh plugin 列表返回的是
// 包名；二者经常不一致（如 @linxin666/dsh-client-ui-git-graph 的 id 是
// ui-git-graph）。要正确启停，必须按包名读出它实际插入的行 id：读该包声明的
// dsh.bundle.patch（或根目录的 cordis.patch.yml），取其 insert 块下的 id 列表。
// 本函数复刻了 dsh-market 的 bundlePatchInsertedIds（#147：只算 insert 块里的
// 行，绝不把“仅重配置他人”的行当作本包的行）。
// ---------------------------------------------------------------------------

// parseInsertedIds 逐行扫描一个 bundle 补丁文本，返回 `insert:` 块下缩进的
// `- id: X` 行。dialect 同 dsh-market：注释被剥掉，非空行按缩进判断是否在
// insert 块内（缩进大于等于 insert 关键字且下一行以 `- id:` 开头即为插进行）。
func parseInsertedIds(text string) []string {
	var ids []string
	lines := strings.Split(text, "\n")
	insertIndent := -1
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		stripped := regexp.MustCompile(`#.*$`).ReplaceAllString(line, "")
		if strings.TrimSpace(stripped) == "" {
			continue
		}
		indent := len(stripped) - len(strings.TrimLeft(stripped, " \t"))
		trim := strings.TrimSpace(stripped)
		// 离开 insert 块：缩进回到 insert 关键字或更浅，且不再是其子条目
		if insertIndent >= 0 && indent <= insertIndent && !regexp.MustCompile(`^-\s+(?:id|name|config):`).MatchString(trim) {
			insertIndent = -1
		}
		if regexp.MustCompile(`^-\s+insert:\s*$`).MatchString(trim) {
			insertIndent = indent
			continue
		}
		if insertIndent >= 0 {
			m := regexp.MustCompile(`^-\s+id:\s*([A-Za-z0-9_.-]+)\s*$`).FindStringSubmatch(trim)
			if m != nil {
				ids = append(ids, m[1])
			}
		}
	}
	return ids
}

// readPackageRowIds 返回一个已安装包在补丁层拥有的行 id 列表（包名 → id 映射）。
// 同时读声明的 dsh.bundle.patch 与根目录 cordis.patch.yml（兼容两种声明位置）。
func readPackageRowIds(profileWebDir, packageName string) []string {
	seen := map[string]bool{}
	var ids []string
	add := func(patchPath string) {
		text, err := os.ReadFile(patchPath)
		if err != nil {
			return
		}
		for _, id := range parseInsertedIds(string(text)) {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	pkgDir := filepath.Join(profileWebDir, "node_modules", packageName)
	if manifest, err := os.ReadFile(filepath.Join(pkgDir, "package.json")); err == nil {
		var meta struct {
			Dsh struct {
				Bundle struct {
					Patch string `json:"patch"`
				} `json:"bundle"`
			} `json:"dsh"`
		}
		if json.Unmarshal(manifest, &meta) == nil && meta.Dsh.Bundle.Patch != "" {
			add(filepath.Join(pkgDir, meta.Dsh.Bundle.Patch))
		}
	}
	add(filepath.Join(pkgDir, "cordis.patch.yml"))
	return ids
}

// ---------------------------------------------------------------------------
// state.json 的 disabled 数组（学自 dsh-market 的 setPluginEnabled + writeMarketState）
//
// 启停的持久契约分两层：cordis.patch.yml 用行 id，state.json 的 disabled 数组
// 用包名（如 ["dsh-easyrewrite","@linxin666/dsh-client-ui-git-graph"]）。补丁层
// 驱动实际编排，state.json 供市场端做运行时判断/回放，二者必须保持同步。
// ---------------------------------------------------------------------------

// marketStateFile 返回 web profile 的市场状态文件路径。
func marketStateFile(profileWebDir string) string {
	return filepath.Join(profileWebDir, ".dsh-market", "state.json")
}

// readMarketDisabled 解析 state.json 的 disabled 数组（包名列表）。
func readMarketDisabled(profileWebDir string) []string {
	text, err := os.ReadFile(marketStateFile(profileWebDir))
	if err != nil {
		return nil
	}
	var state struct {
		Disabled []string `json:"disabled"`
	}
	if json.Unmarshal(text, &state) != nil {
		return nil
	}
	return state.Disabled
}

// writeMarketDisabled 在 state.json 的 disabled 数组中加入/移除一个包名（幂等），
// 保留其余字段（groups/groupOrder/region/notes/favorites 等）。
func writeMarketDisabled(profileWebDir, packageName string, disabled bool) error {
	patchWriteMu.Lock()
	defer patchWriteMu.Unlock()
	path := marketStateFile(profileWebDir)
	var state map[string]interface{}
	if text, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(text, &state) // 解析失败则视为空对象重建
	}
	if state == nil {
		state = map[string]interface{}{}
	}
	// 归一化 disabled 为字符串切片
	var names []string
	switch v := state["disabled"].(type) {
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				names = append(names, s)
			}
		}
	case []string:
		names = v
	}
	idx := -1
	for i, n := range names {
		if n == packageName {
			idx = i
			break
		}
	}
	had := idx >= 0
	if disabled && !had {
		names = append(names, packageName)
	} else if !disabled && had {
		names = append(names[:idx], names[idx+1:]...)
	}
	if names == nil {
		names = []string{}
	}
	state["disabled"] = names
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	out, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o600)
}

// packageHasClientPart 判断一个已装包是否声明了 dsh.client（其 UI 已注入页面，
// 切换后需刷新浏览器才能看到变化；学自 dsh-market 的 packageHasClientPart）。
func packageHasClientPart(profileWebDir, name string) bool {
	manifest, err := os.ReadFile(filepath.Join(profileWebDir, "node_modules", name, "package.json"))
	if err != nil {
		return false
	}
	var meta struct {
		Dsh struct {
			Client interface{} `json:"client"`
		} `json:"dsh"`
	}
	if json.Unmarshal(manifest, &meta) != nil {
		return false
	}
	return meta.Dsh.Client != nil
}

// holdsNativeAddon 判断一个已装包是否携带原生插件（build/Release、prebuilds、
// binding.gyp），或其依赖树里带原生插件。带原生插件或客户端部分的插件无法被
// 热重载即时生效，需重启 dsh 服务（学自 dsh-market 的 holdsNativeAddon）。
func holdsNativeAddon(profileWebDir, name string) bool {
	modules := filepath.Join(profileWebDir, "node_modules")
	shipsAddon := func(pkg string) bool {
		dir := filepath.Join(modules, pkg)
		if fileExists(filepath.Join(dir, "build", "Release")) ||
			fileExists(filepath.Join(dir, "prebuilds")) ||
			fileExists(filepath.Join(dir, "binding.gyp")) {
			return true
		}
		return false
	}
	if shipsAddon(name) {
		return true
	}
	manifest, err := os.ReadFile(filepath.Join(modules, name, "package.json"))
	if err != nil {
		return false
	}
	var meta struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if json.Unmarshal(manifest, &meta) != nil {
		return false
	}
	for dep := range meta.Dependencies {
		if packageNameRe.MatchString(dep) && shipsAddon(dep) {
			return true
		}
	}
	return false
}

// needsRestart 判断启用一个插件是否必须重启 dsh 服务才能生效。学自 dsh-market 的
// restart 判定：客户端插件（UI 在页面里）或带原生依赖的插件无法靠补丁层 HMR 即时
// 生效，需要重启；纯后端无原生依赖的插件走 HMR 即可。
func needsRestart(profileWebDir, name string) bool {
	return packageHasClientPart(profileWebDir, name) || holdsNativeAddon(profileWebDir, name)
}

// packageNameRe 用于校验依赖名是否为合法 npm 包名（与 dsh-market 的 PACKAGE_NAME_RE 对应）。
var packageNameRe = regexp.MustCompile(`^@?[a-zA-Z0-9][a-zA-Z0-9._-]*(?:\/[a-zA-Z0-9][a-zA-Z0-9._-]*)*$`)

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}