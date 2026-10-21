// profile.go —— 按官方 dsh 的 bundle 选择实现插件启用/停用。
//
// 官方 @deepseek-ai/dsh-plugin-manager 的包级开关（setBundleEnabled）只改
// profile 清单里的 `dsh.profile.bundles`：
//   - 停用：从有序列表里移除该包（依赖保留在 dependencies 中，不删 node_modules）；
//   - 启用：把包追加到列表末尾（可能改变配置优先级，这是官方行为）。
//
// profile 清单（package.json）由 dsh-hmr 监听，保存后会重新编排，无需重启 dsh；
// 停用也不会改写 cordis.patch.yml。控制台的插件页列的是 package 依赖，因此这里
// 直接复刻官方这一层，不再使用插件市场自己的 `.dsh-market/state.json`，也不再按
// 市场 dialect 猜测补丁行。
//
// 官方另有行级开关（cordis.patch.yml 的 `disabled`），那是 bundle 详情页里逐行
// 控制的；控制台没有行级 UI，不去改它。
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// profileWriteMu 串行化 profile 清单写入：并发启停不能交错一个 read-modify-write。
var profileWriteMu sync.Mutex

// errNotBundle 表示包没有声明 dsh.bundle，不能参与官方的 bundle 层启停。
var errNotBundle = errors.New("package does not declare dsh.bundle")

// packageNameRe 用于校验依赖名是否为合法 npm 包名（与官方 plugin-manager 的
// package name 规则对应）。
var packageNameRe = regexp.MustCompile(`^@?[a-zA-Z0-9][a-zA-Z0-9._-]*(?:\/[a-zA-Z0-9][a-zA-Z0-9._-]*)*$`)

// orderedObject 是一个保留键顺序的 JSON 对象。
//
// 官方 saveManifest 用 `JSON.stringify(manifest, null, 2)` 回写 profile 清单；
// JavaScript 对象保留插入顺序。Go 的 map 会把键排序，直接 Marshal 会让整个
// package.json 的字段顺序被打乱。这里只解析/回写顶层、dsh、profile 三层对象，
// 其余字段按原始 JSON 片段保留，尽量让清单只发生预期内的变化。
type orderedObject struct {
	keys []string
	vals map[string]json.RawMessage
}

func newOrderedObject() *orderedObject {
	return &orderedObject{vals: map[string]json.RawMessage{}}
}

// parseOrderedObject 解析一个 JSON 对象，保留键顺序与各值的原始片段。
func parseOrderedObject(raw []byte) (*orderedObject, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("expected a JSON object")
	}
	obj := newOrderedObject()
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("expected a JSON object key")
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
		obj.set(key, value)
	}
	if _, err := dec.Token(); err != nil { // closing '}'
		return nil, err
	}
	return obj, nil
}

// set 更新一个键；已存在时保持原位置，否则追加到末尾。
func (o *orderedObject) set(key string, value []byte) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = append(json.RawMessage(nil), value...)
}

// object 读取一个子对象；键不存在或值为 null 时返回 (nil, false, nil)。
func (o *orderedObject) object(key string) (*orderedObject, bool, error) {
	raw, ok := o.vals[key]
	if !ok {
		return nil, false, nil
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, false, nil
	}
	if trimmed[0] != '{' {
		return nil, false, fmt.Errorf("%s must be a JSON object", key)
	}
	parsed, err := parseOrderedObject(trimmed)
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", key, err)
	}
	return parsed, true, nil
}

// marshal 按保留的键顺序输出紧凑 JSON。
func (o *orderedObject) marshal() []byte {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, key := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		encodedKey, _ := json.Marshal(key)
		buf.Write(encodedKey)
		buf.WriteByte(':')
		buf.Write(o.vals[key])
	}
	buf.WriteByte('}')
	return buf.Bytes()
}

// marshalIndent 输出与官方 `JSON.stringify(..., null, 2)` 相近的缩进 JSON。
func (o *orderedObject) marshalIndent() ([]byte, error) {
	var out bytes.Buffer
	if err := json.Indent(&out, o.marshal(), "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// profileManifestPath 返回 web profile 的 package.json 路径。
func profileManifestPath(profileWebDir string) string {
	return filepath.Join(profileWebDir, "package.json")
}

// loadProfileManifest 读取并解析 profile 清单（保留键顺序）。
func loadProfileManifest(profileWebDir string) (*orderedObject, error) {
	raw, err := os.ReadFile(profileManifestPath(profileWebDir))
	if err != nil {
		return nil, err
	}
	return parseOrderedObject(raw)
}

// readProfileBundles 返回 profile 清单里的 `dsh.profile.bundles`（按声明顺序）。
// 缺少 dsh/profile/bundles 时返回空列表（视为没有选中任何 bundle）。
func readProfileBundles(profileWebDir string) ([]string, error) {
	manifest, err := loadProfileManifest(profileWebDir)
	if err != nil {
		return nil, err
	}
	dsh, ok, err := manifest.object("dsh")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	profile, ok, err := dsh.object("profile")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	raw, ok := profile.vals["bundles"]
	if !ok {
		return nil, nil
	}
	var bundles []string
	if err := json.Unmarshal(raw, &bundles); err != nil {
		return nil, fmt.Errorf("dsh.profile.bundles: %w", err)
	}
	return bundles, nil
}

// pluginManifest 是一个已安装插件 package.json 里控制台关心的字段。
//
// 一次读取同时服务于三处判定（启停开关是否可用、切换后是否建议重启、兼容性栅栏），
// 别在各处重复 os.ReadFile + json.Unmarshal。
type pluginManifest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Engines struct {
		Dsh *string `json:"dsh"`
	} `json:"engines"`
	PeerDependencies map[string]string `json:"peerDependencies"`
	Dependencies     map[string]string `json:"dependencies"`
	Dsh              struct {
		Bundle *struct {
			Patch json.RawMessage `json:"patch"`
		} `json:"bundle"`
		Client json.RawMessage `json:"client"`
	} `json:"dsh"`
}

// readPluginManifest 读取 profile/node_modules/<name>/package.json。
func readPluginManifest(profileWebDir, name string) (*pluginManifest, error) {
	raw, err := os.ReadFile(filepath.Join(profileWebDir, "node_modules", name, "package.json"))
	if err != nil {
		return nil, err
	}
	var meta pluginManifest
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// declaresBundle 判断包是否声明了可用的 `dsh.bundle.patch`。官方 bundleManifest 只把
// 带 patch 声明的包当作 bundle（`dsh.bundle: {}` 不算）；控制台的插件页也只对它们
// 开放启停开关（见 PluginInfo.Bundle 与 profile.go 的 setPluginBundleEnabled）。
func (m *pluginManifest) declaresBundle() bool {
	if m == nil || m.Dsh.Bundle == nil {
		return false
	}
	patch := bytes.TrimSpace(m.Dsh.Bundle.Patch)
	if len(patch) == 0 || bytes.Equal(patch, []byte("null")) {
		return false
	}
	var single string
	if err := json.Unmarshal(patch, &single); err == nil {
		return single != ""
	}
	var many []string
	if err := json.Unmarshal(patch, &many); err == nil {
		return len(many) > 0
	}
	return false
}

// hasClientPart 判断包是否声明了 dsh.client（其 UI 已注入页面，切换后可能需要刷新
// 浏览器才能看到变化）。`"client": false` / `null` 都表示没有客户端部分 —— 不能只判
// 「字段存在」：false 也会被解析成非 nil 的 interface。
func (m *pluginManifest) hasClientPart() bool {
	if m == nil {
		return false
	}
	raw := bytes.TrimSpace(m.Dsh.Client)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || bytes.Equal(raw, []byte("false")) {
		return false
	}
	return true
}

// packageDeclaresBundle 判断已安装包是否声明了可用的 `dsh.bundle.patch`。
func packageDeclaresBundle(profileWebDir, name string) (bool, error) {
	meta, err := readPluginManifest(profileWebDir, name)
	if err != nil {
		return false, err
	}
	return meta.declaresBundle(), nil
}

// setPluginBundleEnabled 按官方 setBundleEnabled 的语义写入 `dsh.profile.bundles`：
// 启用时校验 bundle 声明并追加到末尾；停用时移除所有同名条目（官方用 filter）。
// 返回清单是否真的发生了变化。
func setPluginBundleEnabled(profileWebDir, name string, enabled bool) (bool, error) {
	profileWriteMu.Lock()
	defer profileWriteMu.Unlock()

	manifest, err := loadProfileManifest(profileWebDir)
	if err != nil {
		return false, err
	}
	dsh, ok, err := manifest.object("dsh")
	if err != nil {
		return false, err
	}
	if !ok {
		dsh = newOrderedObject()
	}
	profile, ok, err := dsh.object("profile")
	if err != nil {
		return false, err
	}
	if !ok {
		profile = newOrderedObject()
	}

	var current []string
	if raw, ok := profile.vals["bundles"]; ok {
		if err := json.Unmarshal(raw, &current); err != nil {
			return false, fmt.Errorf("dsh.profile.bundles: %w", err)
		}
	}

	next := current
	if enabled || !containsString(current, name) {
		// 与官方一致：启用时（即使已在列表里）以及停用一个不在列表里的包时，
		// 都先确认它仍然是一个可解析的 bundle。
		declares, err := packageDeclaresBundle(profileWebDir, name)
		if err != nil {
			return false, fmt.Errorf("%w: %v", errNotBundle, err)
		}
		if !declares {
			return false, errNotBundle
		}
	}
	if enabled {
		if !containsString(current, name) {
			next = append(append([]string(nil), current...), name)
		}
	} else {
		next = make([]string, 0, len(current))
		for _, bundle := range current {
			if bundle != name {
				next = append(next, bundle)
			}
		}
	}

	if equalStrings(current, next) {
		return false, nil
	}

	bundlesJSON, err := json.Marshal(next)
	if err != nil {
		return false, err
	}
	profile.set("bundles", bundlesJSON)
	dsh.set("profile", profile.marshal())
	manifest.set("dsh", dsh.marshal())
	data, err := manifest.marshalIndent()
	if err != nil {
		return false, err
	}
	if err := writeFileAtomic0600(profileManifestPath(profileWebDir), data); err != nil {
		return false, err
	}
	return true, nil
}

// writeFileAtomic0600 先写临时文件再 rename，避免进程中途退出留下半个 package.json。
// 权限与官方 saveManifest 一致（0600）。
func writeFileAtomic0600(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".package.json.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	renamed = true
	return nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// packageHasClientPart 判断一个已装包是否声明了 dsh.client。
func packageHasClientPart(profileWebDir, name string) bool {
	meta, err := readPluginManifest(profileWebDir, name)
	if err != nil {
		return false
	}
	return meta.hasClientPart()
}

// holdsNativeAddon 判断一个已装包是否携带原生插件（build/Release、prebuilds、
// binding.gyp），或其依赖树里带原生插件。带原生插件的插件可能无法靠 HMR 即时
// 生效，控制台据此给出更保守的重启提示。
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
	var meta pluginManifest
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

// needsRestart 给出控制台的保守提示：客户端插件或带原生依赖的插件切换后，
// 控制台建议重启 dsh 服务（官方 HMR 通常能即时重组，但客户端 UI 与原生模块
// 在控制台这一侧无法确认是否已经生效）。纯后端插件走 HMR 即可。
func needsRestart(profileWebDir, name string) bool {
	return packageHasClientPart(profileWebDir, name) || holdsNativeAddon(profileWebDir, name)
}

// --- 官方兼容性栅栏（对齐 @deepseek-ai/dsh-plugin-manager 的 selectBundle）---

// pluginCompatibilityIssues 复刻官方 evaluatePluginCompatibility 的判定，返回未满足的
// 要求（空 = 兼容）。第二个返回值表示「范围语法是否都被本实现解析」：false 时调用方
// 记一条 WARN —— 结论仍按官方语义（无效范围 → 不满足，见 semverSatisfies）。
//
// 规则（与 @deepseek-ai/dsh-app-boot 的 unmetRequirements 一致）：
//   - 声明了 `engines.dsh` 时**只看它**，peerDependencies 不参与；
//   - 否则检查所有 `@deepseek-ai/dsh` / `@deepseek-ai/dsh-*` 的 peer 范围；
//   - peer 里的 `workspace:^` / `workspace:~` / `workspace:*` 指当前运行时（恒满足），
//     其它 `workspace:` 写法视为不满足；非 dsh 的 peer 一律忽略。
func pluginCompatibilityIssues(meta *pluginManifest, runtimeVersion string) (issues map[string]string, parsed bool) {
	if meta == nil {
		return nil, true
	}
	if meta.Engines.Dsh != nil {
		rangeSpec := *meta.Engines.Dsh
		satisfied, ok := semverSatisfies(runtimeVersion, rangeSpec)
		if !ok {
			// 解析不了 = 无效范围：按官方语义算不满足，同时把解析缺口标出来。
			return map[string]string{"engines.dsh": rangeSpec}, false
		}
		if !satisfied {
			return map[string]string{"engines.dsh": rangeSpec}, true
		}
		return nil, true
	}
	issues = map[string]string{}
	parsed = true
	for name, rangeSpec := range meta.PeerDependencies {
		if name != "@deepseek-ai/dsh" && !strings.HasPrefix(name, "@deepseek-ai/dsh-") {
			continue
		}
		if strings.HasPrefix(rangeSpec, "workspace:") {
			switch rangeSpec {
			case "workspace:^", "workspace:~", "workspace:*":
				continue
			}
			issues[name] = rangeSpec
			continue
		}
		satisfied, ok := semverSatisfies(runtimeVersion, rangeSpec)
		if !ok {
			// 同 engines.dsh：解析不了按无效范围处理（不满足），并标记解析缺口。
			parsed = false
			issues[name] = rangeSpec
			continue
		}
		if !satisfied {
			issues[name] = rangeSpec
		}
	}
	return issues, parsed
}

// checkPluginCompatibility 读取已装插件并给出它在 runtimeVersion 下的不兼容明细。
// manifestName / version 用于查放行记录（compatibility.json 的键是 `name@version`，
// 官方用的是 manifest 里的 name，依赖被 alias 时可能与目录名不同）；解析失败按兼容处理
// （后面的 setPluginBundleEnabled 会报真正的读取错误）。
func checkPluginCompatibility(profileWebDir, name, runtimeVersion string) (issues map[string]string, manifestName, version string, parsed bool) {
	meta, err := readPluginManifest(profileWebDir, name)
	if err != nil {
		return nil, "", "", true
	}
	issues, parsed = pluginCompatibilityIssues(meta, runtimeVersion)
	return issues, meta.Name, meta.Version, parsed
}

// requirementText 把未满足的要求拼成一行可读文本（按名字排序，结果稳定）：
// `engines.dsh: <0.2.1-0, peerDependencies[@deepseek-ai/dsh]: ^0.2.1-rc.1`。
// 这是原始 manifest 数据，不进 i18n 字典（参数里原样展示）。
func requirementText(issues map[string]string) string {
	if len(issues) == 0 {
		return ""
	}
	names := make([]string, 0, len(issues))
	for name := range issues {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		label := name
		if name != "engines.dsh" {
			label = "peerDependencies[" + name + "]"
		}
		parts = append(parts, label+": "+issues[name])
	}
	return strings.Join(parts, ", ")
}

// profileVersionExemptions 读取 profile 里的 compatibility.json（官方
// `dsh plugin allow-version` 写的那份放行记录：`{"name@version": ["0.2.1-rc.2"]}`）。
// 读不到/格式不对按「没有放行记录」处理 —— 与官方一致：坏文件不会让 profile 不可用。
func profileVersionExemptions(profileWebDir string) map[string][]string {
	raw, err := os.ReadFile(filepath.Join(profileWebDir, "compatibility.json"))
	if err != nil {
		return nil
	}
	var parsed map[string]json.RawMessage
	if json.Unmarshal(raw, &parsed) != nil {
		return nil
	}
	out := make(map[string][]string, len(parsed))
	for key, value := range parsed {
		var versions []string
		if json.Unmarshal(value, &versions) != nil {
			continue
		}
		out[key] = versions
	}
	return out
}

// pluginVersionExempt 判断 `name@version` 是否在 compatibility.json 里被显式放行到 runtimeVersion。
func pluginVersionExempt(profileWebDir, name, version, runtimeVersion string) bool {
	if version == "" {
		return false
	}
	for _, exempted := range profileVersionExemptions(profileWebDir)[name+"@"+version] {
		if exempted == runtimeVersion {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
