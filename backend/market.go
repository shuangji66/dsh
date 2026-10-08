package main

// market.go —— 插件市场（dshmarket）的检测、安装、卸载与更新。
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
// 镜像源与 dsh 版本安装完全一致（见 server.go 的 npmMirrors）：阿里云 → 腾讯云 →
// 华为云，5 秒无响应换下一个，不重试，都失败就报错，不试官方源。
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
//     情况；「未安装 → 装上 latest」这条路径只有控制台能提供。

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
	// marketPackageName 是市场的 npm 包名（与 profile bundles 里的名字一致）。
	marketPackageName = "dshmarket"
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

// --- 检测：`dsh plugin --profile web list` ---

// marketInstalled 通过 `dsh plugin --profile web list` 判断市场装没装、装的哪一版。
// 返回 ok=false 表示输出里没有 dshmarket（= 未安装）。
func (m *UpdateManager) marketInstalled() (version string, ok bool, err error) {
	if m.dsh == nil {
		return "", false, uiErr("err_dsh_manager_unavailable", "dsh 管理器不可用")
	}
	// 只读检测，但仍走控制台侧插件命令的统一入口：它执行前会清「持有者已死」的陈旧
	// profile 写锁（list 正是最容易被陈旧锁拖住 120 秒的那条），并把「插件命令进行中」
	// 登记给忙守卫。超时用 pluginCmdTimeout（不是 marketCmdTimeout）—— 这条是只读命令，
	// 不该允许它挂 10 分钟，否则忙守卫会被一次卡住的检测长期占住。
	out, err := m.dsh.runPluginCmd("list")
	if err != nil {
		// 「本机没有选中的 dsh 版本」是最常见的失败原因，而且它的内部错误文本是英文的
		// （会被日志打印，见 AGENTS 规则 7）—— 这里换成中文话术，用户才看得懂该去做什么。
		// 这条错误只进界面（MarketDir 诊断 / 弹窗），不进日志。
		if errors.Is(err, errNoDshVersion) {
			return "", false, uiErr("err_no_dsh_version", "未安装 dsh 服务：请先在控制台「概览」页的版本列表里下载一个版本，再点「切换」")
		}
		// 命令整体失败（dsh 未安装 / pnpm 缺失 / profile 目录不存在等）：
		// 把原因带上，前端在弹窗里能直接看到「为什么检测不到」。
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = err.Error()
		}
		return "", false, uiErr("err_market_detect_failed", "检测插件市场失败（dsh plugin --profile %s list）: %s", "profile", marketProfile, "detail", msg)
	}
	for _, p := range parsePluginList(out) {
		if p.Name == marketPackageName {
			return p.Version, true, nil
		}
	}
	return "", false, nil
}

// marketLocalSnapshot 是「本地检测」的结果（不联网）。
type marketLocalSnapshot struct {
	Version string
	Diag    string // 检测失败/未安装的原因（诊断用，进 MarketDir 字段）
	Err     error
}

// detectMarketLocal 取「本机装的是哪一版」。
//
// force=false 时**优先吃缓存**：这次检测要起一个 `dsh plugin --profile web list` 子进程
// （1~3 秒），而它被调用的场合比「插件真的变了」多得多（弹窗打开、诊断接口、状态重算），
// 每次都跑一遍既慢又容易和 profile 写锁打架。
//
// 重跑的入口（force=true）：市场安装/卸载完成（结果变了，必须重查）、后台自动检测、
// 手动「检查更新」。缓存为空时（控制台刚启动）也会先跑一次。
func (m *UpdateManager) detectMarketLocal(force bool) marketLocalSnapshot {
	if !force {
		m.marketLocalMu.Lock()
		if c := m.marketLocalCache; c != nil {
			snap := *c
			m.marketLocalMu.Unlock()
			return snap
		}
		m.marketLocalMu.Unlock()
	}
	snap := m.probeMarketLocal()
	m.marketLocalMu.Lock()
	m.marketLocalCache = &snap
	m.marketLocalMu.Unlock()
	return snap
}

// probeMarketLocal 真正执行一次本地检测（起子进程，不联网）。
func (m *UpdateManager) probeMarketLocal() marketLocalSnapshot {
	v, ok, err := m.marketInstalled()
	if err != nil {
		return marketLocalSnapshot{Diag: err.Error(), Err: err}
	}
	if !ok {
		return marketLocalSnapshot{Diag: fmt.Sprintf("`dsh plugin --profile %s list` 未列出 %s", marketProfile, marketPackageName)}
	}
	return marketLocalSnapshot{Version: v}
}

// refreshMarketLocal 刷新本地检测并把「当前装的是哪一版」写进市场状态。
// force=false 时吃缓存（启动时先让界面有版本号可用，不额外起子进程）。
// 保留 LatestVersion/HasUpdate 的既有值。
func (m *UpdateManager) refreshMarketLocal(force bool) marketLocalSnapshot {
	snap := m.detectMarketLocal(force)
	m.updateStatus(updateKindMarket, func(st *UpdateStatus) {
		st.Kind = updateKindMarket
		st.LocalVersion = snap.Version
		st.MarketDir = snap.Diag
		st.HasUpdate = snap.Version != "" && st.LatestVersion != "" &&
			compareVersion(st.LatestVersion, snap.Version) > 0
	})
	return snap
}

// --- 版本检测：npm 镜像源 ---

// marketLatest 依次尝试各镜像源的 dshmarket 元数据，返回**版本号最高**的那一版
// （`newestVersion`）与命中的镜像源日志标识（英文标识；界面文案要用 npmMirror.Name，
// 别在这里返回中文显示名 —— 它会被调用方写进日志）。
//
// 刻意不看 `dist-tags.latest`：市场装的是「实际发布的最新版本号」那一版，而不是发布者
// 手动指到哪一版的标签（标签可能滞后、可能指向稳定线而版本集合里已有更新的预览版）。
// 安装时同一条链路的 `add dshmarket@<该版本>` 用的是这里返回的精确版本号。
func (m *UpdateManager) marketLatest() (string, string, error) {
	client := mirrorClient()
	var lastErr error
	for _, mirror := range npmMirrors {
		versions, _, err := fetchMirrorPackument(client, mirror.URL, marketPackageName)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", mirror.mirrorLogName(), err)
			continue
		}
		if v := newestVersion(versions); v != "" {
			return v, mirror.mirrorLogName(), nil
		}
		lastErr = fmt.Errorf("%s: no usable version in metadata", mirror.mirrorLogName())
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("没有可用的 npm 镜像源")
	}
	return "", "", fmt.Errorf("获取 %s 最新版本失败（已尝试镜像源：%s）: %v", marketPackageName, mirrorLogNames(), lastErr)
}

// refreshMarketStatus 做一次完整的市场检测：本地检测 + 镜像源最新版。
// 任何一步失败都只写进市场的 Error，不影响 harness / dsh 两条更新链路。
func (m *UpdateManager) refreshMarketStatus() {
	// 检测跑一趟本来就慢（子进程 + 镜像源），这里强制重查本地版本，保证结论是当下的。
	snap := m.detectMarketLocal(true)
	now := time.Now()
	latest, _, err := m.marketLatest()
	m.updateStatus(updateKindMarket, func(st *UpdateStatus) {
		st.Kind = updateKindMarket
		st.CheckedAt = now
		st.LocalVersion = snap.Version
		st.MarketDir = snap.Diag
		st.ReleaseNotes = ""
		if err != nil {
			st.LatestVersion = ""
			st.HasUpdate = false
			setErrFields(&st.Error, &st.ErrorRef, err)
			return
		}
		setErrFields(&st.Error, &st.ErrorRef, nil)
		st.LatestVersion = latest
		st.HasUpdate = snap.Version != "" && compareVersion(latest, snap.Version) > 0
	})
}

// marketInfo 返回供 /api/market/info 使用的诊断快照：只读、不联网、不起子进程
// （本地检测吃缓存；没有缓存时才会真的查一次）。
func (m *UpdateManager) marketInfo() map[string]interface{} {
	snap := m.detectMarketLocal(false)
	st := m.getStatus(updateKindMarket)
	updatable := snap.Version != "" && st.LatestVersion != "" &&
		compareVersion(st.LatestVersion, snap.Version) > 0
	return map[string]interface{}{
		"ok":        true,
		"installed": snap.Version != "",
		"version":   snap.Version,
		"latest":    st.LatestVersion,
		"updatable": updatable,
		"reason":    snap.Diag,
		"error":     st.Error,
	}
}

// --- 安装 / 更新 / 卸载 ---

// marketCommand 组装 `dsh plugin --profile web <args…>`（不含 registry 参数）。
func marketCommand(args []string) []string {
	return append([]string{"plugin", "--profile", marketProfile}, args...)
}

// runMarketPluginCmd 以**镜像源逐个尝试**的方式执行一次市场安装/更新命令
// （`add`，它会访问 registry）。
//
// registry 通过 `--registry=<镜像源>` 传，只要整条命令失败（元数据取不到、tarball 下载
// 失败、构建失败）就换下一个镜像源重跑一次；**不重试**同一个镜像源，三个都失败就报错。
//
// 注意：只有「会访问 registry 的命令」能走这里 —— pnpm 的 `add` 支持 `--registry`，
// 而 `remove` / `list` 这类命令**不认这个选项**（会直接 `Unknown option: 'registry'`），
// 它们走 runMarketPluginCmdOnce。
func (m *UpdateManager) runMarketPluginCmd(args []string) error {
	var lastErr error
	for _, mirror := range npmMirrors {
		full := append(marketCommand(args), "--registry="+mirror.URL)
		m.updateStatus(updateKindMarket, func(st *UpdateStatus) {
			// 界面进度文案用中文显示名；日志用下面的 mirrorLogName()（英文标识）。
			setMsgFields(&st.Message, &st.MessageRef, "msg_market_from_mirror",
				"正在从 %s 处理 %s", "mirror", mirror.Name, "pkg", marketPackageName)
		})
		logInfo("[market] running dsh %s (via %s)", strings.Join(full, " "), mirror.mirrorLogName())
		out, err := m.dsh.RunPluginCommand(marketCmdTimeout, full)
		if err == nil {
			return nil
		}
		lastErr = fmt.Errorf("%s: %v", mirror.mirrorLogName(), tailLines(out, 3))
		logWarn("[market] dsh plugin command via %s failed: %v", mirror.mirrorLogName(), err)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("没有可用的 npm 镜像源")
	}
	return uiErr("err_market_op_failed", "插件市场操作失败（%s 均失败，不重试、不使用官方源）: %s", "mirrors", mirrorLogNames(), "detail", lastErr.Error())
}

// runMarketPluginCmdOnce 执行一次**与 registry 无关**的市场命令（`remove` 等）：
// 不带 `--registry`、不按镜像源回退（卸载既不下载也不解析依赖，没有任何「换个源再试」
// 的意义），失败就把 dsh/pnpm 的原话报出去。
func (m *UpdateManager) runMarketPluginCmdOnce(args []string) error {
	full := marketCommand(args)
	logInfo("[market] running dsh %s", strings.Join(full, " "))
	out, err := m.dsh.RunPluginCommand(marketCmdTimeout, full)
	if err != nil {
		return uiErr("err_market_cmd_failed", "dsh %s 失败: %s", "cmd", strings.Join(full, " "), "detail", tailLines(out, 3))
	}
	return nil
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
		return "（无输出）"
	}
	return strings.Join(kept, " | ")
}

// marketPluginCmdFn 执行一次「镜像源逐个尝试」的 `dsh plugin …`（变量形式便于
// 单测注入，不必真的起 dsh / pnpm）。
var marketPluginCmdFn = func(m *UpdateManager, args []string) error {
	return m.runMarketPluginCmd(args)
}

// resolveMarketVersion 取「实际最新的一版」的精确版本号（镜像源，非 dist-tags）。
//
// 安装前现取一次（而不是复用状态里那份可能过期的 LatestVersion）：状态是最近一次检测的
// 快照，用户可能在检测之后很久才点安装。
func (m *UpdateManager) resolveMarketVersion() (string, error) {
	version, mirror, err := m.marketLatest()
	if err != nil {
		return "", err
	}
	logInfo("[market] newest %s version is %s (via %s)", marketPackageName, version, mirror)
	return version, nil
}

// InstallMarket 安装插件市场的最新版本号那一版。异步执行，进度经市场状态推送。
func (m *UpdateManager) InstallMarket() error {
	return m.runMarketOp("安装", "market install", m.addNewestMarketCmd)
}

// UpdateMarket 把插件市场更新到最新版本号那一版（语义上就是再装一次那个精确版本）。
func (m *UpdateManager) UpdateMarket() error {
	return m.runMarketOp("更新", "market update", m.addNewestMarketCmd)
}

// addNewestMarketCmd 组装「装到最新版本号」这条命令：先解析出精确版本号，再交给
// 镜像源逐个尝试的执行器（`add dshmarket@<版本>`）。
//
// 用精确版本而不是 `@latest`：`@latest` 让 pnpm 自己按标签解析，标签滞后时会装到旧版本；
// 精确版本还能让「某个镜像源还没同步到这一版」表现为该次尝试失败 → 自动换下一个镜像源。
func (m *UpdateManager) addNewestMarketCmd() error {
	version, err := m.resolveMarketVersion()
	if err != nil {
		return err
	}
	m.updateStatus(updateKindMarket, func(st *UpdateStatus) {
		st.LatestVersion = version
		setMsgFields(&st.Message, &st.MessageRef, "msg_market_installing",
			"正在安装 %s@%s", "pkg", marketPackageName, "version", version)
	})
	return marketPluginCmdFn(m, []string{"add", marketPackageName + "@" + version})
}

// RemoveMarket 卸载插件市场。
//
// 卸载走 runMarketPluginCmdOnce：**不带 `--registry`**（pnpm 的 remove 没有这个选项）、
// 也不按镜像源回退 —— 「装的是哪个源」与卸载无关。
func (m *UpdateManager) RemoveMarket() error {
	return m.runMarketOp("卸载", "market remove", func() error {
		return m.runMarketPluginCmdOnce([]string{"remove", marketPackageName})
	})
}

// runMarketOp 串行执行一次市场操作：置阶段 → 跑命令 → 重启 dsh → 刷新状态。
//
// action 是给用户看的中文动作名（拒绝并发操作时的提示、状态里用），actionLog 是**日志**
// 里的英文动作名 —— 日志一律英文（见 AGENTS 第 4 节规则 7），别把 action 传进日志。
func (m *UpdateManager) runMarketOp(action, actionLog string, run func() error) error {
	if !m.applying.TryLock() {
		return busyf("err_market_busy", "正在执行其它更新/插件操作，请等它结束后再重试")
	}
	// 本次操作的序号：随状态一路带给前端，作为「结果只提示一次」的去重键
	// （否则「装成功」之后再一次「装成功」的 phase 完全相同，会被静默吞掉）。
	seq := m.marketOpSeq.Add(1)
	go func() {
		defer m.applying.Unlock()
		phase := "installing"
		if action == "卸载" {
			phase = "removing"
		}
		m.updateStatus(updateKindMarket, func(st *UpdateStatus) {
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
			err = m.restartDshForMarket()
		}
		if err != nil {
			logError("[market] %s failed: %v", actionLog, err)
			m.updateStatus(updateKindMarket, func(st *UpdateStatus) {
				st.Seq = seq
				st.Phase = ""
				setErrFields(&st.Error, &st.ErrorRef, err)
				setMsgFields(&st.Message, &st.MessageRef, "", "")
			})
			return
		}
		logInfo("[market] %s finished", actionLog)
		// force=true：刚装/卸完市场，本地版本确实变了，必须重查（不能吃缓存）。
		snap := m.refreshMarketLocal(true)
		m.updateStatus(updateKindMarket, func(st *UpdateStatus) {
			st.Seq = seq
			st.Phase = "done"
			setErrFields(&st.Error, &st.ErrorRef, nil)
			setMsgFields(&st.Message, &st.MessageRef, "", "")
			st.LocalVersion = snap.Version
			if st.LatestVersion != "" {
				st.HasUpdate = compareVersion(st.LatestVersion, snap.Version) > 0
			}
		})
	}()
	return nil
}

// DoneMarket 清掉一次市场操作的终态（前端收起结果提示时调用）。
func (m *UpdateManager) DoneMarket() {
	m.updateStatus(updateKindMarket, func(st *UpdateStatus) {
		st.Phase = ""
		setMsgFields(&st.Message, &st.MessageRef, "", "")
		setErrFields(&st.Error, &st.ErrorRef, nil)
	})
}

// restartDshForMarket 重启 dsh 让插件市场的安装/卸载生效。
//
// 顺序要点：插件命令已经跑完（profile 写锁已释放），此时才停 dsh；停之前仍过一次忙
// 守卫，因为可能正好有市场面板发起的另一次插件操作在跑（那一次会被停机连带杀掉，
// 并留下陈旧锁）。
func (m *UpdateManager) restartDshForMarket() error {
	m.updateStatus(updateKindMarket, func(st *UpdateStatus) {
		setMsgFields(&st.Message, &st.MessageRef, "msg_market_restarting", "正在重启 dsh 服务以使插件市场变更生效")
	})
	if err := m.replaceBusyGuard("重启 dsh 服务"); err != nil {
		return err
	}
	if err := dshStopFn(m); err != nil {
		logWarn("[market] failed to stop dsh before restart: %v", err)
	}
	dshPortFreeFn(m, 30*time.Second)
	if err := startDshCapturedFn(m); err != nil {
		return fmt.Errorf("重启 dsh 服务失败（插件市场的变更已落盘，重启控制台后生效）: %w", err)
	}
	// 等它真的开始监听：起不来时把错误抛给用户，而不是让用户点进 dsh 界面看到等待页。
	switch marketReadyFn(m, 30*time.Second) {
	case marketWaitReady:
		return nil
	case marketWaitExited:
		return fmt.Errorf("dsh 服务重启后立即退出（可能是新装的插件市场与当前 dsh 版本不兼容），请查看控制台日志")
	default:
		return fmt.Errorf("dsh 服务重启后 30 秒内没有就绪，请查看控制台日志")
	}
}

// --- 可注入的 dsh 启停钩子（便于对市场操作做单元测试） ---
//
// 生产实现直接调用既有方法：Stop 走精准 PID/进程组终止，start 走 startDshCaptured
// （它内部异步 WaitToken + ExchangeToken，即「拉起服务后换 token」这一步）。
var (
	dshStopFn = func(m *UpdateManager) error { return m.dsh.Stop() }
	// startDshCapturedFn 拉起 dsh 并异步换取会话凭据。所有「重启/换版本后必须重新拿
	// 凭据」的路径都经它（市场变更生效、dsh 版本切换），变量形式便于单测注入。
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
// / 重启 dsh 让市场变更生效」都必须停 dsh（并连带终止它的进程组），正好会杀掉这些持有者。
// 因此动手前先确认没有在跑的操作。
//
// 两个来源互补：
//   - 市场面板内的安装 → 它 spawn 的 `dsh plugin …` 子进程 → 查市场的 /dsh-market/status；
//   - 控制台自己的插件命令（插件页的列表/卸载、市场的安装/卸载）→ 查 DshManager 的命令计数。
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
