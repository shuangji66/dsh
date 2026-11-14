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
	"strings"
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
	logInfo("[plugin] update check %s: installed=%s latest=%s hasUpdate=%v (via %s)",
		name, installed.Version, latest, hasUpdate, mirror.mirrorLogName())
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
