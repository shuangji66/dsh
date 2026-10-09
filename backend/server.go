package main

// server.go —— dsh 服务的「多版本下载 / 切换」。
//
// 与旧实现的根本区别：dsh 服务**不再是内置在 fpk 安装包里的预构建产物**，也不再从
// GitHub Release 下载压缩包。控制台改为按需从国内 npm 镜像源安装官方 npm 包：
//
//	${TRIM_PKGVAR}/server/<版本>/node_modules/.bin/dsh
//
// 安装方式就是 dsh 官方的 npm 安装方式（`npm install --prefix <版本目录> @deepseek-ai/dsh`），
// 于是目录结构与「本机 npm 装的 dsh」完全一致：换版本 = 换一个版本目录，控制台把该
// 目录的 node_modules/.bin 前置到 dsh 的 PATH（见 DshManager.buildEnv）。没有压缩包
// 备份、没有整目录替换、没有回滚 —— 旧版本目录留着就是「回退」。
//
// 几条硬约束（每条都对应一个真实约束/踩过的坑）：
//  1. 【只走国内镜像源】阿里云 → 腾讯云 → 华为云，5 秒无响应即换下一个，**不重试**，
//     三个都失败就报错。刻意不试官方 registry.npmjs.org（用户要求）。
//  2. 【5 秒 = npm 单请求超时】整棵依赖树约 500MB / 45 秒（实测），按「整包 5 秒」
//     判定会把每次安装都判死。因此用 `--fetch-timeout=5000 --fetch-retries=0`：
//     任意一次 HTTP 请求 5 秒拿不到响应即该请求失败，进而整体换下一个镜像源重跑。
//  3. 【下载后必须打附件 fsync 补丁】dsh 的 attachment-local 落盘附件时会逐级上溯
//     fsync 到文件系统根 "/"，而 fnOS 把 /vol1 与 /vol1/@appshare 设为 mode 000
//     （仅由 trim_acl 授予穿越），非 root 打不开 → EACCES → WEB 端上传文件/图片整体
//     失败。这个补丁过去由 CI 打在预构建包里（见 .github/workflows/server-build.yaml），
//     现在必须由控制台在 npm install 之后补上（patchAttachmentFsync）。
//  4. 【版本列表来源也是镜像源，且过滤掉 0.1.7-alpha.1 之前】0.1.7-alpha.1 起 dsh 前端
//     才全部使用文档相对路径，能在控制台的子路径挂载下正常工作（更早的版本必然 404）。
//  5. 【取消必须清干净】安装目录与 npm 缓存一起删：否则下次「重新下载」会命中半成品。

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// dshPackageName 是 dsh 在 npm 上的包名。
	dshPackageName = "@deepseek-ai/dsh"
	// dshMinVersion 是控制台允许下载/切换的最低 dsh 版本：**列表里更早的版本一律隐藏**
	// （用户要求从 0.1.7-rc.1 起，见 filterDshVersions）。
	// 技术背景：0.1.7-alpha.1 起 dsh 前端才全部走文档相对路径（靠 <base href="./"> 自己
	// 拼出挂载前缀），更早的版本在控制台的子路径挂载（HARNESS_PROXY_BASEURL，默认
	// /app/Harness/dsh）下必然 404；这条底线现在抬到 rc.1，比「能跑」的最低要求更严。
	dshMinVersion = "0.1.7-rc.1"
	// dshMirrorTimeout 是单次镜像源请求的超时（「5 秒没反应就换下一个」）。
	dshMirrorTimeout = 5 * time.Second
	// dshInstallCancelGrace 是取消安装时先 SIGTERM、再 SIGKILL 的宽限时间。
	dshInstallCancelGrace = 3 * time.Second
	// dshVersionVerifyTimeout 是安装完成后执行 `<版本>/node_modules/.bin/dsh -V` 的上限。
	dshVersionVerifyTimeout = 60 * time.Second
	// dshMaxPackument 是版本元数据响应的读取上限（dsh 的 packument 约 1~3MB）。
	dshMaxPackument = 16 << 20
	// dshInstallLogTail 是保留的 npm 输出尾行长度上限（只用于展示）。
	dshInstallLogTail = 400
)

// npmMirror 是一个 npm 镜像源。
//
// **显示名与日志标识是两回事，别混用**：
//   - Name 中文显示名：只进界面（进度文案「正在从 阿里云 下载 …」、安装状态里的源标签）；
//   - Slug 英文标识：只进日志与错误链（日志一律英文，见 AGENTS 第 4 节规则 7）。
//
// 为什么错误链也用 Slug 而不是中文名：这些错误会被 `logError(..., err)` 原样打进日志
// （插件市场操作失败那条就是），中文名一旦混进去，日志里就又出现了中文镜像名。
type npmMirror struct {
	Name string // 显示名（仅界面）
	Slug string // 日志/错误链里的英文标识（aliyun / tencent / huawei）
	URL  string // registry 地址（不带尾斜杠）
}

// mirrorLogName 返回该镜像源在日志/错误链里的英文标识：优先 Slug，Slug 为空时退回
// URL 主机名（单测里临时构造的镜像源就是这种，不必为每个假源补 Slug）。
func (m npmMirror) mirrorLogName() string {
	if s := strings.TrimSpace(m.Slug); s != "" {
		return s
	}
	if u, err := url.Parse(m.URL); err == nil && u.Host != "" {
		return u.Host
	}
	return m.URL
}

// npmMirrors 是 dsh 与插件市场的镜像源候选，顺序固定为阿里云 → 腾讯云 → 华为云。
// 刻意不含官方 registry.npmjs.org：国内设备直连官方源普遍不可用，混进去只会让
// 「都失败就报错」这个结论变得不可信。
var npmMirrors = []npmMirror{
	{Name: "阿里云", Slug: "aliyun", URL: "https://registry.npmmirror.com"},
	{Name: "腾讯云", Slug: "tencent", URL: "https://mirrors.cloud.tencent.com/npm"},
	{Name: "华为云", Slug: "huawei", URL: "https://repo.huaweicloud.com/repository/npm"},
}

// mirrorLogNames 返回全部镜像源的日志标识（英文，逗号分隔）。
// 日志、以及**可能被日志打印的错误链**一律用它；中文显示名只进界面文案。
func mirrorLogNames() string {
	names := make([]string, 0, len(npmMirrors))
	for _, m := range npmMirrors {
		names = append(names, m.mirrorLogName())
	}
	return strings.Join(names, ", ")
}

// versionArgRe 校验版本号字符集：只允许 semver 字符，绝不允许路径分隔符或 ".."
// （版本号会直接成为目录名，也会作为 npm 参数拼接 —— dsh 版本与插件市场版本共用）。
var versionArgRe = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+-]*$`)

// validVersionArg 判断版本号是否可用作目录名与 npm 参数（dsh 版本与市场版本共用）。
func validVersionArg(v string) bool {
	return v != "" && len(v) <= 64 && versionArgRe.MatchString(v) && !strings.Contains(v, "..")
}

// --- server 目录路径 ---
//
// 「dsh 各版本装在哪」只有下面两个函数一个入口：DshManager（PATH 注入、启动）与
// ServerManager（列表/安装/删除/切换）共用，避免两处各拼一次路径而漂移。

// serverRootFor 返回 dsh 版本根目录：统一数据目录下的 server/
// （默认即 ${TRIM_PKGVAR}/server）。renv 为空时退回环境变量解析（单测用）。
func serverRootFor(renv *RuntimeEnv) string {
	dir := ""
	if renv != nil && renv.DataDir != "" {
		dir = renv.DataDir
	} else {
		dir = dataDirFromEnv()
	}
	return filepath.Join(dir, "server")
}

// versionDirFor 返回某个 dsh 版本的安装目录。
func versionDirFor(renv *RuntimeEnv, version string) string {
	return filepath.Join(serverRootFor(renv), version)
}

// versionBinDirFor 返回某个 dsh 版本的 node_modules/.bin 目录（要前置到 PATH 的那个）。
func versionBinDirFor(renv *RuntimeEnv, version string) string {
	return filepath.Join(versionDirFor(renv, version), "node_modules", ".bin")
}

// versionDshBinFor 返回某个 dsh 版本里 dsh 可执行文件的路径。
//
// 必须是**绝对路径**：`exec.Command("dsh")` 的 LookPath 走的是 harness 自己的 PATH，
// 而不是我们要下发给子进程的 PATH —— 旧代码依赖 fpk 注入的
// `/var/apps/Harness/target/server/node_modules/.bin`，那条路径在新形态下已不存在。
func versionDshBinFor(renv *RuntimeEnv, version string) string {
	return filepath.Join(versionBinDirFor(renv, version), "dsh")
}

// dshVersionInstalled 判断某个版本目录里是否有一份可用的 dsh 可执行文件。
func dshVersionInstalled(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, "node_modules", ".bin", "dsh"))
	return err == nil && fi.Mode().IsRegular()
}

// --- 版本元数据 ---

// DshVersionEntry 是版本列表里的一行（镜像源上的一个版本，或本地已安装但镜像上
// 已下架的版本）。
type DshVersionEntry struct {
	Version string `json:"version"`
	// Tags 是该版本命中的 npm dist-tag（latest / alpha / next …，可能多个）。
	Tags []string `json:"tags,omitempty"`
	// Installed 表示本地已有这份安装（换版本不需要重新下载）。
	Installed bool `json:"installed,omitempty"`
	// Active 表示它正是当前选中的版本。
	Active bool `json:"active,omitempty"`
}

// DshInstallState 描述一次「下载并安装某个 dsh 版本」的进行中状态。
// 同一时刻只允许一个安装（见 applying），因此整份状态是单槽的。
type DshInstallState struct {
	Version string `json:"version"`
	// Mirror 是当前正在使用的镜像源显示名（阿里云 / 腾讯云 / 华为云）。
	Mirror string `json:"mirror,omitempty"`
	// Phase：""（空闲）/ downloading（解析并下载依赖）/ installing（npm 处理依赖树：
	// 解压、执行依赖构建脚本）/ verifying（校验可执行文件并打补丁）/ done / error。
	Phase string `json:"phase,omitempty"`
	// Fetched 是 npm 已完成的包获取次数（--loglevel=http 的计数），单调递增。
	// 依赖总量 npm 不会预先给出，因此前端不显示百分比，只显示这个计数 + 不确定进度条。
	Fetched int `json:"fetched,omitempty"`
	// Message 是 npm 最近一行输出（尾行），让用户看到「正在做什么」。
	Message string `json:"message,omitempty"`
	// Error 非空表示安装失败（错误文本）。用户取消不置错误，只置 Cancelled。
	Error string `json:"error,omitempty"`
	// Cancelled 表示这次安装被用户取消（安装目录与下载缓存都已清除）。
	Cancelled bool `json:"cancelled,omitempty"`
	// Seq 是「第几次安装」的序号（每次 Install +1）：前端拿它做「结果只提示一次」的
	// 去重键。只按 version+phase 去重时，「同一个版本装成功两次」（装成功 → 删掉 →
	// 再装成功）的第二次会被静默吞掉。
	Seq int64 `json:"seq,omitempty"`
	// ErrorRef / MessageRef 是 Error / Message 的界面文案引用（code + 参数，见 uimsg.go）：
	// 前端按 code 走 i18n，取不到才回退原文。npm 原始输出尾行这类诊断文本不设引用。
	ErrorRef   *uiMsg `json:"errorRef,omitempty"`
	MessageRef *uiMsg `json:"messageRef,omitempty"`
}

// ServerVersions 是给前端/接口的 dsh 版本快照（列表 + 选中 + 安装进度）。
type ServerVersions struct {
	// Selected 是当前选中的版本；空串表示「未安装」（没有选中任何可用版本）。
	Selected string `json:"selected"`
	// Installed 是本地已安装的版本（版本号降序）。删掉选中版本之外的任何一个都可以。
	Installed []string `json:"installed"`
	// Versions 是可选版本列表（镜像源 + 本地独有版本，版本号降序，已过滤 0.1.7-alpha.1 之前）。
	Versions []DshVersionEntry `json:"versions"`
	// Latest 是列表里**版本号最高**的一版（= 界面上的「最新版本」与红点判定基准；
	// 刻意不看 dist-tags，见 refreshDshStatus）。
	Latest string `json:"latest,omitempty"`
	// Tags 是镜像源的 dist-tags（原样透出，前端可用作诊断）。
	Tags      map[string]string `json:"tags,omitempty"`
	CheckedAt time.Time         `json:"checkedAt"`
	// Error 是最近一次版本列表拉取失败的原因（为空表示正常）。
	Error string `json:"error,omitempty"`
	// Install 非空表示有安装正在进行（或刚结束但状态未被下一次操作覆盖）。
	Install *DshInstallState `json:"install,omitempty"`
}

// ServerManager 管理 dsh 各版本的列表、安装、删除与切换。
type ServerManager struct {
	renv *RuntimeEnv
	dsh  *DshManager
	upd  *UpdateManager

	// mu 保护下面的缓存字段与 install 状态。
	mu       sync.Mutex
	versions []DshVersionEntry
	tags     map[string]string
	// newest 是列表里版本号最高的一版（红点与「最新版本」的唯一基准）。
	newest string
	// latestTag 是镜像源的 dist-tags.latest，**只用于日志诊断**，不参与任何判定：
	// 标签可能滞后或指向另一条线（见 refreshDshStatus 的说明）。
	latestTag string
	checkedAt time.Time
	verErr    string
	install   *DshInstallState

	// applying 串行化「安装 / 删除 / 切换」三类会改盘或停 dsh 的操作。
	applying sync.Mutex

	// cancelInstall 取消正在进行的那次安装（nil 表示没有安装在进行）。
	cancelInstall context.CancelFunc
	// installDone 在安装 goroutine 结束时关闭（安装收尾要清理目录/缓存，
	// 取消后前端刷新要能看到「已取消」而不是又看到半成品）。
	installDone chan struct{}
	// installSeq 是安装序号（每次 Install 自增，写进 DshInstallState.Seq 供前端去重）。
	installSeq int64

	// 节流 SSE 推送：npm 输出很密，逐行推送只会把连接刷满。
	notifyMu   sync.Mutex
	lastNotify time.Time
}

func newServerManager(renv *RuntimeEnv, dsh *DshManager, upd *UpdateManager) *ServerManager {
	return &ServerManager{renv: renv, dsh: dsh, upd: upd}
}

// LookupVersionNotes 取某个 dsh 版本的更新日志（来自上游 deepseek-ai/deepseek-harness
// 的 GitHub Release，tag 形如 `dsh-v0.2.1-alpha.2`）。第二个返回值为 false 表示
// **这次没取到**（断网 / 限流）—— 与「取到了但该版本没有日志」（true + 空）区分开，
// 前端据此决定说「暂时取不到，稍后重试」还是「该版本没有更新日志」。
func (m *ServerManager) LookupVersionNotes(version string) (releaseNotes, bool) {
	if m.upd == nil || m.upd.dshNotes == nil {
		return releaseNotes{}, false
	}
	return m.upd.dshNotes.Lookup(version)
}

// notify 广播一次状态变更（默认节流 300ms；phase 变化等关键节点用 force=true）。
func (m *ServerManager) notify(force bool) {
	m.notifyMu.Lock()
	if !force && time.Since(m.lastNotify) < 300*time.Millisecond {
		m.notifyMu.Unlock()
		return
	}
	m.lastNotify = time.Now()
	m.notifyMu.Unlock()
	if m.upd != nil {
		m.upd.notify()
	}
}

// --- 本地已安装版本 ---

// installedVersions 扫描版本根目录，返回「确实有一份可用 dsh」的版本号（降序）。
func (m *ServerManager) installedVersions() []string {
	entries, err := os.ReadDir(m.serverRoot())
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || !validVersionArg(e.Name()) {
			continue
		}
		if dshVersionInstalled(filepath.Join(m.serverRoot(), e.Name())) {
			out = append(out, e.Name())
		}
	}
	sort.Slice(out, func(i, j int) bool { return compareVersion(out[i], out[j]) > 0 })
	return out
}

// selectedVersion 返回当前选中的版本：配置里的版本号必须**确实装着**才算数
// （目录被外部删掉时退回空串 = 未安装），避免控制台显示一个跑不起来的版本号。
func (m *ServerManager) selectedVersion() string {
	v := strings.TrimSpace(GetConfig().DshVersion)
	if v == "" || !validVersionArg(v) {
		return ""
	}
	if !dshVersionInstalled(filepath.Join(m.serverRoot(), v)) {
		return ""
	}
	return v
}

// snapshot 组装一份版本快照（纯读盘 + 读内存，不联网）。
func (m *ServerManager) snapshot() ServerVersions {
	m.mu.Lock()
	versions := append([]DshVersionEntry(nil), m.versions...)
	tags := make(map[string]string, len(m.tags))
	for k, v := range m.tags {
		tags[k] = v
	}
	newest, checkedAt, verErr := m.newest, m.checkedAt, m.verErr
	install := m.install
	if install != nil {
		cp := *install
		install = &cp
	}
	m.mu.Unlock()

	selected := m.selectedVersion()
	installed := m.installedVersions()
	have := make(map[string]bool, len(installed))
	for _, v := range installed {
		have[v] = true
	}
	seen := make(map[string]bool, len(versions))
	for i := range versions {
		versions[i].Installed = have[versions[i].Version]
		versions[i].Active = selected != "" && versions[i].Version == selected
		seen[versions[i].Version] = true
	}
	// 镜像源已下架、但本地装着的版本也要出现在列表里：否则用户既看不到它、也没法
	// 删掉它（磁盘会一直占着 500MB）。
	for _, v := range installed {
		if seen[v] {
			continue
		}
		versions = append(versions, DshVersionEntry{
			Version: v, Installed: true, Active: selected == v,
		})
	}
	sort.SliceStable(versions, func(i, j int) bool {
		return compareVersion(versions[i].Version, versions[j].Version) > 0
	})

	return ServerVersions{
		Selected:  selected,
		Installed: installed,
		Versions:  versions,
		Latest:    newest,
		Tags:      tags,
		CheckedAt: checkedAt,
		Error:     verErr,
		Install:   install,
	}
}

// serverRoot 是本管理器使用的版本根目录。
func (m *ServerManager) serverRoot() string { return serverRootFor(m.renv) }

// versionDir 返回某个版本的安装目录。
func (m *ServerManager) versionDir(v string) string { return versionDirFor(m.renv, v) }

// npmCacheDir 返回控制台自己的 npm 缓存目录。
//
// 刻意不用默认的 `$HOME/.npm`：dsh 的 HOME 是本应用的共享目录
// （/vol1/@appshare/Harness），飞牛的共享目录权限模型下应用写进去的文件可能连属主
// 都读不到（见 AGENTS 的「主目录不可切换」一节）。放在应用自己的数据目录里既安全，
// 也便于「取消下载」时随安装目录一起清掉。
func (m *ServerManager) npmCacheDir() string {
	dir := ""
	if m.renv != nil && m.renv.DataDir != "" {
		dir = m.renv.DataDir
	} else {
		dir = dataDirFromEnv()
	}
	return filepath.Join(dir, "npm-cache")
}

// --- 镜像源版本列表 ---

// mirrorClient 构造访问 npm 镜像源的 HTTP 客户端：单次请求 5 秒上限。
//
// 刻意直连不管「代理更新」开关：那个开关只作用于 GitHub 发布资产（harness 自更新），
// 国内镜像源经代理反而更慢或不可达。
func mirrorClient() *http.Client {
	return &http.Client{Timeout: dshMirrorTimeout}
}

// fetchMirrorPackument 从某个镜像源读取指定包的版本元数据（versions + dist-tags）。
// dsh 版本列表与插件市场的最新版都走它（两个包的取法完全一样）。
func fetchMirrorPackument(client *http.Client, registry, pkg string) (map[string]json.RawMessage, map[string]string, error) {
	url := strings.TrimRight(registry, "/") + "/" + strings.ReplaceAll(pkg, "/", "%2f")
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "harness-console")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, dshMaxPackument))
	if err != nil {
		return nil, nil, err
	}
	var doc struct {
		Name     string                     `json:"name"`
		Versions map[string]json.RawMessage `json:"versions"`
		DistTags map[string]string          `json:"dist-tags"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, nil, fmt.Errorf("failed to parse registry metadata: %w", err)
	}
	if len(doc.Versions) == 0 {
		return nil, nil, fmt.Errorf("registry metadata carries no version list")
	}
	return doc.Versions, doc.DistTags, nil
}

// filterDshVersions 把镜像源上的版本号过滤成可用列表（隐藏 dshMinVersion 之前的，降序）。
// 列表里的**最高版本**就是「最新版本」与红点的判定基准（见 refreshDshStatus）。
func filterDshVersions(versions map[string]json.RawMessage, tags map[string]string) []DshVersionEntry {
	byVersion := make(map[string][]string)
	for tag, ver := range tags {
		byVersion[ver] = append(byVersion[ver], tag)
	}
	out := make([]DshVersionEntry, 0, len(versions))
	for v := range versions {
		if !validVersionArg(v) {
			continue
		}
		if compareVersion(v, dshMinVersion) < 0 {
			continue
		}
		entry := DshVersionEntry{Version: v}
		if ts := byVersion[v]; len(ts) > 0 {
			sort.Strings(ts)
			entry.Tags = ts
		}
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return compareVersion(out[i].Version, out[j].Version) > 0
	})
	return out
}

// newestOf 取已过滤版本列表里**版本号最高**的一版（空列表返回空串）。
// 逐项比较，不依赖调用方是否排过序。
func newestOf(list []DshVersionEntry) string {
	best := ""
	for _, e := range list {
		if best == "" || compareVersion(e.Version, best) > 0 {
			best = e.Version
		}
	}
	return best
}

// newestVersion 从 packument 的版本集合里挑出**版本号最高**的那一版（semver 语义，
// 由 compareVersion 决定），**不看 dist-tags**。
//
// 为什么不用 `latest` 标签：标签是发布者手动移动的元数据 —— 可能滞后（发了新版忘了挪标签）、
// 可能指向另一条线（稳定线 vs 预览线），镜像源同步时也可能只更新了版本集合而没更新标签。
// 「实际最新的一版」只看版本号本身。dsh 版本列表与插件市场安装都用它来定位「最新版本号」。
func newestVersion(versions map[string]json.RawMessage) string {
	best := ""
	for v := range versions {
		if !validVersionArg(v) {
			continue
		}
		if best == "" || compareVersion(v, best) > 0 {
			best = v
		}
	}
	return best
}

// refreshVersions 取一次版本列表：**缓存优先**。
//
// force=false（打开弹窗、常规状态刷新）在已有缓存时直接返回、**完全不联网**；只有缓存
// 还空着（控制台刚启动、第一次检测还没跑完）才去拉。这是刻意的：版本列表是「镜像源上
// 有什么」的快照，不该每打开一次弹窗就打三个镜像源。
//
// 刷新缓存的入口只有两个（都走 force=true）：
//   - 用户在版本弹窗里点「刷新」（`GET /api/dsh/versions?refresh=1`）；
//   - 后台自动检测（每小时）与手动「检查更新」（`/api/update/check`）——
//     它们本来就要联网判断有没有新版本，顺带把这份列表刷新掉。
//
// 拉取失败时**保留上一份缓存**（旧列表仍然可用），只把 verErr 写给界面。
func (m *ServerManager) refreshVersions(force bool) error {
	m.mu.Lock()
	cached := len(m.versions) > 0
	m.mu.Unlock()
	if !force && cached {
		return nil
	}

	client := mirrorClient()
	var lastErr error
	for _, mirror := range npmMirrors {
		versions, tags, err := fetchMirrorPackument(client, mirror.URL, dshPackageName)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", mirror.mirrorLogName(), err)
			logWarn("[dsh] version list from %s failed: %v", mirror.mirrorLogName(), err)
			continue
		}
		list := filterDshVersions(versions, tags)
		newest := newestOf(list)
		latestTag := tags["latest"]
		m.mu.Lock()
		m.versions, m.tags, m.newest, m.latestTag, m.checkedAt, m.verErr = list, tags, newest, latestTag, time.Now(), ""
		m.mu.Unlock()
		logInfo("[dsh] version list updated from %s: %d available versions, newest=%s (dist-tags.latest=%s)",
			mirror.mirrorLogName(), len(list), newest, latestTag)
		m.refreshDshStatus()
		m.notify(true)
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no npm mirror available")
	}
	m.mu.Lock()
	m.verErr = fmt.Sprintf("获取 dsh 版本列表失败（%s 均不可用）: %v", mirrorLogNames(), lastErr)
	m.checkedAt = time.Now()
	m.mu.Unlock()
	logWarn("[dsh] version list unavailable: %v", lastErr)
	m.refreshDshStatus()
	m.notify(true)
	return lastErr
}

// refreshDshStatus 把「选中的版本 + 列表里最高的一版」写进 dsh 更新状态，
// 供概览页版本行显示与红点判定。
//
// 红点**只按版本号大小**判：列表里最高的一版比当前选中的高就亮，不区分 dist-tags
// （用户要求）。理由是标签只是发布者手动移动的元数据 —— 用它当基准会出现
// 「明明有更高版本却不提示」（标签滞后）或「换了条线就不提示」（标签指向另一条线）；
// 而标签仍然照常展示在版本列表里（latest / alpha / next…），只是不参与判定。
func (m *ServerManager) refreshDshStatus() {
	if m.upd == nil {
		return
	}
	m.mu.Lock()
	newest, verErr := m.newest, m.verErr
	m.mu.Unlock()
	selected := m.selectedVersion()
	m.upd.updateStatus(updateKindDsh, func(st *UpdateStatus) {
		st.Kind = updateKindDsh
		st.LocalVersion = selected
		st.CheckedAt = time.Now()
		st.LatestVersion = newest
		st.HasUpdate = selected != "" && newest != "" && compareVersion(newest, selected) > 0
		st.Error = verErr
		st.ErrorRef = nil
		st.ReleaseNotes = ""
	})
}

// --- 安装 ---

// installInProgress 报告某个安装状态是否**真的在进行中**。
//
// 必须只看这三个「进行中」阶段，不能用 `Phase != ""`：done / error / cancelled 都是
// 已经结束的终态，它们留在状态里只是为了给前端显示结果（成功提示、"已取消"），不该
// 再把后续安装挡在门外 —— 早期版本正是这么写的，表现为「装好一个版本之后再也装不了
// 别的版本，一直提示『已有 dsh 版本正在安装』」。
func installInProgress(st *DshInstallState) bool {
	if st == nil {
		return false
	}
	switch st.Phase {
	case "downloading", "installing", "verifying":
		return true
	}
	return false
}

// Install 启动一次异步安装（下载并安装某个 dsh 版本）。
//
// 立即返回；进度经 ServerVersions.Install 推送（SSE）。同一时刻只允许一个安装/删除/
// 切换操作，重复触发返回错误由前端提示。上一轮的终态（done/error/cancelled）会被本次
// 新状态直接覆盖，不需要调用方先清理。
func (m *ServerManager) Install(version string) error {
	if !validVersionArg(version) {
		return uiErr("err_version_invalid", "非法的版本号: %s", "version", version)
	}
	// 低于 dshMinVersion 的版本在列表里根本不展示；直接调 API 也必须拒绝 ——
	// 更早的 dsh 前端不做文档相对路径，在控制台的子路径挂载下必然 404（见 dshMinVersion），
	// 装出来只会是一个「装上了但打不开」的版本。
	if compareVersion(version, dshMinVersion) < 0 {
		return uiErr("err_version_below_min", "dsh %s 低于控制台支持的最低版本 %s", "version", version, "min", dshMinVersion)
	}
	m.mu.Lock()
	if installInProgress(m.install) {
		busy := m.install.Version
		m.mu.Unlock()
		return busyf("err_version_install_running", "已有 dsh 版本正在安装（%s），请等它结束或先取消", "version", busy)
	}
	m.mu.Unlock()

	if dshVersionInstalled(m.versionDir(version)) {
		return uiErr("err_version_installed", "dsh %s 已安装，无需重复下载", "version", version)
	}
	if !m.applying.TryLock() {
		return busyf("err_version_op_busy", "正在执行其它 dsh 版本操作，请稍后再试")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	m.mu.Lock()
	m.cancelInstall = cancel
	m.installDone = done
	m.installSeq++
	m.install = &DshInstallState{
		Version: version,
		Phase:   "downloading",
		Mirror:  npmMirrors[0].Name,
		Seq:     m.installSeq,
	}
	m.mu.Unlock()
	m.refreshDshStatus()
	m.notify(true)

	go func() {
		defer close(done)
		defer m.applying.Unlock()
		err := m.doInstall(ctx, version)
		m.mu.Lock()
		st := m.install
		m.cancelInstall = nil
		m.mu.Unlock()
		// 终态的**唯一写入者**就是这里（CancelInstall 只发取消信号、不写状态）：
		// 否则「取消」紧跟在一次安装收尾之后时，会把随后新起的那次安装的界面状态
		// 改写成「已取消」。done 由 doInstall 自己置位（带「已安装」的收尾）。
		//
		// 用户取消**不置 error** —— 那是中性结果，只置 Cancelled，置上 error 只会让
		// 前端闪一下红色报错。
		switch {
		case st == nil:
		case st.Phase == "done":
		case err == nil:
		case errors.Is(err, errUpdateCancelled) || ctx.Err() != nil:
			m.updateInstall(func(s *DshInstallState) {
				s.Phase = ""
				setErrFields(&s.Error, &s.ErrorRef, nil)
				s.Cancelled = true
				setMsgFields(&s.Message, &s.MessageRef, "", "")
			})
		default:
			m.updateInstall(func(s *DshInstallState) {
				s.Phase = "error"
				setErrFields(&s.Error, &s.ErrorRef, err)
			})
		}
		// 本地已安装版本集合变了（新装成功 / 失败清理），状态跟着刷。
		m.refreshDshStatus()
		m.notify(true)
	}()
	return nil
}

// updateInstall 就地修改安装状态并推送。
func (m *ServerManager) updateInstall(f func(*DshInstallState)) {
	m.mu.Lock()
	if m.install != nil {
		f(m.install)
	}
	m.mu.Unlock()
	m.notify(false)
}

// CancelInstall 取消正在进行的那次安装：终止 npm（整个进程组）、删除安装目录与
// npm 下载缓存，然后把状态复位。
//
// 「取消必须清除下载缓存」是刻意的：半成品目录留着会让下次「重新下载」命中残缺的
// node_modules，而 npm 缓存里那部分字节也不再可信。
//
// 这里**只发取消信号并等它收尾，自己不写状态** —— 终态由安装 goroutine 统一落定
// （见 Install）。早期实现在这里无条件写 Cancelled=true，如果这次取消正好紧跟在一轮
// 安装收尾之后（cancelInstall 还没被置空、而新一次安装已经拿到 applying），就会把
// 新一轮安装的界面状态改写成「已取消」。
func (m *ServerManager) CancelInstall() bool {
	m.mu.Lock()
	cancel := m.cancelInstall
	done := m.installDone
	ver := ""
	if m.install != nil {
		ver = m.install.Version
	}
	m.mu.Unlock()
	if cancel == nil {
		return false
	}
	logInfo("[dsh] install cancelled by user (version %s)", ver)
	cancel()
	if done != nil {
		// 等到安装 goroutine 真正收尾（它关 done 之前已经写好了终态并解锁 applying）。
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			// 收尾超时：不阻塞请求线程，清理交给安装 goroutine（它一定会执行 defer）。
			// 此时状态仍是「进行中」—— 前端会继续轮询，直到那次安装自己落定。
			logWarn("[dsh] install cancel cleanup is still running")
		}
	}
	return true
}

// clearInstallState 清掉一次已完成安装的状态（前端刷新后不再显示上一轮的进度）。
func (m *ServerManager) clearInstallState() {
	m.mu.Lock()
	m.install = nil
	m.mu.Unlock()
	m.notify(true)
}

// doInstall 是一次安装的实际执行体：镜像源逐个尝试，成功后打补丁并校验。
func (m *ServerManager) doInstall(ctx context.Context, version string) error {
	dir := m.versionDir(version)
	// 半成品残留（上次失败/异常退出留下）先清掉：npm 遇到残缺 node_modules 可能直接
	// 复用坏树，装出来的东西不可信。同时重建 npm install 需要的骨架。
	pkgJSON := fmt.Sprintf("{\n  \"name\": \"dsh-server\",\n  \"version\": \"1.0.0\",\n  \"private\": true\n}\n")
	if err := m.resetVersionDir(dir, pkgJSON); err != nil {
		return err
	}

	var lastErr error
	for _, mirror := range npmMirrors {
		if ctx.Err() != nil {
			break
		}
		m.updateInstall(func(s *DshInstallState) {
			// 这两处是**界面**文案（版本弹窗里的源标签与进度行），刻意用中文显示名；
			// 日志一律用下面的 mirrorLogName()（英文标识）。
			s.Mirror = mirror.Name
			s.Phase = "downloading"
			setMsgFields(&s.Message, &s.MessageRef, "msg_install_from_mirror",
				"正在从 %s 下载 %s@%s", "mirror", mirror.Name, "pkg", dshPackageName, "version", version)
		})
		logInfo("[dsh] installing %s from %s", version, mirror.mirrorLogName())
		err := npmInstallFn(m, ctx, dir, version, mirror)
		if ctx.Err() != nil {
			// 用户取消：清干净（目录 + 下载缓存）后退出，错误文本由前端按「已取消」呈现。
			m.cleanCancelledInstall(dir, version)
			return errUpdateCancelled
		}
		if err == nil {
			if verr := m.verifyInstall(ctx, version); verr != nil {
				// 换下一个镜像源前必须把目录重建出来：npm install 的 --prefix 目录
				// 不存在时 exec 会直接以 chdir 失败，那次「换源」就白丢了，错误信息
				// 还会被算到镜像源头上（掩盖真正的校验失败原因）。
				if rerr := m.resetVersionDir(dir, pkgJSON); rerr != nil {
					return rerr
				}
				lastErr = fmt.Errorf("%s: %w", mirror.mirrorLogName(), verr)
				logWarn("[dsh] %s install from %s verified failed: %v", version, mirror.mirrorLogName(), verr)
				continue
			}
			m.updateInstall(func(s *DshInstallState) {
				s.Phase = "done"
				setErrFields(&s.Error, &s.ErrorRef, nil)
				s.Cancelled = false
				setMsgFields(&s.Message, &s.MessageRef, "msg_install_done", "dsh %s 安装完成", "version", version)
			})
			logInfo("[dsh] %s installed to %s (%s)", version, dir, mirror.mirrorLogName())
			m.refreshDshStatus()
			return nil
		}
		lastErr = fmt.Errorf("%s: %w", mirror.mirrorLogName(), err)
		logWarn("[dsh] install %s from %s failed: %v", version, mirror.mirrorLogName(), err)
		// 换下一个镜像源前清掉这次的半成品：npm 会在残缺树上续装，跨源混装不可信。
		if rerr := m.resetVersionDir(dir, pkgJSON); rerr != nil {
			return rerr
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no npm mirror available")
	}
	os.RemoveAll(dir)
	return uiErr("err_install_all_mirrors_failed",
		"从 %s 下载 dsh %s 均失败（不重试、不使用官方源）: %s",
		"mirrors", mirrorLogNames(), "version", version, "detail", lastErr.Error())
}

// resetVersionDir 把版本目录恢复成「npm install 可以往里装」的干净骨架：
// 删掉旧目录（半成品/落选镜像源的残骸），重建目录与 npm install --prefix 需要的
// package.json（与 dsh 官方安装形态一致，也让「这个目录是哪一版」在磁盘上直接可读）。
//
// 换镜像源重试前**必须**调用：`npm install --prefix <dir>` 的 dir 不存在时 exec 直接
// 以 chdir 失败，那次换源就白丢了，而且错误会被算到镜像源头上（掩盖真正的原因）。
func (m *ServerManager) resetVersionDir(dir, pkgJSON string) error {
	if err := os.RemoveAll(dir); err != nil {
		return uiErr("err_install_prepare_failed", "准备安装目录失败: %s", "detail", err.Error())
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return uiErr("err_install_prepare_failed", "准备安装目录失败: %s", "detail", err.Error())
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0644); err != nil {
		return uiErr("err_install_prepare_failed", "准备安装目录失败: %s", "detail", err.Error())
	}
	return nil
}

// cleanCancelledInstall 删除取消留下的安装目录与 npm 下载缓存。
func (m *ServerManager) cleanCancelledInstall(dir, version string) {
	if err := os.RemoveAll(dir); err != nil {
		logWarn("[dsh] failed to remove cancelled install dir %s: %v", dir, err)
	}
	if err := os.RemoveAll(m.npmCacheDir()); err != nil {
		logWarn("[dsh] failed to clear npm cache %s: %v", m.npmCacheDir(), err)
	}
	logInfo("[dsh] install of %s cancelled, install dir and npm cache removed", version)
}

// npmInstallFn 执行一次 `npm install --prefix <目录> @deepseek-ai/dsh@<版本>`。
// 变量形式便于单测注入：真实安装要拉约 500MB 依赖，测试里绝不执行。
var npmInstallFn = func(m *ServerManager, ctx context.Context, dir, version string, mirror npmMirror) error {
	return m.runNpmInstall(ctx, dir, version, mirror)
}

// runNpmInstall 执行一次 `npm install --prefix <目录> @deepseek-ai/dsh@<版本>`，
// 并把它输出的进度接到安装状态上。
func (m *ServerManager) runNpmInstall(ctx context.Context, dir, version string, mirror npmMirror) error {
	args := []string{
		"install",
		"--prefix", dir,
		dshPackageName + "@" + version,
		"--registry=" + mirror.URL,
		// 「5 秒没反应就换下一个镜像源」：npm 的单次请求 5 秒拿不到响应即失败，
		// 且不重试（重试会让「换源」变成「原地死等」）。
		"--fetch-timeout=5000",
		"--fetch-retries=0",
		"--save-exact",
		"--no-audit",
		"--no-fund",
		"--no-update-notifier",
		// http 级日志：每取一个包一行，用来数「已获取 N 个包」并显示尾行。
		"--loglevel=http",
	}
	cmd := exec.CommandContext(ctx, m.npmBin(), args...)
	cmd.Dir = dir
	cmd.Env = m.installEnv()
	// 独立进程组：取消时要连同 npm 派生出的构建脚本子进程一起终止。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// npm 的进度在 stderr，普通输出在 stdout，两路都接。
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); m.consumeNpmOutput(stderr) }()
	go func() { defer wg.Done(); m.consumeNpmOutput(stdout) }()

	// 取消时先把整个进程组 SIGTERM，宽限期内没退出再 SIGKILL —— CommandContext
	// 只杀直接子进程，构建脚本子进程会继续跑并占着版本目录。
	killed := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			m.killProcessGroup(cmd, dshInstallCancelGrace)
		case <-killed:
		}
	}()
	err = cmd.Wait()
	close(killed)
	wg.Wait()
	return err
}

// killProcessGroup 终止 npm 及其派生子进程（先 TERM 后 KILL）。
func (m *ServerManager) killProcessGroup(cmd *exec.Cmd, grace time.Duration) {
	if cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

// npmFetchRe 匹配 npm --loglevel=http 的包获取行，例如：
//
//	npm http fetch GET 200 https://registry.npmmirror.com/js-yaml 39ms (cache miss)
var npmFetchRe = regexp.MustCompile(`\bhttp fetch\b`)

// npmBuildRe 匹配「依赖树处理」阶段的行（解压、执行依赖构建脚本）。
var npmBuildRe = regexp.MustCompile(`\b(reify|postinstall|preinstall|install script|node-gyp|prebuild|added \d+ package)\b`)

// consumeNpmOutput 逐行消费 npm 输出，把进度写进安装状态。
//
// 进度语义（刻意不编造百分比）：npm 不会预先给出依赖总量，因此这里只累计
// 「已获取的包数」（单调递增），并在出现构建/收尾行时把阶段切到 installing。
func (m *ServerManager) consumeNpmOutput(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > dshInstallLogTail {
			line = line[:dshInstallLogTail] + "…"
		}
		switch {
		case npmFetchRe.MatchString(line):
			m.updateInstall(func(s *DshInstallState) {
				s.Phase = "downloading"
				s.Fetched++
				setMsgFields(&s.Message, &s.MessageRef, "", line)
			})
		case npmBuildRe.MatchString(line):
			m.updateInstall(func(s *DshInstallState) {
				s.Phase = "installing"
				setMsgFields(&s.Message, &s.MessageRef, "", line)
			})
		default:
			// 其它行（npm notice / warn / error）只更新尾行，不改阶段：npm 的
			// 告警没有可靠的阶段含义，用它们推断阶段只会误导。
			m.updateInstall(func(s *DshInstallState) { setMsgFields(&s.Message, &s.MessageRef, "", line) })
		}
	}
}

// npmBin 返回要使用的 npm 可执行文件。
//
// 必须与 dsh 将运行在的 node 版本一致：node 版本可配置（node24/node26），而 npm 装
// 原生依赖（node-pty / sharp / sherpa-onnx 的 prebuilds）时要按当前 node 的 ABI 解析。
func (m *ServerManager) npmBin() string {
	cfg := GetConfig()
	if prefix := nodeVersionBinPrefix(cfg.NodeVersion); prefix != "" {
		candidate := filepath.Join(prefix, "npm")
		if fi, err := os.Stat(candidate); err == nil && fi.Mode().IsRegular() {
			return candidate
		}
	}
	return "npm"
}

// installEnv 构造 npm install 的环境变量。
func (m *ServerManager) installEnv() []string {
	cfg := GetConfig()
	env := os.Environ()
	path := os.Getenv("PATH")
	if m.renv != nil && m.renv.Path != "" {
		path = m.renv.Path
	}
	if prefix := nodeVersionBinPrefix(cfg.NodeVersion); prefix != "" {
		path = prefix + ":" + path
	}
	env = setEnv(env, "PATH", path)
	env = setEnv(env, "npm_config_cache", m.npmCacheDir())
	env = setEnv(env, "npm_config_audit", "false")
	env = setEnv(env, "npm_config_fund", "false")
	env = setEnv(env, "npm_config_update_notifier", "false")
	// HOME 保持平台给的（dsh 的主目录）：npm 只把它当缓存以外的用户级配置查找路径，
	// 真正的缓存已被上面改到应用数据目录。
	return env
}

// verifyInstall 校验刚装好的版本确实能跑，并补上 fnOS 需要的补丁。
func (m *ServerManager) verifyInstall(ctx context.Context, version string) error {
	bin := versionDshBinFor(m.renv, version)
	if fi, err := os.Stat(bin); err != nil || !fi.Mode().IsRegular() {
		return uiErr("err_verify_no_binary", "no runnable dsh binary in the version dir (%s)", "path", bin)
	}
	m.updateInstall(func(s *DshInstallState) {
		s.Phase = "verifying"
		setMsgFields(&s.Message, &s.MessageRef, "msg_verifying", "正在校验安装并应用兼容补丁")
	})
	// 1. 附件 fsync 补丁（不打的话 fnOS 上上传文件/图片会整体失败，见文件头第 3 条）。
	if err := m.patchAttachmentFsync(m.versionDir(version)); err != nil {
		// 补丁失败不判定安装失败：500MB 已经下好了，因为一个可选补丁把它删掉更糟；
		// 但必须留下明确的 WARN，否则「上传文件失败」会变成一个没有线索的怪现象。
		logWarn("[dsh] attachment fsync patch not applied for %s: %v", version, err)
	}
	// 2. 确认可执行文件真的能跑（node 缺失、prebuilds 架构不符等问题都在这一步暴露）。
	vctx, cancel := context.WithTimeout(ctx, dshVersionVerifyTimeout)
	defer cancel()
	out, err := exec.CommandContext(vctx, bin, "-V").Output()
	if err != nil {
		return uiErr("err_verify_exec_failed", "dsh -V failed: %s", "detail", err.Error())
	}
	got := strings.TrimSpace(string(out))
	if got != version {
		return uiErr("err_verify_version_mismatch", "installed version is %q, want %s", "got", got, "want", version)
	}
	return nil
}

// --- 附件 fsync 补丁（从 CI 的 server-build.yaml 移植） ---

// fsyncPatchMarker 是补丁标记：已打过就跳过，便于幂等。
const fsyncPatchMarker = "dsh:fnos-ancestor-fsync"

// fsyncAnchorRe 与 CI 补丁用的锚点一致：attachment-local 在「逐级上溯目录 fsync」时
// 的那段 `let level = target; while (level !== stop) { const parent = dirname(level);
// await syncDirectory(parent); }`。
var fsyncAnchorRe = regexp.MustCompile(
	`([ \t]+let level = target;\r?\n[ \t]+while \(level !== stop\) \{\r?\n[ \t]+const parent = dirname\(level\);\r?\n)([ \t]+)(await syncDirectory\(parent\);)(\r?\n)`)

// patchAttachmentFsync 给刚装好的 dsh 打「祖先目录 fsync」补丁。
//
// 背景：@deepseek-ai/dsh-attachment-local 落盘附件时做崩溃可耐久发布，会对每一级祖先
// 目录 open(O_RDONLY) + fsync，并一路上溯到文件系统根 "/"。fnOS 把 /vol1 与
// /vol1/@appshare 设为 mode 000（仅由 trim_acl 授予穿越 x，不给读 r），应用以自身 uid
// 无法读取这些祖先目录，首次上传即抛 EACCES，WEB 端上传文件与图片整体失败。
// 修法：上溯时遇到打不开的祖先即停止（不可读的祖先本就无法 fsync），授权目录内已创建
// 的条目仍保持原有同步语义。
//
// 这个补丁过去由 CI 打在预构建包里，现在 dsh 由控制台自己 npm install，所以必须在这里补。
// 锚点缺失时只返回错误（调用方记 WARN 不阻塞安装）：上游改写了这段逻辑而我们没跟上，
// 那种情况需要人工核对，但也不该因此判定整个安装失败。
func (m *ServerManager) patchAttachmentFsync(versionDir string) error {
	path, err := findAttachmentLocalIndex(versionDir)
	if err != nil {
		return err
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(src)
	if strings.Contains(text, fsyncPatchMarker) {
		return nil // 幂等：已打过
	}
	loc := fsyncAnchorRe.FindStringSubmatchIndex(text)
	if loc == nil {
		if strings.Contains(text, "parse(home).root") {
			return fmt.Errorf("the durability anchor is missing while the walk-up-to-root logic still exists: %s (check the upstream change by hand)", path)
		}
		return fmt.Errorf("upstream seems to have dropped the walk-up-to-root logic, patch skipped: %s", path)
	}
	// loc 的分组：1 = 循环头（含缩进），2 = 循环体缩进，3 = await syncDirectory(parent);，
	// 4 = 换行。替换体是「循环头 + try/catch 包住的 fsync」，与 CI 的补丁逐字一致。
	indent := text[loc[4]:loc[5]]
	nl := text[loc[8]:loc[9]]
	out := text[:loc[0]] +
		text[loc[2]:loc[3]] +
		indent + "try {" + nl +
		indent + "\t" + "await syncDirectory(parent);" + nl +
		indent + "} catch (error) {" + nl +
		indent + "\t/* " + fsyncPatchMarker + " —— fnOS 把 /vol1 与 /vol1/@appshare 设为 mode 000，" + nl +
		indent + "\t   仅由 trim_acl 授予穿越；非 root 无法 open(O_RDONLY) 祖先目录，" + nl +
		indent + "\t   耐久性 fsync 因此抛 EACCES，使 WEB 端上传文件/图片整体失败。" + nl +
		indent + "\t   不可读的祖先本就无法同步，故停止上溯；授权目录内已创建的" + nl +
		indent + "\t   条目仍已同步。 */" + nl +
		indent + "\tif (error && (error.code === \"EACCES\" || error.code === \"EPERM\")) return;" + nl +
		indent + "\tthrow error;" + nl +
		indent + "}" + nl +
		text[loc[1]:]
	if !strings.Contains(out, fsyncPatchMarker) {
		return fmt.Errorf("patched file failed its marker check: %s", path)
	}
	if err := os.WriteFile(path, []byte(out), 0644); err != nil {
		return err
	}
	logInfo("[dsh] attachment fsync patch applied to %s", path)
	return nil
}

// findAttachmentLocalIndex 定位 dsh 安装目录里 attachment-local 的编译产物。
// npm 的提升规则不保证它在顶层，因此先看顶层，再递归兜底。
func findAttachmentLocalIndex(versionDir string) (string, error) {
	top := filepath.Join(versionDir, "node_modules", "@deepseek-ai",
		"dsh-attachment-local", "lib", "index.js")
	if fi, err := os.Stat(top); err == nil && fi.Mode().IsRegular() {
		return top, nil
	}
	var found string
	_ = filepath.Walk(filepath.Join(versionDir, "node_modules"), func(p string, info os.FileInfo, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if info.IsDir() {
			if info.Name() == ".bin" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Base(p) == "index.js" &&
			strings.HasSuffix(filepath.ToSlash(filepath.Dir(p)), "dsh-attachment-local/lib") {
			found = p
			return filepath.SkipAll
		}
		return nil
	})
	if found != "" {
		return found, nil
	}
	return "", fmt.Errorf("lib/index.js of @deepseek-ai/dsh-attachment-local not found (did the dsh package layout change?)")
}

// --- 删除与切换 ---

// Delete 删除一个已安装的版本目录（同步执行，返回时目录已移除）。
func (m *ServerManager) Delete(version string) error {
	if !validVersionArg(version) {
		return uiErr("err_version_invalid", "非法的版本号: %s", "version", version)
	}
	if !m.applying.TryLock() {
		return busyf("err_version_op_busy", "正在执行其它 dsh 版本操作，请稍后再试")
	}
	defer m.applying.Unlock()

	if selected := m.selectedVersion(); selected != "" && selected == version {
		return uiErr("err_version_in_use", "dsh %s 是当前正在使用的版本，请先切换到其它版本再删除", "version", version)
	}
	dir := m.versionDir(version)
	if _, err := os.Stat(dir); err != nil {
		return uiErr("err_version_missing", "dsh %s 未安装", "version", version)
	}
	if err := os.RemoveAll(dir); err != nil {
		return uiErr("err_version_delete_failed", "删除 dsh %s 失败: %s", "version", version, "detail", err.Error())
	}
	// 删完把这一轮的安装状态清掉：否则前端会看到「上一轮 done」和「这个版本没了」并存。
	m.clearInstallState()
	logInfo("[dsh] version %s removed from %s", version, dir)
	m.refreshDshStatus()
	m.notify(true)
	return nil
}

// Switch 切换到某个已安装的版本：写配置 → 停 dsh → 用新版本启动 dsh（异步换凭据）。
func (m *ServerManager) Switch(version string) error {
	if !validVersionArg(version) {
		return uiErr("err_version_invalid", "非法的版本号: %s", "version", version)
	}
	if !dshVersionInstalled(m.versionDir(version)) {
		return uiErr("err_version_not_installed", "dsh %s 尚未安装，请先下载", "version", version)
	}
	if m.selectedVersion() == version {
		return uiErr("err_version_current", "dsh %s 已经是当前版本", "version", version)
	}
	if !m.applying.TryLock() {
		return busyf("err_version_op_busy", "正在执行其它 dsh 版本操作，请稍后再试")
	}
	defer m.applying.Unlock()

	// 切版本要停 dsh：正在跑的插件安装/卸载会被这次停机连带终止，并可能留下陈旧的
	// profile 写锁（见 AGENTS 的「忙守卫」一节），因此动手前先过守卫。
	if m.upd != nil {
		if err := m.upd.replaceBusyGuard("切换 dsh 版本"); err != nil {
			return err
		}
	}

	cfg := GetConfig()
	prev := strings.TrimSpace(cfg.DshVersion)
	cfg.DshVersion = version
	if err := SaveConfig(m.renv, &cfg, false); err != nil {
		return fmt.Errorf("保存 dsh 版本配置失败: %w", err)
	}
	logInfo("[dsh] switching version %s -> %s", prev, version)

	// 停旧、起新：停机与启动都走统一钩子（dshStopFn / startDshCapturedFn），与
	// 「市场变更后重启 dsh」一致 —— 启动后必须经 captureDshSession 换取本代会话凭据，
	// 否则反代永远停在等待页（见 AGENTS 的启动门禁一节）。
	if m.upd != nil {
		if err := dshStopFn(m.upd); err != nil {
			logWarn("[dsh] stop failed during version switch: %v", err)
		}
		dshPortFreeFn(m.upd, 30*time.Second)
	}
	m.refreshDshStatus()
	m.notify(true)
	if m.upd != nil {
		go func() {
			if err := startDshCapturedFn(m.upd); err != nil {
				logWarn("[dsh] start failed after version switch: %v", err)
				return
			}
			// 补一次 node-pty 校正：控制台启动时若处于「未安装」，主流程那次校正被跳过了
			// （dsh 压根没起来），这里一起补上；需要重建依赖时会再重启一次 dsh。
			m.upd.ensureNodePtyAfterBoot()
			// 换版本前若因「没装 dsh」而查不到市场版本（界面「—」），换完之后必须重查一次，
			// 否则「—」会一直挂着（见 RefreshMarketAfterDshStart）。
			m.upd.RefreshMarketAfterDshStart()
		}()
	}
	return nil
}
