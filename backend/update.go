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
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// harnessVersion 是控制台自身的构建版本号。默认从 1.0.0 起；构建时可经
// -ldflags "-X main.harnessVersion=..." 覆盖。它代表"harness 控制台"的版本，
// 与 dsh 服务版本（dsh -V）相互独立。
var harnessVersion = "1.0.0"

// errUpdateCancelled 表示一次安装被用户主动取消（目前是 dsh 版本安装：
// server.go 中止 npm 并清掉安装目录与缓存，状态回到空闲）。
var errUpdateCancelled = errors.New("用户取消更新")

// busyf 构造一条「并发拒绝」的用户错误（HTTP 层回 409，文案走前端 i18n）。
// code 见 uimsg.go 的约定；zh 里的 %s 按 kv 的 value 顺序填充。
func busyf(code, zh string, kv ...string) error {
	return &userError{msg: uiMsg{Code: code, Params: kvMap(kv)}, text: formatKV(zh, kv), busy: true}
}

// --- GitHub 仓库常量 ---
const (
	updateRepoOwner = "shuangji66"
	updateRepoName  = "dsh"

	// updateCheckInterval 是每小时自动检测更新的周期。
	updateCheckInterval = time.Hour
)

// updateKind 表示更新目标：dsh 服务、插件市场（dshmarket）或增强插件
// （dsh-fnos-advanced）。
type updateKind string

const (
	updateKindDsh updateKind = "dsh"
	// updateKindMarket 表示「就地把 server 目录里那份 dshmarket 换成 npm 最新版」。
	// 它不是 GitHub release 资产（版本来自 npm registry），细节见 market.go。
	updateKindMarket updateKind = "market"
	// updateKindAdvanced 表示增强插件 dsh-fnos-advanced：与市场同一套形态（普通
	// profile 插件，检测/安装/更新/卸载全走 `dsh plugin …`，见 market.go）。
	updateKindAdvanced updateKind = "advanced"
)

// updateLogTag 返回某个更新目标的日志标签：dsh（dsh 服务）、market（插件市场）
// 与 advanced（增强插件）的更新日志分开输出，便于按目标筛日志、看清「这次更新的是谁」。
func updateLogTag(k updateKind) string {
	switch k {
	case updateKindDsh:
		return "[dsh]"
	case updateKindMarket:
		return "[market]"
	case updateKindAdvanced:
		return "[advanced]"
	default:
		return "[update]"
	}
}

// UpdateStatus 是一次更新检测的状态（dsh 服务 / 插件市场 / 增强插件各一份）。
//
// 三条链路的**状态字段同构，数据来源完全不同**：
//   - dsh：本地已安装版本目录 + npm 镜像源的 dist-tags（只有版本号与「有更新」，
//     下载/切换走 server.go 自己的进度状态，不再经这里）；
//   - 市场与增强插件：`dsh plugin --profile web list` 的检测结果 + npm 镜像源
//     （安装/卸载/更新走 market.go，进度用 Phase/Message 表达）。
type UpdateStatus struct {
	Kind          updateKind `json:"kind"`
	LocalVersion  string     `json:"localVersion"`  // 本地版本号
	LatestVersion string     `json:"latestVersion"` // 最新版本号（空表示未获取到）
	// HasUpdate 表示「有可就地安装的更新」（dsh 服务 / 受管插件）。
	HasUpdate    bool      `json:"hasUpdate"`
	CheckedAt    time.Time `json:"checkedAt"`              // 最近检测时间
	Error        string    `json:"error,omitempty"`        // 最近一次检测/拉取失败原因
	ReleaseNotes string    `json:"releaseNotes,omitempty"` // 最新 release 的更新内容（正文，不含标题）

	// 流程阶段：""(空闲) / installing(安装中) / removing(卸载中)。dsh 版本安装的进度
	// 不走这里（见 ServerVersions.Install）。
	Phase string `json:"phase,omitempty"`
	// Cancelled 表示最近一次操作被用户主动取消（仅失败推送时置位）。
	Cancelled bool `json:"cancelled,omitempty"`
	// Message 是当前阶段的说明/实时尾行（受管插件用：`dsh plugin …` 的进度与输出尾行）。
	Message string `json:"message,omitempty"`
	// 市场安装位置诊断（kind=market / advanced）：`dsh plugin --profile web list` 的
	// 原始输出，用于解释「没检测到插件」这类情况。字段名沿用 marketDir（历史命名），
	// 两个受管插件共用。
	MarketDir string `json:"marketDir,omitempty"`
	// Unavailable（kind=market / advanced）表示**这次没能查到**本机装的是哪一版：dsh
	// 服务未就绪（启动中 / 已停止 / 没选中任何版本）或 `dsh plugin --profile web list`
	// 报错。这时 LocalVersion 为空**不代表「未安装」**—— 前端据此显示「—」而不是
	// 「未安装」，否则用户在 dsh 还没起来时会以为插件没装。
	// 首帧（进程刚起、还没检测过）也是这个状态：没检测过 = 不知道。
	Unavailable bool `json:"unavailable,omitempty"`
	// Seq 是「第几次操作」的序号（插件市场 / 增强插件共用同一个计数器，见
	// runPluginOp）：前端用它给「操作结果只提示一次」去重 —— 只按 phase/error 去重时，
	// 第二次同样的结果（装成功后再卸成功，phase 都是 done/error 都为空）会被静默吞掉。
	Seq int64 `json:"seq,omitempty"`
	// ErrorRef / MessageRef 是 Error / Message 的「界面文案引用」（code + 参数）：
	// 前端按 code 走 i18n，取不到才回退到原文。只给**面向用户**的文案设它们，
	// 诊断类文本（npm/exec 原始输出）不设。见 uimsg.go 与 setErrFields/setMsgFields。
	ErrorRef   *uiMsg `json:"errorRef,omitempty"`
	MessageRef *uiMsg `json:"messageRef,omitempty"`
}

// UpdateManager 管理 dsh 与受管插件的版本检测、SSE 推送，以及 dsh 数据备份/恢复。
type UpdateManager struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
	// statuses 分别保存 dsh、插件市场与增强插件的最近检测结果。
	statuses map[updateKind]*UpdateStatus
	// applying 用于防止并发执行会停/启 dsh 或改 ~/.dsh 的操作（同一时刻只允许一个）。
	applying sync.Mutex
	// pluginOpSeq 是受管插件操作的序号（每次安装/更新/卸载 +1，市场与增强插件共用），
	// 随状态推给前端用于「结果只提示一次」的去重键。用原子量：它只在 runPluginOp 里
	// 自增，不需要参与 statuses 的锁。
	pluginOpSeq atomic.Int64
	// backupMu 保证「同一时刻只有一个 dsh 数据备份」在跑（备份本身与更新互斥走
	// applying，见 BackupDshData）。单独一把锁只为把「已有备份在进行」和
	// 「正在更新」区分成不同提示。
	backupMu sync.Mutex
	renv     *RuntimeEnv
	dsh      *DshManager // 用于执行 `dsh plugin …` / `dsh -V` 等命令（复用其运行环境）
	// server 管理 dsh 服务的多版本（列表/安装/删除/切换），见 server.go。
	server *ServerManager

	// marketLocalMu 保护 pluginLocalCache：受管插件（市场 / 增强插件）「本机装的是
	// 哪一版」的本地检测结果，按 kind 分开缓存。那次检测要起一个
	// `dsh plugin --profile web list` 子进程（1~3 秒），因此像版本列表一样缓存起来，
	// 只在显式刷新时重跑（见 market.go 的 detectPluginLocal）。
	marketLocalMu    sync.Mutex
	pluginLocalCache map[updateKind]*marketLocalSnapshot

	// checkLogMu 保护 lastCheckSig：按目标分别记录「上次检测结论」，
	// 只在某个目标的结论发生变化时才记一行，避免每小时自动检测重复输出同样的内容。
	checkLogMu   sync.Mutex
	lastCheckSig map[updateKind]string

	// dshNotes 是「按版本号查更新日志」的索引（见 notesIndex）：dsh 版本列表里点某个
	// 版本号时按需拉取，结果缓存在这里。存的是构造函数而不是客户端实例。
	// 受管插件（插件市场 / 增强插件）**不用索引**：它们只取「要装的那一版」的正文
	// （见 market.go 的 refreshPluginStatus）——一个 tag 一次请求，不需要整仓日志。
	dshNotes *notesIndex
}

// newUpdateManager 创建更新管理器并依据运行时环境填充本地版本。
func newUpdateManager(renv *RuntimeEnv, dsh *DshManager) *UpdateManager {
	m := &UpdateManager{
		subs:     make(map[chan struct{}]struct{}),
		statuses: make(map[updateKind]*UpdateStatus),
		renv:     renv,
		dsh:      dsh,
	}
	// 「按版本号查更新日志」的索引（只有 dsh 版本列表用）。超时 15 秒：更新日志是点开才
	// 看的次要内容，不能像版本检测那样握着请求慢慢等。
	// **只拉最近 dshNotesReleases 个 release**：版本列表本身也只有最近这些版本（更早的
	// 被 dshMinVersion 挡掉），一次请求就能覆盖，不必把整个仓库的日志都拖回来。
	m.dshNotes = newNotesIndex(dshReleaseRepo, dshReleaseTag, dshNotesReleases, m.httpClientForUpdate, 15*time.Second)
	// dsh 的本地版本号 = 当前选中版本的目录名（server.go），不联网、不执行命令：
	// 「哪个版本装着」是磁盘上的事实，`dsh -V` 只用于安装完成后的校验。
	m.server = newServerManager(renv, dsh, m)
	m.statuses[updateKindDsh] = &UpdateStatus{Kind: updateKindDsh}
	// 市场（dshmarket）与增强插件（dsh-fnos-advanced）的本地检测都要起一个
	// `dsh plugin --profile web list` 子进程（约 1~3 秒），因此**异步**做：控制台
	// 启动路径不能被它拖住。检测结果经 SSE 推给前端；最新版等 checkOnce/手动检查时
	// 才查镜像源。
	// 初始 Unavailable=true：这一刻还没检测过，界面显示「—」（而不是「未安装」）；
	// 第一次检测出结论后由 applyPluginLocalResult 改掉。
	m.statuses[updateKindMarket] = &UpdateStatus{Kind: updateKindMarket, Unavailable: true}
	m.statuses[updateKindAdvanced] = &UpdateStatus{Kind: updateKindAdvanced, Unavailable: true}
	go func() {
		// 稍等一会再检测：让启动流水线（含 dsh 自身的启动）先走完，避免与它抢 profile。
		// 这一次**只是尽早拿到结果**：它很可能撞上「dsh 还没起来」而失败，那时状态是
		// 「查不了」（界面「—」）；启动流水线收尾时 main.go 会再查一次（见
		// RefreshMarketAfterDshStart）。
		// force=true：缓存此时是空的（或上一轮的旧值），启动这一次要拿到当下的真实安装情况。
		time.Sleep(5 * time.Second)
		m.refreshManagedPluginsLocal(true)
	}()
	m.server.refreshDshStatus()
	// 清理旧版自我更新遗留的待安装包（backup/pending/）：升级只走 fpk，这些包不会再被
	// 任何路径读取，留着只会占磁盘。
	m.removeLegacyPendingDir()
	// 清理上次打包中途被杀留下的半成品备份（<名称>.part）：它不会被备份列表列出，
	// 但一直占着磁盘 —— 备份是 GB 级，漏一个就够明显。
	m.sweepPartialBackups()
	return m
}

// removeLegacyPendingDir 删除旧版自我更新遗留的「已下载待安装」包目录。
//
// 自我更新已整体移除（见 AGENTS 规则 14）：pending 包只用于「下载 → 安装」两步之间，
// 现在既没有下载入口也没有安装入口，任何一份留在盘上都是永远用不到的几十 MB。
func (m *UpdateManager) removeLegacyPendingDir() {
	dir := filepath.Join(m.backupDir(), "pending")
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		logWarn("[backup] failed to remove legacy pending update packages: %v", err)
		return
	}
	logInfo("[backup] removed legacy pending update packages")
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

// releaseRepo 描述「更新日志取自哪个 GitHub 仓库」。
type releaseRepo struct {
	owner string
	name  string
}

// url 返回仓库页面地址（拉 release 页面 HTML 用）。
func (r releaseRepo) url() string {
	return "https://github.com/" + r.owner + "/" + r.name
}

// dshReleaseRepo / marketReleaseRepo / advancedReleaseRepo 是 dsh 服务、插件市场与
// 增强插件各自的**上游**发布仓库。三者与 npm 包一一对应（包元数据里的
// repository.url 就是它们），因此版本号的来源（npm 镜像源）与更新日志的来源
// （GitHub Release）不会漂移，不必再去解析包元数据：
//   - @deepseek-ai/dsh     → deepseek-ai/deepseek-harness，tag 形如 `dsh-v0.2.1-alpha.2`；
//   - dshmarket           → dsh-market/dsh-market，tag 形如 `v1.66.14`；
//   - dsh-fnos-advanced   → shuangji66/dsh（本仓库），tag 形如
//     `dsh-fnos-advanced-v0.2.3`。
//
// tag 前缀的差异集中在 dshReleaseTag / marketReleaseTag / advancedReleaseTag，
// 别在别处再拼一份。
var (
	dshReleaseRepo      = releaseRepo{owner: "deepseek-ai", name: "deepseek-harness"}
	marketReleaseRepo   = releaseRepo{owner: "dsh-market", name: "dsh-market"}
	advancedReleaseRepo = releaseRepo{owner: updateRepoOwner, name: updateRepoName}
)

// dshReleaseTag / marketReleaseTag / advancedReleaseTag 把 npm 版本号还原成对应仓库的
// release tag。
func dshReleaseTag(version string) string      { return "dsh-v" + version }
func marketReleaseTag(version string) string   { return "v" + version }
func advancedReleaseTag(version string) string { return "dsh-fnos-advanced-v" + version }

// dshNotesReleases 是 dsh 版本列表的更新日志**一次拉取的 release 数量上限**：只覆盖
// 最近 10 个版本。版本列表本身也只列最近这些版本，所以正常情况下每个能点的版本号都查得到；
// 超出这个窗口的版本会得到「没有更新日志」，该文案里已写明覆盖范围（release_notes_empty）。
const dshNotesReleases = 10

// fetchReleaseNotes 获取指定仓库、指定 tag 的 release 正文（不含标题 name）。优先走
// GitHub Releases API（取 body 字段）；API 受速率限制或不可用时，回退到非 API
// 的 release 页面 HTML（此页面不受 API 限流），解析其中的 markdown-body 正文。
// 任何失败都返回空串，不影响更新检测主流程。返回的正文保留原始 Markdown
// 文本（API 路径）或经 stripHTMLToText 还原的可读文本（HTML 回退路径），
// 由前端做轻量 Markdown 渲染展示。
func fetchReleaseNotes(client *http.Client, repo releaseRepo, tag string) string {
	if body := fetchReleaseNotesViaAPI(client, repo, tag); body != "" {
		return body
	}
	return fetchReleaseNotesViaHTML(client, repo, tag)
}

// fetchReleaseNotesViaAPI 通过 GitHub Releases API 的 body 字段获取正文。
func fetchReleaseNotesViaAPI(client *http.Client, repo releaseRepo, tag string) string {
	apiURL := fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s",
		githubAPIBase, repo.owner, repo.name, url.PathEscape(tag))
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
func fetchReleaseNotesViaHTML(client *http.Client, repo releaseRepo, tag string) string {
	pageURL := fmt.Sprintf("%s/releases/tag/%s", repo.url(), url.PathEscape(tag))
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

// --- 按版本查更新日志（dsh 版本列表 / 插件市场） ---

// githubAPIBase 是 GitHub REST API 的根地址。抽成变量只为单测能把请求打到假服务器
// （见 release_notes_test.go）；运行时不会被改，也没有对应的环境变量。
var githubAPIBase = "https://api.github.com"

// releaseNotes 是一个版本的中英双语更新日志（任一为空串表示该语言没有对应段落）。
type releaseNotes struct {
	// Note 是中文正文（界面语言为中文时显示）。
	Note string `json:"note"`
	// NoteEn 是英文正文（界面语言为英文时显示）。取不到英文段落时**留空**，
	// 前端会回退显示中文 —— 不在这里复制一份，免得同一份文本在两个字段里各存一遍。
	NoteEn string `json:"noteEn"`
}

// notesIndex 按「npm 版本号」索引某个 GitHub 仓库的 release 更新日志（目前只有 dsh
// 版本列表在用；受管插件的日志只取「要装的那一版」走单 tag 拉取，不用这个索引）。
//
// 为什么要按版本号索引、而不是每次都去问 GitHub：一个版本点一次就发一次请求，
// 用户来回点几下就会撞上未认证 API 的限流（60 次/小时）。这里把**最近 perPage 个
// release 一次拉全**（dsh 用 10 个，见 dshNotesReleases）并按版本号建索引，之后所有
// 查询都命中缓存；索引里没有的版本号由 loaded 一次性判定为「没有日志」，不会被反复重查。
//
// 并发：一把互斥锁 + inFlight/done（单飞）——并发的同一个拉取合并成一次，且**不在持锁
// 期间发网络请求**（否则首次拉取失败时 15 秒超时会让所有等锁的人一起卡住）。
type notesIndex struct {
	// repo 是索引对应的 GitHub 仓库；tag 把版本号还原成该仓库的 release tag。
	repo releaseRepo
	tag  func(version string) string
	// perPage 是一次拉取的 release 数量上限（= 索引覆盖的版本数）。
	// 只覆盖最近这些版本：更早的版本查不到日志时会得到「没有更新日志」，
	// 因此前端的空文案必须说清这个覆盖范围（见 useI18n 的 release_notes_empty）。
	perPage int
	// client 与 timeout 由调用方给（与更新检测共用「代理更新」开关的客户端）。
	client  func() *http.Client
	timeout time.Duration

	mu       sync.Mutex
	versions map[string]releaseNotes
	// loaded 表示「索引已经成功拉过一次」。为真时**任何**未命中的版本号都可以直接
	// 判定为「该版本没有更新日志」，不必再打一次 GitHub（这是限流下最关键的一条）；
	// 为假（还没拉过 / 上次拉失败）时未命中才需要真去拉。
	loaded bool
	// inFlight 记录「已有一次拉取在跑」：并发的第二次调用直接等它跑完，
	// 避免同时打两次 API（限流下这是双重浪费）。
	inFlight bool
	done     chan struct{}
}

func newNotesIndex(repo releaseRepo, tag func(string) string, perPage int, client func() *http.Client, timeout time.Duration) *notesIndex {
	if perPage <= 0 {
		perPage = 10
	}
	return &notesIndex{
		repo:     repo,
		tag:      tag,
		perPage:  perPage,
		client:   client,
		timeout:  timeout,
		versions: map[string]releaseNotes{},
	}
}

// Lookup 返回某个版本的更新日志。第二个返回值表示「拉取是否成功」：
//   - 成功但没有该版本的 release → (空 releaseNotes, true)：界面显示「该版本没有更新日志」；
//   - 拉取失败（断网 / 限流）→ (空 releaseNotes, false)：界面显示「暂时取不到」，
//     且**不写缓存**，下次点还能重试。
func (ix *notesIndex) Lookup(version string) (releaseNotes, bool) {
	if version == "" {
		return releaseNotes{}, true
	}
	for attempt := 0; attempt < 2; attempt++ {
		ix.mu.Lock()
		if n, ok := ix.versions[version]; ok {
			ix.mu.Unlock()
			return n, true
		}
		if ix.loaded {
			// 整仓索引已拉全，缓存里没有就是「该版本没有更新日志」—— 不必再去问 GitHub。
			ix.mu.Unlock()
			return releaseNotes{}, true
		}
		if ix.inFlight {
			// 已有一次拉取在跑：等它结束，然后回头再按缓存/loaded 判定一次
			// （这一轮不发请求，也不占着锁等网络）。
			done := ix.done
			ix.mu.Unlock()
			select {
			case <-done:
			case <-time.After(ix.timeout + 5*time.Second):
				// 等超时（理论上不该发生）：当作这次没取到，让前端可以重试。
				return releaseNotes{}, false
			}
			continue
		}
		ix.inFlight = true
		ix.done = make(chan struct{})
		ix.mu.Unlock()

		fetched, err := ix.fetchAll()

		ix.mu.Lock()
		if err == nil {
			ix.loaded = true
			// **合并**拉到的版本，而不是整份替换：并发查询的请求在等这次拉取期间，
			// 可能已经把某个「拉取成功但确实没有 release」的版本号记成空日志（负缓存）；
			// 整份替换会把这些记录连同结论一起丢掉，下一个请求又会重查一遍。
			for v, n := range fetched {
				ix.versions[v] = n
			}
		}
		delete(ix.versions, "")
		ix.inFlight = false
		close(ix.done)
		// 缓存里没有这个版本 → 零值 releaseNotes + 下面的 true，即「成功但该版本没有日志」。
		n := ix.versions[version]
		ix.mu.Unlock()
		if err != nil {
			return releaseNotes{}, false
		}
		return n, true
	}
	return releaseNotes{}, false
}

// fetchAll 拉取该仓库**最近 perPage 个 release**（dsh 用 10 个，见 dshNotesReleases）
// 并按版本号建索引。
// Release 正文里可能带未展开的 HTML（上游用 <h3 id="…"> 做中英锚点、用 <details>
// 收起长内容），这里统一用 stripHTMLToText 压成纯文本，前端按纯文本展示（不解析 HTML）。
func (ix *notesIndex) fetchAll() (map[string]releaseNotes, error) {
	apiURL := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=%d",
		githubAPIBase, ix.repo.owner, ix.repo.name, ix.perPage)
	client := ix.client()
	if client == nil {
		client = &http.Client{Timeout: ix.timeout}
	}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "harness-console")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("releases API returned %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	var rels []struct {
		TagName string `json:"tag_name"`
		Body    string `json:"body"`
		Draft   bool   `json:"draft"`
	}
	if err := json.Unmarshal(raw, &rels); err != nil {
		return nil, err
	}
	out := make(map[string]releaseNotes, len(rels))
	prefix := ix.tag("")
	for _, r := range rels {
		if r.Draft {
			continue
		}
		v := r.TagName
		if prefix != "" && strings.HasPrefix(v, prefix) {
			v = strings.TrimPrefix(v, prefix)
		}
		if !validVersionArg(v) {
			continue
		}
		cn, en := splitBilingualNotes(r.Body)
		out[v] = releaseNotes{Note: cn, NoteEn: en}
	}
	logInfo("[update] release notes index for %s/%s: %d versions", ix.repo.owner, ix.repo.name, len(out))
	return out, nil
}

// splitBilingualNotes 把 dsh release 正文切成中文 / 英文两块。
//
// 上游正文形如（见 https://github.com/deepseek-ai/deepseek-harness/releases）：
//
//	[中文](#cn-<版本>-community) | [English](#en-<版本>-community)
//	<h3 id="cn-<版本>-community">新增功能</h3>
//	- …
//	<h3 id="en-<版本>-community">New Features</h3>
//	- …
//
// 因此按两个**锚点 id**（不是标题文字）切分：中文块从 cn 锚点开始，英文块从 en 锚点
// 开始。顶部那行导航链接在 cn 锚点之前，会被自然排除。
//
// 两种退化形态：
//   - 只有 cn 锚点（英文段落缺失或旧格式没有锚点）→ 中文块 = 全文，英文留空，
//     前端按界面语言回退到中文；
//   - 两个锚点都没有（极早期版本的 id 是 `chinese` / `english`，与 `cn-` / `en-` 不一致）
//     → 无法判定语言，中文块 = 全文（那是 0.2.0-rc.1 之前的版本，控制台根本不允许安装，
//     见 dshMinVersion，所以不会再出现在这个弹窗里）。
//
// 刻意不按标题文字（如「新增功能」/「New Features」）判断：那是会变的展示文本，
// 判错会把整段日志吞掉。
func splitBilingualNotes(body string) (cn, en string) {
	b := strings.TrimSpace(body)
	if b == "" {
		return "", ""
	}
	cnRe := regexp.MustCompile(`(?i)<h[1-6][^>]*id="cn-`)
	enRe := regexp.MustCompile(`(?i)<h[1-6][^>]*id="en-`)
	cnIdx := cnRe.FindStringIndex(b)
	enIdx := enRe.FindStringIndex(b)
	switch {
	case cnIdx != nil && enIdx != nil && enIdx[0] > cnIdx[0]:
		return stripNotesNav(stripHTMLToText(b[cnIdx[0]:enIdx[0]])), stripNotesNav(stripHTMLToText(b[enIdx[0]:]))
	case enIdx != nil && (cnIdx == nil || enIdx[0] < cnIdx[0]):
		// 只有英文锚点：整段当英文，中文留空（前端会回退到英文）。
		return "", stripNotesNav(stripHTMLToText(b[enIdx[0]:]))
	case cnIdx != nil:
		return stripNotesNav(stripHTMLToText(b[cnIdx[0]:])), ""
	default:
		return stripNotesNav(stripHTMLToText(b)), ""
	}
}

// stripNotesNav 去掉正文开头的「语言导航」行（`[中文](#…) | [English](#…)`）。
//
// 正常情况下它在 cn 锚点之前、切分时就已被排除；但对**没有语言锚点**的旧正文，
// 整段都会落到中文块里，于是导航行成了正文第一行（真机上实测到过：0.1.3-alpha.2 的
// 正文以 `[中文](#chinese) | [English](#english)` 开头）。它不是更新内容，去掉。
func stripNotesNav(block string) string {
	lines := strings.Split(block, "\n")
	navRe := regexp.MustCompile(`^\s*\[中文\]\(#[^)]*\)\s*\|\s*\[English\]\(#[^)]*\)\s*$`)
	for len(lines) > 0 && navRe.MatchString(lines[0]) {
		lines = lines[1:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// --- HTTP 客户端 ---

// httpClientForUpdate 构造用于「元数据拉取」（GitHub release notes / npm registry）
// 的 HTTP 客户端，带 60s 总超时。控制台不再下载更新包，因此没有「代理/直连双通路」
// 那套逻辑，也不需要代理探测。
func (m *UpdateManager) httpClientForUpdate() *http.Client {
	return &http.Client{Timeout: 60 * time.Second}
}

// --- 检测主流程 ---

// checkOnce 执行一次完整的版本检测：dsh 走 npm 镜像源的版本列表，插件市场与增强插件
// 走 `dsh plugin list` + 镜像源的版本列表（三条独立链路）。
// harness 控制台自身的版本不在这里检测：它只能通过更新 fpk 安装包升级。
func (m *UpdateManager) checkOnce() {
	// dsh 服务版本：本地版本 = 选中的版本目录，最新版 = 版本列表里**版本号最高**的一版
	// （不看 dist-tags，见 server.go 的 refreshDshStatus）。
	// force=true：这一趟本来就是联网检测，顺带刷新版本列表缓存（见 refreshVersions 注释）。
	m.server.refreshVersions(true)

	// 插件市场 / 增强插件：版本来自 `dsh plugin list` + 镜像源，与 dsh 的版本列表是
	// 两条独立链路。
	m.refreshManagedPluginStatuses()

	// dsh、市场、增强插件各自一行，且只有**自己**的结论变化时才打印（三者的
	// 「上次结论」分别记录）：三类更新是彼此独立的升级动作，合并成一行既难读、也没法
	// 按目标筛日志；而每小时自动检测若每次都输出同样的结论，只会把日志刷满。
	ds := m.getStatus(updateKindDsh)
	ms := m.getStatus(updateKindMarket)
	as := m.getStatus(updateKindAdvanced)
	changed := func(k updateKind, latest string) bool {
		if m.lastCheckSig[k] == latest {
			return false
		}
		m.lastCheckSig[k] = latest
		return true
	}
	m.checkLogMu.Lock()
	if m.lastCheckSig == nil {
		m.lastCheckSig = map[updateKind]string{}
	}
	showDsh := changed(updateKindDsh, ds.LatestVersion)
	showMarket := changed(updateKindMarket, ms.LatestVersion)
	showAdvanced := changed(updateKindAdvanced, as.LatestVersion)
	m.checkLogMu.Unlock()

	if showDsh && ds.HasUpdate {
		logInfo("%s update available: %s -> %s",
			updateLogTag(updateKindDsh), ds.LocalVersion, ds.LatestVersion)
	}
	if showMarket && ms.HasUpdate {
		logInfo("%s update available: %s -> %s",
			updateLogTag(updateKindMarket), ms.LocalVersion, ms.LatestVersion)
	}
	if showAdvanced && as.HasUpdate {
		logInfo("%s update available: %s -> %s",
			updateLogTag(updateKindAdvanced), as.LocalVersion, as.LatestVersion)
	}
}

// RefreshMarketAfterDshStart 在 dsh 刚（重新）启动起来之后重查一次「本机装的受管插件
// 是哪一版」（插件市场与增强插件；函数名沿用历史命名）。
//
// 为什么需要它：插件版本只能靠 `dsh plugin --profile web list` 查，而这条命令在 dsh 起来
// 之前必然失败；失败的结论是「查不了」（UpdateStatus.Unavailable → 界面「—」）。因此每一个
// 「dsh 就绪」的时刻都要跟一次重查，否则「—」会一直挂到下一次小时级检测或用户手动检查：
//   - 控制台启动流水线收尾（dsh 起来 + 依赖装好，见 main.go）；
//   - 概览页的「启动 / 重启 dsh」（见 admin.go）；
//   - 切换 dsh 版本（见 server.go 的 Switch）。
//
// 异步执行：探测要起 1~3 秒的只读子进程，调用方（HTTP handler / 启动流水线）不该等它。
func (m *UpdateManager) RefreshMarketAfterDshStart() {
	go func() {
		// 稍等一小段：dsh 刚启动时还在装配 profile / 插件树，立刻查容易撞上「还没写好」
		// 的假失败（与插件变更后重启 dsh 那条路径等 marketReadySettle 同一个理由）。
		time.Sleep(marketReadySettle)
		m.refreshManagedPluginsLocal(true)
	}()
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

// --- 归档解压 / 打包 ---

// extractArchive 解压一个下载下来的备份包（当前只支持 .tar.gz）。
func extractArchive(src, dest string) error {
	return extractTarGz(src, dest)
}

// extractTarGz 解压 .tar.gz（.tgz 同理）到目标目录，用于 dsh 数据备份的还原
// （`dsh-data-*`）。
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
	return extractTar(gz, dest)
}

// extractTar 解压一个已解压的 tar 流到目标目录。保持 tar 内的相对路径不变
// （不剥离顶层目录）。解压器现在只有 gz 一条，路径越界与软链逃逸的防护也只有一份。
func extractTar(rd io.Reader, dest string) error {
	tr := tar.NewReader(rd)
	// destRoot 用于「解析软链后仍在目标内」的判定；destLex 用于便宜的字符串前缀检查。
	destLex := filepath.Clean(dest)
	destRoot, rerr := filepath.EvalSymlinks(destLex)
	if rerr != nil {
		destRoot = destLex
	}
	// outsideLinks 统计「目标在 dest 之外」的软链条目。它们照原样创建（见 TypeSymlink
	// 分支的说明），只在收尾时汇总一行，避免逐条刷日志。
	outsideLinks := 0
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
		target := filepath.Join(destLex, filepath.Clean(name))
		if !strings.HasPrefix(target, destLex+string(os.PathSeparator)) && target != destLex {
			return fmt.Errorf("解压路径越界: %s", target)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			// 父目录必须先过软链校验：归档里先前的 `link -> /etc` + `link/sub` 会在
			// dest 之外把目录建出来（zip-slip 变体）。
			if _, err := extractParentDir(target, destRoot); err != nil {
				return err
			}
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)&0777); err != nil {
				return err
			}
		case tar.TypeReg:
			// 同上：校验「解析软链后的父目录」仍在 dest 内，否则 OpenFile 会跟着
			// 归档里先前的软链把字节写到 dest 之外。
			if _, err := extractParentDir(target, destRoot); err != nil {
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
			if _, err := extractParentDir(target, destRoot); err != nil {
				return err
			}
			os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
			// 软链目标在 dest 之外时**不跳过**：真实归档里就存在这种链接 ——
			// dsh 数据备份中的 ~/.dsh/profiles/node_modules/@deepseek-ai/*（本机实测
			// 546 条软链里有 505 条是指向 server 目录的绝对链接），跳过会让恢复出来的
			// profile 少掉整层「profile → server 闭包」镜像。创建软链本身不会在 dest
			// 之外写字节，而「通过软链写内容」已被上面的父目录校验挡死，因此这里只统计。
			if !linkInside(destRoot, filepath.Dir(target), hdr.Linkname) {
				outsideLinks++
			}
		}
	}
	if outsideLinks > 0 {
		logInfo("[update] extracted %d symlinks pointing outside %s (kept as-is: symlink creation writes nothing outside, and writes through them are blocked)", outsideLinks, destLex)
	}
	return nil
}

// extractParentDir 在写入 target 之前校验：它的父目录（解析软链后）必须仍落在
// destRoot 之内，必要时把父目录建出来，返回父目录路径。
//
// 为什么必须解析而不是只比字符串：归档可以先用一个软链条目 `link -> /etc` 打下钉子，
// 再用 `link/evil` 让 MkdirAll / OpenFile 跟着软链写到 dest 之外 —— 字符串前缀检查在
// 这种情况下完全失效（target 字面上是 dest/link/evil）。
func extractParentDir(target, destRoot string) (string, error) {
	dir := filepath.Dir(target)
	// 从最深的「已存在祖先」开始解析：路径里尚未创建的部分本来就不可能被软链替换。
	probe := dir
	for {
		real, err := filepath.EvalSymlinks(probe)
		if err == nil {
			if !pathInside(real, destRoot) {
				return "", fmt.Errorf("解压路径越界（软链逃逸）: %s", target)
			}
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", fmt.Errorf("解压路径无法解析: %s", target)
		}
		probe = parent
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

// pathInside 报告 p 是否等于 root 或位于 root 之内（两者都应是已 Clean/解析的绝对路径）。
func pathInside(p, root string) bool {
	if p == root {
		return true
	}
	return strings.HasPrefix(p, root+string(os.PathSeparator))
}

// linkInside 报告软链 <dir>/<linkname> 解析后是否仍在 root 内（仅用于统计/说明，
// 见 extractTarGz 的 TypeSymlink 分支）。绝对 Linkname 按原样判断，相对 Linkname
// 相对 <dir> 解析。
func linkInside(root, dir, linkname string) bool {
	if linkname == "" {
		return false
	}
	resolved := filepath.Clean(linkname)
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Clean(filepath.Join(dir, linkname))
	}
	return pathInside(resolved, root)
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

// tgzProgress 是打包过程中的进度与取消回调（nil = 既不汇报进度也不可取消）。
//   - onBytes：每写完一块（tgzCopyChunk）汇报一次本次新增的**原始字节数**（未压缩），
//     调用方自己累加即得进度；
//   - onFile：每开始打包一个常规文件时调用一次；
//   - cancelled：返回 true 时立刻中止打包并返回 errBackupCancelled。
//
// 三个回调都是可选的，打包本身的正确性不依赖它们。
type tgzProgress struct {
	onBytes   func(n int64)
	onFile    func()
	cancelled func() bool
}

// errBackupCancelled 是「用户取消备份」的哨兵错误：调用方据此把**取消**与**失败**
// 分开 —— 取消不是异常，不记 ERROR 日志、不向用户报失败。产物文件由错误路径删除。
var errBackupCancelled = errors.New("备份已取消")

// backupPartSuffix 是打包过程中的临时后缀：备份先写成 <目标文件名>.part，成功后再
// 改名成最终文件名。这样「进程被杀 / 平台重启」留下的半成品不会与完成包同名 ——
// 备份列表只认 -<14 位时间戳>.tar.gz（见 isBackupFile），恢复又是「先删 ~/.dsh 再解压」，
// 拿半成包恢复等于直接丢数据。
const backupPartSuffix = ".part"

// tgzCopyChunk 是进度汇报与取消检查的块大小。取 255KB：既让 GB 级的大文件也能被及时
// 取消（每块一检查），又不至于把进度回调打成高频热点（每块一次汇报）。
const tgzCopyChunk = 255 * 1024

// tgzDirAs 将 srcDir 目录内容压缩，tar 中的条目以 rootName 作为顶层前缀。
// 例：tgzDirAs("/home/user/.dsh", "b.tar.gz", ".dsh") 生成 ".dsh/KEY"、".dsh/..." 等条目，
// 仅包含 .dsh 目录自身（不含 HOME 其它内容），解压到 /home/user 可还原完整的 ~/.dsh。
//
// 排除规则只针对**本次的备份产物自身**（destFile 恰好落在 srcDir 内时跳过，避免
// 一边写一边把自己读进去）。旧实现按「相对路径后缀 .tar.gz」排除，会把 srcDir 下
// 用户自己的所有 .tar.gz（例如 `~/.dsh/backups/x.tar.gz`）静默漏掉 —— 用户以为备份
// 完整，恢复时才发现少了数据。判断依据：dsh 数据备份的 destFile 来自
// UpdateManager.backupDir()（<数据目录>/backup），而 srcDir 是 $HOME/.dsh，
// 两者正常部署下互不包含，因此「排除产物自身」这条规则在正常路径上根本不触发，
// 它只是防御性的；真要防的也只有这一个文件。
func tgzDirAs(srcDir, destFile, rootName string) error {
	return tgzDirAsProgress(srcDir, destFile, rootName, nil)
}

// tgzDirAsProgress 是 tgzDirAs 的「带进度 + 可取消」版本，也是唯一的打包实现
// （tgzDirAs 只是 prog=nil 的包装，别另写一份打包逻辑）。
//
// 产物是**分两步落盘**的：先写 destFile+backupPartSuffix，成功后再改名成 destFile。
// 否则进程被杀/平台重启留下的半成品会与完成包同名同目录，而备份恢复是「先删 ~/.dsh
// 再解压」，拿半成包恢复等于直接丢数据（见 BackupDshData / RestoreDshData）。
func tgzDirAsProgress(srcDir, destFile, rootName string, prog *tgzProgress) error {
	partFile := destFile + backupPartSuffix
	out, err := os.Create(partFile)
	if err != nil {
		return err
	}
	defer out.Close()
	// 没走到「改名成功」的出口一律删掉半成品（失败、取消、进程内 panic 都算）。
	committed := false
	defer func() {
		if !committed {
			out.Close()
			os.Remove(partFile)
		}
	}()
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	base := filepath.Clean(srcDir)
	// destFile 可能是相对路径，而 walk 回调里的 p 基于 base（可能是绝对路径），
	// 两侧都换算成绝对路径再比较，避免漏排除而把半写状态的备份包读进去。
	// 产物自身（`.part` 与最终名）都要排除：正常部署下它们不在 srcDir 里，属防御性规则。
	destAbs, derr := filepath.Abs(destFile)
	if derr != nil {
		destAbs = filepath.Clean(destFile)
	}
	partAbs, perr := filepath.Abs(partFile)
	if perr != nil {
		partAbs = filepath.Clean(partFile)
	}
	baseAbs, berr := filepath.Abs(base)
	if berr != nil {
		baseAbs = base
	}
	root := strings.Trim(rootName, "/")
	err = filepath.Walk(base, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// 每个条目（目录/文件）都查一次取消：大目录里点取消能立刻停下，
		// 不必等到下一个大文件读完。
		if prog.cancelledNow() {
			return errBackupCancelled
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
		// 只排除本次备份产物自身（见函数注释），不要按扩展名整类排除。
		if abs := filepath.Join(baseAbs, rel); abs == destAbs || abs == partAbs {
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
		if prog != nil && prog.onFile != nil {
			prog.onFile()
		}
		cerr := tgzCopy(tw, f, prog)
		f.Close()
		return cerr
	})
	if err != nil {
		// 失败与取消都会走到这里：关掉两个 writer 后退出，半成品由上面的 defer 删掉
		// （取消时也要删 —— 半个 .tar.gz 留在备份列表里比没有更糟）。
		tw.Close()
		gz.Close()
		return err
	}
	if err := tw.Close(); err != nil {
		gz.Close()
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	// 写句柄都关掉之后再改名：改名成功才算是「一份可用备份」。
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Rename(partFile, destFile); err != nil {
		return err
	}
	committed = true
	return nil
}

// cancelledNow 报告是否已请求取消（prog 为空时恒为 false）。
func (p *tgzProgress) cancelledNow() bool {
	return p != nil && p.cancelled != nil && p.cancelled()
}

// tgzCopy 把 src 写进 dst，按块汇报进度并响应取消。用自写的循环而非 io.Copy，
// 正是为了在**单个大文件内部**也能检查取消（io.Copy 只在读返回后才有可能中断，
// 而在 gzip 写入时不返回错误就会一路把整个文件写完）。
func tgzCopy(dst io.Writer, src io.Reader, prog *tgzProgress) error {
	buf := make([]byte, tgzCopyChunk)
	for {
		if prog.cancelledNow() {
			return errBackupCancelled
		}
		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}
			if prog != nil && prog.onBytes != nil {
				prog.onBytes(int64(n))
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

// backupDir 返回备份产物的存放目录（统一数据目录下的 backup/，避免写入只读的
// target）。
func (m *UpdateManager) backupDir() string {
	dir := filepath.Join(m.dataDir(), "backup")
	os.MkdirAll(dir, 0755)
	return dir
}

// dataDir 返回统一的后端数据目录：优先用运行时解析结果；单测里构造的空
// RuntimeEnv（只为碰临时目录）退回环境变量解析（HARNESS_DATA_DIR / TRIM_PKGVAR）。
func (m *UpdateManager) dataDir() string {
	if m.renv != nil && m.renv.DataDir != "" {
		return m.renv.DataDir
	}
	return dataDirFromEnv()
}

// --- 状态推送 ---

// updateStatus 修改并广播某个 kind 的状态（线程安全）。
func (m *UpdateManager) updateStatus(k updateKind, f func(*UpdateStatus)) {
	m.mu.Lock()
	if st, ok := m.statuses[k]; ok {
		f(st)
	}
	m.mu.Unlock()
	m.notify()
}

func (m *UpdateManager) ensureNodePtyAfterBoot() bool {
	if m.renv == nil {
		return false
	}
	restartNeeded, err := ensureNodePty(m.renv, "")
	if err != nil {
		logWarn("[node-pty] setup failed: %v, dsh may not work", err)
		return false
	}
	if !restartNeeded {
		return false
	}
	logInfo("[node-pty] workspace changed by setup, restarting dsh")
	if err := dshStopFn(m); err != nil {
		logError("[node-pty] restart dsh (stop) failed: %v", err)
		return false
	}
	if err := startDshCapturedFn(m); err != nil {
		logError("[node-pty] restart dsh (start) failed: %v", err)
		return false
	}
	return true
}

func (m *UpdateManager) startDshCaptured() error {
	if err := m.dsh.Start(); err != nil {
		return err
	}
	// 异步等待捕获 token 并换取 cookie，不阻塞调用方主流程（最多等 15 秒）。
	go captureDshSession(m.dsh)
	return nil
}

// --- dsh 数据备份：列表 / 删除 / 恢复 ---

// backupTimestampRe 匹配备份文件名尾部的 -<YYYYMMDDHHMMSS>.tar.gz 时间戳后缀。
// 格式：<类型>-<版本号>-<时间戳>.tar.gz，如 dsh-data-0.2.1-20260903154421.tar.gz。
var backupTimestampRe = regexp.MustCompile(`-\d{14}\.tar\.gz$`)

// isBackupFile 判断文件名是否为指定类型的备份文件（以 prefix 开头，且以 -<14位时间戳>.tar.gz 结尾）。
func isBackupFile(name, prefix string) bool {
	return strings.HasPrefix(name, prefix) && backupTimestampRe.MatchString(name)
}

// sweepPartialBackups 删除 backupDir 下打包中途留下的半成品（<名称>.part）。
//
// 打包先写 .part、成功后改名（见 tgzDirAsProgress），因此 .part 一定是「没写完」的：
// 进程被杀/平台重启/dsh 数据备份被强杀都会留下它，而它既不会被备份列表列出（名字不含
// 合法的时间戳后缀），也不会被 30 天清理任务碰到。控制台启动时与每次开始新备份前各清一次。
func (m *UpdateManager) sweepPartialBackups() {
	dir := m.backupDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	removed := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), backupPartSuffix) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err == nil {
			removed++
			logInfo("[backup] removing partial backup file: %s", e.Name())
		}
	}
	if removed > 0 {
		logInfo("[backup] removed %d partial backup files", removed)
	}
}

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
		return uiErr("err_backup_bad_name", "非法的备份文件名: %s", "name", name)
	}
	// 正在写的那一份不能删：备份是服务端异步跑的，删掉正在写的文件后这次备份仍会报
	// 「成功」，盘上却查无此包（写的是已被 unlink 的 inode）。要中断它请走
	// POST /api/dsh/backup/cancel（它会中止打包并删掉半成品）。
	if st := dshBackup.status(); st.Running && st.Name == name {
		return uiErr("err_backup_in_flight", "这份备份正在写入中，请等它结束或先取消这次备份")
	}
	return os.Remove(target)
}

// --- dsh 数据备份：异步 + 进度 + 可取消 ---
//
// 备份在**服务端 goroutine** 里跑，与前端弹窗的生命周期无关：关掉弹窗（甚至刷新页面、
// 换一个浏览器标签）都不终止备份，重新打开弹窗时用 GetDshBackupStatus 的快照追平进度。
// 唯一的产出目录仍是 backupDir（见 tgzDirAs 的排除规则说明），唯一的打包实现是
// tgzDirAsProgress。

// DshBackupStatus 是一次 dsh 数据备份的状态快照（空闲时各字段为零值）。
//
// Bytes/TotalBytes/Files/TotalFiles 用于渲染进度：TotalBytes 是**预扫描**得到的原始
// 总字节数，0 表示未知（此时前端用不确定进度条，不伪造百分比）；Files 是已开始打包的
// 常规文件数。Size 只在结束时有效（压缩后的包大小）。
type DshBackupStatus struct {
	// Seq 是本次备份的序号（进程内自增）：前端用它去重「同一次备份的终态提示」——
	// 取消请求与轮询可能同时把同一份 done 快照送回去，没有它就会弹两次 toast。
	Seq        int64  `json:"seq"`
	Running    bool   `json:"running"`
	Cancelling bool   `json:"cancelling"` // 已请求取消，还在收尾
	Done       bool   `json:"done"`
	Ok         bool   `json:"ok"`
	Cancelled  bool   `json:"cancelled"`
	Error      string `json:"error,omitempty"`
	// ErrorRef 是 Error 的界面文案引用（code + 参数，见 uimsg.go）。
	ErrorRef   *uiMsg `json:"errorRef,omitempty"`
	Name       string `json:"name,omitempty"`
	Path       string `json:"path,omitempty"`
	Size       int64  `json:"size"`
	Bytes      int64  `json:"bytes"`
	TotalBytes int64  `json:"totalBytes"`
	Files      int    `json:"files"`
	TotalFiles int    `json:"totalFiles"`
}

// dshBackupTracker 跟踪当前这一次 dsh 数据备份（全局单例 + 互斥锁，与 dshRestore
// 同一套写法）。状态是**进程级**的：前端弹窗只是观察者，因此它的读写必须加锁。
type dshBackupTracker struct {
	mu         sync.Mutex
	st         DshBackupStatus
	seq        int64 // 已开始的备份次数（每次 begin 自增，写进 st.Seq）
	active     bool  // 本次进程里是否有一次备份正在进行
	wantCancel bool
}

var dshBackup dshBackupTracker

// status 返回当前状态快照。Running 由 active 派生，而不是由 Done 派生：开始前各字段
// 都是零值，用 !Done 会让「从没跑过」被误读成「正在运行」。
func (t *dshBackupTracker) status() DshBackupStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.st
	st.Running = t.active
	return st
}

// begin 重置状态并进入「进行中」。
//
// 总量（TotalBytes / TotalFiles）不在这里给：预扫描放在打包 goroutine 里做（接口要
// 立刻返回，而 ~/.dsh 的目录遍历可能上千个文件），扫完由 setTotals 补上；在此之前
// 前端按「总量未知」渲染不确定进度条。
func (t *dshBackupTracker) begin(name, path string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seq++
	t.st = DshBackupStatus{
		Seq:     t.seq,
		Running: true,
		Name:    name,
		Path:    path,
	}
	t.active = true
	t.wantCancel = false
}

// setTotals 补上预扫描得到的总量（0/0 = 总量未知）。
func (t *dshBackupTracker) setTotals(totalBytes int64, totalFiles int) {
	t.mu.Lock()
	t.st.TotalBytes = totalBytes
	t.st.TotalFiles = totalFiles
	t.mu.Unlock()
}

// addBytes 累加已打包的原始字节数（打包循环按块调用，非常高频）。
func (t *dshBackupTracker) addBytes(n int64) {
	t.mu.Lock()
	t.st.Bytes += n
	t.mu.Unlock()
}

// addFile 记一个已开始打包的常规文件。
func (t *dshBackupTracker) addFile() {
	t.mu.Lock()
	t.st.Files++
	t.mu.Unlock()
}

// finish 落定状态。size 只在成功时有意义（压缩后的包大小）。
func (t *dshBackupTracker) finish(ok, cancelled bool, err error, size int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.st.Running = false
	t.st.Done = true
	t.st.Ok = ok
	t.st.Cancelled = cancelled
	setErrFields(&t.st.Error, &t.st.ErrorRef, err)
	t.st.Size = size
	t.st.Cancelling = false
	t.active = false
	t.wantCancel = false
}

// requestCancel 请求取消当前备份，返回是否确有一次备份在跑（供日志区分）。
// 打包循环在下一块的边界上察觉并中止，因此这里只置标志、不做任何等待。
func (t *dshBackupTracker) requestCancel() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active {
		return false
	}
	t.wantCancel = true
	t.st.Cancelling = true
	return true
}

// cancelled 供打包循环轮询（高频路径，必须短小）。
func (t *dshBackupTracker) cancelled() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.wantCancel
}

// beginExclusiveDshDataOp 为「会删/改 ~/.dsh 内容」的短操作（插件页的重置插件）取得
// 与备份 / 恢复 / 更新同一把互斥锁。备份与恢复都拿 applying，因此这里也拿才是**双向**
// 互斥：备份在跑时重置插件会被拒（否则会打出一份「成功但缺 profiles」的包，或让那次
// 备份在 walk 中途失败），重置插件进行中时备份同样会被拒（见 BackupDshData）。
//
// 用 TryLock：拿不到就立刻拒绝请求，不阻塞请求线程、不产生任何删除与停机。
func (m *UpdateManager) beginExclusiveDshDataOp() (func(), error) {
	if !m.applying.TryLock() {
		return nil, busyf("err_data_op_busy", "正在执行其它更新/备份/恢复操作，请等它结束后再重试")
	}
	return m.applying.Unlock, nil
}

// GetDshBackupStatus 返回当前 dsh 数据备份的状态快照。
func (m *UpdateManager) GetDshBackupStatus() DshBackupStatus {
	return dshBackup.status()
}

// CancelDshBackup 取消正在进行的 dsh 数据备份（不完整的备份文件会被删除）。
// 没有备份在跑时是空操作（幂等），返回取消后的状态快照。
func (m *UpdateManager) CancelDshBackup() DshBackupStatus {
	if dshBackup.requestCancel() {
		logInfo("[backup] dsh data backup cancel requested")
	}
	return dshBackup.status()
}

// BackupDshData 启动一次 dsh 数据备份（异步）：
// 把当前 HOME 下的 ~/.dsh 打包为 dsh-data-<版本>-<时间戳>.tar.gz 放进统一备份目录。
//
// 立即返回：只做「主目录 / .dsh 存在性」与互斥检查，打包在 goroutine 里进行。
// 因此前端请求超时、弹窗关闭、页面刷新都不会影响这次备份；进度与结果经
// GetDshBackupStatus 读取，取消走 CancelDshBackup。
func (m *UpdateManager) BackupDshData() error {
	home := m.dsh.effectiveHome()
	if home == "" {
		return uiErr("err_backup_no_home", "无法获取当前主目录")
	}
	src := filepath.Join(home, ".dsh")
	if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
		return uiErr("err_backup_no_dsh_dir", ".dsh 目录不存在")
	}
	// 一次只允许一个备份：并发打包会让共享的进度状态互相覆盖，也没人需要两份同样的包。
	if !m.backupMu.TryLock() {
		return busyf("err_backup_running", "已有备份任务正在进行")
	}
	// 备份要读整个 ~/.dsh，而“恢复 dsh 数据 / 重置插件 / 安装更新”会删目录或替换产物。
	// 用与恢复同一把 applying 互斥（TryLock 不阻塞请求线程）：拿不到就拒绝，不产生副作用。
	// 两者都拿 applying，互斥才是**双向**的（恢复侧另有友好提示，见 RestoreDshData）。
	if !m.applying.TryLock() {
		m.backupMu.Unlock()
		return busyf("err_backup_busy", "正在执行其它更新/恢复操作，请等它结束后再备份")
	}
	dest := filepath.Join(m.backupDir(), fmt.Sprintf("dsh-data-%s-%s.tar.gz",
		m.dshDataBackupVersion(), time.Now().Format("20060102150405")))
	// 上一次打包被杀留下的半成品先清掉：它们占着磁盘，而且不该与新备份混在一起。
	m.sweepPartialBackups()
	dshBackup.begin(filepath.Base(dest), dest)
	logInfo("[backup] dsh data backup started: %s", filepath.Base(dest))

	go func() {
		defer m.applying.Unlock()
		defer m.backupMu.Unlock()
		// 预扫描一遍待打包的原始总量（只 stat，不读内容）用于百分比。刻意放在这里而不是
		// 接口里：接口只做校验后立刻返回（弹窗只是观察者，关掉它不影响备份），而这次
		// 全目录遍历在大 ~/.dsh 上要按秒计。扫描失败不阻塞备份，只是退化为「总量未知」。
		dshBackup.setTotals(scanDirSize(src, dest))
		prog := &tgzProgress{
			onBytes:   dshBackup.addBytes,
			onFile:    dshBackup.addFile,
			cancelled: dshBackup.cancelled,
		}
		err := tgzDirAsProgress(src, dest, ".dsh", prog)
		switch {
		case err == nil:
			var size int64
			if fi, serr := os.Stat(dest); serr == nil {
				size = fi.Size()
			}
			dshBackup.finish(true, false, nil, size)
			logInfo("[backup] dsh data backed up to %s (%d bytes)", dest, size)
		case errors.Is(err, errBackupCancelled):
			// tgzDirAsProgress 只写 .part、失败/取消时自己删掉；这里再清一次最终名与
			// .part，纯属防御（留着半个包比没有更糟）。
			os.Remove(dest)
			os.Remove(dest + backupPartSuffix)
			dshBackup.finish(false, true, nil, 0)
			logInfo("[backup] dsh data backup cancelled, partial file removed: %s", filepath.Base(dest))
		default:
			os.Remove(dest)
			os.Remove(dest + backupPartSuffix)
			dshBackup.finish(false, false, err, 0)
			logError("[backup] dsh data backup failed: %v", err)
		}
	}()
	return nil
}

// dshDataBackupVersion 返回备份文件名里的 dsh 版本号（= 当前选中版本的目录名），
// 没装版本时为 unknown。server 为 nil 只出现在单测里（那些测试不碰版本目录）。
func (m *UpdateManager) dshDataBackupVersion() string {
	if m.server == nil {
		return "unknown"
	}
	if v := m.server.selectedVersion(); v != "" {
		return v
	}
	return "unknown"
}

// scanDirSize 统计 srcDir 下待打包的原始字节数与常规文件数，供备份进度用。
// 排除规则与 tgzDirAsProgress 保持一致（跳过本次的产物文件 destFile），只 stat、不读内容，
// 因此代价是「一次目录遍历」。遍历失败时返回已统计到的部分 —— 进度条宁可不精确，
// 也不能因此拦住备份。
func scanDirSize(srcDir, destFile string) (int64, int) {
	var total int64
	var files int
	base := filepath.Clean(srcDir)
	destAbs, derr := filepath.Abs(destFile)
	if derr != nil {
		destAbs = filepath.Clean(destFile)
	}
	partAbs, perr := filepath.Abs(destFile + backupPartSuffix)
	if perr != nil {
		partAbs = filepath.Clean(destFile + backupPartSuffix)
	}
	_ = filepath.Walk(base, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // 单个条目读不到就跳过：不能让一处权限问题打断统计
		}
		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if abs, aerr := filepath.Abs(p); aerr == nil && (abs == destAbs || abs == partAbs) {
			return nil
		}
		total += info.Size()
		files++
		return nil
	})
	return total, files
}

// DshRestoreStatus 是 dsh 数据恢复状态。
type DshRestoreStatus struct {
	Running bool   `json:"running"`
	Done    bool   `json:"done"`
	Ok      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	// ErrorRef 是 Error 的界面文案引用（code + 参数，见 uimsg.go）。
	ErrorRef *uiMsg `json:"errorRef,omitempty"`
}

// dsh restore 状态跟踪
type dshRestoreTracker struct {
	mu     sync.Mutex
	done   bool
	ok     bool
	err    string
	errRef *uiMsg
}

var dshRestore dshRestoreTracker

func (t *dshRestoreTracker) status() DshRestoreStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	return DshRestoreStatus{Running: !t.done, Done: t.done, Ok: t.ok, Error: t.err, ErrorRef: t.errRef}
}

func (t *dshRestoreTracker) complete(ok bool, err error) {
	t.mu.Lock()
	t.done = true
	t.ok = ok
	setErrFields(&t.err, &t.errRef, err)
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
		return uiErr("err_backup_bad_path", "非法的备份路径: %s", "path", backupPath)
	}
	name := filepath.Base(backupPath)
	if !isBackupFile(name, "dsh-data-") {
		return uiErr("err_backup_bad_name", "非法的备份文件名: %s", "name", name)
	}
	if _, err := os.Stat(backupPath); err != nil {
		return uiErr("err_backup_missing", "备份文件不存在: %s", "detail", err.Error())
	}
	// 备份正在读 ~/.dsh：此时删掉它再解压备份，会得到「一半备份前、一半恢复后」的
	// 数据，而且那次备份还会「成功」。先给出明确提示（真正的互斥在下方的 applying：
	// BackupDshData 同样要拿它，因此这里即使被抢跑也拦得住）。
	if dshBackup.status().Running {
		return busyf("err_restore_busy_backup", "正在备份 dsh 数据，请先取消备份或等它结束后再恢复")
	}
	// 互斥与重入保护：恢复会 RemoveAll(~/.dsh) 再解压备份，必须与「下载/安装更新」、
	// 数据备份以及另一次恢复串行 —— 交错执行会在同一个 HOME 上并发删目录/解压。
	// 与备份一样用 TryLock：拿不到就拒绝，不阻塞请求线程。
	if !m.applying.TryLock() {
		return busyf("err_restore_busy", "正在执行其它更新/备份操作，请等它结束后再恢复 dsh 数据")
	}
	// 重置状态
	dshRestore.mu.Lock()
	dshRestore.done = false
	dshRestore.ok = false
	setErrFields(&dshRestore.err, &dshRestore.errRef, nil)
	dshRestore.mu.Unlock()

	go func() {
		defer m.applying.Unlock()
		err := m.doRestoreDshData(backupPath)
		if err != nil {
			logError("[restore] dsh data restore failed: %v", err)
			dshRestore.complete(false, err)
		} else {
			dshRestore.complete(true, nil)
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
	logInfo("[restore] restoring dsh data from backup %s", backupPath)

	// 这条路径会删掉整个 ~/.dsh 并重建，先确认没有插件操作在跑：正在装的插件会被
	// 连同 profile 一起删掉，用户会看到一次莫名其妙的失败。被拒绝时不产生任何破坏。
	// （市场不回答时视为不忙，所以「dsh 坏了要恢复数据」不会被挡。）
	if err := m.replaceBusyGuard("恢复 dsh 数据"); err != nil {
		return err
	}

	// 1. 停止 dsh 服务
	logInfo("[restore] stopping dsh service")
	if err := m.dsh.Stop(); err != nil {
		logError("[restore] failed to stop dsh: %v", err)
	}

	// 2. 删除当前 ~/.dsh 目录
	logInfo("[restore] removing current ~/.dsh at %s", dshDir)
	if err := os.RemoveAll(dshDir); err != nil {
		return uiErr("err_restore_remove_failed", "删除 ~/.dsh 目录失败: %s", "detail", err.Error())
	}

	// 3. 解压备份到 HOME（tar 中顶层为 .dsh/，解压后在 HOME 下还原 ~/.dsh）。
	//    数据备份恒为 .tar.gz（tgzDirAs），故直接用 gz 解压器。
	logInfo("[restore] extracting backup to %s", home)
	if err := extractTarGz(backupPath, home); err != nil {
		return uiErr("err_restore_extract_failed", "解压备份失败: %s", "detail", err.Error())
	}

	// 4. 启动 dsh 服务（并异步捕获新 token 换取会话 cookie，供反代转发）
	logInfo("[restore] starting dsh service")
	if err := m.startDshCaptured(); err != nil {
		return uiErr("err_dsh_start_failed", "启动失败: %s", "detail", err.Error())
	}

	logInfo("[restore] dsh data restore finished")
	return nil
}

// BackupDir 返回统一备份目录路径。
func (m *UpdateManager) BackupDir() string {
	return m.backupDir()
}

// startDailyCleanup 启动每天一次的备份清理任务。
// 扫描 backupDir 中老版本更新留下的 harness-/server-/market- 备份包，超过 30 天的
// 自动删除；dsh-data-<版本>-<时间戳>.tar.gz 不在自动清理范围内。
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
//
// 控制台不再产生这些更新包备份（自我更新与发布资产下载已整体移除），这里只为
// 「老版本安装留下的包」兜底，避免它们永久占着磁盘。
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
		// 自动清理 harness / server / 市场三种**老版本残留**的备份包；用户主动的
		// dsh 数据备份（dsh-data-*）不在自动清理范围内 —— 那是用户自己要留的数据，
		// 由用户在目录页里手动删。
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
				logInfo("[cleanup] removed expired backup: %s", name)
			}
		}
	}
	if removed > 0 {
		logInfo("[cleanup] removed %d expired backups", removed)
	}
}
