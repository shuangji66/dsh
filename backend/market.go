package main

// market.go —— 受管插件（插件市场 dshmarket / 增强插件 dsh-fnos-advanced）的检测、
// 安装、更新与卸载。
//
// **形态变了**：过去 dshmarket 是被 CI 写进 dsh server 包 dependencies 的「内置
// bundle」（见 .github/workflows/server-build.yaml），控制台能做的是「就地把 server
// 目录里那份 dshmarket 换成 npm 上的最新版」。现在 dsh 由控制台从镜像源
// `npm install` 官方包（见 server.go），**里面不含任何预置插件市场**，于是市场退化
// 成一个再普通不过的 profile 插件，四种操作全部经 dsh 自己的插件命令完成：
//
//	检测：`dsh plugin --profile web list` 的输出里有没有 dshmarket
//	安装：`dsh plugin --profile web add dshmarket@<最新版本号> --registry=<镜像源>`
//	更新：同安装（再 add 一次最新版本号）
//	卸载：`dsh plugin --profile web remove dshmarket`
//
// 增强插件 dsh-fnos-advanced（飞牛目录选择器）是**同一形态的第二个受管插件**：它同样
// 发布在 npm、同样由 `dsh plugin add|remove` 安装卸载，因此这里用 managedPlugin 把
// 「包名 / 状态槽位 / 更新日志来源」参数化，两套动作共用同一份实现与同一套镜像源选择。
//
// 镜像源与 dsh 版本安装完全一致（见 server.go 的 selectedNpmMirror）：**只用设置页
// 选中的那一个源**（默认阿里云，可选腾讯 / 华为 / 官方），不重试同一个源，失败就报错
// 并提示用户去设置页换源。
//
// 几条必须守住的约束：
//  1. 【顺序：先跑插件命令，再重启 dsh】安装/卸载都是 pnpm 在 profile 目录里跑，
//     会持有 profile 写锁（`profiles/web/package.json.lock`）。**绝不能在它跑的时候
//     停 dsh** —— 停 dsh 会连带终止持有锁的那个进程组，留下陈旧锁，之后所有插件操作
//     都会白等 120 秒再失败（见 AGENTS 的忙守卫一节）。因此：命令跑完 → 过忙守卫 →
//     重启 dsh 让新 bundle 生效。
//  2. 【没有备份/回滚】安装失败就是失败，错误直接进弹窗；不生成备份包，也没有回滚入口。
//  3. 【不自己下载 tarball】也不做完整性校验：pnpm 会按 registry 元数据里的 integrity
//     校验它下载的字节，控制台再下一份只会重复劳动。
//  4. 【控制台要能处理「压根没装」】市场面板自带的自更新入口只适用于「已经装好」的
//     情况；「未安装 → 装上最新版」这条路径只有控制台能提供（增强插件也一样）。

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

const (
	// marketPackageName 是插件市场的 npm 包名（与 profile bundles 里的名字一致）。
	marketPackageName = "dshmarket"
	// advancedPackageName 是增强插件（飞牛目录选择器）的 npm 包名。
	advancedPackageName = "dsh-fnos-advanced"
	// marketProfile 是控制台管理的 dsh profile 名（与 `dsh web` 用的那个一致）。
	marketProfile = "web"
	// marketCmdTimeout 是单次 `dsh plugin …` 命令的上限：pnpm 要下载并构建依赖，
	// 给足余量（超时只终止这一次尝试，不影响已装的插件）。
	marketCmdTimeout = 10 * time.Minute
	// marketReadySettle 是端口开放后的确认时间：dsh 先监听、再装配插件树，坏 bundle
	// 可能「先开放端口，再装配失败退出」。等一小段再复核，避免把「起来又死」当成成功。
	marketReadySettle = 2 * time.Second
	// marketBackupPrefix 是历史遗留的市场备份文件名前缀。
	//
	// 新版本不再生成市场备份（没有回滚入口），这里只为让每日清理仍能回收老版本
	// 留下的 `market-<版本>-<时间戳>.tar.gz`。
	marketBackupPrefix = "market-"
)

// managedPlugin 描述一个由控制台管理的 profile 插件（npm 包）。
//
// 两个受管插件（市场 / 增强插件）的形态完全一致，差异只有：状态写到哪个 kind、
// npm 包名是什么、更新日志取自哪个仓库与 tag。把这三项参数化后，检测/安装/更新/
// 卸载都只有一份实现 —— 别再为某个插件复制一份命令执行与镜像源选择逻辑。
type managedPlugin struct {
	kind      updateKind
	pkg       string
	notesRepo releaseRepo         // 更新日志来源仓库
	notesTag  func(string) string // 版本号 → release tag
}

var (
	marketPlugin = managedPlugin{
		kind:      updateKindMarket,
		pkg:       marketPackageName,
		notesRepo: marketReleaseRepo,
		notesTag:  marketReleaseTag,
	}
	advancedPlugin = managedPlugin{
		kind:      updateKindAdvanced,
		pkg:       advancedPackageName,
		notesRepo: advancedReleaseRepo,
		notesTag:  advancedReleaseTag,
	}
	// managedPlugins 是所有受管插件（刷新、检测与后台任务按这个顺序遍历）。
	managedPlugins = []managedPlugin{marketPlugin, advancedPlugin}
)

// --- 检测：`dsh plugin --profile web list` ---

// pluginList 是一次 `dsh plugin --profile web list` 的结果：已安装的包 → 版本号。
//
// 两个受管插件共用同一条命令输出 —— 每次检测要起一个 1~3 秒的子进程，而「dsh 刚就绪」
// 这类时刻本来就要同时刷新两个插件，各跑一遍只是白等一倍时间（见
// refreshManagedPluginsLocal / refreshManagedPluginStatuses）。
type pluginList struct {
	versions map[string]string
	raw      string
	err      error
}

// readPluginList 执行一次 `dsh plugin --profile web list`（不联网）。
//
// 只读检测，但仍走控制台侧插件命令的统一入口：它执行前会清「持有者已死」的陈旧
// profile 写锁（list 正是最容易被陈旧锁拖住 120 秒的那条），并把「插件命令进行中」
// 登记给忙守卫。超时用 pluginCmdTimeout（不是 marketCmdTimeout）—— 这条是只读命令，
// 不该允许它挂 10 分钟，否则忙守卫会被一次卡住的检测长期占住。
func (m *UpdateManager) readPluginList() pluginList {
	if m.dsh == nil {
		return pluginList{err: uiErr("err_dsh_manager_unavailable", "dsh 管理器不可用")}
	}
	out, err := m.dsh.runPluginCmd("list")
	if err != nil {
		return pluginList{raw: out, err: err}
	}
	versions := make(map[string]string)
	for _, pl := range parsePluginList(out) {
		versions[pl.Name] = pl.Version
	}
	return pluginList{versions: versions, raw: out}
}

// snapshotFromPluginList 把一次列表输出换算成某个插件的本地检测结果。
//
// 错误按**每个插件各自的包名**包装（界面诊断里要知道是哪一个插件查不了）；「查不了」
// 与「查到了、没装」必须分开，见 applyPluginLocalResult。
func snapshotFromPluginList(p managedPlugin, list pluginList) marketLocalSnapshot {
	if list.err != nil {
		err := list.err
		// 已经是面向用户的错误（dsh 管理器不可用等）：原样透出。
		if _, ok := uiMsgOf(err); ok {
			return marketLocalSnapshot{Diag: err.Error(), Err: err}
		}
		// 「本机没有选中的 dsh 版本」是最常见的失败原因，而且它的内部错误文本是英文的
		// （会被日志打印，见 AGENTS 规则 7）—— 这里换成中文话术，用户才看得懂该去做什么。
		// 这条错误只进界面（弹窗诊断），不进日志。
		if errors.Is(err, errNoDshVersion) {
			err = uiErr("err_no_dsh_version", "未安装 dsh 服务：请先在控制台「概览」页的版本列表里下载一个版本，再点「切换」")
		} else {
			// 命令整体失败（dsh 未安装 / pnpm 缺失 / profile 目录不存在等）：
			// 把原因带上，前端在弹窗里能直接看到「为什么检测不到」。
			msg := strings.TrimSpace(list.raw)
			if msg == "" {
				msg = err.Error()
			}
			err = uiErr("err_plugin_detect_failed",
				"检测插件 %s 失败（dsh plugin --profile %s list）: %s",
				"pkg", p.pkg, "profile", marketProfile, "detail", msg)
		}
		return marketLocalSnapshot{Diag: err.Error(), Err: err}
	}
	if v, ok := list.versions[p.pkg]; ok {
		return marketLocalSnapshot{Version: v}
	}
	return marketLocalSnapshot{Diag: fmt.Sprintf("`dsh plugin --profile %s list` 未列出 %s", marketProfile, p.pkg)}
}

// pluginInstalled 判断某个受管插件装没装、装的哪一版（单插件入口，保留给测试与调用方）。
// 返回 ok=false 表示输出里没有该包（= 未安装）。刷新路径会共用一次列表输出，见
// readPluginList。
func (m *UpdateManager) pluginInstalled(p managedPlugin) (version string, ok bool, err error) {
	snap := snapshotFromPluginList(p, m.readPluginList())
	if snap.Err != nil {
		return "", false, snap.Err
	}
	return snap.Version, snap.Version != "", nil
}

// marketInstalled 是插件市场的检测入口（保留给既有测试与调用方）。
func (m *UpdateManager) marketInstalled() (string, bool, error) {
	return m.pluginInstalled(marketPlugin)
}

// marketLocalSnapshot 是「本地检测」的结果（不联网）。名字沿用历史命名，现在两个受管
// 插件共用这一种快照。
//
// Err 非空表示**查不了**（dsh 未就绪 / 没选中版本 / 命令报错），与「查到了，没装」
// （Err == nil 且 Version == ""）是两件事 —— 界面上前者显示「—」、后者显示「未安装」，
// 见 applyPluginLocalResult。
type marketLocalSnapshot struct {
	Version string
	Diag    string // 检测失败/未安装的原因（诊断用，进 MarketDir 字段）
	Err     error
}

// detectPluginLocal 取「本机装的是哪一版」。
//
// force=false 时**优先吃缓存**（按 kind 分开缓存）：这次检测要起一个
// `dsh plugin --profile web list` 子进程（1~3 秒），而它被调用的场合比「插件真的变了」
// 多得多（弹窗打开、诊断接口、状态重算），每次都跑一遍既慢又容易和 profile 写锁打架。
//
// 重跑的入口（force=true）：插件安装/卸载完成（结果变了，必须重查）、后台自动检测、
// 手动「检查更新」，以及「dsh 刚就绪」的时刻（见 RefreshMarketAfterDshStart）。
func (m *UpdateManager) detectPluginLocal(p managedPlugin, force bool) marketLocalSnapshot {
	if !force {
		m.marketLocalMu.Lock()
		if c := m.pluginLocalCache[p.kind]; c != nil {
			snap := *c
			m.marketLocalMu.Unlock()
			return snap
		}
		m.marketLocalMu.Unlock()
	}
	snap := m.probePluginLocal(p)
	m.storePluginSnapshot(p, snap)
	return snap
}

// storePluginSnapshot 把一次本地检测写进按 kind 分开的缓存。
//
// 「查不了」的结果不进缓存：缓存只装**确定的结论**（装了哪一版 / 没装）。把失败
// 缓存住，界面上的「—」就会一直挂到下一次触发（最坏一小时）；而重新探测很便宜 ——
// 没选中版本时立刻返回，否则也只是一个 1~3 秒的只读子进程。
func (m *UpdateManager) storePluginSnapshot(p managedPlugin, snap marketLocalSnapshot) {
	m.marketLocalMu.Lock()
	defer m.marketLocalMu.Unlock()
	if snap.Err != nil {
		delete(m.pluginLocalCache, p.kind)
		return
	}
	if m.pluginLocalCache == nil {
		m.pluginLocalCache = make(map[updateKind]*marketLocalSnapshot)
	}
	m.pluginLocalCache[p.kind] = &snap
}

// detectMarketLocal 是插件市场的本地检测入口（保留给既有测试与调用方）。
func (m *UpdateManager) detectMarketLocal(force bool) marketLocalSnapshot {
	return m.detectPluginLocal(marketPlugin, force)
}

// probePluginLocal 真正执行一次本地检测（起子进程，不联网）。
func (m *UpdateManager) probePluginLocal(p managedPlugin) marketLocalSnapshot {
	return snapshotFromPluginList(p, m.readPluginList())
}

// applyPluginLocalResult 把一次本地检测结果写进受管插件的状态。
//
// 两条铁律（错了界面就会说谎）：
//   - **查不了 ≠ 没装**：snap.Err != nil（dsh 未就绪、没选中版本、命令报错）时置
//     Unavailable，前端据此显示「—」；只有**确定**查到了（Err == nil）才写 LocalVersion
//     —— 否则「查不到」会退化成「未安装」，正是 dsh 还没起来时最容易看到的错话。
//   - **查不了时保留上一次已知的版本号**：已经知道装的是 1.66.11，就不该因为一次探测失败
//     把它清成空 —— 那比留着旧值更像谎话（插件不会因为 dsh 重启就消失）。
//
// HasUpdate（红点）也在这里统一算：只有「已知本地版本」且「最新版更高」时才亮。
func applyPluginLocalResult(st *UpdateStatus, snap marketLocalSnapshot) {
	st.Unavailable = snap.Err != nil
	if snap.Err == nil {
		st.LocalVersion = snap.Version
	}
	// 诊断文本照写（含清空）：查不到是「为什么查不到」，查到了就该把上一次的原因抹掉，
	// 否则弹窗里会挂着一条已经过时的解释。
	st.MarketDir = snap.Diag
	st.HasUpdate = st.LocalVersion != "" && st.LatestVersion != "" &&
		compareVersion(st.LatestVersion, st.LocalVersion) > 0
}

// applyMarketLocalResult 是插件市场的写入口（保留给既有测试）。
func applyMarketLocalResult(st *UpdateStatus, snap marketLocalSnapshot) {
	applyPluginLocalResult(st, snap)
}

// refreshPluginLocal 刷新本地检测并把「当前装的是哪一版」写进状态。
// force=false 时吃缓存（启动时先让界面有版本号可用，不额外起子进程）。
// 保留 LatestVersion 的既有值（HasUpdate 由 applyPluginLocalResult 统一算）。
func (m *UpdateManager) refreshPluginLocal(p managedPlugin, force bool) marketLocalSnapshot {
	snap := m.detectPluginLocal(p, force)
	m.applyPluginSnapshot(p, snap)
	return snap
}

// applyPluginSnapshot 把一份本地检测结果写进该插件的状态槽位。
func (m *UpdateManager) applyPluginSnapshot(p managedPlugin, snap marketLocalSnapshot) {
	m.updateStatus(p.kind, func(st *UpdateStatus) {
		st.Kind = p.kind
		applyPluginLocalResult(st, snap)
	})
}

// refreshMarketLocal 是插件市场的本地刷新入口（保留给既有测试）。
func (m *UpdateManager) refreshMarketLocal(force bool) marketLocalSnapshot {
	return m.refreshPluginLocal(marketPlugin, force)
}

// refreshManagedPluginsLocal 刷新所有受管插件的本地版本（dsh 刚就绪时用）。
//
// 需要真查时两个插件**共用一次** `dsh plugin list`：这条命令 1~3 秒，而 dsh 每次就绪
// 都要刷新两个插件，各跑一遍等于让用户多等一个子进程。非强制时优先吃各自缓存；两个都
// 命中就完全不起子进程。
func (m *UpdateManager) refreshManagedPluginsLocal(force bool) {
	if !force {
		m.marketLocalMu.Lock()
		cached := make([]marketLocalSnapshot, len(managedPlugins))
		complete := true
		for i, p := range managedPlugins {
			c := m.pluginLocalCache[p.kind]
			if c == nil {
				complete = false
				break
			}
			cached[i] = *c
		}
		m.marketLocalMu.Unlock()
		if complete {
			for i, p := range managedPlugins {
				m.applyPluginSnapshot(p, cached[i])
			}
			return
		}
	}
	list := m.readPluginList()
	for _, p := range managedPlugins {
		// 非强制刷新时，已有缓存结论的插件不该因为这一次 list 失败被降级成「查不了」
		// （与 detectPluginLocal 的「缓存优先」语义一致：查不到就沿用上一次的确定结论）。
		if !force && list.err != nil {
			m.marketLocalMu.Lock()
			c := m.pluginLocalCache[p.kind]
			var cached marketLocalSnapshot
			if c != nil {
				cached = *c
			}
			m.marketLocalMu.Unlock()
			if c != nil {
				m.applyPluginSnapshot(p, cached)
				continue
			}
		}
		snap := snapshotFromPluginList(p, list)
		m.storePluginSnapshot(p, snap)
		m.applyPluginSnapshot(p, snap)
	}
}

// --- 版本检测：npm 镜像源 ---

// pluginLatest 用设置页选中的那一个镜像源取元数据，返回**版本号最高**的那一版
// （`newestVersion`）与命中源的日志标识（英文标识；界面文案要用 npmMirror.Name，
// 别在这里返回中文显示名 —— 它会被调用方写进日志）。
//
// 刻意不看 `dist-tags.latest`：装的是「实际发布的最新版本号」那一版，而不是发布者
// 手动指到哪一版的标签（标签可能滞后、可能指向稳定线而版本集合里已有更新的预览版）。
// 安装时同一条链路的 `add <pkg>@<该版本>` 用的是这里返回的精确版本号。
func (m *UpdateManager) pluginLatest(p managedPlugin) (string, string, error) {
	mirror := selectedNpmMirror()
	versions, _, err := fetchMirrorPackument(mirrorClient(), mirror.URL, p.pkg)
	if err != nil {
		// 这条会顺着安装/更新链路进日志（runPluginOp 的 logError），必须英文；界面文案由
		// pluginLatestErr 包装成 code + 中文兜底。
		return "", "", fmt.Errorf("fetch %s latest version failed (registry %s): %v",
			p.pkg, mirror.mirrorLogName(), err)
	}
	v := newestVersion(versions)
	if v == "" {
		return "", "", fmt.Errorf("fetch %s latest version from registry %s failed: no usable version in metadata",
			p.pkg, mirror.mirrorLogName())
	}
	return v, mirror.mirrorLogName(), nil
}

// pluginLatestErr 把英文的底层检测错误包成面向用户的文案（code + 中文兜底）。
func pluginLatestErr(p managedPlugin, err error) error {
	return uiErr("err_plugin_latest_failed",
		"获取 %s 最新版本失败（镜像源：%s）: %s",
		"pkg", p.pkg, "mirror", selectedNpmMirror().Name, "detail", err.Error())
}

// marketLatest 是插件市场的最新版检测入口（保留给既有测试与调用方）。
func (m *UpdateManager) marketLatest() (string, string, error) {
	return m.pluginLatest(marketPlugin)
}

// refreshPluginStatus 做一次完整的受管插件检测：本地检测 + 镜像源最新版。
// 任何一步失败都只写进该插件自己的 Error，不影响其它更新链路。
func (m *UpdateManager) refreshPluginStatus(p managedPlugin) {
	// 检测跑一趟本来就慢（子进程 + 镜像源），这里强制重查本地版本，保证结论是当下的。
	m.refreshPluginStatusWith(p, m.detectPluginLocal(p, true))
}

// refreshPluginStatusWith 用一份已算好的本地快照做完整检测（本地 + 镜像源最新版）。
// 两个插件同时刷新时共用一次 `dsh plugin list`（见 refreshManagedPluginStatuses）。
func (m *UpdateManager) refreshPluginStatusWith(p managedPlugin, snap marketLocalSnapshot) {
	now := time.Now()
	latest, _, err := m.pluginLatest(p)
	// 更新日志：与 harness 同一套做法 —— 只取**要装的那一版**（最新版）的 release 正文，
	// 按 tag 单次请求（见 managedPlugin.notesTag），不做整仓索引。
	// 只在「确实有更新」时才取：没更新时弹窗不显示日志块，取回来也没人看，白打一次 GitHub。
	// 必须在这里取 —— updateStatus 的回调在 m.mu 里执行，不能在里面发网络请求。
	notes := ""
	if err == nil && latest != "" && snap.Version != "" && compareVersion(latest, snap.Version) > 0 {
		notes = fetchReleaseNotes(m.httpClientForUpdate(), p.notesRepo, p.notesTag(latest))
	}
	m.updateStatus(p.kind, func(st *UpdateStatus) {
		st.Kind = p.kind
		st.CheckedAt = now
		// 本地那一半走同一个入口：查不了 → Unavailable（界面「—」）并保留旧版本号。
		applyPluginLocalResult(st, snap)
		st.ReleaseNotes = notes
		if err != nil {
			st.LatestVersion = ""
			// 镜像源没答上时无法比较版本，红点必须熄掉（本地状态仍由上面维护）。
			st.HasUpdate = false
			setErrFields(&st.Error, &st.ErrorRef, pluginLatestErr(p, err))
			return
		}
		setErrFields(&st.Error, &st.ErrorRef, nil)
		st.LatestVersion = latest
		st.HasUpdate = st.LocalVersion != "" && compareVersion(latest, st.LocalVersion) > 0
	})
}

// refreshMarketStatus 是插件市场的完整检测入口（保留给既有测试与调用方）。
func (m *UpdateManager) refreshMarketStatus() {
	m.refreshPluginStatus(marketPlugin)
}

// refreshManagedPluginStatuses 刷新所有受管插件的状态（含镜像源最新版）。
//
// 本地检测共用一次 `dsh plugin list`（两插件各跑一遍等于白等一个 1~3 秒的子进程）。
func (m *UpdateManager) refreshManagedPluginStatuses() {
	list := m.readPluginList()
	for _, p := range managedPlugins {
		snap := snapshotFromPluginList(p, list)
		m.storePluginSnapshot(p, snap)
		m.refreshPluginStatusWith(p, snap)
	}
}

// pluginInfo 返回受管插件的诊断快照：只读、不联网、不起子进程
// （本地检测吃缓存；没有缓存时才会真的查一次）。
//
// available=false 表示**没查到**（dsh 未就绪 / 命令报错），此时 installed=false 只表示
// 「不知道」，不是「没装」—— 调用方别把它当否命题用。
func (m *UpdateManager) pluginInfo(p managedPlugin) map[string]interface{} {
	snap := m.detectPluginLocal(p, false)
	st := m.getStatus(p.kind)
	available := snap.Err == nil
	updatable := available && snap.Version != "" && st.LatestVersion != "" &&
		compareVersion(st.LatestVersion, snap.Version) > 0
	return map[string]interface{}{
		"ok":        true,
		"pkg":       p.pkg,
		"available": available,
		"installed": available && snap.Version != "",
		"version":   snap.Version,
		"latest":    st.LatestVersion,
		"updatable": updatable,
		"reason":    snap.Diag,
		"error":     st.Error,
	}
}

// marketInfo 返回供 /api/market/info 使用的诊断快照。
func (m *UpdateManager) marketInfo() map[string]interface{} {
	return m.pluginInfo(marketPlugin)
}

// --- 安装 / 更新 / 卸载 ---

// marketCommand 组装 `dsh plugin --profile web <args…>`（不含 registry 参数）。
func marketCommand(args []string) []string {
	return append([]string{"plugin", "--profile", marketProfile}, args...)
}

// runPluginCmdFor 用设置页选中的那一个镜像源执行一次受管插件的安装/更新命令
// （`add`，它会访问 registry）。
//
// registry 通过 `--registry=<镜像源>` 传；命令失败（元数据取不到、tarball 下载失败、
// 构建失败）就**直接报错并提示换源**，不在内部自动换下一个源 —— 用户选的是哪个源，就
// 用哪个源，失败原因也必须对得上。
//
// 注意：只有「会访问 registry 的命令」能走这里 —— pnpm 的 `add` 支持 `--registry`，
// 而 `remove` / `list` 这类命令**不认这个选项**（会直接 `Unknown option: 'registry'`），
// 它们走 runPluginCmdOnceFor。
func (m *UpdateManager) runPluginCmdFor(p managedPlugin, args []string) error {
	mirror := selectedNpmMirror()
	full := append(marketCommand(args), "--registry="+mirror.URL)
	m.updateStatus(p.kind, func(st *UpdateStatus) {
		// 界面进度文案用中文显示名；日志用下面的 mirrorLogName()（英文标识）。
		setMsgFields(&st.Message, &st.MessageRef, "msg_plugin_from_mirror",
			"正在从 %s 处理 %s", "mirror", mirror.Name, "pkg", p.pkg)
	})
	logInfo("%s running dsh %s (via %s)", updateLogTag(p.kind), strings.Join(full, " "), mirror.mirrorLogName())
	out, err := m.dsh.RunPluginCommand(marketCmdTimeout, full)
	if err != nil {
		logWarn("%s dsh plugin command via %s failed: %v", updateLogTag(p.kind), mirror.mirrorLogName(), err)
		return uiErr("err_plugin_op_failed",
			"插件 %s 操作失败（镜像源：%s）: %s；可在设置页切换 npm 镜像源后重试",
			"pkg", p.pkg, "mirror", mirror.Name, "detail", tailLines(out, 3))
	}
	return nil
}

// runMarketPluginCmd 是插件市场的安装/更新执行入口（保留给既有测试与调用方）。
func (m *UpdateManager) runMarketPluginCmd(args []string) error {
	return m.runPluginCmdFor(marketPlugin, args)
}

// runPluginCmdOnceFor 执行一次**与 registry 无关**的受管插件命令（`remove` 等）：
// 不带 `--registry`、也不涉及镜像源选择（卸载既不下载也不解析依赖，没有任何「换个源再试」
// 的意义），失败就把 dsh/pnpm 的原话报出去。
func (m *UpdateManager) runPluginCmdOnceFor(p managedPlugin, args []string) error {
	full := marketCommand(args)
	logInfo("%s running dsh %s", updateLogTag(p.kind), strings.Join(full, " "))
	out, err := m.dsh.RunPluginCommand(marketCmdTimeout, full)
	if err != nil {
		return uiErr("err_plugin_cmd_failed", "dsh %s 失败: %s", "cmd", strings.Join(full, " "), "detail", tailLines(out, 3))
	}
	return nil
}

// runMarketPluginCmdOnce 是插件市场的单次执行入口（保留给既有测试与调用方）。
func (m *UpdateManager) runMarketPluginCmdOnce(args []string) error {
	return m.runPluginCmdOnceFor(marketPlugin, args)
}

// tailLines 取输出结尾若干行非空内容（pnpm 的诊断通常在结尾），用于错误信息。
func tailLines(out string, n int) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var kept []string
	for i := len(lines) - 1; i >= 0 && len(kept) < n; i-- {
		if s := strings.TrimSpace(lines[i]); s != "" {
			kept = append([]string{s}, kept...)
		}
	}
	if len(kept) == 0 {
		// 这段会作为 detail 参数进日志（见 pluginOpLogText），保持英文。
		return "(no output)"
	}
	return strings.Join(kept, " | ")
}

// pluginCmdFn 执行一次「使用选中镜像源」的 `dsh plugin …`（变量形式便于
// 单测注入，不必真的起 dsh / pnpm）。
var pluginCmdFn = func(m *UpdateManager, p managedPlugin, args []string) error {
	return m.runPluginCmdFor(p, args)
}

// resolvePluginVersion 取「实际最新的一版」的精确版本号（镜像源，非 dist-tags）。
//
// 安装前现取一次（而不是复用状态里那份可能过期的 LatestVersion）：状态是最近一次检测的
// 快照，用户可能在检测之后很久才点安装。
func (m *UpdateManager) resolvePluginVersion(p managedPlugin) (string, error) {
	version, mirror, err := m.pluginLatest(p)
	if err != nil {
		return "", pluginLatestErr(p, err)
	}
	logInfo("%s newest %s version is %s (via %s)", updateLogTag(p.kind), p.pkg, version, mirror)
	return version, nil
}

// InstallMarket 安装插件市场的最新版本号那一版。异步执行，进度经市场状态推送。
func (m *UpdateManager) InstallMarket() error {
	return m.installPlugin(marketPlugin)
}

// UpdateMarket 把插件市场更新到最新版本号那一版（语义上就是再装一次那个精确版本）。
func (m *UpdateManager) UpdateMarket() error {
	return m.installPlugin(marketPlugin)
}

// InstallAdvanced 安装增强插件的最新版本号那一版（异步，进度经 SSE 推送）。
func (m *UpdateManager) InstallAdvanced() error {
	return m.installPlugin(advancedPlugin)
}

// UpdateAdvanced 把增强插件更新到最新版本号那一版。
func (m *UpdateManager) UpdateAdvanced() error {
	return m.installPlugin(advancedPlugin)
}

// installPlugin 是「装/更新到最新版本号」的统一入口。
func (m *UpdateManager) installPlugin(p managedPlugin) error {
	return m.runPluginOp(p, "installing", "install", func() error {
		return m.addNewestPluginCmd(p)
	})
}

// addNewestPluginCmd 组装「装到最新版本号」这条命令：先解析出精确版本号，再交给
// 使用选中镜像源的执行器（`add <pkg>@<版本>`）。
//
// 用精确版本而不是 `@latest`：`@latest` 让 pnpm 自己按标签解析，标签滞后时会装到旧版本；
// 精确版本还能让「所选镜像源还没同步到这一版」表现为命令失败并带出 pnpm 的原话。
func (m *UpdateManager) addNewestPluginCmd(p managedPlugin) error {
	version, err := m.resolvePluginVersion(p)
	if err != nil {
		return err
	}
	m.updateStatus(p.kind, func(st *UpdateStatus) {
		st.LatestVersion = version
		setMsgFields(&st.Message, &st.MessageRef, "msg_plugin_installing",
			"正在安装 %s@%s", "pkg", p.pkg, "version", version)
	})
	return pluginCmdFn(m, p, []string{"add", p.pkg + "@" + version})
}

// RemoveMarket 卸载插件市场。
//
// 卸载走 runPluginCmdOnceFor：**不带 `--registry`**（pnpm 的 remove 没有这个选项）、
// 也不涉及镜像源选择 —— 「装的是哪个源」与卸载无关。
func (m *UpdateManager) RemoveMarket() error {
	return m.removePlugin(marketPlugin)
}

// RemoveAdvanced 卸载增强插件。
func (m *UpdateManager) RemoveAdvanced() error {
	return m.removePlugin(advancedPlugin)
}

// removePlugin 是「卸载」的统一入口。
func (m *UpdateManager) removePlugin(p managedPlugin) error {
	return m.runPluginOp(p, "removing", "remove", func() error {
		return m.runPluginCmdOnceFor(p, []string{"remove", p.pkg})
	})
}

// pluginOpLogText 取出适合写日志的错误文本。
//
// 面向用户的错误（userError）的 Error() 是**中文兜底文案**（见 uimsg.go），直接打日志会
// 违反「日志必须英文」（AGENTS 规则 7）。这里优先取参数里的英文 detail / cmd（都是
// npm/dsh 的原始诊断），两者都没有时只记 code。
func pluginOpLogText(err error) string {
	if m, ok := uiMsgOf(err); ok {
		if detail := m.Params["detail"]; detail != "" {
			return detail
		}
		if cmd := m.Params["cmd"]; cmd != "" {
			return cmd
		}
		return "[" + m.Code + "]"
	}
	return err.Error()
}

// runPluginOp 串行执行一次受管插件操作：置阶段 → 跑命令 → 重启 dsh → 刷新状态。
//
// phase 是状态机里的阶段名（installing / removing），actionLog 是**日志**里的英文
// 动作名 —— 日志一律英文（见 AGENTS 第 4 节规则 7），别把中文动作名传进去。
func (m *UpdateManager) runPluginOp(p managedPlugin, phase, actionLog string, run func() error) error {
	if !m.applying.TryLock() {
		return busyf("err_plugin_busy", "正在执行其它更新/插件操作，请等它结束后再重试")
	}
	// 本次操作的序号：随状态一路带给前端，作为「结果只提示一次」的去重键
	// （否则「装成功」之后再一次「装成功」的 phase 完全相同，会被静默吞掉）。
	seq := m.pluginOpSeq.Add(1)
	go func() {
		defer m.applying.Unlock()
		m.updateStatus(p.kind, func(st *UpdateStatus) {
			st.Seq = seq
			st.Phase = phase
			setErrFields(&st.Error, &st.ErrorRef, nil)
			setMsgFields(&st.Message, &st.MessageRef, "", "")
			st.Cancelled = false
		})
		err := run()
		if err == nil {
			// 插件命令成功：bundle 选择已经落盘（安装会默认启用新 bundle），但正在跑的
			// dsh 进程不会凭空装配它 —— 重启一次让它生效。
			err = m.restartDshForPlugin(p)
		}
		if err != nil {
			logError("%s %s failed: %s", updateLogTag(p.kind), actionLog, pluginOpLogText(err))
			m.updateStatus(p.kind, func(st *UpdateStatus) {
				st.Seq = seq
				st.Phase = ""
				setErrFields(&st.Error, &st.ErrorRef, err)
				setMsgFields(&st.Message, &st.MessageRef, "", "")
			})
			return
		}
		logInfo("%s %s finished", updateLogTag(p.kind), actionLog)
		// force=true：刚装/卸完，本地版本确实变了，必须重查（不能吃缓存）。
		// 这次重查已经把 LocalVersion/Unavailable/HasUpdate 都写好了（含「查不了时保留
		// 上一次已知版本」的语义）—— 这里只补终态字段，别再用 snap.Version 覆盖一遍：
		// 那会把一次失败重查的结果当成「未安装」写回去（见 applyPluginLocalResult）。
		m.refreshPluginLocal(p, true)
		m.updateStatus(p.kind, func(st *UpdateStatus) {
			st.Seq = seq
			st.Phase = "done"
			setErrFields(&st.Error, &st.ErrorRef, nil)
			setMsgFields(&st.Message, &st.MessageRef, "", "")
		})
	}()
	return nil
}

// DoneMarket 清掉一次插件市场操作的终态（前端收起结果提示时调用）。
func (m *UpdateManager) DoneMarket() {
	m.donePlugin(marketPlugin)
}

// DoneAdvanced 清掉一次增强插件操作的终态。
func (m *UpdateManager) DoneAdvanced() {
	m.donePlugin(advancedPlugin)
}

// donePlugin 清掉某个受管插件操作的终态。
func (m *UpdateManager) donePlugin(p managedPlugin) {
	m.updateStatus(p.kind, func(st *UpdateStatus) {
		st.Phase = ""
		setMsgFields(&st.Message, &st.MessageRef, "", "")
		setErrFields(&st.Error, &st.ErrorRef, nil)
	})
}

// restartDshForPlugin 重启 dsh 让受管插件的安装/卸载生效。
//
// 顺序要点：插件命令已经跑完（profile 写锁已释放），此时才停 dsh；停之前仍过一次忙
// 守卫，因为可能正好有市场面板发起的另一次插件操作在跑（那一次会被停机连带杀掉，
// 并留下陈旧锁）。
func (m *UpdateManager) restartDshForPlugin(p managedPlugin) error {
	m.updateStatus(p.kind, func(st *UpdateStatus) {
		setMsgFields(&st.Message, &st.MessageRef, "msg_plugin_restarting",
			"正在重启 dsh 服务以使插件变更生效（%s）", "pkg", p.pkg)
	})
	if err := m.replaceBusyGuard("重启 dsh 服务"); err != nil {
		return err
	}
	if err := dshStopFn(m); err != nil {
		logWarn("%s failed to stop dsh before restart: %v", updateLogTag(p.kind), err)
	}
	dshPortFreeFn(m, 30*time.Second)
	if err := startDshCapturedFn(m); err != nil {
		return uiErr("err_plugin_restart_failed",
			"重启 dsh 服务失败（插件变更已落盘，重启控制台后生效）: %s", "detail", err.Error())
	}
	// 等它真的开始监听：起不来时把错误抛给用户，而不是让用户点进 dsh 界面看到等待页。
	switch marketReadyFn(m, 30*time.Second) {
	case marketWaitReady:
		return nil
	case marketWaitExited:
		return uiErr("err_plugin_restart_exited",
			"dsh 服务重启后立即退出（可能是新插件与当前 dsh 版本不兼容），请查看控制台日志")
	default:
		return uiErr("err_plugin_restart_timeout",
			"dsh 服务重启后 30 秒内没有就绪，请查看控制台日志")
	}
}

// restartDshForMarket 是插件市场的重启入口（保留给既有测试与调用方）。
func (m *UpdateManager) restartDshForMarket() error {
	return m.restartDshForPlugin(marketPlugin)
}

// --- 可注入的 dsh 启停钩子（便于对插件操作做单元测试） ---
//
// 生产实现直接调用既有方法：Stop 走精准 PID/进程组终止，start 走 startDshCaptured
// （它内部异步 WaitToken + ExchangeToken，即「拉起服务后换 token」这一步）。
var (
	dshStopFn = func(m *UpdateManager) error { return m.dsh.Stop() }
	// startDshCapturedFn 拉起 dsh 并异步换取会话凭据。所有「重启/换版本后必须重新拿
	// 凭据」的路径都经它（插件变更生效、dsh 版本切换），变量形式便于单测注入。
	startDshCapturedFn = func(m *UpdateManager) error { return m.startDshCaptured() }
	marketReadyFn      = func(m *UpdateManager, max time.Duration) marketWaitResult {
		return waitMarketDsh(m, max)
	}
	dshPortFreeFn = func(m *UpdateManager, max time.Duration) {
		checker := newBackendChecker(GetConfig().DshPort)
		deadline := time.Now().Add(max)
		for time.Now().Before(deadline) {
			if !checker.quick(300 * time.Millisecond) {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
)

// marketWaitResult 是「等 dsh 起来」的结果。区分超时与进程退出，是为了让失败信息
// 有意义，也为了让「进程已经死了」不再白等满超时上限。
type marketWaitResult int

const (
	// marketWaitReady：端口已开放且稳定存活。
	marketWaitReady marketWaitResult = iota
	// marketWaitTimeout：到上限仍未监听（进程可能还活着但极慢）。
	marketWaitTimeout
	// marketWaitExited：新进程已经退出 —— 立即判定失败，不等满上限。
	marketWaitExited
)

// waitMarketDsh 等 dsh 重新监听端口：端口开放后确认一小段仍存活；进程已退出则立即判失败。
func waitMarketDsh(m *UpdateManager, max time.Duration) marketWaitResult {
	checker := newBackendChecker(GetConfig().DshPort)
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		if checker.quick(500 * time.Millisecond) {
			time.Sleep(marketReadySettle)
			if !checker.quick(500*time.Millisecond) || !m.dsh.Running() {
				return marketWaitExited
			}
			return marketWaitReady
		}
		if !m.dsh.Running() {
			// 宽限一次：刚 Start 完的极短窗口里 /proc/self 可能还读不到；300ms 后
			// 端口与进程都仍不可见，才判定为「已退出」。
			time.Sleep(300 * time.Millisecond)
			if !checker.quick(300*time.Millisecond) && !m.dsh.Running() {
				return marketWaitExited
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return marketWaitTimeout
}

// --- 市场「忙」探测与停机守卫 ---

// marketBusyFn 探测插件市场此刻是否正在安装/更新插件。市场自带
// `GET /dsh-market/status`（返回 busy / phase / target 等），这里只读其中的 busy。
// 变量形式便于测试注入。
var marketBusyFn = func(m *UpdateManager) (bool, string) {
	cfg := GetConfig()
	if cfg.DshPort <= 0 {
		return false, ""
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/dsh-market/status", cfg.DshPort)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		// 市场没回答（没装市场、dsh 正在启动等）→ 不阻塞更新。
		return false, ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return false, ""
	}
	var st struct {
		Busy    bool   `json:"busy"`
		Phase   string `json:"phase"`
		Target  string `json:"target"`
		Pending bool   `json:"installing"`
	}
	if err := json.Unmarshal(body, &st); err != nil {
		return false, ""
	}
	if !st.Busy && !st.Pending {
		return false, ""
	}
	detail := st.Target
	if detail == "" {
		detail = st.Phase
	}
	return true, detail
}

// replaceBusyGuard 在「停 dsh 之前」检查是否有插件操作正在跑，返回拒绝原因。
//
// 为什么必须挡（线上故障的根因）：插件的安装/卸载会在 dsh 进程内持有 plugin-manager
// 的 profile 写锁（`profiles/web/package.json.lock`）。那把锁只在正常收尾时删除，
// 且 dsh 侧**不清理陈旧锁**、等待上限 120 秒 —— 一旦持有者被杀，之后所有插件操作
// （含插件列表）都会白等 120 秒再失败。而「更新 harness / 切换 dsh 版本 / 恢复 dsh 数据
// / 重启 dsh 让插件变更生效」都必须停 dsh（并连带终止它的进程组），正好会杀掉这些持有者。
// 因此动手前先确认没有在跑的操作。
//
// 两个来源互补：
//   - 市场面板内的安装 → 它 spawn 的 `dsh plugin …` 子进程 → 查市场的 /dsh-market/status；
//   - 控制台自己的插件命令（插件页的列表/卸载、市场与增强插件的安装/卸载）→
//     查 DshManager 的命令计数。
func (m *UpdateManager) replaceBusyGuard(action string) error {
	if m.dsh != nil && m.dsh.PluginCmdRunning() {
		return busyf("err_plugin_cmd_running", "控制台正在执行插件命令（dsh plugin …）。需要停止 dsh 的操作"+
			"会中断它并留下陈旧的 profile 写锁（之后插件列表/安装都会失败）；请稍等它结束再重试")
	}
	busy, detail := marketBusyFn(m)
	if !busy {
		return nil
	}
	if detail != "" {
		detail = "正在处理 " + detail
	} else {
		detail = "正在安装/更新插件"
	}
	return busyf("err_market_op_running", "插件市场%s。需要停止 dsh 的操作会中断它并留下陈旧的 profile 写锁"+
		"（之后插件列表/安装都会失败）；请等它完成或先在市场里取消，再重试", "detail", detail)
}

// stopDshForReplacement 为「替换 dsh 产物」停 dsh：先过忙守卫（拒绝时不产生任何停机），
// 再停止并等端口释放。用于恢复 dsh 数据前的停机。
//
// kind 是本次动作的目标 —— 日志按目标打标签（AGENTS 第 3 节）。
func (m *UpdateManager) stopDshForReplacement(action string, kind updateKind) error {
	if err := m.replaceBusyGuard(action); err != nil {
		return err
	}
	if err := dshStopFn(m); err != nil {
		// 与旧行为一致：停止失败也继续，但后面必须确认它真的停了。
		logWarn("%s failed to stop dsh (continuing): %v", updateLogTag(kind), err)
	}
	dshPortFreeFn(m, 30*time.Second)
	return nil
}

// --- 目录归属判断与 profile 位置 ---

// isUnder 判断 path 是否位于 root 之内（都应是规范路径）。
func isUnder(path, root string) bool {
	if root == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}

// canonicalPath 把路径规范化为「解析软链后的绝对路径」，解析不出来时退回 Clean 后的绝对路径。
func canonicalPath(path string) string {
	if path == "" {
		return ""
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return abs
}

// profileDir 返回控制台管理的 profile 目录（`$DSH_HOME/profiles/web`）。
func (m *UpdateManager) profileDir() string {
	home := ""
	if m.dsh != nil {
		home = m.dsh.effectiveHome()
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".dsh", "profiles", marketProfile)
}
