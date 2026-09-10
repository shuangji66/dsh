package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
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

// errUpdateCancelled 表示更新下载被用户主动取消（前端点“取消更新”触发）。
var errUpdateCancelled = errors.New("用户取消更新")

// --- GitHub 仓库与发布资源常量 ---
const (
	updateRepoOwner = "shuangji66"
	updateRepoName  = "dsh"
	updateRepoURL   = "https://github.com/" + updateRepoOwner + "/" + updateRepoName

	// updateCheckInterval 是每小时自动检测更新的周期。
	updateCheckInterval = time.Hour
)

// updateAccelerators 是常见 GitHub 加速源前缀（按顺序回退）。
var updateAccelerators = []string{
	"https://gh-proxy.com/",
	"https://ghproxy.net/",
	"https://ghfast.top/",
	"https://gh.api.99988866.xyz/",
}

// updateKind 表示更新目标：harness 控制台或 dsh 服务。
type updateKind string

const (
	updateKindHarness updateKind = "harness"
	updateKindDsh     updateKind = "dsh"
)

// tagInfo 描述一个从仓库读取到的 tag 及其解析出的版本号。
type tagInfo struct {
	name    string // 完整 tag 名，如 harness-1.0.1 / dsh-0.1.2-alpha.5
	version string // 去掉前缀后的版本号，如 1.0.1 / 0.1.2-alpha.5
}

// UpdateStatus 是一次更新检测的状态（harness 与 dsh 各自一份）。
type UpdateStatus struct {
	Kind         updateKind `json:"kind"`
	LocalVersion string     `json:"localVersion"`     // 本地版本号
	LatestVersion string    `json:"latestVersion"`    // 仓库最新 tag 版本号（空表示未获取到）
	HasUpdate    bool       `json:"hasUpdate"`        // 是否有可用更新
	CheckedAt    time.Time  `json:"checkedAt"`        // 最近检测时间
	Error        string     `json:"error,omitempty"`  // 最近一次检测/拉取失败原因
	ReleaseNotes string     `json:"releaseNotes,omitempty"` // 最新 release 的更新内容（正文，不含标题）

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
}

// PendingUpdate 记录某个 kind 已下载完成、等待用户确认安装的更新包。
// 只会保留一个 kind 的一份待安装包；重新下载或安装完成后即被清理。
type PendingUpdate struct {
	Kind    updateKind
	Version string
	PkgPath string // 已下载更新包的 .tar.gz 完整路径
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

	// cancelMu 保护 cancelCh：cancelCh 非 nil 表示正在下载更新包，
	// 关闭它即通知下载协程中断（前端“取消更新”按钮触发）。
	cancelMu sync.Mutex
	cancelCh chan struct{}

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

// httpClientForUpdate 构造用于更新下载/拉取的 HTTP 客户端。优先使用持久化
// JSON 配置（GetConfig().ProxyAddr）中的代理地址；若不可用，回退到不带代理的
// 直连客户端（后续下载再叠加加速源前缀）。探测代理可用性通过一次轻量 HEAD 完成。
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
		for _, st := range m.statuses {
			st.LatestVersion = ""
			st.HasUpdate = false
			st.CheckedAt = now
			st.Error = err.Error()
			st.ReleaseNotes = ""
		}
		m.mu.Unlock()
		m.notify()
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

	// 仅当 harness 或 dsh 任一个有更新时才打印检测结果，无更新时不刷日志。
	hs := m.getStatus(updateKindHarness)
	ds := m.getStatus(updateKindDsh)
	if hs.HasUpdate || ds.HasUpdate {
		logger().Printf("[update] 发现更新 harness 本地=%s 最新=%s | dsh 本地=%s 最新=%s",
			harnessVersion, hs.LatestVersion,
			dshLocal, ds.LatestVersion)
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

// downloadToFile 下载 url 到本地文件，返回文件大小。会依次尝试加速源回退，
// 并沿用既有的代理直连客户端。先尝试加速源（若命中 200 即成功），否则直连。
// progress 为可选的下载进度回调（downloadled/total 字节，节流上报）；cancel 为
// 可选的取消信号 —— 关闭后立即中断下载（已下载的临时文件会被删除），并返回
// errUpdateCancelled。
func (m *UpdateManager) downloadToFile(rawURL, dest string, progress func(downloaded, total int64), cancel <-chan struct{}) (int64, error) {
	// 待尝试的 URL 序列：加速源前缀 + 直连。
	candidates := []string{rawURL}
	for _, acc := range updateAccelerators {
		candidates = append(candidates, acc+rawURL)
	}
	client := m.httpClientForUpdate()

	var lastErr error
	for _, u := range candidates {
		req, err := http.NewRequest("GET", u, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", "harness-console")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("请求 %s 失败: %w", u, err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("请求 %s 返回 %d", u, resp.StatusCode)
			continue
		}
		out, err := os.Create(dest)
		if err != nil {
			resp.Body.Close()
			return 0, err
		}
		// 用可取消 reader 包装响应体：支持进度上报与取消中断。
		var total int64
		if resp.ContentLength > 0 {
			total = resp.ContentLength
		}
		reader := io.Reader(resp.Body)
		if cancel != nil {
			reader = &cancelProgressReader{
				r:        resp.Body,
				cancel:   cancel,
				progress: progress,
				total:    total,
			}
		}
		n, err := io.Copy(out, reader)
		out.Close()
		resp.Body.Close()
		if err != nil {
			os.Remove(dest)
			if errors.Is(err, errUpdateCancelled) {
				// 用户取消：不再尝试其它镜像源。
				lastErr = errUpdateCancelled
				break
			}
			lastErr = fmt.Errorf("下载 %s 中断: %w", u, err)
			continue
		}
		// 下载完成时上报一次最终进度
		if progress != nil {
			progress(n, total)
		}
		logger().Printf("[update] 下载成功 %s (%d bytes)", u, n)
		return n, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("所有下载源均失败")
	}
	if errors.Is(lastErr, errUpdateCancelled) {
		logger().Printf("[update] 下载已被用户取消")
	}
	return 0, lastErr
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

// CancelUpdate 请求取消当前正在进行的更新下载。若当前没有下载（cancelCh 为
// nil），调用被安全忽略。取消会中断下载并在 applyUpdate 中表现为“下载失败: 用户取消”，
// 已下载的临时文件会被清理，不会触碰磁盘上的二进制/server 目录。
func (m *UpdateManager) CancelUpdate() {
	m.cancelMu.Lock()
	defer m.cancelMu.Unlock()
	if m.cancelCh != nil {
		close(m.cancelCh)
		m.cancelCh = nil
	}
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

// cancelProgressReader 包装下载响应体：在每次读取时检查取消信号，并按节流
// 节奏上报进度（每 500ms 或每 256KB 一次），避免高频回调刷爆 SSE。
type cancelProgressReader struct {
	r        io.Reader
	cancel   <-chan struct{}
	progress func(downloaded, total int64)
	total    int64
	n        int64
	lastAt   time.Time
	lastN    int64
}

func (cr *cancelProgressReader) Read(p []byte) (int, error) {
	select {
	case <-cr.cancel:
		return 0, fmt.Errorf("update cancelled")
	default:
	}
	n, err := cr.r.Read(p)
	cr.n += int64(n)
	if cr.progress != nil {
		now := time.Now()
		if now.Sub(cr.lastAt) >= 500*time.Millisecond || cr.n-cr.lastN >= 256<<10 {
			cr.progress(cr.n, cr.total)
			cr.lastAt = now
			cr.lastN = cr.n
		}
	}
	return n, err
}

// downloadUpdate 只下载更新包（第一步），不安装。可在下载过程中取消（CancelUpdate
// 关闭 cancelCh 中断），下载成功后把 .tar.gz 放到持久的“待安装”目录并记入 pending，
// 推送 phase=downloaded / readyToInstall=true，等待用户在弹窗里点“安装”。
// 返回错误表示下载失败或被用户取消。
func (m *UpdateManager) downloadUpdate(k updateKind) error {
	m.applying.Lock()
	defer m.applying.Unlock()

	st := m.getStatus(k)
	if st.LatestVersion == "" {
		return fmt.Errorf("尚未获取到最新版本号，请先执行检查更新")
	}
	version := st.LatestVersion
	arch := m.updateArch()
	logger().Printf("[update] 开始下载 %s 到 %s (arch=%s)", k, version, arch)

	// 目标包持久保存在“待安装”目录，跨“下载→安装”两步保留。
	// 文件名带时间戳，避免与同版本上次下载冲突（setPending 会清理同 kind 旧包）。
	pkgPath := filepath.Join(m.pendingDir(), string(k)+"-"+version+"-"+time.Now().Format("20060102150405")+".tar.gz")

	// 建立取消信号：CancelUpdate 关闭 cancelCh 以中断本次下载。
	cancelCh := make(chan struct{})
	m.cancelMu.Lock()
	m.cancelCh = cancelCh
	m.cancelMu.Unlock()
	// 进入“下载中”状态（清空上一次的进度/错误/取消残留）。
	m.updateStatus(k, func(s *UpdateStatus) {
		s.Phase = "downloading"
		s.ReadyToInstall = false
		s.Cancelled = false
		s.Error = ""
		s.Downloading = true
		s.DownloadPct = 0
		s.DownloadedBytes = 0
		s.TotalBytes = 0
	})

	n, err := m.downloadToFile(m.assetURL(k, version, arch), pkgPath,
		func(downloaded, total int64) {
			pct := 0
			if total > 0 {
				pct = int(downloaded * 100 / total)
				if pct > 100 {
					pct = 100
				}
			}
			m.setDownloadProgress(k, true, pct, downloaded, total)
		}, cancelCh)
	m.cancelMu.Lock()
	m.cancelCh = nil
	m.cancelMu.Unlock()

	if err != nil {
		os.Remove(pkgPath)
		// 取消或失败：退出“下载中”状态（失败原因由 error / cancelled 字段呈现）。
		m.updateStatus(k, func(s *UpdateStatus) {
			s.Phase = ""
			s.Downloading = false
			s.DownloadPct = 0
			s.DownloadedBytes = 0
			s.TotalBytes = 0
		})
		return fmt.Errorf("下载失败: %w", err)
	}

	// 下载成功：记录待安装包，推送“已下载待安装”。
	m.setPending(&PendingUpdate{Kind: k, Version: version, PkgPath: pkgPath})
	m.updateStatus(k, func(s *UpdateStatus) {
		s.Phase = "downloaded"
		s.ReadyToInstall = true
		s.Downloading = false
		s.DownloadPct = 100
		s.DownloadedBytes = n
		if s.TotalBytes <= 0 {
			s.TotalBytes = n
		}
		s.Error = ""
		s.Cancelled = false
	})
	logger().Printf("[update] %s 更新包已下载到 %s (%d bytes)，等待安装", k, pkgPath, n)
	return nil
}

// installUpdate 安装已下载的更新包（第二步）。读取 pending 中的 .tar.gz，解压后
// 调用 applyHarness / applyServer 执行“备份→替换→重启”。安装阶段耗时短、不可取消。
// 安装失败时保留 pending（用户可重试安装）；安装成功后仅 dsh 情形清除 pending
// （harness 成功时进程会被 exec 换新映像，根本不会执行到这里）。
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
		installErr = m.applyHarness(tmpDir)
	case updateKindDsh:
		installErr = m.applyServer(tmpDir)
	default:
		installErr = fmt.Errorf("未知的更新类型 %s", k)
	}
	if installErr != nil {
		// 安装失败：保留待安装包（可重试安装），退出“安装中”状态。
		m.updateStatus(k, func(s *UpdateStatus) { s.Phase = "" })
		return installErr
	}
	// 安装成功：dsh 情形清除待安装包；harness 情形进程已被 exec 替换（若 exec
	// 失败返回 nil，则保留包供重试）。
	if k == updateKindDsh {
		m.clearPending()
	}
	return nil
}

// DiscardUpdate 删除已下载待安装的更新包（前端“删除更新包”按钮触发），
// 并把该 kind 重置为初始待更新状态（phase 清空、readyToInstall=false、
// 取消/错误标记清空），前端按钮回到“下载更新”。无待安装包时忽略。
func (m *UpdateManager) DiscardUpdate(k updateKind) error {
	m.applying.Lock()
	defer m.applying.Unlock()

	if m.getPending() == nil {
		return nil // 没有待安装更新包，忽略即可
	}
	m.clearPending()
	m.updateStatus(k, func(s *UpdateStatus) {
		s.Phase = ""
		s.ReadyToInstall = false
		s.Downloading = false
		s.DownloadPct = 0
		s.DownloadedBytes = 0
		s.TotalBytes = 0
		s.Error = ""
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

// applyHarness 备份并替换控制台二进制，随后重启控制台。
func (m *UpdateManager) applyHarness(extractDir string) error {
	newBin, err := findExecutable(extractDir, "harness")
	if err != nil {
		return err
	}
	binDir := m.harnessBinDir()
	dest := filepath.Join(binDir, "harness")

	// 先做备份（压缩当前二进制）。用独立 staging 目录避免打包整棵临时树。
	backupName := fmt.Sprintf("harness-%s-%s.tar.gz", harnessVersion, time.Now().Format("20060102150405"))
	backupPath := filepath.Join(m.backupDir(), backupName)
	stage := filepath.Join(filepath.Dir(extractDir), "backup-stage")
	if err := os.MkdirAll(stage, 0755); err != nil {
		return fmt.Errorf("创建备份临时目录失败: %w", err)
	}
	defer os.RemoveAll(stage)
	if err := copyFile(dest, filepath.Join(stage, "harness")); err != nil {
		return fmt.Errorf("读取当前二进制用于备份失败: %w", err)
	}
	if err := tgzDir(stage, backupPath); err != nil {
		return fmt.Errorf("备份当前二进制失败: %w", err)
	}
	logger().Printf("[update] harness 已备份到 %s", backupPath)

	// 替换二进制：在目标同目录下先写入临时文件，再 atomic rename 替换。
	// 不能用 copyFile 直接覆盖（os.Create 截断正在运行的可执行文件会报 "text file busy"）；
	// 临时文件必须与 dest 同目录（否则跨文件系统的 rename 会报 "invalid cross-device link"）。
	tmpNew := filepath.Join(binDir, ".harness.new")
	if err := copyFile(newBin, tmpNew); err != nil {
		os.Remove(tmpNew)
		return fmt.Errorf("复制新二进制失败: %w", err)
	}
	if err := os.Rename(tmpNew, dest); err != nil {
		os.Remove(tmpNew)
		return fmt.Errorf("替换二进制失败: %w", err)
	}
	if err := os.Chmod(dest, 0755); err != nil {
		logger().Printf("[update] chmod 失败: %v", err)
	}

	logger().Printf("[update] harness 二进制已更新，准备重启控制台")
	// 先停止 dsh 服务，再由新二进制 exec 覆盖当前进程镜像（保持同一 PID，fnOS 监管不失效）。
	// 新 harness 进程启动时会自动拉起 dsh，先停止可避免端口冲突或残留进程。
	logger().Printf("[update] 停止 dsh 服务")
	if err := m.dsh.Stop(); err != nil {
		logger().Printf("[update] 停止 dsh 失败: %v", err)
	}
	m.restartHarness(dest)
	return nil
}

// restartHarness 用新二进制替换当前进程镜像。
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

// runBackupCleanup 扫描 backupDir，删除超过 30 天的 harness/server/dsh-data 备份文件。
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
		// 仅自动清理 harness 与 server 备份；dsh-data-* 不在自动清理范围内
		if !isBackupFile(name, "harness-") && !isBackupFile(name, "server-") {
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