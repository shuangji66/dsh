package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// harnessVersion 是控制台自身的构建版本号。默认从 1.0.0 起；构建时可经
// -ldflags "-X main.harnessVersion=..." 覆盖。它代表"harness 控制台"的版本，
// 与 dsh 服务版本（dsh -V）相互独立。
var harnessVersion = "1.0.0"

// errUpdateCancelled 表示更新下载被用户主动取消（前端点“取消更新”触发）：
// 半成品文件会被删除，状态回到空闲，下次从零开始。
var errUpdateCancelled = errors.New("用户取消更新")

// errUpdatePaused 表示更新下载被用户暂停（前端点“暂停”触发）：半成品文件保留，
// 状态置 paused，下次“继续下载”用 HTTP Range 从已下载字节续传。
var errUpdatePaused = errors.New("用户暂停下载")

// errUpdateNetworkFailed 表示「代理」与「直连」两条路各 2 次机会全部失败。
// 用它把「网络/代理问题」与「包本身的问题」（如完整性校验失败、404）区分开：
// 前端据此显示“请检查网络或代理”的本地化提示，而不是让用户去猜。
var errUpdateNetworkFailed = errors.New("代理与直连均失败")

// --- GitHub 仓库与发布资源常量 ---
const (
	updateRepoOwner = "shuangji66"
	updateRepoName  = "dsh"
	updateRepoURL   = "https://github.com/" + updateRepoOwner + "/" + updateRepoName

	// updateCheckInterval 是每小时自动检测更新的周期。
	updateCheckInterval = time.Hour

	// updateHeaderTimeout 是单次下载请求等待响应头的上限（不限制整体时长：
	// 大包在慢网下可能下很久，整体超时会把它掐断）。
	updateHeaderTimeout = 30 * time.Second
	// updateIdleTimeout 是「传输空闲」上限：连续这么久没有新字节才判定本次尝试失败。
	updateIdleTimeout = 60 * time.Second
)

// updateRetryBackoff 是同一通路上两次尝试之间的退避时间（变量而非常量：测试里会调小）。
var updateRetryBackoff = 800 * time.Millisecond

// updateKind 表示更新目标：harness 控制台、dsh 服务或插件市场（dshmarket）。
type updateKind string

const (
	updateKindHarness updateKind = "harness"
	updateKindDsh     updateKind = "dsh"
	// updateKindMarket 表示「就地把 server 目录里那份 dshmarket 换成 npm 最新版」。
	// 它不是 GitHub release 资产（版本来自 npm registry），细节见 market.go。
	updateKindMarket updateKind = "market"
)

// tagInfo 描述一个从仓库读取到的 tag 及其解析出的版本号。
type tagInfo struct {
	name    string // 完整 tag 名，如 harness-1.0.1 / dsh-0.1.2-alpha.5
	version string // 去掉前缀后的版本号，如 1.0.1 / 0.1.2-alpha.5
}

// UpdateStatus 是一次更新检测的状态（harness 与 dsh 各自一份）。
type UpdateStatus struct {
	Kind          updateKind `json:"kind"`
	LocalVersion  string     `json:"localVersion"`           // 本地版本号
	LatestVersion string     `json:"latestVersion"`          // 仓库最新 tag 版本号（空表示未获取到）
	HasUpdate     bool       `json:"hasUpdate"`              // 是否有可用更新
	CheckedAt     time.Time  `json:"checkedAt"`              // 最近检测时间
	Error         string     `json:"error,omitempty"`        // 最近一次检测/拉取失败原因
	ReleaseNotes  string     `json:"releaseNotes,omitempty"` // 最新 release 的更新内容（正文，不含标题）

	// 下载进度（仅更新包下载期间有值；下载完成后清空）。
	Downloading     bool  `json:"downloading,omitempty"`     // 是否正在下载更新包
	DownloadPct     int   `json:"downloadPct,omitempty"`     // 下载进度百分比（0-100）
	DownloadedBytes int64 `json:"downloadedBytes,omitempty"` // 已下载字节数
	TotalBytes      int64 `json:"totalBytes,omitempty"`      // 总字节数（未知为 0）

	// 流程阶段：""(空闲) / downloading(下载中) / downloaded(已下载待安装) / installing(安装中)。
	Phase string `json:"phase,omitempty"`
	// ReadyToInstall 表示更新包已下载就绪，等待用户点击“安装”。
	ReadyToInstall bool `json:"readyToInstall,omitempty"`
	// Cancelled 表示最近一次更新被用户主动取消（仅失败推送时置位）。
	Cancelled bool `json:"cancelled,omitempty"`
	// Paused 表示下载被用户暂停，半成品已保留，可继续下载（phase=paused）。
	Paused bool `json:"paused,omitempty"`
	// ErrorHint 是给前端的结构化错误归类（目前只有 "network"：代理与直连均失败），
	// 前端据此显示本地化的“请检查网络或代理”提示；Error 里则是原始错误文本。
	ErrorHint string `json:"errorHint,omitempty"`

	// 以下两个字段仅市场（kind=market）使用，见 market.go：
	// MarketScope 说明当前生效的那份 dshmarket 由谁提供 ——
	// server（由 server 包提供，控制台可就地更新）/ profile（由 profile 提供，
	// 应在市场面板内更新）/ external（位置在 server 目录之外）/ missing（未找到）。
	MarketScope string `json:"marketScope,omitempty"`
	// MarketDir 是当前生效的 dshmarket 安装目录（诊断用）。
	MarketDir string `json:"marketDir,omitempty"`
}

// PendingUpdate 记录某个 kind 已下载完成、等待用户确认安装的更新包。
// 只会保留一个 kind 的一份待安装包；重新下载或安装完成后即被清理。
type PendingUpdate struct {
	Kind    updateKind
	Version string
	PkgPath string // 已下载更新包的 .tar.gz 完整路径
	// 仅市场使用：npm registry 给出的完整性值，安装前再复核一次（见 market.go）。
	Integrity string
	Shasum    string
}

// UpdateManager 管理控制台与 dsh 的版本检测、SSE 推送与自我更新。
type UpdateManager struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
	// statuses 分别保存 harness 与 dsh 的最近检测结果。
	statuses map[updateKind]*UpdateStatus
	// applying 用于防止并发执行自我更新（同一时刻只允许一个更新任务）。
	applying sync.Mutex
	renv     *RuntimeEnv
	dsh      *DshManager // 用于执行 `dsh -V` 等命令（复用其运行环境）
	// rollback 状态跟踪
	rollbackMu   sync.Mutex
	rollbackDone bool
	rollbackOk   bool
	rollbackErr  string

	// ctrlMu 保护 ctrl：ctrl 非 nil 表示正在下载更新包。中断原因决定半成品的
	// 去留：取消（cancel）删除，暂停（pause）保留以便续传（见 downloadControl）。
	ctrlMu sync.Mutex
	ctrl   *downloadControl

	// pendingMu 保护 pending：记录某个 kind 已下载完成、等待用户确认安装的更新包。
	pendingMu sync.Mutex
	pending   *PendingUpdate
}

// newUpdateManager 创建更新管理器并依据运行时环境填充本地版本。
func newUpdateManager(renv *RuntimeEnv, dsh *DshManager) *UpdateManager {
	m := &UpdateManager{
		subs:     make(map[chan struct{}]struct{}),
		statuses: make(map[updateKind]*UpdateStatus),
		renv:     renv,
		dsh:      dsh,
	}
	m.statuses[updateKindHarness] = &UpdateStatus{Kind: updateKindHarness, LocalVersion: harnessVersion}
	// 本地 dsh 版本立即通过 `dsh -V` 获取（原 /api/dsh/version 端点已移除，
	// 改由更新状态统一提供 dsh 版本号）。
	m.statuses[updateKindDsh] = &UpdateStatus{Kind: updateKindDsh, LocalVersion: m.localDshVersion()}
	// 市场（dshmarket）先做一次本地解析：不联网，只把「当前生效的那份在哪、什么
	// 版本、由谁提供」填进状态；最新版等 checkOnce/手动检查时才查 registry。
	m.statuses[updateKindMarket] = &UpdateStatus{Kind: updateKindMarket}
	m.refreshMarketLocal()
	// 启动时清理上次未能回收的更新包（自我更新 exec、异常退出等场景的残留）。
	m.clearOrphanPending()
	return m
}

// subscribe 注册一个 SSE 订阅通道，返回退订函数。
func (m *UpdateManager) subscribe() (chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	m.mu.Lock()
	m.subs[ch] = struct{}{}
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		delete(m.subs, ch)
		m.mu.Unlock()
	}
}

// notify 广播变更给所有 SSE 订阅者（非阻塞、合并突发）。
func (m *UpdateManager) notify() {
	m.mu.Lock()
	for ch := range m.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	m.mu.Unlock()
}

// getStatus 返回某个 kind 的检测结果快照副本。
func (m *UpdateManager) getStatus(k updateKind) UpdateStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.statuses[k]; ok {
		return *s
	}
	return UpdateStatus{Kind: k}
}

// setStatus 保存某个 kind 的检测结果并广播推送。
func (m *UpdateManager) setStatus(k updateKind, st *UpdateStatus) {
	m.mu.Lock()
	m.statuses[k] = st
	m.mu.Unlock()
	m.notify()
}

// snapshot 返回全部检测结果（供 REST 接口一次性返回）。
func (m *UpdateManager) snapshot() map[updateKind]UpdateStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[updateKind]UpdateStatus, len(m.statuses))
	for k, v := range m.statuses {
		out[k] = *v
	}
	return out
}

// --- 版本号解析与比较 ---

// splitVersion 按 `-` 拆出主版本与预发布部分（如 1.0.1 -> ["1","0","1"]；
// 0.1.2-alpha.5 -> 主 ["0","1","2"]，预发布 "alpha.5"）。
func splitVersion(v string) (nums []int, prerelease string) {
	main := v
	if i := strings.IndexByte(main, '-'); i >= 0 {
		main = v[:i]
		prerelease = v[i+1:]
	}
	for _, part := range strings.Split(main, ".") {
		n, _ := strconv.Atoi(part)
		nums = append(nums, n)
	}
	return nums, prerelease
}

// compareVersion 比较两个版本号字符串。语义：主版本数字优先；主版本相等时，
// 无预发布后缀的版本高于带预发布后缀的；预发布按点分段逐段比较（数字按数值）。
// 返回 <0 表示 a 更旧，>0 表示 a 更新，==0 表示相等。
func compareVersion(a, b string) int {
	an, apre := splitVersion(a)
	bn, bpre := splitVersion(b)
	for i := 0; i < len(an) || i < len(bn); i++ {
		var av, bv int
		if i < len(an) {
			av = an[i]
		}
		if i < len(bn) {
			bv = bn[i]
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	// 主版本相同：无预发布 > 有预发布
	if apre == "" && bpre != "" {
		return 1
	}
	if apre != "" && bpre == "" {
		return -1
	}
	if apre == bpre {
		return 0
	}
	// 预发布逐段比较
	aseg := strings.Split(apre, ".")
	bseg := strings.Split(bpre, ".")
	for i := 0; i < len(aseg) || i < len(bseg); i++ {
		var av, bv string
		if i < len(aseg) {
			av = aseg[i]
		}
		if i < len(bseg) {
			bv = bseg[i]
		}
		an2, aErr := strconv.Atoi(av)
		bn2, bErr := strconv.Atoi(bv)
		switch {
		case av == bv:
			continue
		case av == "":
			return -1 // a 更短
		case bv == "":
			return 1
		case aErr == nil && bErr == nil:
			if an2 != bn2 {
				if an2 < bn2 {
					return -1
				}
				return 1
			}
		default:
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}

// pickLatest 从全部 tag 中筛出指定前缀（harness- / dsh-）的 tag，按版本号排序取最新。
func pickLatest(tags []string, prefix string) *tagInfo {
	var matched []tagInfo
	for _, t := range tags {
		if !strings.HasPrefix(t, prefix) {
			continue
		}
		ver := strings.TrimPrefix(t, prefix)
		if ver == "" {
			continue
		}
		// 只接受看起来像版本号的（以数字开头），避免误收 harness-notes 之类的 tag。
		if !regexp.MustCompile(`^[0-9]`).MatchString(ver) {
			continue
		}
		matched = append(matched, tagInfo{name: t, version: ver})
	}
	if len(matched) == 0 {
		return nil
	}
	sort.SliceStable(matched, func(i, j int) bool {
		return compareVersion(matched[i].version, matched[j].version) > 0
	})
	best := matched[0]
	return &best
}

// --- tag 获取 ---

// fetchGitTags 从仓库获取全部 tag 名。依次尝试：GitHub API -> tags.atom 订阅源
// -> tags 页面 HTML。任一成功即返回。返回错误表示所有来源都失败。
func (m *UpdateManager) fetchGitTags() ([]string, error) {
	// 先取持久化配置中的代理地址可用性，决定是否经代理请求。
	client := m.httpClientForUpdate()

	// 1) GitHub API tags（最多 100 个/页，语义上按 ref 创建时间倒序，最新在前）。
	if names, err := fetchTagsViaAPI(client); err == nil && len(names) > 0 {
		return names, nil
	}
	// 2) tags.atom 订阅源（无需 API token，不受速率限制）。
	if names, err := fetchTagsViaAtom(client); err == nil && len(names) > 0 {
		return names, nil
	}
	// 3) tags 页面 HTML 兜底。
	if names, err := fetchTagsViaHTML(client); err == nil && len(names) > 0 {
		return names, nil
	}
	return nil, fmt.Errorf("无法获取 GitHub tag 列表（API/Atom/HTML 均失败）")
}

// fetchTagsViaAPI 从 GitHub REST API 的 tags 接口读取 tag 名。
func fetchTagsViaAPI(client *http.Client) ([]string, error) {
	var all []string
	next := fmt.Sprintf("https://api.github.com/repos/%s/%s/tags?per_page=100", updateRepoOwner, updateRepoName)
	for next != "" {
		req, err := http.NewRequest("GET", next, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "harness-console")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GitHub API 返回 %d", resp.StatusCode)
		}
		var items []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(body, &items); err != nil {
			return nil, err
		}
		for _, it := range items {
			all = append(all, it.Name)
		}
		// 分页：GitHub API 用响应头 Link 指示下一页。
		next = ""
		if link := resp.Header.Get("Link"); link != "" {
			for _, seg := range strings.Split(link, ",") {
				if strings.Contains(seg, `rel="next"`) {
					if i := strings.IndexByte(seg, '<'); i >= 0 {
						if j := strings.IndexByte(seg, '>'); j > i {
							next = seg[i+1 : j]
							break
						}
					}
				}
			}
		}
		if len(all) > 0 && next == "" {
			break
		}
	}
	return all, nil
}

// fetchTagsViaAtom 解析 tags.atom 订阅源，从条目链接里提取 tag 名。
func fetchTagsViaAtom(client *http.Client) ([]string, error) {
	feedURL := fmt.Sprintf("%s/tags.atom", updateRepoURL)
	req, err := http.NewRequest("GET", feedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "harness-console")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tags.atom 返回 %d", resp.StatusCode)
	}
	var names []string
	// <id>tag:github.com,2008:Repository/<repoId>/<tagName></id> 或
	// <link rel="alternate" href=".../releases/tag/<tagName>"/>
	idRe := regexp.MustCompile(`<id>[^<]*Repository/[^/<]+/([^<]+)</id>`)
	for _, m := range idRe.FindAllStringSubmatch(string(body), -1) {
		names = append(names, m[1])
	}
	// 若 id 未匹配，退回用链接路径提取
	if len(names) == 0 {
		linkRe := regexp.MustCompile(`/releases/tag/([A-Za-z0-9._-]+)`)
		for _, m := range linkRe.FindAllStringSubmatch(string(body), -1) {
			names = append(names, m[1])
		}
	}
	return names, nil
}

// fetchTagsViaHTML 解析 tags 页面 HTML，从 /releases/tag/<name> 链接提取 tag 名。
func fetchTagsViaHTML(client *http.Client) ([]string, error) {
	tagsURL := fmt.Sprintf("%s/tags", updateRepoURL)
	req, err := http.NewRequest("GET", tagsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "harness-console")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 6<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tags 页面返回 %d", resp.StatusCode)
	}
	re := regexp.MustCompile(`/releases/tag/([A-Za-z0-9._-]+)`)
	seen := map[string]bool{}
	var names []string
	for _, m := range re.FindAllStringSubmatch(string(body), -1) {
		n := m[1]
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	return names, nil
}

// fetchReleaseNotes 获取指定 tag 的 release 正文（不含标题 name）。优先走
// GitHub Releases API（取 body 字段）；API 受速率限制或不可用时，回退到非 API
// 的 release 页面 HTML（此页面不受 API 限流），解析其中的 markdown-body 正文。
// 任何失败都返回空串，不影响更新检测主流程。返回的正文保留原始 Markdown
// 文本（API 路径）或经 stripHTMLToText 还原的可读文本（HTML 回退路径），
// 由前端做轻量 Markdown 渲染展示。
func fetchReleaseNotes(client *http.Client, tag string) string {
	if body := fetchReleaseNotesViaAPI(client, tag); body != "" {
		return body
	}
	return fetchReleaseNotesViaHTML(client, tag)
}

// fetchReleaseNotesViaAPI 通过 GitHub Releases API 的 body 字段获取正文。
func fetchReleaseNotesViaAPI(client *http.Client, tag string) string {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s",
		updateRepoOwner, updateRepoName, url.PathEscape(tag))
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "harness-console")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ""
	}
	var rel struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(raw, &rel); err != nil {
		return ""
	}
	return strings.TrimSpace(rel.Body)
}

// fetchReleaseNotesViaHTML 从非 API 的 release 页面 HTML 提取正文。该页面不受
// GitHub API 速率限制。正文位于 <div ... data-test-selector="body-content"
// class="markdown-body ...">...</div>，仅含正文（标题单独在页面其它位置）。
// 提取后用纯文本方式展开，保留换行。
func fetchReleaseNotesViaHTML(client *http.Client, tag string) string {
	pageURL := fmt.Sprintf("%s/releases/tag/%s", updateRepoURL, url.PathEscape(tag))
	req, err := http.NewRequest("GET", pageURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "harness-console")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 6<<20))
	if err != nil {
		return ""
	}
	body := string(raw)
	marker := `data-test-selector="body-content"`
	i := strings.Index(body, marker)
	if i < 0 {
		return ""
	}
	// 回退定位 div 起点，避免把属性本身带进正文
	start := strings.LastIndex(body[:i], "<div")
	if start < 0 || start > i {
		start = i
	}
	// 截取到该 div 闭合（找下一个 </div>，正文内部一般不含未配对 div）
	j := strings.Index(body[i:], "</div>")
	if j < 0 {
		return ""
	}
	seg := body[start : i+j+len("</div>")]
	return stripHTMLToText(seg)
}

// stripHTMLToText 把一段 HTML 转成纯文本：块级/换行标签替换为换行，其余标签删除，
// 并解码实体、归一化连续空行。用于把 release 正文 HTML 还原成可读的多行文本。
func stripHTMLToText(seg string) string {
	// 常见块级标签与 <br> 视为换行
	for _, tag := range []string{"</p>", "</div>", "</li>", "</h1>", "</h2>", "</h3>", "</h4>", "</h5>", "</h6>", "</br>", "<br>", "<br/>", "<br />"} {
		seg = strings.ReplaceAll(seg, tag, "\n")
	}
	seg = strings.ReplaceAll(seg, "<li>", "• ")
	seg = strings.ReplaceAll(seg, "</li>", "\n")
	seg = strings.ReplaceAll(seg, "</pre>", "\n")
	// 其余标签全部剔除（保留文本内容）
	seg = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(seg, "")
	// 解码 HTML 实体（如 &amp; &lt;）
	seg = html.UnescapeString(seg)
	// 归一化：将空白行折叠为单个空行，去掉多余行首/行尾空白
	lines := []string{}
	for _, ln := range strings.Split(seg, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			if len(lines) > 0 && lines[len(lines)-1] != "" {
				lines = append(lines, "")
			}
			continue
		}
		lines = append(lines, ln)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// --- HTTP 客户端（代理回退） ---

// httpClientForUpdate 构造用于「元数据拉取」（GitHub tag / release notes / npm
// registry）的 HTTP 客户端：优先用配置里的代理，不可用时回退直连，带 60s 总超时。
// 注意大文件下载不走它 —— 下载用 updateClients() 的「代理/直连」双通路 + 断点续传，
// 且不设总超时（见该函数注释）。探测代理可用性通过一次轻量 HEAD 完成。
func (m *UpdateManager) httpClientForUpdate() *http.Client {
	cfg := GetConfig()
	if cfg.ProxyEnabled && cfg.ProxyAddr != "" && proxyReachable(cfg.ProxyAddr) {
		proxyURL, err := url.Parse(cfg.ProxyAddr)
		if err == nil {
			tr := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
			return &http.Client{Timeout: 60 * time.Second, Transport: tr}
		}
	}
	return &http.Client{Timeout: 60 * time.Second}
}

// proxyReachable 探测某个代理地址是否可用：尝试经它访问 GitHub（超时 8 秒）。
func proxyReachable(proxyAddr string) bool {
	proxyURL, err := url.Parse(proxyAddr)
	if err != nil {
		return false
	}
	tr := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	client := &http.Client{Timeout: 8 * time.Second, Transport: tr}
	req, _ := http.NewRequest("HEAD", updateRepoURL, nil)
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 500
}

// --- 检测主流程 ---

// checkOnce 执行一次完整的版本检测（harness 与 dsh 各自拉取最新 tag 并比对）。
func (m *UpdateManager) checkOnce() {
	tags, err := m.fetchGitTags()
	now := time.Now()

	// 无论 tag 拉取是否成功，都尝试更新 dsh 本地版本（dsh -V）。
	dshLocal := m.localDshVersion()

	m.mu.Lock()
	dshStatus := m.statuses[updateKindDsh]
	dshStatus.LocalVersion = dshLocal
	m.mu.Unlock()

	if err != nil {
		logger().Printf("[update] 拉取 tag 失败: %v", err)
		m.mu.Lock()
		for k, st := range m.statuses {
			// 市场的版本来自 npm registry，与 GitHub tag 拉取无关；把 tag 的错误
			// 覆盖到市场状态上只会误导（它的错误由 refreshMarketStatus 负责写）。
			if k == updateKindMarket {
				continue
			}
			st.LatestVersion = ""
			st.HasUpdate = false
			st.CheckedAt = now
			st.Error = err.Error()
			st.ReleaseNotes = ""
		}
		m.mu.Unlock()
		m.notify()
		m.refreshMarketStatus()
		return
	}

	h := pickLatest(tags, "harness-")
	d := pickLatest(tags, "dsh-")

	// 更新内容：只取最新 release 的正文（不含标题），失败时保持空串。
	notesClient := m.httpClientForUpdate()
	harnessNotes := ""
	if h != nil {
		harnessNotes = fetchReleaseNotes(notesClient, h.name)
	}
	dshNotes := ""
	if d != nil {
		dshNotes = fetchReleaseNotes(notesClient, d.name)
	}

	// 用 updateStatus 就地修改（而非 setStatus 全量替换），保留 Phase / ReadyToInstall
	// 等“两阶段更新”的进行中状态，避免每小时自动检测把“已下载待安装”状态清掉。
	m.updateStatus(updateKindHarness, func(st *UpdateStatus) {
		st.Kind = updateKindHarness
		st.LocalVersion = harnessVersion
		st.CheckedAt = now
		st.Error = ""
		st.ReleaseNotes = harnessNotes
		if h != nil {
			st.LatestVersion = h.version
			st.HasUpdate = compareVersion(h.version, harnessVersion) > 0
		} else {
			st.LatestVersion = ""
			st.HasUpdate = false
		}
	})

	m.updateStatus(updateKindDsh, func(st *UpdateStatus) {
		st.Kind = updateKindDsh
		st.LocalVersion = dshLocal
		st.CheckedAt = now
		st.Error = ""
		st.ReleaseNotes = dshNotes
		if d != nil {
			st.LatestVersion = d.version
			st.HasUpdate = compareVersion(d.version, dshLocal) > 0
		} else {
			st.LatestVersion = ""
			st.HasUpdate = false
		}
	})

	// 市场检测：版本来自 npm registry，与 GitHub tag 是两条独立链路。
	m.refreshMarketStatus()

	// 仅当 harness / dsh / 市场 任一个有更新时才打印检测结果，无更新时不刷日志。
	hs := m.getStatus(updateKindHarness)
	ds := m.getStatus(updateKindDsh)
	ms := m.getStatus(updateKindMarket)
	if hs.HasUpdate || ds.HasUpdate {
		logger().Printf("[update] 发现更新 harness 本地=%s 最新=%s | dsh 本地=%s 最新=%s",
			harnessVersion, hs.LatestVersion,
			dshLocal, ds.LatestVersion)
	}
	if ms.HasUpdate {
		logger().Printf("[market] 发现更新 市场本地=%s 最新=%s（%s）",
			ms.LocalVersion, ms.LatestVersion, ms.MarketScope)
	}
}

// startAutoCheck 启动每小时一次的自动检测后台任务。
func (m *UpdateManager) startAutoCheck() {
	go func() {
		// 启动后先等一小段时间再首次检测，避免与后端冷启动争抢资源。
		time.Sleep(15 * time.Second)
		m.checkOnce()
		ticker := time.NewTicker(updateCheckInterval)
		defer ticker.Stop()
		for range ticker.C {
			m.checkOnce()
		}
	}()
}

// localDshVersion 执行 `dsh -V` 获取本地 dsh 版本号；失败返回空串。
// 复用 DshManager 的运行环境（PATH/HOME/代理等），与原先 /api/dsh/version 行为一致。
func (m *UpdateManager) localDshVersion() string {
	if m.dsh == nil {
		return ""
	}
	out, err := m.dsh.runDshCmd("-V")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// --- 下载与自我更新 ---

// updateArch 返回用于文件名的架构后缀：x86 / arm。优先读 TRIM_SYS_ARCH，否则
// 按 runtime.GOARCH 推断。
func (m *UpdateManager) updateArch() string {
	if a := os.Getenv("TRIM_SYS_ARCH"); a != "" {
		al := strings.ToLower(a)
		if strings.HasPrefix(al, "arm") {
			return "arm"
		}
		return "x86"
	}
	switch runtime.GOARCH {
	case "arm64", "arm":
		return "arm"
	default:
		return "x86"
	}
}

// assetURL 构造某个 kind/version/arch 对应的发布资源下载地址。
func (m *UpdateManager) assetURL(k updateKind, version, arch string) string {
	tag := string(k) + "-" + version
	var asset string
	switch k {
	case updateKindHarness:
		asset = fmt.Sprintf("harness-%s-%s.tar.gz", version, arch)
	case updateKindDsh:
		asset = fmt.Sprintf("server-%s-%s.tar.gz", arch, version)
	}
	return fmt.Sprintf("%s/releases/download/%s/%s", updateRepoURL, tag, asset)
}

// extractTarGz 解压 .tar.gz 到目标目录。保持 tar 内的相对路径不变（不剥离顶层目录）。
// 既用于下载的发布包，也用于回滚时还原 server 备份。
func extractTarGz(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := hdr.Name
		if name == "" {
			continue
		}
		target := filepath.Join(dest, filepath.Clean(name))
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) && target != filepath.Clean(dest) {
			return fmt.Errorf("解压路径越界: %s", target)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)&0777); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			w, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(w, tr); err != nil {
				w.Close()
				return err
			}
			w.Close()
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		}
	}
	return nil
}

// tgzDir 把目录压缩成 .tar.gz（用于备份）。
func tgzDir(srcDir, destFile string) error {
	out, err := os.Create(destFile)
	if err != nil {
		return err
	}
	defer out.Close()
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	base := filepath.Clean(srcDir)
	err = filepath.Walk(base, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if p == base {
			return nil
		}
		rel, err := filepath.Rel(base, p)
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = rel
		if info.IsDir() {
			hdr.Name += "/"
			return tw.WriteHeader(hdr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			hdr.Typeflag = tar.TypeSymlink
			hdr.Linkname = link
			hdr.Size = 0
			return tw.WriteHeader(hdr)
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		tw.Close()
		gz.Close()
		os.Remove(destFile)
		return err
	}
	if err := tw.Close(); err != nil {
		gz.Close()
		os.Remove(destFile)
		return err
	}
	return gz.Close()
}

// tgzDirAs 将 srcDir 目录内容压缩，tar 中的条目以 rootName 作为顶层前缀。
// 例：tgzDirAs("/home/user/.dsh", "b.tar.gz", ".dsh") 生成 ".dsh/KEY"、".dsh/..." 等条目，
// 仅包含 .dsh 目录自身（不含 HOME 其它内容），解压到 /home/user 可还原完整的 ~/.dsh。
func tgzDirAs(srcDir, destFile, rootName string) error {
	out, err := os.Create(destFile)
	if err != nil {
		return err
	}
	defer out.Close()
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	base := filepath.Clean(srcDir)
	root := strings.Trim(rootName, "/")
	err = filepath.Walk(base, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(base, p)
		if rerr != nil {
			return rerr
		}
		var name string
		if p == base {
			name = root + "/"
		} else {
			name = root + "/" + rel
		}
		// 不包含备份产物自身，避免递归膨胀
		if strings.HasSuffix(rel, ".tar.gz") {
			return nil
		}
		if info.IsDir() {
			if !strings.HasSuffix(name, "/") {
				name += "/"
			}
			hdr, herr := tar.FileInfoHeader(info, "")
			if herr != nil {
				return herr
			}
			hdr.Name = name
			return tw.WriteHeader(hdr)
		}
		hdr, herr := tar.FileInfoHeader(info, "")
		if herr != nil {
			return herr
		}
		hdr.Name = name
		if info.Mode()&os.ModeSymlink != 0 {
			link, lerr := os.Readlink(p)
			if lerr != nil {
				return lerr
			}
			hdr.Typeflag = tar.TypeSymlink
			hdr.Linkname = link
			hdr.Size = 0
			return tw.WriteHeader(hdr)
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, ferr := os.Open(p)
		if ferr != nil {
			return ferr
		}
		_, cerr := io.Copy(tw, f)
		f.Close()
		return cerr
	})
	if err != nil {
		tw.Close()
		gz.Close()
		os.Remove(destFile)
		return err
	}
	if err := tw.Close(); err != nil {
		gz.Close()
		os.Remove(destFile)
		return err
	}
	return gz.Close()
}

// harnessBinPath 返回控制台二进制所在目录与路径。优先用 /var/apps/Harness/target，
// 否则用运行时 TRIM_APPDEST。
func (m *UpdateManager) harnessBinDir() string {
	if fi, err := os.Stat("/var/apps/Harness/target/bin/harness"); err == nil && fi.Mode().IsRegular() {
		return "/var/apps/Harness/target/bin"
	}
	app := m.renv.TRIMAppDest
	if app != "" {
		return filepath.Join(app, "bin")
	}
	return "/var/apps/Harness/target/bin"
}

// serverDir 返回 dsh server 目录。优先 /var/apps/Harness/target/server。
func (m *UpdateManager) serverDir() string {
	if fi, err := os.Stat("/var/apps/Harness/target/server"); err == nil && fi.IsDir() {
		return "/var/apps/Harness/target/server"
	}
	app := m.renv.TRIMAppDest
	if app != "" {
		return filepath.Join(app, "server")
	}
	return "/var/apps/Harness/target/server"
}

// backupDir 返回备份产物的存放目录（放在应用数据目录，避免写入只读的 target）。
func (m *UpdateManager) backupDir() string {
	pkgvar := os.Getenv("TRIM_PKGVAR")
	if pkgvar == "" {
		pkgvar = "/vol1/@appdata/Harness"
	}
	dir := filepath.Join(pkgvar, "backup")
	os.MkdirAll(dir, 0755)
	return dir
}

// --- 下载取消与进度 ---

// updateStatus 修改并广播某个 kind 的状态（线程安全）。
func (m *UpdateManager) updateStatus(k updateKind, f func(*UpdateStatus)) {
	m.mu.Lock()
	if st, ok := m.statuses[k]; ok {
		f(st)
	}
	m.mu.Unlock()
	m.notify()
}

// setDownloadProgress 更新某个 kind 的下载进度状态并广播给前端（SSE）。
func (m *UpdateManager) setDownloadProgress(k updateKind, downloading bool, pct int, downloaded, total int64) {
	m.updateStatus(k, func(st *UpdateStatus) {
		st.Downloading = downloading
		st.DownloadPct = pct
		st.DownloadedBytes = downloaded
		st.TotalBytes = total
	})
}

// pendingDir 返回存放“已下载待安装”更新包的目录（在持久备份目录下，跨两步保留）。
func (m *UpdateManager) pendingDir() string {
	dir := filepath.Join(m.backupDir(), "pending")
	os.MkdirAll(dir, 0755)
	return dir
}

// getPending 读取当前待安装更新包（可能为 nil）。
func (m *UpdateManager) getPending() *PendingUpdate {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	return m.pending
}

// setPending 记录新的待安装更新包；若该 kind 已有旧包则清理旧文件。
func (m *UpdateManager) setPending(p *PendingUpdate) {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	// 清理同 kind 旧的待安装包文件。
	if old := m.pending; old != nil && old.Kind == p.Kind && old.PkgPath != "" && old.PkgPath != p.PkgPath {
		os.Remove(old.PkgPath)
	}
	m.pending = p
}

// clearPending 清除当前待安装更新包并删除其文件。
func (m *UpdateManager) clearPending() {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	if m.pending != nil && m.pending.PkgPath != "" {
		os.Remove(m.pending.PkgPath)
	}
	m.pending = nil
}

// clearOrphanPending 删除 pendingDir 下所有“无主”更新包文件。
//
// pending 只存在于内存，进程重启后即丢失：无论上次是安装前崩溃、安装过程被中断，
// 还是 harness 自我更新（旧进程被 exec 换掉，内存中的 pending 随进程消亡），重启后
// 磁盘上都会留下永远不会被 clearPending 回收的 .tar.gz。这些文件只用于“下载→安装”
// 两步之间传递，没有任何跨重启续用价值（版本号变化后也无法复用），因此在启动时
// 整目录清理，避免每次自我更新都残留一个更新包。
//
// 注意：只清 pendingDir，不触碰 backupDir 里的 harness-*/server-* 备份（那些是回滚
// 依据，由 30 天清理任务负责）。
func (m *UpdateManager) clearOrphanPending() {
	dir := m.pendingDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	removed := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		full := filepath.Join(dir, e.Name())
		if err := os.Remove(full); err == nil {
			removed++
			logger().Printf("[update] 清理残留更新包: %s", e.Name())
		}
	}
	if removed > 0 {
		logger().Printf("[update] 共清理 %d 个残留更新包", removed)
	}
}

// --- 下载中断控制 ---

// downloadControl 承载一次下载的中断信号，并区分原因：取消要删半成品、暂停要留。
type downloadControl struct {
	ch chan struct{}
	mu sync.Mutex
	// reason 为空表示未被中断；"cancel" / "pause" 由前端按钮设置。
	reason string
	// pausable 为 false 时忽略暂停请求（插件市场：包小、不走续传，暂停没有意义）。
	pausable bool
}

func newDownloadControl(pausable bool) *downloadControl {
	return &downloadControl{ch: make(chan struct{}), pausable: pausable}
}

// stop 记录中断原因并广播（只生效一次：先到者为准）。
// 返回 true 表示这次请求真的生效了（暂停在不支持暂停的下载上会被忽略）。
func (c *downloadControl) stop(reason string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if reason == "pause" && !c.pausable {
		return false
	}
	if c.reason != "" {
		return false
	}
	c.reason = reason
	close(c.ch)
	return true
}

// stopped 返回是否被中断及原因。
func (c *downloadControl) stopped() (bool, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reason != "", c.reason
}

// stopDownload 请求中断当前下载（reason 为 "cancel" 或 "pause"）。
// 返回是否真的生效；没有进行中的下载、或该下载不支持暂停时返回 false。
func (m *UpdateManager) stopDownload(reason string) bool {
	m.ctrlMu.Lock()
	c := m.ctrl
	m.ctrlMu.Unlock()
	if c == nil {
		return false
	}
	return c.stop(reason)
}

// CancelUpdate 请求取消当前正在进行的更新下载：半成品文件会被删除，状态回到空闲。
func (m *UpdateManager) CancelUpdate() {
	if m.stopDownload("cancel") {
		logger().Printf("[update] 已请求取消下载")
	}
}

// PauseUpdate 请求暂停当前正在进行的更新下载：半成品文件保留，可继续续传。
// 返回是否真的命中了一个进行中的下载（没有则忽略）。
func (m *UpdateManager) PauseUpdate() bool {
	if m.stopDownload("pause") {
		logger().Printf("[update] 已请求暂停下载（半成品保留，可继续）")
		return true
	}
	return false
}

// --- 下载策略（通路 / 续传 / 暂停） ---

// updateRoute 是一条下载通路：要么走配置里的代理，要么直连。
// 按用户要求已移除 GitHub 加速源前缀分支 —— 只剩这两条。
type updateRoute struct {
	label  string
	client *http.Client
}

// downloadPlan 描述一次下载的策略：走哪些通路、能否续传、能否暂停。
//
// 目前有两种组合，差异是刻意的：
//   - harness / dsh 的发布资产：代理+直连各 2 次，支持 Range 续传与暂停（包大、网络差）；
//   - 插件市场 tarball：**只直连、不走代理**，失败重试，不支持暂停与续传
//     （包只有几百 KB，续传带来的复杂度不值得；registry 通常也不需要代理）。
type downloadPlan struct {
	routes   []updateRoute
	resume   bool
	pausable bool
}

// downloadPlanFor 按更新类型给出下载策略。
func (m *UpdateManager) downloadPlanFor(k updateKind) downloadPlan {
	if k == updateKindMarket {
		return downloadPlan{routes: marketRoutesFn(m), resume: false, pausable: false}
	}
	return downloadPlan{routes: updateRoutesFn(m), resume: true, pausable: true}
}

// updateRoutesFn 便于测试注入发布资产的通路列表；生产实现见 updateClients。
var updateRoutesFn = func(m *UpdateManager) []updateRoute { return m.updateClients() }

// marketRoutesFn 给出插件市场的通路：**只有直连**（用户要求：市场不走代理）。
// 同样是变量，便于测试注入。
var marketRoutesFn = func(m *UpdateManager) []updateRoute { return []updateRoute{m.directRoute()} }

// updateClients 构造发布资产的下载通路序列：代理（已启用且可达时）在前，直连兜底。
//
// 说明两点与旧实现不同的地方：
//   - 不再使用 GitHub 加速源前缀（用户要求），包只能从原始地址取；
//   - 下载客户端不设总超时（Timeout=0）：旧的 60 秒总超时会掐断大包/慢网，
//     改为「建连 30s + 响应头 30s + 传输空闲 60s」三个更贴合实际的限制
//     （见 updateTransport 与 downloadOnce 的看门狗）。
func (m *UpdateManager) updateClients() []updateRoute {
	routes := make([]updateRoute, 0, 2)
	cfg := GetConfig()
	if cfg.ProxyEnabled && cfg.ProxyAddr != "" && proxyReachableFn(cfg.ProxyAddr) {
		if proxyURL, err := url.Parse(cfg.ProxyAddr); err == nil {
			routes = append(routes, updateRoute{
				label:  "代理 " + cfg.ProxyAddr,
				client: &http.Client{Transport: updateTransport(proxyURL)},
			})
		} else {
			logger().Printf("[update] 代理地址无法解析，本次只用直连: %v", err)
		}
	}
	routes = append(routes, m.directRoute())
	return routes
}

// directRoute 构造直连通路。
func (m *UpdateManager) directRoute() updateRoute {
	return updateRoute{label: "直连", client: &http.Client{Transport: updateTransport(nil)}}
}

// updateTransport 构造下载用的 Transport：proxyURL 为 nil 表示直连。
// 显式给建连/TLS/响应头超时，避免黑洞路由下无限等待（http.Transport 的零值
// DialContext 是不带超时的 net.Dial）。
func updateTransport(proxyURL *url.URL) *http.Transport {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	tr := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: updateHeaderTimeout,
		ExpectContinueTimeout: time.Second,
	}
	if proxyURL != nil {
		tr.Proxy = http.ProxyURL(proxyURL)
	}
	return tr
}

// proxyReachableFn 便于测试注入代理可达性探测；生产实现见 proxyReachable。
var proxyReachableFn = func(addr string) bool { return proxyReachable(addr) }

// downloadToFile 下载 rawURL 到 dest，支持断点续传、暂停与重试。
//
// 重试策略（用户要求）：每条通路各 2 次机会，代理在前、直连在后；每次失败后保留
// 已下载字节，下一次用 `Range: bytes=<offset>-` 续传，所以代理断在 60% 时直连会
// 从 60% 接着下，而不是重来。
//
// 返回：(已下载总字节, 错误)。错误可能是：
//   - errUpdateCancelled：用户取消（半成品已删除）；
//   - errUpdatePaused：用户暂停（半成品保留，可续传）；
//   - errUpdateNetworkFailed：所有通路与重试均失败（前端据此提示检查网络/代理）。
//
// progress 为节流后的进度回调；ctrl 为中断信号（可为 nil，表示不可中断）；
// plan 决定通路与续传/暂停能力（见 downloadPlan）。
func (m *UpdateManager) downloadToFile(rawURL, dest string, progress func(downloaded, total int64), ctrl *downloadControl, plan downloadPlan) (int64, error) {
	const attemptsPerRoute = 2

	var lastErr error
	// networkOnly 记录「所有失败都是网络/服务端性质」。只要有任何一次败在本地磁盘
	// （目录不可写、磁盘满），最终就不该提示用户「检查网络或代理」。
	networkOnly := true
	for _, route := range plan.routes {
		for try := 1; try <= attemptsPerRoute; try++ {
			if ctrl != nil {
				if stop, reason := ctrl.stopped(); stop {
					return finishInterruptedDownload(dest, reason)
				}
			}
			if !plan.resume {
				// 不支持续传（插件市场）：每次尝试都从零开始，清掉上一轮的残留，
				// 避免半截文件与本次写入拼在一起。
				os.Remove(dest)
			}
			offset := partialSize(dest)
			logger().Printf("[update] 下载尝试 %s 第 %d/%d 次（已有 %d 字节）", route.label, try, attemptsPerRoute, offset)

			n, err := m.downloadOnce(route, rawURL, dest, progress, ctrl, plan.resume)
			if err == nil {
				logger().Printf("[update] 下载成功（%s，共 %d 字节）", route.label, n)
				return n, nil
			}
			if errors.Is(err, errUpdateCancelled) || errors.Is(err, errUpdatePaused) {
				reason := "cancel"
				if stop, r := ctrlState(ctrl); stop {
					reason = r
				}
				return finishInterruptedDownload(dest, reason)
			}
			if isLocalIOError(err) {
				networkOnly = false
			}
			lastErr = fmt.Errorf("%s 第 %d 次: %w", route.label, try, err)
			logger().Printf("[update] %v", lastErr)
			if try < attemptsPerRoute {
				// 短暂退避再试，避免对同一故障点连续猛打。
				time.Sleep(updateRetryBackoff)
			}
		}
	}
	if !plan.resume {
		// 不留半成品：这份下载的语义就是「要么完整拿到，要么什么都没有」。
		os.Remove(dest)
	}
	if lastErr == nil {
		lastErr = errors.New("没有可用的下载通路")
	}
	if !networkOnly {
		// 败在本地磁盘（写不进去）：修网络没用，不要给「检查网络」的误导性提示。
		return partialSize(dest), fmt.Errorf("下载失败（%d 条通路各 %d 次均未成功）: %v", len(plan.routes), attemptsPerRoute, lastErr)
	}
	return partialSize(dest), fmt.Errorf("%w（%d 条通路各 %d 次均未成功）: %v；请检查网络或代理设置后重试",
		errUpdateNetworkFailed, len(plan.routes), attemptsPerRoute, lastErr)
}

// isLocalIOError 判断失败是否来自本地文件系统（而非网络/服务端）。
// 用于决定要不要提示用户「检查网络或代理」。
func isLocalIOError(err error) bool {
	var pathErr *os.PathError
	return errors.As(err, &pathErr)
}

// finishInterruptedDownload 处理被取消/暂停的下载：取消要删半成品，暂停要留。
func finishInterruptedDownload(dest, reason string) (int64, error) {
	if reason == "pause" {
		n := partialSize(dest)
		logger().Printf("[update] 下载已暂停，保留半成品 %s（%d 字节）", dest, n)
		return n, errUpdatePaused
	}
	os.Remove(dest)
	logger().Printf("[update] 下载已取消，已清理半成品 %s", dest)
	return 0, errUpdateCancelled
}

// partialSize 返回已下载的字节数（文件不存在时为 0）。
func partialSize(path string) int64 {
	if fi, err := os.Stat(path); err == nil && fi.Mode().IsRegular() {
		return fi.Size()
	}
	return 0
}

// removeOtherPendingFiles 清掉同 kind 的其它待安装/半成品文件（保留 keep 这一个）。
//
// 为什么需要：包名按 kind+版本固定，换版本后旧版本的半成品不会被自动覆盖，
// 否则会在 pending 目录里无限堆积（只有启动时和“删除更新包”才清）。
func (m *UpdateManager) removeOtherPendingFiles(k updateKind, keep string) {
	dir := m.pendingDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	prefix := string(k) + "-"
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		full := filepath.Join(dir, name)
		if filepath.Clean(full) == filepath.Clean(keep) {
			continue
		}
		if err := os.Remove(full); err == nil {
			logger().Printf("[update] 清理旧版本半成品: %s", name)
		}
	}
}

// downloadOnce 执行一次下载尝试（resume 为真时可续传），返回「已下载总字节」。
//
// 续传规则（仅 resume=true，即 harness/dsh 的发布资产）：
//   - 本地已有 offset 字节时带 Range 头；服务器回 206 则追加写入；
//   - 服务器忽略 Range 回 200（有些代理/CDN 会剥掉 Range）→ 截断重写，本次从头下，
//     保证文件内容一定是「从头开始的连续前缀」，不会拼出坏包；
//   - 回 416（本地字节比远端还长）→ 删除半成品重新完整下载，绝不把坏文件当「已下完」。
//
// resume=false（插件市场）时：不发 Range、永远截断重写，任何中断都从零重来。
//
// 注意：resume=true 时任何错误路径都保留已写入的字节（先 Sync + Close），供下一次续传。
func (m *UpdateManager) downloadOnce(route updateRoute, rawURL, dest string, progress func(int64, int64), ctrl *downloadControl, resume bool) (int64, error) {
	offset := int64(0)
	if resume {
		offset = partialSize(dest)
	}
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return offset, err
	}
	req.Header.Set("User-Agent", "harness-console")
	if resume && offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	// 传输空闲看门狗：超过 updateIdleTimeout 没有任何新字节就中断本次尝试
	// （HTTP 客户端本身不设总超时，否则大包/慢网会被整体掐断）。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	idle := &idleWatchdog{timeout: updateIdleTimeout, onIdle: cancel}
	defer idle.stop()
	req = req.WithContext(ctx)

	resp, err := route.client.Do(req)
	if err != nil {
		return offset, err
	}
	defer resp.Body.Close()

	// 不续传时永远从头写：O_TRUNC 保证不会把上一轮的残留拼进来。
	truncate := !resume
	switch resp.StatusCode {
	case http.StatusPartialContent:
		// 206：按 Range 续传（只在 resume=true 时可能收到）
		if !resume {
			return 0, fmt.Errorf("服务器返回 206，但本次下载不支持续传")
		}
	case http.StatusOK:
		// 200：服务器未按 Range 返回（或 offset 本来为 0）→ 从头写
		offset = 0
		truncate = true
	case http.StatusRequestedRangeNotSatisfiable:
		// 416：本地字节数与远端不一致（远端换了资产，或半成品本身损坏）。
		// 删掉半成品让下一次尝试从头开始 —— 绝不能把这份坏文件当成「已下完」。
		os.Remove(dest)
		return 0, fmt.Errorf("远端拒绝了续传（本地 %d 字节与远端不匹配），已清理半成品", offset)
	default:
		return offset, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	total := int64(0)
	if resp.ContentLength > 0 {
		total = resp.ContentLength + offset
	}
	flags := os.O_CREATE | os.O_WRONLY
	if truncate {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_APPEND
	}
	out, err := os.OpenFile(dest, flags, 0o644)
	if err != nil {
		return offset, err
	}

	reader := &downloadReader{
		r:        resp.Body,
		ctrl:     ctrl,
		progress: progress,
		offset:   offset,
		total:    total,
		idle:     idle,
	}
	n, copyErr := io.Copy(out, reader)
	// 先落盘再关闭：半成品必须是「已完整写入的字节」，否则下次续传会从错误位置接。
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr == nil {
		if syncErr != nil {
			copyErr = syncErr
		} else if closeErr != nil {
			copyErr = closeErr
		}
	}
	done := offset + n
	if copyErr != nil {
		if wasSet, reason := ctrlState(ctrl); wasSet {
			return done, fmt.Errorf("%w", interruptedError(reason))
		}
		if idle.fired() {
			return done, fmt.Errorf("传输空闲超过 %s（已下载 %d 字节）", updateIdleTimeout, done)
		}
		return done, copyErr
	}
	if progress != nil {
		progress(done, total)
	}
	return done, nil
}

// ctrlState 安全读取中断状态（ctrl 为 nil 时视为未中断）。
func ctrlState(ctrl *downloadControl) (bool, string) {
	if ctrl == nil {
		return false, ""
	}
	return ctrl.stopped()
}

// interruptedError 把中断原因映射为哨兵错误。
func interruptedError(reason string) error {
	if reason == "pause" {
		return errUpdatePaused
	}
	return errUpdateCancelled
}

// idleWatchdog 是「传输空闲」看门狗：每次读取成功都重置截止时间，
// 到点没有新字节则触发回调（取消本次请求）。
type idleWatchdog struct {
	timeout time.Duration
	onIdle  func()
	mu      sync.Mutex
	timer   *time.Timer
	dead    bool
}

func (w *idleWatchdog) reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer == nil {
		w.timer = time.AfterFunc(w.timeout, func() {
			w.mu.Lock()
			w.dead = true
			w.mu.Unlock()
			w.onIdle()
		})
		return
	}
	w.timer.Reset(w.timeout)
}

func (w *idleWatchdog) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
}

func (w *idleWatchdog) fired() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.dead
}

// downloadReader 包装响应体：检查中断信号、按节流上报进度（含续传偏移量），
// 并在每次读到数据时重置空闲看门狗。
type downloadReader struct {
	r        io.Reader
	ctrl     *downloadControl
	progress func(downloaded, total int64)
	offset   int64
	total    int64
	n        int64
	lastAt   time.Time
	lastN    int64
	idle     *idleWatchdog
}

func (dr *downloadReader) Read(p []byte) (int, error) {
	if stop, reason := ctrlState(dr.ctrl); stop {
		return 0, interruptedError(reason)
	}
	n, err := dr.r.Read(p)
	if n > 0 {
		dr.n += int64(n)
		if dr.idle != nil {
			dr.idle.reset()
		}
		// 节流上报：每 500ms 或每 256KB 一次，避免高频回调刷爆 SSE。
		if dr.progress != nil {
			now := time.Now()
			if now.Sub(dr.lastAt) >= 500*time.Millisecond || dr.n-drrLastN(dr) >= 256<<10 {
				dr.progress(dr.offset+dr.n, dr.total)
				dr.lastAt = now
			}
		}
	}
	return n, err
}

// drrLastN 返回自上次上报以来新增的字节数，并推进上报水位。
// （把「读取水位」与「上次上报水位」分开，是因为进度回调必须报「含续传偏移」的总量，
// 而节流判断只看本次尝试新增了多少。）
func drrLastN(dr *downloadReader) int64 {
	delta := dr.n - dr.lastN
	dr.lastN = dr.n
	return delta
}

// downloadUpdate 只下载更新包（第一步），不安装。可在下载过程中取消（CancelUpdate
// 关闭 cancelCh 中断），下载成功后把 .tar.gz 放到持久的“待安装”目录并记入 pending，
// 推送 phase=downloaded / readyToInstall=true，等待用户在弹窗里点“安装”。
// 返回错误表示下载失败或被用户取消。
func (m *UpdateManager) downloadUpdate(k updateKind) error {
	m.applying.Lock()
	defer m.applying.Unlock()

	st := m.getStatus(k)
	// 版本与下载地址的来源按 kind 分流：harness/dsh 用 GitHub tag + release 资产，
	// 市场用 npm registry 的 /dshmarket/latest（顺带拿到 integrity，见 market.go）。
	var (
		version string
		rawURL  string
		rel     *marketRelease
	)
	if k == updateKindMarket {
		// 先确认这份市场确实归控制台管：由 profile 提供的那份改了也不生效
		// （profile 条目优先于安装闭包，见 market.go），没必要白下一份。
		if target := m.resolveMarketTarget(); target.Scope != marketScopeServer {
			return fmt.Errorf("当前市场由 %s 提供（%s），控制台不更新这份安装", target.Scope, target.Reason)
		}
		r, err := m.marketLatest()
		if err != nil {
			return fmt.Errorf("获取市场最新版本失败: %w", err)
		}
		rel = r
		version = r.Version
		rawURL = r.Tarball
	} else {
		if st.LatestVersion == "" {
			return fmt.Errorf("尚未获取到最新版本号，请先执行检查更新")
		}
		version = st.LatestVersion
		rawURL = m.assetURL(k, version, m.updateArch())
	}
	arch := m.updateArch()
	logger().Printf("[update] 开始下载 %s 到 %s (arch=%s)", k, version, arch)

	// 下载策略：harness/dsh = 代理+直连各 2 次 + Range 续传 + 可暂停；
	// 插件市场 = 只直连重试，不续传、不暂停（见 downloadPlanFor）。
	plan := m.downloadPlanFor(k)

	// 目标包持久保存在“待安装”目录，跨“下载→安装”两步保留。
	// 文件名刻意「按 kind+版本固定」（不再带时间戳）：这样暂停后继续、或失败后重试
	// 都能命中同一个半成品，断点续传才有意义（市场不续传，另有清理）。
	// 同 kind 其它版本的残留会被清掉。
	pkgPath := filepath.Join(m.pendingDir(), string(k)+"-"+version+".tar.gz")
	m.removeOtherPendingFiles(k, pkgPath)
	resumeBytes := int64(0)
	if plan.resume {
		resumeBytes = partialSize(pkgPath)
	}

	// 建立中断信号：取消（删半成品）与暂停（留半成品）都经它传达。
	// 不支持暂停的下载（市场）会直接忽略暂停请求。
	ctrl := newDownloadControl(plan.pausable)
	m.ctrlMu.Lock()
	m.ctrl = ctrl
	m.ctrlMu.Unlock()
	defer func() {
		m.ctrlMu.Lock()
		m.ctrl = nil
		m.ctrlMu.Unlock()
	}()
	// 进入“下载中”状态。注意保留续传起点：从 0 开始会让进度条瞬间回跳。
	m.updateStatus(k, func(s *UpdateStatus) {
		s.Phase = "downloading"
		s.ReadyToInstall = false
		s.Cancelled = false
		s.Paused = false
		s.Error = ""
		s.ErrorHint = ""
		s.Downloading = true
		s.DownloadedBytes = resumeBytes
		s.DownloadPct = 0
		if resumeBytes == 0 {
			s.TotalBytes = 0
		}
	})

	progress := func(downloaded, total int64) {
		pct := 0
		if total > 0 {
			pct = int(downloaded * 100 / total)
			if pct > 100 {
				pct = 100
			}
		}
		m.setDownloadProgress(k, true, pct, downloaded, total)
	}
	var n int64
	var err error
	if rel != nil {
		// 市场包从 npm registry 下载（只直连），并在下载后立刻校验完整性
		// （元数据与字节的绑定关系）；不通过就当场失败，不进入“已下载待安装”。
		// 安装阶段会再复核一次。
		n, err = m.downloadMarketTarball(rel, pkgPath, progress, ctrl, plan)
	} else {
		// GitHub release 资产：代理 / 直连各 2 次机会，支持 Range 续传。
		n, err = m.downloadToFile(rawURL, pkgPath, progress, ctrl, plan)
	}

	switch {
	case err == nil:
		// 继续走下面的“已下载待安装”。
	case errors.Is(err, errUpdatePaused):
		// 暂停（只有可暂停的下载会走到这里）：半成品保留（downloadToFile 已处理），
		// 状态置 paused 供前端显示“继续下载”。
		m.updateStatus(k, func(s *UpdateStatus) {
			s.Phase = "paused"
			s.Paused = true
			s.Downloading = false
			s.DownloadedBytes = partialSize(pkgPath)
			s.Error = ""
			s.ErrorHint = ""
			s.Cancelled = false
		})
		logger().Printf("[update] %s 下载已暂停，已下载 %d 字节", k, partialSize(pkgPath))
		return err
	case errors.Is(err, errUpdateCancelled):
		m.updateStatus(k, func(s *UpdateStatus) {
			s.Phase = ""
			s.Paused = false
			s.Downloading = false
			s.DownloadPct = 0
			s.DownloadedBytes = 0
			s.TotalBytes = 0
		})
		return fmt.Errorf("下载失败: %w", err)
	default:
		// 失败：可续传的下载保留半成品（下次重试接着下）；市场这类不续传的
		// 下载已被 downloadToFile 清空。退出“下载中”状态并带上错误归类。
		m.updateStatus(k, func(s *UpdateStatus) {
			s.Phase = ""
			s.Paused = false
			s.Downloading = false
			if !plan.resume {
				s.DownloadedBytes = 0
				s.TotalBytes = 0
			}
			if errors.Is(err, errUpdateNetworkFailed) {
				s.ErrorHint = "network"
			}
		})
		return fmt.Errorf("下载失败: %w", err)
	}

	// 下载成功：记录待安装包，推送“已下载待安装”。
	pending := &PendingUpdate{Kind: k, Version: version, PkgPath: pkgPath}
	if rel != nil {
		pending.Integrity = rel.Integrity
		pending.Shasum = rel.Shasum
	}
	m.setPending(pending)
	m.updateStatus(k, func(s *UpdateStatus) {
		s.Phase = "downloaded"
		s.ReadyToInstall = true
		s.Downloading = false
		s.Paused = false
		s.ErrorHint = ""
		s.DownloadPct = 100
		s.DownloadedBytes = n
		if s.TotalBytes <= 0 {
			s.TotalBytes = n
		}
		s.Error = ""
		s.Cancelled = false
		if rel != nil {
			// 让“仓库最新版本”与刚下载到的这份保持一致（检测与下载之间可能
			// 刚好有新版发布）。
			s.LatestVersion = version
		}
	})
	logger().Printf("[update] %s 更新包已下载到 %s (%d bytes)，等待安装", k, pkgPath, n)
	return nil
}

// installUpdate 安装已下载的更新包（第二步）。读取 pending 中的 .tar.gz，解压后
// 调用 installHarness / installDsh / installMarket 执行“备份→替换→重启”。
// 安装阶段耗时短、不可取消。
// 安装失败时保留 pending（用户可重试安装）；成功时由各分支清 pending：
//   - dsh / 市场：清 pending 并正常返回，由调用方推送 phase=done。
//   - harness：先清 pending 再 exec 换新映像，本函数永不返回（故不会有 phase=done 推送，
//     前端以轮询新进程版本号判定就绪——见 UpdateSection.vue 的 startHarnessReadyPoll）。
func (m *UpdateManager) installUpdate(k updateKind) error {
	m.applying.Lock()
	defer m.applying.Unlock()

	p := m.getPending()
	if p == nil || p.Kind != k {
		return fmt.Errorf("尚未下载 %s 更新包，请先下载更新", k)
	}
	if _, err := os.Stat(p.PkgPath); err != nil {
		m.clearPending()
		return fmt.Errorf("待安装更新包已不存在（可能被清理），请重新下载: %w", err)
	}
	logger().Printf("[update] 开始安装 %s 到 %s", k, p.Version)

	// 进入“安装中”状态。
	m.updateStatus(k, func(s *UpdateStatus) {
		s.Phase = "installing"
		s.ReadyToInstall = false
		s.Cancelled = false
		s.Error = ""
	})

	// 解压到临时目录。
	tmpDir, err := os.MkdirTemp("", "harness-install-"+string(k)+"-"+time.Now().Format("20060102150405"))
	if err != nil {
		m.updateStatus(k, func(s *UpdateStatus) { s.Phase = "" })
		return fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	if err := extractTarGz(p.PkgPath, tmpDir); err != nil {
		m.updateStatus(k, func(s *UpdateStatus) { s.Phase = "" })
		return fmt.Errorf("解压更新包失败: %w", err)
	}

	var installErr error
	switch k {
	case updateKindHarness:
		installErr = m.installHarness(tmpDir)
	case updateKindDsh:
		installErr = m.installDsh(tmpDir)
	case updateKindMarket:
		installErr = m.installMarket(p, tmpDir)
	default:
		installErr = fmt.Errorf("未知的更新类型 %s", k)
	}
	if installErr != nil {
		// 安装失败：保留待安装包（可重试安装），退出“安装中”状态。
		m.updateStatus(k, func(s *UpdateStatus) { s.Phase = "" })
		return installErr
	}
	return nil
}

// installHarness 完成 harness 自我更新的最后阶段：替换二进制、停止 dsh、清理
// 更新包与临时目录，最后 exec 换新映像。
//
// 关键顺序约束：syscall.Exec 会**立刻**用新程序替换当前进程映像，本进程此后的
// 任何语句都不会再执行（defer 也不会触发）。因此删除待安装更新包、清理解压临时
// 目录这些收尾动作必须在 exec 之前显式完成，否则它们会永久残留——这正是此前
// 「harness 自我更新后更新包未被删除」的原因。
//
// 返回值：exec 成功则永不返回；exec 失败返回错误（此时新二进制已就位，重启后生效）。
func (m *UpdateManager) installHarness(extractDir string) error {
	newBin, err := m.applyHarness(extractDir)
	if err != nil {
		return err
	}
	// 收尾（必须在 exec 之前）：删除待安装更新包、清理解压临时目录。
	m.clearPending()
	os.RemoveAll(extractDir)
	logger().Printf("[update] harness 更新包与临时目录已清理，准备重启控制台")
	m.restartHarness(newBin)
	// 仅当 exec 失败时才会走到这里。
	return fmt.Errorf("重启控制台失败（新二进制已就位，手动重启后生效）")
}

// installDsh 完成 dsh 服务更新的最后阶段：替换 server 目录并（异步）重启 dsh，
// 随后删除待安装更新包。返回 nil 表示安装成功；调用方负责推送成功状态。
func (m *UpdateManager) installDsh(extractDir string) error {
	if err := m.applyServer(extractDir); err != nil {
		return err
	}
	m.clearPending()
	return nil
}

// DiscardUpdate 删除已下载待安装的更新包（前端“删除更新包”按钮触发），
// 并把该 kind 重置为初始待更新状态（phase 清空、readyToInstall=false、
// 取消/错误/暂停标记清空），前端按钮回到“下载更新”。
//
// 同时清理该 kind 的「半成品」（暂停或失败留下的续传文件）—— 用户点“删除更新包”
// 的语义就是「这些字节我不要了」，否则下次下载会莫名从中间继续。
func (m *UpdateManager) DiscardUpdate(k updateKind) error {
	m.applying.Lock()
	defer m.applying.Unlock()

	m.removeOtherPendingFiles(k, "")
	if m.getPending() == nil {
		// 没有待安装包也照样复位状态：暂停中的半成品已被上面清掉。
		m.updateStatus(k, func(s *UpdateStatus) {
			s.Phase = ""
			s.Paused = false
			s.ReadyToInstall = false
			s.Downloading = false
			s.DownloadPct = 0
			s.DownloadedBytes = 0
			s.TotalBytes = 0
			s.Error = ""
			s.ErrorHint = ""
			s.Cancelled = false
		})
		return nil
	}
	m.clearPending()
	m.updateStatus(k, func(s *UpdateStatus) {
		s.Phase = ""
		s.Paused = false
		s.ReadyToInstall = false
		s.Downloading = false
		s.DownloadPct = 0
		s.DownloadedBytes = 0
		s.TotalBytes = 0
		s.Error = ""
		s.ErrorHint = ""
		s.Cancelled = false
	})
	logger().Printf("[update] 已删除 %s 的待安装更新包", k)
	return nil
}

// findExecutable 在解压目录中递归查找可执行文件 harness。
func findExecutable(dir, name string) (string, error) {
	var found string
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && info.Mode().IsRegular() && filepath.Base(p) == name {
			found = p
			return filepath.SkipAll
		}
		return nil
	})
	if found == "" {
		return "", fmt.Errorf("解压包中未找到 %s 二进制", name)
	}
	return found, nil
}

// applyHarness 备份并替换控制台二进制，并停止 dsh 服务；**不**重启控制台，而是
// 返回新二进制路径交由调用方在收尾之后 exec。
//
// 之所以不在此处直接 exec：exec 会立刻用新映像替换当前进程，本进程的内存状态与
// 后续语句（清理更新包、清理解压目录）全部消失——这正是此前“自我更新后更新包
// 残留”的原因。所有收尾动作必须由调用方在 exec 之前完成。
func (m *UpdateManager) applyHarness(extractDir string) (string, error) {
	newBin, err := findExecutable(extractDir, "harness")
	if err != nil {
		return "", err
	}
	binDir := m.harnessBinDir()
	dest := filepath.Join(binDir, "harness")

	// 先做备份（压缩当前二进制）。用独立 staging 目录避免打包整棵临时树。
	// 这里**不用 defer** 清理 staging 目录：本函数返回后调用方还会 exec 换新映像，
	// 但 exec 只发生在 applyHarness 返回之后，因此只要在返回前显式删除即可；
	// 用 defer 反而会在 exec 后永不执行、留下 /tmp 残留（此前 /tmp/backup-stage
	// 就是这样攒下来的）。目录名带随机后缀，避免并发/残留目录互相干扰。
	stage, err := os.MkdirTemp("", "harness-backup-stage-")
	if err != nil {
		return "", fmt.Errorf("创建备份临时目录失败: %w", err)
	}
	backupName := fmt.Sprintf("harness-%s-%s.tar.gz", harnessVersion, time.Now().Format("20060102150405"))
	backupPath := filepath.Join(m.backupDir(), backupName)
	if err := copyFile(dest, filepath.Join(stage, "harness")); err != nil {
		os.RemoveAll(stage)
		return "", fmt.Errorf("读取当前二进制用于备份失败: %w", err)
	}
	if err := tgzDir(stage, backupPath); err != nil {
		os.RemoveAll(stage)
		return "", fmt.Errorf("备份当前二进制失败: %w", err)
	}
	os.RemoveAll(stage)
	logger().Printf("[update] harness 已备份到 %s", backupPath)

	// 替换二进制：在目标同目录下先写入临时文件，再 atomic rename 替换。
	// 不能用 copyFile 直接覆盖（os.Create 截断正在运行的可执行文件会报 "text file busy"）；
	// 临时文件必须与 dest 同目录（否则跨文件系统的 rename 会报 "invalid cross-device link"）。
	tmpNew := filepath.Join(binDir, ".harness.new")
	if err := copyFile(newBin, tmpNew); err != nil {
		os.Remove(tmpNew)
		return "", fmt.Errorf("复制新二进制失败: %w", err)
	}
	if err := os.Rename(tmpNew, dest); err != nil {
		os.Remove(tmpNew)
		return "", fmt.Errorf("替换二进制失败: %w", err)
	}
	if err := os.Chmod(dest, 0755); err != nil {
		logger().Printf("[update] chmod 失败: %v", err)
	}

	// 先停止 dsh 服务，再由新二进制 exec 覆盖当前进程镜像（保持同一 PID，fnOS 监管不失效）。
	// 新 harness 进程启动时会自动拉起 dsh，先停止可避免端口冲突或残留进程。
	logger().Printf("[update] 停止 dsh 服务")
	if err := m.dsh.Stop(); err != nil {
		logger().Printf("[update] 停止 dsh 失败: %v", err)
	}
	logger().Printf("[update] harness 二进制已更新，等待收尾后重启控制台")
	return dest, nil
}

// restartHarness 用新二进制替换当前进程镜像。正常情况下不会返回（进程映像已被
// 替换）；仅当 exec 失败时返回，此时进程仍以旧镜像运行。
func (m *UpdateManager) restartHarness(newBin string) {
	// 让当前进程以新二进制重新 exec；若失败，记录错误（进程仍以旧镜像运行）。
	env := os.Environ()
	argv := append([]string{newBin}, os.Args[1:]...)
	if err := syscall.Exec(newBin, argv, env); err != nil {
		logger().Printf("[update] 重启控制台失败: %v", err)
	}
}

// startDshCaptured 启动 dsh 并异步捕获新的一次性访问 token、换取 dsh 会话 cookie。
// 每次 dsh 启动都会生成新的 token（Start 也会重置旧 token 与会话 cookie），因此
// 更新 server / 回滚 / 数据恢复等“重启 dsh 后必须刷新会话凭据”的路径都必须经过
// 本方法，否则反代仍携带旧 cookie（或空 cookie）转发到新启动的 dsh，导致会话失效。
func (m *UpdateManager) startDshCaptured() error {
	if err := m.dsh.Start(); err != nil {
		return err
	}
	// 异步等待捕获 token 并换取 cookie，不阻塞更新/回滚主流程（最多等 15 秒）。
	go captureDshSession(m.dsh)
	return nil
}

// applyServer 备份并替换 dsh server 目录。
// 顺序：找到 server 根 → 先停止 dsh 服务 → 再备份替换 → 最后启动 dsh。
func (m *UpdateManager) applyServer(extractDir string) error {
	// 找到解压包中的 server 目录。可能为：
	//   a) 顶层 server/ 目录（未被剥离）—— extractDir/server
	//   b) 剥离开顶层后的 server 内容直接位于 extractDir（含 package.json）—— extractDir 即根
	//   c) 其它位置含 package.json 的 server 目录——递归查找
	srcServer := filepath.Join(extractDir, "server")
	if fi, err := os.Stat(srcServer); err == nil && fi.IsDir() {
		// 情况 a
	} else if _, err := os.Stat(filepath.Join(extractDir, "package.json")); err == nil {
		// 情况 b：extractDir 即 server 根
		srcServer = extractDir
	} else {
		// 情况 c：递归查找含 package.json 的 server 目录
		var found string
		filepath.Walk(extractDir, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				if _, err := os.Stat(filepath.Join(p, "package.json")); err == nil {
					found = p
					return filepath.SkipAll
				}
			}
			return nil
		})
		if found == "" {
			return fmt.Errorf("解压包中未找到 server 目录")
		}
		srcServer = found
	}

	serverDir := m.serverDir()
	parent := filepath.Dir(serverDir)

	// 下载已成功；先停止 dsh 服务，再执行备份替换，确保备份一致、替换不冲突。
	logger().Printf("[update] 停止 dsh 服务")
	if err := m.dsh.Stop(); err != nil {
		logger().Printf("[update] 停止 dsh 服务失败: %v", err)
		// 即便停止失败也继续尝试备份替换
	}

	// 备份当前 server 目录（文件名带上当前 dsh 版本号）
	dshVer := m.localDshVersion()
	if dshVer == "" {
		dshVer = "unknown"
	}
	backupName := fmt.Sprintf("server-%s-%s.tar.gz", dshVer, time.Now().Format("20060102150405"))
	backupPath := filepath.Join(m.backupDir(), backupName)
	if err := tgzDir(serverDir, backupPath); err != nil {
		// 更新失败，尽量恢复 dsh（重启后需重新捕获会话凭据）
		if serr := m.startDshCaptured(); serr != nil {
			logger().Printf("[update] 恢复 dsh 启动失败: %v", serr)
		}
		return fmt.Errorf("备份 server 目录失败: %w", err)
	}
	logger().Printf("[update] server 已备份到 %s", backupPath)

	// 替换：把旧的 server 移到临时位置，放入新的，再删除临时旧目录。
	oldTmp := filepath.Join(parent, ".server-old-"+time.Now().Format("20060102150405"))
	if err := os.Rename(serverDir, oldTmp); err != nil {
		// 更新失败，尽量恢复 dsh（重启后需重新捕获会话凭据）
		if serr := m.startDshCaptured(); serr != nil {
			logger().Printf("[update] 恢复 dsh 启动失败: %v", serr)
		}
		return fmt.Errorf("移动旧 server 目录失败: %w", err)
	}
	if err := copyDir(srcServer, serverDir); err != nil {
		// 回滚：把旧目录放回去，并恢复 dsh（重启后需重新捕获会话凭据）
		os.Rename(oldTmp, serverDir)
		if serr := m.startDshCaptured(); serr != nil {
			logger().Printf("[update] 恢复 dsh 启动失败: %v", serr)
		}
		return fmt.Errorf("复制新 server 失败: %w", err)
	}
	os.RemoveAll(oldTmp)

	logger().Printf("[update] server 目录已更新，启动 dsh 服务")
	// 启动 dsh（fire-and-forget）：解压+备份替换已完成，安装流程立即返回成功，
	// 不等待 dsh 完全启动（会话 cookie 由异步 captureDshSession 后台换取）。
	// 若 dsh 启动失败，只记录日志，不阻塞“安装成功”的返回。
	go func() {
		if err := m.startDshCaptured(); err != nil {
			logger().Printf("[update] 启动 dsh 失败（异步，安装已完成）: %v", err)
		}
	}()
	return nil
}

// copyFile 复制单个文件（保留权限）。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if info, err := os.Stat(src); err == nil {
		os.Chmod(dst, info.Mode())
	}
	return nil
}

// --- Server 备份列表与回滚 ---

// ServerBackup 描述一个 dsh server 的备份条目。
type ServerBackup struct {
	Name     string `json:"name"`     // 文件名，如 server-0.1.2-alpha.5-20260903154421.tar.gz
	Size     int64  `json:"size"`     // 文件大小（字节）
	Modified string `json:"modified"` // 修改时间（RFC3339）
	Path     string `json:"path"`     // 完整路径
}

// backupTimestampRe 匹配新格式备份文件名尾部的 -<YYYYMMDDHHMMSS>.tar.gz 时间戳后缀。
// 新格式：<类型>-<版本号>-<时间戳>.tar.gz，如 harness-1.0.0-20260903154421.tar.gz。
var backupTimestampRe = regexp.MustCompile(`-\d{14}\.tar\.gz$`)

// isBackupFile 判断文件名是否为指定类型的备份文件（以 prefix 开头，且以 -<14位时间戳>.tar.gz 结尾）。
func isBackupFile(name, prefix string) bool {
	return strings.HasPrefix(name, prefix) && backupTimestampRe.MatchString(name)
}

// ListServerBackups 列出 backupDir 中所有 server-<版本>-<时间戳>.tar.gz 文件，按修改时间倒序。
func (m *UpdateManager) ListServerBackups() ([]ServerBackup, error) {
	dir := m.backupDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var backups []ServerBackup
	for _, e := range entries {
		name := e.Name()
		if !isBackupFile(name, "server-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		fullPath := filepath.Join(dir, name)
		backups = append(backups, ServerBackup{
			Name:     name,
			Size:     info.Size(),
			Modified: info.ModTime().Format(time.RFC3339),
			Path:     fullPath,
		})
	}
	// 按修改时间倒序（最新在前）
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Modified > backups[j].Modified
	})
	return backups, nil
}

// DeleteServerBackup 删除一个 server 备份文件（仅限 backupDir 下的 server-<版本>-<时间戳>.tar.gz）。
func (m *UpdateManager) DeleteServerBackup(name string) error {
	dir := m.backupDir()
	target := filepath.Join(dir, name)
	// 安全校验：文件必须在 backupDir 下且符合命名规范
	if filepath.Dir(target) != dir || !isBackupFile(name, "server-") {
		return fmt.Errorf("非法的备份文件名: %s", name)
	}
	return os.Remove(target)
}

// RollbackServerStatus 是回滚状态的返回结构。
type RollbackServerStatus struct {
	Running bool   `json:"running"`
	Done    bool   `json:"done"`
	Ok      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

// GetRollbackStatus 返回当前回滚状态快照。
func (m *UpdateManager) GetRollbackStatus() RollbackServerStatus {
	m.rollbackMu.Lock()
	defer m.rollbackMu.Unlock()
	return RollbackServerStatus{
		Running: !m.rollbackDone && !m.rollbackOk,
		Done:    m.rollbackDone,
		Ok:      m.rollbackOk,
		Error:   m.rollbackErr,
	}
}

// RollbackServer 执行 dsh server 回滚：停止 dsh → 删除当前 server 目录 →
// 解压备份到 server 目录 → 删除备份文件 → 启动 dsh。异步执行。
func (m *UpdateManager) RollbackServer(backupPath string) error {
	// 安全校验：路径必须在 backupDir 下
	dir := m.backupDir()
	if filepath.Dir(backupPath) != dir {
		return fmt.Errorf("非法的备份路径: %s", backupPath)
	}
	name := filepath.Base(backupPath)
	if !isBackupFile(name, "server-") {
		return fmt.Errorf("非法的备份文件名: %s", name)
	}
	// 检查文件存在
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("备份文件不存在: %w", err)
	}
	// 重置回滚状态
	m.rollbackMu.Lock()
	m.rollbackDone = false
	m.rollbackOk = false
	m.rollbackErr = ""
	m.rollbackMu.Unlock()

	// 异步执行
	go func() {
		err := m.doRollbackServer(backupPath)
		m.rollbackMu.Lock()
		m.rollbackDone = true
		if err != nil {
			m.rollbackOk = false
			m.rollbackErr = err.Error()
		} else {
			m.rollbackOk = true
		}
		m.rollbackMu.Unlock()
	}()
	return nil
}

// doRollbackServer 执行实际的回滚步骤。
func (m *UpdateManager) doRollbackServer(backupPath string) error {
	logger().Printf("[rollback] 开始回滚 server，备份文件: %s", backupPath)
	serverDir := m.serverDir()

	// 1. 停止 dsh 服务
	logger().Printf("[rollback] 停止 dsh 服务")
	if err := m.dsh.Stop(); err != nil {
		logger().Printf("[rollback] 停止 dsh 失败: %v", err)
		// 继续尝试回滚，即使 stop 失败
	}

	// 2. 删除当前 server 目录
	logger().Printf("[rollback] 删除当前 server 目录: %s", serverDir)
	if err := os.RemoveAll(serverDir); err != nil {
		return fmt.Errorf("删除 server 目录失败: %w", err)
	}

	// 3. 解压备份到 server 目录
	logger().Printf("[rollback] 解压备份到 %s", serverDir)
	if err := os.MkdirAll(serverDir, 0755); err != nil {
		return fmt.Errorf("创建 server 目录失败: %w", err)
	}
	if err := extractTarGz(backupPath, serverDir); err != nil {
		return fmt.Errorf("解压备份失败: %w", err)
	}

	// 4. 保留备份压缩包（不做删除）：回退后用户仍可再次回滚到其它版本，
	//   备份文件仅由用户手动删除或每日清理任务按 30 天过期清理。

	// 5. 启动 dsh 服务（并异步捕获新 token 换取会话 cookie，供反代转发）。
	//   注：不在此处等待 dsh 完全启动成功——启动动作发出即可，会话 cookie
	//   由异步 captureDshSession 在后台换取，前端无需等待。
	logger().Printf("[rollback] 启动 dsh 服务")
	if err := m.startDshCaptured(); err != nil {
		return fmt.Errorf("启动 dsh 失败: %w", err)
	}

	// 6. 回滚完成后刷新 dsh 版本号状态，前端 reload 后版本行立即显示新版本。
	m.refreshDshVersion()

	logger().Printf("[rollback] server 回滚完成")
	return nil
}

// refreshDshVersion 重新执行 `dsh -V` 并就地更新 dsh 的 LocalVersion 状态
// （仅当取到非空版本号时更新，避免把版本号刷成空串）。用于回滚/安装完成后
// 让前端刷新页面时立即显示新版本，而不是等到下一次自动检测。
func (m *UpdateManager) refreshDshVersion() {
	if v := m.localDshVersion(); v != "" {
		m.updateStatus(updateKindDsh, func(st *UpdateStatus) {
			st.LocalVersion = v
		})
	}
}

// --- DSH 数据备份列表与恢复 ---

// DshDataBackup 描述一个 dsh 数据备份条目。
type DshDataBackup struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
	Path     string `json:"path"`
}

// ListDshDataBackups 列出 backupDir 中所有 dsh-data-<版本>-<时间戳>.tar.gz 文件，按修改时间倒序。
func (m *UpdateManager) ListDshDataBackups() ([]DshDataBackup, error) {
	dir := m.backupDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var backups []DshDataBackup
	for _, e := range entries {
		name := e.Name()
		if !isBackupFile(name, "dsh-data-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		fullPath := filepath.Join(dir, name)
		backups = append(backups, DshDataBackup{
			Name:     name,
			Size:     info.Size(),
			Modified: info.ModTime().Format(time.RFC3339),
			Path:     fullPath,
		})
	}
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Modified > backups[j].Modified
	})
	return backups, nil
}

// DeleteDshDataBackup 删除一个 dsh 数据备份文件（仅限 backupDir 下的 dsh-data-<版本>-<时间戳>.tar.gz）。
func (m *UpdateManager) DeleteDshDataBackup(name string) error {
	dir := m.backupDir()
	target := filepath.Join(dir, name)
	if filepath.Dir(target) != dir || !isBackupFile(name, "dsh-data-") {
		return fmt.Errorf("非法的备份文件名: %s", name)
	}
	return os.Remove(target)
}

// DshRestoreStatus 是 dsh 数据恢复状态。
type DshRestoreStatus struct {
	Running bool   `json:"running"`
	Done    bool   `json:"done"`
	Ok      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

// dsh restore 状态跟踪
type dshRestoreTracker struct {
	mu   sync.Mutex
	done bool
	ok   bool
	err  string
}

var dshRestore dshRestoreTracker

func (t *dshRestoreTracker) status() DshRestoreStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	return DshRestoreStatus{Running: !t.done, Done: t.done, Ok: t.ok, Error: t.err}
}

func (t *dshRestoreTracker) complete(ok bool, errMsg string) {
	t.mu.Lock()
	t.done = true
	t.ok = ok
	t.err = errMsg
	t.mu.Unlock()
}

// GetDshRestoreStatus 返回当前 dsh 数据恢复状态快照。
func (m *UpdateManager) GetDshRestoreStatus() DshRestoreStatus {
	return dshRestore.status()
}

// RestoreDshData 执行 dsh 数据恢复：停止 dsh → 删除 ~/.dsh → 解压备份到 HOME → 启动 dsh。异步。
func (m *UpdateManager) RestoreDshData(backupPath string) error {
	dir := m.backupDir()
	if filepath.Dir(backupPath) != dir {
		return fmt.Errorf("非法的备份路径: %s", backupPath)
	}
	name := filepath.Base(backupPath)
	if !isBackupFile(name, "dsh-data-") {
		return fmt.Errorf("非法的备份文件名: %s", name)
	}
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("备份文件不存在: %w", err)
	}
	// 重置状态
	dshRestore.mu.Lock()
	dshRestore.done = false
	dshRestore.ok = false
	dshRestore.err = ""
	dshRestore.mu.Unlock()

	go func() {
		err := m.doRestoreDshData(backupPath)
		if err != nil {
			dshRestore.complete(false, err.Error())
		} else {
			dshRestore.complete(true, "")
		}
	}()
	return nil
}

func (m *UpdateManager) doRestoreDshData(backupPath string) error {
	home := m.dsh.effectiveHome()
	if home == "" {
		return fmt.Errorf("无法获取主目录")
	}
	dshDir := filepath.Join(home, ".dsh")
	logger().Printf("[restore] 开始恢复 dsh 数据，备份文件: %s", backupPath)

	// 1. 停止 dsh 服务
	logger().Printf("[restore] 停止 dsh 服务")
	if err := m.dsh.Stop(); err != nil {
		logger().Printf("[restore] 停止 dsh 失败: %v", err)
	}

	// 2. 删除当前 ~/.dsh 目录
	logger().Printf("[restore] 删除当前 ~/.dsh 目录: %s", dshDir)
	if err := os.RemoveAll(dshDir); err != nil {
		return fmt.Errorf("删除 ~/.dsh 目录失败: %w", err)
	}

	// 3. 解压备份到 HOME（tar 中顶层为 .dsh/，解压后在 HOME 下还原 ~/.dsh）
	logger().Printf("[restore] 解压备份到 %s", home)
	if err := extractTarGz(backupPath, home); err != nil {
		return fmt.Errorf("解压备份失败: %w", err)
	}

	// 4. 启动 dsh 服务（并异步捕获新 token 换取会话 cookie，供反代转发）
	logger().Printf("[restore] 启动 dsh 服务")
	if err := m.startDshCaptured(); err != nil {
		return fmt.Errorf("启动 dsh 失败: %w", err)
	}

	logger().Printf("[restore] dsh 数据恢复完成")
	return nil
}

// BackupDir 返回统一备份目录路径。
func (m *UpdateManager) BackupDir() string {
	return m.backupDir()
}

// startDailyCleanup 启动每天一次的备份清理任务。
// 扫描 backupDir 中的 harness-<版本>-<时间戳>.tar.gz 与 server-<版本>-<时间戳>.tar.gz，
// 超过 30 天的自动删除；dsh-data-<版本>-<时间戳>.tar.gz 不在自动清理范围内。
func (m *UpdateManager) startDailyCleanup() {
	go func() {
		// 启动后延迟 30 秒执行首次清理
		time.Sleep(30 * time.Second)
		m.runBackupCleanup()
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			m.runBackupCleanup()
		}
	}()
}

// runBackupCleanup 扫描 backupDir，删除超过 30 天的 harness/server/market 备份文件。
func (m *UpdateManager) runBackupCleanup() {
	dir := m.backupDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -30)
	removed := 0
	for _, e := range entries {
		name := e.Name()
		// 自动清理 harness / server / 市场备份；dsh-data-* 不在自动清理范围内。
		// 市场备份用 market- 前缀（不能用 server-，否则会出现在「dsh 服务回滚」
		// 列表里 —— 见 market.go 文件头第 5 条），所以这里要显式带上它，
		// 否则市场备份永远不会被回收。
		if !isBackupFile(name, "harness-") && !isBackupFile(name, "server-") &&
			!isBackupFile(name, marketBackupPrefix) {
			continue
		}
		if !strings.HasSuffix(name, ".tar.gz") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			fullPath := filepath.Join(dir, name)
			if err := os.Remove(fullPath); err == nil {
				removed++
				logger().Printf("[cleanup] 已删除过期备份: %s", name)
			}
		}
	}
	if removed > 0 {
		logger().Printf("[cleanup] 共删除 %d 个过期备份文件", removed)
	}
}
