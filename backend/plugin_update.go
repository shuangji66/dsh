package main

// plugin_update.go —— 插件卡片上的「检查更新」。
//
// 参考官方插件中心（dsh-plugin-manager 的 newerVersions + versionFacts + updateCandidate）：
// 只有**比当前版本新、非预发布、未被废弃、且与当前 dsh 版本兼容**的版本才算候选；已安装
// 的是 bundle 时，候选也必须仍然声明 dsh.bundle（否则装上去就不再是插件了）。官方先用
// `pnpm view` 拿版本列表、再逐版本读 manifest；控制台用镜像源的 packument 一次拿到全部
// 版本对象（与市场/dsh 版本检测同一个取法），判定规则一致、少打 N 次网络请求。
//
// 只对「profile 里记录的是 registry 版本范围/标签」的依赖做这项检查：Git / 本地路径安装
// 的包在 npm 上可能同名，拿镜像源的最新版去比会给出错误的「有更新」。
//
// 安装 / 升级仍走既有的后台任务（plugin_install.go）：`dsh plugin add <name>@<version>`。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// pluginUpdateCheck 是 GET /api/plugins/update-check 的成功载荷。
type pluginUpdateCheck struct {
	OK        bool   `json:"ok"`
	Name      string `json:"name"`
	Current   string `json:"current"`
	Latest    string `json:"latest,omitempty"`
	HasUpdate bool   `json:"hasUpdate"`
}

// 对应官方 install-spec 的 NON_REGISTRY_VALUE：带协议前缀的（file: / link: / github: /
// git+https: / https: …）与 scp 形式的 Git 地址不是 registry 依赖；npm / jsr / workspace /
// catalog 这几个别名仍按 registry 处理。Go 的 RE2 不支持负向前瞻，所以先取 scheme 再判断。
var (
	dependencySchemeRe = regexp.MustCompile(`^([a-z][a-z0-9+.-]*):`)
	scpDependencyRe    = regexp.MustCompile(`^[^@/:\s]+@[^:/\s]+:`)
)

// handlePluginUpdateCheck 检查一个已安装插件在所选镜像源上有没有可用的新版本。
func (m *AdminMux) handlePluginUpdateCheck(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		writeErrU(w, http.StatusBadRequest, "err_need_plugin_name", "缺少插件名")
		return
	}
	// 名字会用于读 node_modules/<name> 与 registry URL，先过 npm 包名校验。
	if !validPluginName(name) {
		writeErrU(w, http.StatusBadRequest, "err_plugin_name_invalid", "插件名不合法（仅允许 npm 包名）")
		return
	}
	profileWebDir := filepath.Join(m.dsh.effectiveHome(), ".dsh", "profiles", "web")
	spec, err := profileDependencySpec(profileWebDir, name)
	if err != nil {
		writeErrU(w, http.StatusInternalServerError, "err_plugin_update_check_failed",
			"检查更新失败（镜像源：%s）：%s", "mirror", selectedNpmMirror().Slug, "detail", err.Error())
		return
	}
	if !pluginDependencyFromRegistry(spec) {
		// 非 registry 安装（git / 本地 / tarball）：镜像源上的同名包可能是另一个东西。
		writeErrU(w, http.StatusBadRequest, "err_plugin_update_not_registry",
			"该插件不是从 npm 镜像源安装的（%s），无法检查版本更新", "spec", spec)
		return
	}
	installed, err := readPluginManifest(profileWebDir, name)
	if err != nil {
		writeErrU(w, http.StatusInternalServerError, "err_plugin_update_check_failed",
			"检查更新失败（镜像源：%s）：%s", "mirror", selectedNpmMirror().Slug,
			"detail", fmt.Sprintf("read installed package manifest: %v", err))
		return
	}

	mirror := selectedNpmMirror()
	versions, _, err := fetchMirrorPackument(mirrorClient(), mirror.URL, name)
	if err != nil {
		logWarn("[plugin] update check for %s failed (registry %s): %v", name, mirror.mirrorLogName(), err)
		writeErrU(w, http.StatusInternalServerError, "err_plugin_update_check_failed",
			"检查更新失败（镜像源：%s）：%s", "mirror", mirror.Slug, "detail", err.Error())
		return
	}
	latest := newestCompatiblePluginVersion(versions, installed, m.dsh.selectedDshVersion())
	hasUpdate := latest != "" && installed.Version != "" && compareVersion(latest, installed.Version) > 0
	// 只在确实有更新时记一行：这张卡片的检查按钮是用户随手点的，没更新也记会把日志刷满。
	if hasUpdate {
		logInfo("[plugin] update available: %s %s -> %s (via %s)",
			name, installed.Version, latest, mirror.mirrorLogName())
	}
	writeJSON(w, pluginUpdateCheck{OK: true, Name: name, Current: installed.Version, Latest: latest, HasUpdate: hasUpdate})
}

// profileDependencySpec 返回 profile 清单里记录的这个依赖值（`^1.2.3` / `github:…` 等）。
func profileDependencySpec(profileWebDir, name string) (string, error) {
	manifest, err := loadProfileManifest(profileWebDir)
	if err != nil {
		return "", fmt.Errorf("read profile manifest: %w", err)
	}
	deps, ok, err := manifest.object("dependencies")
	if err != nil {
		return "", fmt.Errorf("read profile dependencies: %w", err)
	}
	if !ok {
		return "", fmt.Errorf("profile declares no dependencies")
	}
	raw, ok := deps.vals[name]
	if !ok {
		return "", fmt.Errorf("plugin is not a profile dependency")
	}
	var spec string
	if err := json.Unmarshal(raw, &spec); err != nil {
		return "", fmt.Errorf("read dependency spec: %w", err)
	}
	return strings.TrimSpace(spec), nil
}

// pluginDependencyFromRegistry 判断 profile 记录的依赖值是不是 registry 版本范围/标签。
func pluginDependencyFromRegistry(spec string) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return false
	}
	// 绝对/相对路径（pnpm 记 file:/link: 时也可能直接写路径）不是 registry。
	if strings.HasPrefix(spec, "/") || strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") {
		return false
	}
	// scp 形式的 Git 地址（git@host:owner/repo）不是 registry。
	if scpDependencyRe.MatchString(spec) {
		return false
	}
	if match := dependencySchemeRe.FindStringSubmatch(strings.ToLower(spec)); match != nil {
		switch match[1] {
		case "npm", "jsr", "workspace", "catalog":
			return true
		default:
			return false
		}
	}
	return true
}

// newestCompatiblePluginVersion 选出「官方语义」下的更新候选：比当前版本新、非预发布、
// 未被废弃、与当前 dsh 版本兼容；已安装的是 bundle 时候选也必须声明 dsh.bundle。
// 没有候选时返回空串。
func newestCompatiblePluginVersion(versions map[string]json.RawMessage, installed *pluginManifest, runtimeVersion string) string {
	if installed == nil || installed.Version == "" {
		return ""
	}
	installedIsBundle := installed.declaresBundle()
	best := ""
	for version, raw := range versions {
		if !validVersionArg(version) {
			continue
		}
		if compareVersion(version, installed.Version) <= 0 {
			continue
		}
		// 官方 updateCandidate 不把预发布版当作更新候选（用户要尝鲜可以在安装弹窗里指定）。
		if _, pre := splitVersion(version); pre != "" {
			continue
		}
		var candidate struct {
			pluginManifest
			Deprecated string `json:"deprecated"`
		}
		if err := json.Unmarshal(raw, &candidate); err != nil {
			continue
		}
		if strings.TrimSpace(candidate.Deprecated) != "" {
			continue
		}
		if installedIsBundle && !candidate.declaresBundle() {
			continue
		}
		if runtimeVersion != "" {
			// 与启用前的兼容性栅栏同一套判定（engines.dsh / dsh peer 范围）。
			if issues, _ := pluginCompatibilityIssues(&candidate.pluginManifest, runtimeVersion); len(issues) > 0 {
				continue
			}
		}
		if best == "" || compareVersion(version, best) > 0 {
			best = version
		}
	}
	return best
}

// PluginUpdateInfo 是定时检查给「插件页里市场/增强插件之外的插件」算出的结论。
// 只有确实有更新时才进缓存；没有更新的插件不在表里（页面据此不显示箭头）。
type PluginUpdateInfo struct {
	LatestVersion string `json:"latestVersion"`
	HasUpdate     bool   `json:"hasUpdate"`
}

// pagePluginCheckConcurrency 是定时检查其它插件时的并发上限：每个插件都要打一次所选
// 镜像源，插件多时一次性全发既容易触发限流，也会和 dsh 自身的请求抢连接。
const pagePluginCheckConcurrency = 3

// managedPluginName 判断一个包是不是控制台单独管理的插件（市场 / 增强插件）：
// 它们有各自的完整检测链路，不该再被「其它插件」这一轮重复检查。
func managedPluginName(name string) bool {
	for _, p := range managedPlugins {
		if p.pkg == name {
			return true
		}
	}
	return false
}

// checkPagePluginsWith 检查插件页里除市场/增强插件之外的插件有没有新版本。
//
// 结果只写内存缓存（pageUpdates），供 GET /api/plugins/updates 轮询；不写入
// UpdateStatus、也不推 SSE —— 那三个槽位只属于 dsh / 市场 / 增强插件。
//
// 只对 profile 里记录为 registry 范围/标签的依赖生效（git/本地安装在镜像源上可能同名）；
// 单个插件失败不会清掉它上一次的结论，只在本轮结束时汇总记一条 WARN。
func (m *UpdateManager) checkPagePluginsWith(list pluginList) {
	if m == nil || m.dsh == nil || list.err != nil {
		// dsh 还没就绪 / 列表命令失败：这一轮跳过，下一小时再试（不为「查不了」刷日志）。
		// 列表成功但为空是另一回事：说明插件都被卸载了，下面会把旧结论清掉。
		return
	}
	profileWebDir := filepath.Join(m.dsh.effectiveHome(), ".dsh", "profiles", "web")
	runtimeVersion := m.dsh.selectedDshVersion()
	mirror := selectedNpmMirror()
	client := mirrorClient()

	names := make([]string, 0, len(list.versions))
	for name, version := range list.versions {
		if version == "" || managedPluginName(name) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names) // 稳定顺序：日志与测试都更好读

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		checked  = make(map[string]string, len(names)) // name -> 可用最新版（空串 = 没有更新）
		skipped  = make(map[string]bool)               // 非 registry 安装，不参与比较
		failures []string
	)
	sem := make(chan struct{}, pagePluginCheckConcurrency)
	for _, name := range names {
		name := name
		spec, err := profileDependencySpec(profileWebDir, name)
		if err != nil || !pluginDependencyFromRegistry(spec) {
			skipped[name] = true
			continue
		}
		installed, err := readPluginManifest(profileWebDir, name)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			versions, _, err := fetchMirrorPackument(client, mirror.URL, name)
			if err != nil {
				mu.Lock()
				failures = append(failures, fmt.Sprintf("%s: %v", name, err))
				mu.Unlock()
				return
			}
			latest := newestCompatiblePluginVersion(versions, installed, runtimeVersion)
			mu.Lock()
			checked[name] = latest
			mu.Unlock()
		}()
	}
	wg.Wait()
	m.applyPagePluginCheckResults(list, checked, skipped, failures, mirror)
}

// applyPagePluginCheckResults 把一轮检查结果合并进缓存，并按「有更新才记一行」输出日志。
//
// 合并规则：
//   - 已卸载的插件：清掉旧结论与日志去重记录；
//   - 非 registry 安装：清掉旧结论（镜像源上的同名包不可比）；
//   - 检查失败：保留上一次的结论（不把「查不了」当成「没更新」）；
//   - 查到了：有更新则写入缓存，没有更新则删除缓存。
func (m *UpdateManager) applyPagePluginCheckResults(
	list pluginList,
	checked map[string]string,
	skipped map[string]bool,
	failures []string,
	mirror npmMirror,
) {
	m.pageUpdatesMu.Lock()
	if m.pageUpdates == nil {
		m.pageUpdates = make(map[string]PluginUpdateInfo)
	}
	if m.pageUpdateLog == nil {
		m.pageUpdateLog = make(map[string]string)
	}
	for name := range m.pageUpdates {
		if _, ok := list.versions[name]; !ok {
			delete(m.pageUpdates, name)
		}
	}
	for name := range m.pageUpdateLog {
		if _, ok := list.versions[name]; !ok {
			delete(m.pageUpdateLog, name)
		}
	}
	for name := range skipped {
		delete(m.pageUpdates, name)
		delete(m.pageUpdateLog, name)
	}
	var toLog []string
	for _, name := range sortedKeys(checked) {
		latest := checked[name]
		if latest == "" {
			delete(m.pageUpdates, name)
			delete(m.pageUpdateLog, name)
			continue
		}
		m.pageUpdates[name] = PluginUpdateInfo{LatestVersion: latest, HasUpdate: true}
		if m.pageUpdateLog[name] != latest {
			m.pageUpdateLog[name] = latest
			toLog = append(toLog, fmt.Sprintf("%s %s -> %s", name, list.versions[name], latest))
		}
	}
	m.pageUpdatesMu.Unlock()

	for _, line := range toLog {
		logInfo("[plugin] update available: %s (via %s)", line, mirror.mirrorLogName())
	}
	if len(failures) > 0 {
		// 一次失败只记一条：每个插件一行的话，镜像源挂掉时每小时会刷出一整页。
		logWarn("[plugin] update check failed for %d plugin(s) (registry %s): %s",
			len(failures), mirror.mirrorLogName(), failures[0])
	}
}

// sortedKeys 返回 map 的键并排序（日志与测试用稳定顺序）。
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// pagePluginUpdates 返回一份当前缓存的副本（GET /api/plugins/updates）。
func (m *UpdateManager) pagePluginUpdates() map[string]PluginUpdateInfo {
	m.pageUpdatesMu.Lock()
	defer m.pageUpdatesMu.Unlock()
	out := make(map[string]PluginUpdateInfo, len(m.pageUpdates))
	for name, info := range m.pageUpdates {
		out[name] = info
	}
	return out
}

// clearPagePluginUpdate 丢掉某个插件的检查结论（装/卸/升级之后它就过期了）。
func (m *UpdateManager) clearPagePluginUpdate(name string) {
	m.pageUpdatesMu.Lock()
	defer m.pageUpdatesMu.Unlock()
	delete(m.pageUpdates, name)
	delete(m.pageUpdateLog, name)
}

// clearPagePluginUpdates 清空全部结论（重置插件用）。
func (m *UpdateManager) clearPagePluginUpdates() {
	m.pageUpdatesMu.Lock()
	defer m.pageUpdatesMu.Unlock()
	m.pageUpdates = make(map[string]PluginUpdateInfo)
	m.pageUpdateLog = make(map[string]string)
}

// handlePluginUpdates 返回定时检查缓存的「其它插件更新」结果。
//
// 只读内存、不联网、不起子进程：插件页按分钟级轮询它即可，不需要重跑
// `dsh plugin list`（那条命令要 1~3 秒）。
func (m *AdminMux) handlePluginUpdates(w http.ResponseWriter, r *http.Request) {
	updates := map[string]PluginUpdateInfo{}
	if m.update != nil {
		updates = m.update.pagePluginUpdates()
	}
	writeJSON(w, map[string]interface{}{"ok": true, "updates": updates})
}
