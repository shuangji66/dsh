package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
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
	Kind        updateKind `json:"kind"`
	LocalVersion string    `json:"localVersion"`  // 本地版本号
	LatestVersion string   `json:"latestVersion"` // 仓库最新 tag 版本号（空表示未获取到）
	HasUpdate   bool       `json:"hasUpdate"`     // 是否有可用更新
	CheckedAt   time.Time  `json:"checkedAt"`     // 最近检测时间
	Error       string     `json:"error,omitempty"` // 最近一次检测/拉取失败原因
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
		}
		m.mu.Unlock()
		m.notify()
		return
	}

	h := pickLatest(tags, "harness-")
	d := pickLatest(tags, "dsh-")

	harnessStatus := m.getStatus(updateKindHarness)
	harnessStatus.Kind = updateKindHarness
	harnessStatus.LocalVersion = harnessVersion
	harnessStatus.CheckedAt = now
	harnessStatus.Error = ""
	if h != nil {
		harnessStatus.LatestVersion = h.version
		harnessStatus.HasUpdate = compareVersion(h.version, harnessVersion) > 0
	} else {
		harnessStatus.LatestVersion = ""
		harnessStatus.HasUpdate = false
	}
	m.setStatus(updateKindHarness, &harnessStatus)

	dshNext := m.getStatus(updateKindDsh)
	dshNext.Kind = updateKindDsh
	dshNext.LocalVersion = dshLocal
	dshNext.CheckedAt = now
	dshNext.Error = ""
	if d != nil {
		dshNext.LatestVersion = d.version
		dshNext.HasUpdate = compareVersion(d.version, dshLocal) > 0
	} else {
		dshNext.LatestVersion = ""
		dshNext.HasUpdate = false
	}
	m.setStatus(updateKindDsh, &dshNext)

	logger().Printf("[update] 检测完成 harness 本地=%s 最新=%s 有更新=%v | dsh 本地=%s 最新=%s 有更新=%v",
		harnessVersion, harnessStatus.LatestVersion, harnessStatus.HasUpdate,
		dshLocal, dshStatus.LatestVersion, dshStatus.HasUpdate)
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
func (m *UpdateManager) downloadToFile(rawURL, dest string) (int64, error) {
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
		n, err := io.Copy(out, resp.Body)
		out.Close()
		resp.Body.Close()
		if err != nil {
			os.Remove(dest)
			lastErr = fmt.Errorf("下载 %s 中断: %w", u, err)
			continue
		}
		logger().Printf("[update] 下载成功 %s (%d bytes)", u, n)
		return n, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("所有下载源均失败")
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

// applyUpdate 执行自我更新。kind 指定更新 harness 还是 dsh。流程严格遵循：
// 先下载成功，再备份替换，最后重启。返回错误则说明更新失败及原因。
func (m *UpdateManager) applyUpdate(k updateKind) error {
	m.applying.Lock()
	defer m.applying.Unlock()

	st := m.getStatus(k)
	if st.LatestVersion == "" {
		return fmt.Errorf("尚未获取到最新版本号，请先执行检查更新")
	}
	version := st.LatestVersion
	arch := m.updateArch()
	logger().Printf("[update] 开始更新 %s 到 %s (arch=%s)", k, version, arch)

	tmpDir := filepath.Join(os.TempDir(), "harness-update-"+string(k)+"-"+time.Now().Format("20060102150405"))
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tgzPath := filepath.Join(tmpDir, "pkg.tar.gz")
	if _, err := m.downloadToFile(m.assetURL(k, version, arch), tgzPath); err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	// 下载成功后解压到临时解压目录
	extractDir := filepath.Join(tmpDir, "extract")
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return fmt.Errorf("创建解压目录失败: %w", err)
	}
	if err := extractTarGz(tgzPath, extractDir); err != nil {
		return fmt.Errorf("解压失败: %w", err)
	}

	switch k {
	case updateKindHarness:
		return m.applyHarness(extractDir)
	case updateKindDsh:
		return m.applyServer(extractDir)
	}
	return fmt.Errorf("未知的更新类型 %s", k)
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
	backupName := fmt.Sprintf("harness-backup-%s.tar.gz", time.Now().Format("20060102150405"))
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

	// 替换二进制：先写入临时文件，再 atomic rename 替换。
	// 不能用 copyFile 直接覆盖（os.Create 截断正在运行的可执行文件会报 "text file busy"）。
	tmpNew := filepath.Join(filepath.Dir(extractDir), "harness.new")
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
	// 重启：以新二进制 exec 覆盖当前进程镜像（保持同一 PID，fnOS 监管不失效）。
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

	// 备份当前 server 目录
	backupName := fmt.Sprintf("server-backup-%s.tar.gz", time.Now().Format("20060102150405"))
	backupPath := filepath.Join(m.backupDir(), backupName)
	if err := tgzDir(serverDir, backupPath); err != nil {
		m.dsh.Start() // 更新失败，尽量恢复 dsh
		return fmt.Errorf("备份 server 目录失败: %w", err)
	}
	logger().Printf("[update] server 已备份到 %s", backupPath)

	// 替换：把旧的 server 移到临时位置，放入新的，再删除临时旧目录。
	oldTmp := filepath.Join(parent, ".server-old-"+time.Now().Format("20060102150405"))
	if err := os.Rename(serverDir, oldTmp); err != nil {
		m.dsh.Start() // 更新失败，尽量恢复 dsh
		return fmt.Errorf("移动旧 server 目录失败: %w", err)
	}
	if err := copyDir(srcServer, serverDir); err != nil {
		// 回滚：把旧目录放回去，并恢复 dsh
		os.Rename(oldTmp, serverDir)
		m.dsh.Start()
		return fmt.Errorf("复制新 server 失败: %w", err)
	}
	os.RemoveAll(oldTmp)

	logger().Printf("[update] server 目录已更新，启动 dsh 服务")
	// 启动 dsh
	if err := m.dsh.Start(); err != nil {
		return fmt.Errorf("更新完成，但启动 dsh 失败: %w", err)
	}
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
	Name     string `json:"name"`     // 文件名，如 server-backup-20260903154421.tar.gz
	Size     int64  `json:"size"`     // 文件大小（字节）
	Modified string `json:"modified"` // 修改时间（RFC3339）
	Path     string `json:"path"`     // 完整路径
}

// ListServerBackups 列出 backupDir 中所有 server-backup-*.tar.gz 文件，按修改时间倒序。
func (m *UpdateManager) ListServerBackups() ([]ServerBackup, error) {
	dir := m.backupDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var backups []ServerBackup
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "server-backup-") || !strings.HasSuffix(name, ".tar.gz") {
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

// DeleteServerBackup 删除一个 server 备份文件（仅限 backupDir 下的 server-backup-*.tar.gz）。
func (m *UpdateManager) DeleteServerBackup(name string) error {
	dir := m.backupDir()
	target := filepath.Join(dir, name)
	// 安全校验：文件必须在 backupDir 下且符合命名规范
	if filepath.Dir(target) != dir || !strings.HasPrefix(name, "server-backup-") || !strings.HasSuffix(name, ".tar.gz") {
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
	if !strings.HasPrefix(name, "server-backup-") || !strings.HasSuffix(name, ".tar.gz") {
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

	// 4. 删除备份压缩包
	logger().Printf("[rollback] 删除备份文件: %s", backupPath)
	if err := os.Remove(backupPath); err != nil {
		logger().Printf("[rollback] 删除备份文件失败（非致命）: %v", err)
		// 不作为回滚失败
	}

	// 5. 启动 dsh 服务
	logger().Printf("[rollback] 启动 dsh 服务")
	if err := m.dsh.Start(); err != nil {
		return fmt.Errorf("启动 dsh 失败: %w", err)
	}

	logger().Printf("[rollback] server 回滚完成")
	return nil
}

// --- DSH 数据备份列表与恢复 ---

// DshDataBackup 描述一个 dsh 数据备份条目。
type DshDataBackup struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
	Path     string `json:"path"`
}

// ListDshDataBackups 列出 backupDir 中所有 dsh-data-backup-*.tar.gz 文件，按修改时间倒序。
func (m *UpdateManager) ListDshDataBackups() ([]DshDataBackup, error) {
	dir := m.backupDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var backups []DshDataBackup
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "dsh-data-backup-") || !strings.HasSuffix(name, ".tar.gz") {
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

// DeleteDshDataBackup 删除一个 dsh 数据备份文件（仅限 backupDir 下的 dsh-data-backup-*.tar.gz）。
func (m *UpdateManager) DeleteDshDataBackup(name string) error {
	dir := m.backupDir()
	target := filepath.Join(dir, name)
	if filepath.Dir(target) != dir || !strings.HasPrefix(name, "dsh-data-backup-") || !strings.HasSuffix(name, ".tar.gz") {
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
	if !strings.HasPrefix(name, "dsh-data-backup-") || !strings.HasSuffix(name, ".tar.gz") {
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

	// 4. 启动 dsh 服务
	logger().Printf("[restore] 启动 dsh 服务")
	if err := m.dsh.Start(); err != nil {
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
// 扫描 backupDir 中的 harness-backup-*.tar.gz 与 server-backup-*.tar.gz，
// 超过 30 天的自动删除；dsh-data-backup-*.tar.gz 不在自动清理范围内。
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
		// 仅自动清理 harness 与 server 备份；dsh-data-backup-*.tar.gz 不在自动清理范围内
		if !strings.HasPrefix(name, "harness-backup-") && !strings.HasPrefix(name, "server-backup-") {
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