package main

// market.go —— 控制台侧的「插件市场（dshmarket）就地更新」。
//
// 背景：本项目的 dshmarket 不是 profile 依赖，而是构建 server 包时被写进
// @deepseek-ai/dsh 的 dependencies（见 .github/workflows/server-build.yaml 的
// 「添加 dshmarket 依赖」与「修改 PROFILE_TEMPLATES.web.bundles」两步）。安装后它
// 落在 <serverDir>/node_modules/@deepseek-ai/dsh/node_modules/dshmarket；dsh 启动
// 时由 dsh-app-boot 把它镜像成 $DSH_HOME/profiles/node_modules/dshmarket 这个
// symlink —— 也就是说「市场跑的是哪一份字节」由 server 目录决定。
//
// 由此产生两个后果：
//   1. 市场面板的 selfManaged 恒为 false（它不在 profile package.json 的
//      dependencies 里，见 dsh-market 的 readInstalled），面板内既没有自更新入口，
//      打 /dsh-market/update 也会被 400「plugin is not installed」挡掉。
//   2. dsh server 包只在 @deepseek-ai/dsh 有新版本时才重建，于是 dsh 版本空窗期内
//      市场会一直停在构建 server 包时 npm 解析出来的那个版本。
//
// 本模块做一件事：把上面那份 dshmarket 换成 npm 上的最新版，然后重启 dsh。
// 实现要点（每条都对应一个真实的坑）：
//   1. 【必须先停 dsh】市场是宿主内插件，运行中替换文件会让 client 半区（磁盘）
//      与 server 半区（进程内存）版本错配。
//   2. 【原子替换】在目标同级目录建 staging 再 rename 就位；跨设备 rename 会
//      EXDEV，所以 staging 绝不能放 /tmp。
//   3. 【必须能自动回滚】市场是 profile 的 bundle 之一，文件坏了会让 dsh 起不来；
//      起不来就把旧目录换回去并重新拉起 dsh + 重新换 token。
//   4. 【必须校验完整性】registry 的 dist.integrity / dist.shasum 是元数据与字节
//      之间唯一的绑定关系；两个都没有就拒绝安装（不冒静默投毒的风险）。
//   5. 【备份前缀必须是 market-】runBackupCleanup 只自动清理 harness-*/server-*，
//      而 ListServerBackups / RollbackServer 认 server- 前缀 —— 用 server- 前缀会让
//      市场备份出现在「dsh 服务回滚」列表里，一旦被点就会拿市场包覆盖整个 server 目录。
//   6. 【client bundle 的 rev 会自己变】dsh 用 sha1(client.js 内容 + mtimeMs) 生成
//      rev（@deepseek-ai/dsh-client-modules 的 artifactRevision），该路由严格校验
//      rev，所以替换 + 重启后浏览器会自动拿到新字节，不需要任何清缓存手段。

import (
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// marketPackageName 是市场的 npm 包名（与 profile bundles 里的名字一致）。
	marketPackageName = "dshmarket"
	// marketBackupPrefix 是市场备份文件名前缀（见文件头第 5 条）。
	marketBackupPrefix = "market-"
	// marketStagingSuffix / marketOldSuffix 是替换过程中在目标同级目录留下的
	// 中间目录后缀（staging = 待就位的新版本，old = 已经就位的旧版本）。
	// 两者都必须是「目标名 + 后缀」，便于安装前扫掉上次崩溃留下的残留。
	marketStagingSuffix = ".staging-"
	marketOldSuffix     = ".old-"
	// marketReadyTimeout 是「替换后等 dsh 监听端口」的上限。实测本机 dsh 从
	// 进程启动到插件树装配完成约 2.4 秒（端口开放更早），10 秒有数倍余量。
	// 注意：真正的失败不会等满这个上限 —— 进程退出会立即判定失败（见 waitMarketDsh）。
	marketReadyTimeout = 10 * time.Second
	// marketReadySettle 是端口开放后的确认时间：dsh 先监听、再装配插件树，
	// 坏 bundle 可能「先开放端口，再装配失败退出」。等一小段再复核，避免把
	// 这种「起来又死」当成成功。
	marketReadySettle = 2 * time.Second
	// marketRegistryTimeout 是单次 registry 元数据请求的超时。
	marketRegistryTimeout = 20 * time.Second
)

// marketRegistries 是 npm registry 候选列表（按顺序回退）。顺序参考 dsh-market
// 自身的区域路由（china 区用腾讯云镜像）+ 实测延迟（npmmirror 0.24s、
// 腾讯云 0.49s、npmjs 1.17s）。只用于取元数据与 tarball，不涉及 pnpm 配置。
var marketRegistries = []string{
	"https://registry.npmmirror.com",
	"https://mirrors.cloud.tencent.com/npm",
	"https://registry.npmjs.org",
}

// marketScope 说明「当前生效的那份 dshmarket 由谁提供」。
type marketScope string

const (
	// marketScopeServer：由 server 包提供，控制台可以就地更新（正常情形）。
	marketScopeServer marketScope = "server"
	// marketScopeProfile：由 profile 自己的 node_modules 提供（用户按 dsh 官方
	// 方式装了市场）。此时控制台改了也不会生效 —— profile 的条目优先于安装闭包。
	marketScopeProfile marketScope = "profile"
	// marketScopeExternal：解析出来的目录不在 server 目录内（如 link: 本地开发
	// 安装）——控制台不碰。
	marketScopeExternal marketScope = "external"
	// marketScopeMissing：找不到市场安装位置。
	marketScopeMissing marketScope = "missing"
)

// marketTarget 描述「当前真正生效的 dshmarket 安装位置」。
type marketTarget struct {
	Dir     string      `json:"dir"`
	Version string      `json:"version"`
	Scope   marketScope `json:"scope"`
	Reason  string      `json:"reason"`
}

// marketRelease 是 npm registry 上某个版本的元数据（只保留下载与校验需要的字段）。
type marketRelease struct {
	Version   string
	Tarball   string
	Integrity string // dist.integrity，形如 sha512-<base64>
	Shasum    string // dist.shasum，sha1 十六进制
	Registry  string // 命中的 registry（日志/诊断用）
}

// marketWaitResult 是「等新版本 dsh 起来」的结果。区分超时与进程退出，是为了
// 让失败信息有意义，也为了让「进程已经死了」不再白等满超时上限。
type marketWaitResult int

const (
	// marketWaitReady：端口已开放且稳定存活。
	marketWaitReady marketWaitResult = iota
	// marketWaitTimeout：到上限仍未监听（进程可能还活着但极慢）。
	marketWaitTimeout
	// marketWaitExited：新进程已经退出 —— 立即判定失败，不等满上限。
	marketWaitExited
)

// --- 可注入的 dsh 启停钩子（便于对替换/回滚逻辑做单元测试） ---
//
// 生产实现直接调用既有方法：Stop 走 pkill 语义，start 走 startDshCaptured
// （它内部异步 WaitToken + ExchangeToken，即「拉起服务后换 token」这一步）。
var (
	marketStopDshFn  = func(m *UpdateManager) error { return m.dsh.Stop() }
	marketStartDshFn = func(m *UpdateManager) error {
		return m.startDshCaptured()
	}
	marketReadyFn = func(m *UpdateManager, max time.Duration) marketWaitResult {
		return waitMarketDsh(m, max)
	}
	// marketServerDirFn 便于测试注入 server 目录；生产实现即 UpdateManager.serverDir。
	marketServerDirFn = func(m *UpdateManager) string { return m.serverDir() }
	marketPortFreeFn  = func(m *UpdateManager, max time.Duration) {
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

// --- 安装位置解析 ---

// readMarketManifest 读取某个目录下的 package.json 并确认它就是 dshmarket。
// 返回的 version 为空表示该目录不是一份市场安装。
func readMarketManifest(dir string) (version string, ok bool) {
	raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return "", false
	}
	var doc struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", false
	}
	if doc.Name != marketPackageName || doc.Version == "" {
		return "", false
	}
	return doc.Version, true
}

// isUnder 判断 path 是否位于 root 之下（两者都做 Clean，避免 ".." 绕过）。
// 注意：只做字符串比较，调用方必须先规范化路径 —— 平台把
// /var/apps/Harness/target 做成了指向 /vol1/@appcenter/Harness 的软链，
// 一侧规范化、另一侧不规范化时比较必然为假（见 canonicalPath）。
func isUnder(path, root string) bool {
	if root == "" {
		return false
	}
	p := filepath.Clean(path)
	r := filepath.Clean(root)
	return p == r || strings.HasPrefix(p, r+string(os.PathSeparator))
}

// canonicalPath 返回路径的「规范形式」（解析所有软链）。解析失败（路径不存在等）
// 时退回 Clean 结果，保证调用方总能拿到一个可用于比较的路径。
//
// 为什么必须做：fnOS 平台上 /var/apps/<App>/target 是指向 /vol1/@appcenter/<App>
// 的软链，而 dsh 自己维护的 profile 镜像软链解析出来是规范路径。于是
// serverDir()（非规范）与 EvalSymlinks 的结果（规范）字符串前缀比较必然失败，
// 会把「server 包自带的 dshmarket」误判为 external（用户看到按钮被禁用）。
func canonicalPath(path string) string {
	if path == "" {
		return ""
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return filepath.Clean(path)
}

// resolveMarketTarget 判定「当前真正生效的 dshmarket 在哪、由谁提供」。
//
// 判定顺序与 dsh-app-boot 的解析顺序保持一致（profile 里 pnpm 管的条目优先，
// 其次是 $DSH_HOME/profiles/node_modules 这个安装闭包镜像）：
//  1. $DSH_HOME/profiles/web/node_modules/dshmarket 是真实目录 → profile 提供；
//     是 symlink 则解析后判断它到底落在 server 目录还是别处。
//  2. $DSH_HOME/profiles/node_modules/dshmarket（共享 fallback symlink）→ 解析。
//  3. 直接扫 server 目录下的两个已知位置（npm 是否提升取决于依赖树，两种都可能有）。
func (m *UpdateManager) resolveMarketTarget() marketTarget {
	return resolveMarketTargetIn(m.dsh.effectiveHome(), marketServerDirFn(m))
}

// resolveMarketTargetIn 是 resolveMarketTarget 的实际实现（拆出来便于单测用临时目录
// 构造各种安装布局）。
func resolveMarketTargetIn(home, serverDir string) marketTarget {
	if home == "" {
		return marketTarget{Scope: marketScopeMissing, Reason: "无法确定 HOME，读不到 dsh profile 目录"}
	}
	dshHome := filepath.Join(home, ".dsh")
	// 统一到规范路径后再比较/拼接：serverDir 常常是软链路径（见 canonicalPath）。
	serverDir = canonicalPath(serverDir)

	profileDir := filepath.Join(dshHome, "profiles", "web", "node_modules", marketPackageName)
	if fi, err := os.Lstat(profileDir); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			// symlink：可能是 dsh 自己的 fallback 链接（指回 server 目录），也可能是
			// 用户的 link: 本地开发安装。解析后按真实位置归类。
			if real, err := filepath.EvalSymlinks(profileDir); err == nil {
				if v, ok := readMarketManifest(real); ok {
					if isUnder(real, serverDir) {
						return marketTarget{Dir: real, Version: v, Scope: marketScopeServer,
							Reason: "由 server 包提供（profile 内为其镜像软链）"}
					}
					return marketTarget{Dir: real, Version: v, Scope: marketScopeExternal,
						Reason: "安装位置不在 dsh server 目录内（可能是 link:/file: 本地安装），控制台不管理"}
				}
			}
		} else if v, ok := readMarketManifest(profileDir); ok {
			return marketTarget{Dir: profileDir, Version: v, Scope: marketScopeProfile,
				Reason: "由 profile 的 node_modules 提供，请在市场面板内更新"}
		}
	}

	sharedLink := filepath.Join(dshHome, "profiles", "node_modules", marketPackageName)
	if real, err := filepath.EvalSymlinks(sharedLink); err == nil {
		if v, ok := readMarketManifest(real); ok {
			if isUnder(real, serverDir) {
				return marketTarget{Dir: real, Version: v, Scope: marketScopeServer,
					Reason: "由 server 包提供（经 $DSH_HOME/profiles/node_modules 镜像生效）"}
			}
			return marketTarget{Dir: real, Version: v, Scope: marketScopeExternal,
				Reason: "安装位置不在 dsh server 目录内，控制台不管理"}
		}
	}

	// 兜底扫描：server 目录下的两个已知位置（npm 是否提升这份嵌套依赖取决于依赖树，
	// 两种布局都见过）。
	for _, cand := range []string{
		filepath.Join(serverDir, "node_modules", "@deepseek-ai", "dsh", "node_modules", marketPackageName),
		filepath.Join(serverDir, "node_modules", marketPackageName),
	} {
		// 万一该条目本身是软链（如 npm 的 file: 依赖），要按解析后的真实位置归类，
		// 不能直接把它当成 server 内的一份。
		if fi, err := os.Lstat(cand); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			real, err := filepath.EvalSymlinks(cand)
			if err != nil {
				continue
			}
			v, ok := readMarketManifest(real)
			if !ok {
				continue
			}
			if isUnder(real, serverDir) {
				return marketTarget{Dir: real, Version: v, Scope: marketScopeServer,
					Reason: "由 server 包提供（软链指向 server 目录内）"}
			}
			return marketTarget{Dir: real, Version: v, Scope: marketScopeExternal,
				Reason: "安装位置不在 dsh server 目录内（软链指向别处），控制台不管理"}
		}
		if v, ok := readMarketManifest(cand); ok {
			return marketTarget{Dir: cand, Version: v, Scope: marketScopeServer,
				Reason: "由 server 包提供"}
		}
	}

	return marketTarget{Scope: marketScopeMissing,
		Reason: fmt.Sprintf("在 %s 下未找到 %s 安装", serverDir, marketPackageName)}
}

// --- 版本检测 ---

// marketLatest 依次尝试各 registry 的 /dshmarket/latest，返回第一个可用结果。
// 全部失败时返回最后一次错误（前端以 Error 呈现，不影响 harness/dsh 两条链路）。
func (m *UpdateManager) marketLatest() (*marketRelease, error) {
	client := m.httpClientForUpdate()
	var lastErr error
	for _, registry := range marketRegistries {
		base := strings.TrimRight(registry, "/")
		url := base + "/" + marketPackageName + "/latest"
		rel, err := fetchMarketRelease(client, url, base)
		if err != nil {
			lastErr = err
			continue
		}
		return rel, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("没有可用的 npm registry")
	}
	return nil, lastErr
}

// fetchMarketRelease 拉取单个 registry 的 latest 元数据。超时与错误都不致命，
// 由调用方回退到下一个 registry。
func fetchMarketRelease(client *http.Client, url, registry string) (*marketRelease, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "harness-console")
	req.Header.Set("Accept", "application/json")
	// 单独的超时：httpClientForUpdate 是 60s，registry 元数据不该拖那么久。
	metaClient := *client
	metaClient.Timeout = marketRegistryTimeout
	resp, err := metaClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 %s 失败: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s 返回 HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("读取 %s 响应失败: %w", url, err)
	}
	var doc struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Dist    struct {
			Tarball   string `json:"tarball"`
			Integrity string `json:"integrity"`
			Shasum    string `json:"shasum"`
		} `json:"dist"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("解析 %s 响应失败: %w", url, err)
	}
	if doc.Name != marketPackageName {
		return nil, fmt.Errorf("%s 返回的包名是 %q，不是 %s", url, doc.Name, marketPackageName)
	}
	if doc.Version == "" || doc.Dist.Tarball == "" {
		return nil, fmt.Errorf("%s 缺少 version 或 dist.tarball", url)
	}
	return &marketRelease{
		Version:   doc.Version,
		Tarball:   doc.Dist.Tarball,
		Integrity: doc.Dist.Integrity,
		Shasum:    doc.Dist.Shasum,
		Registry:  registry,
	}, nil
}

// refreshMarketLocal 只做本地解析（无网络），把「当前生效的那份」写进市场状态。
// 保留 LatestVersion/HasUpdate 的既有值，供启动时（不联网）先显示出本地版本。
func (m *UpdateManager) refreshMarketLocal() marketTarget {
	target := m.resolveMarketTarget()
	m.updateStatus(updateKindMarket, func(st *UpdateStatus) {
		st.Kind = updateKindMarket
		st.LocalVersion = target.Version
		st.MarketScope = string(target.Scope)
		st.MarketDir = target.Dir
		st.HasUpdate = target.Version != "" && st.LatestVersion != "" &&
			compareVersion(st.LatestVersion, target.Version) > 0
	})
	return target
}

// refreshMarketStatus 做一次完整的市场检测：本地解析 + registry 最新版。
// 任何一步失败都只写进市场的 Error，不影响 harness/dsh 两条更新链路。
func (m *UpdateManager) refreshMarketStatus() {
	target := m.resolveMarketTarget()
	now := time.Now()
	rel, err := m.marketLatest()
	m.updateStatus(updateKindMarket, func(st *UpdateStatus) {
		st.Kind = updateKindMarket
		st.CheckedAt = now
		st.LocalVersion = target.Version
		st.MarketScope = string(target.Scope)
		st.MarketDir = target.Dir
		if err != nil {
			st.LatestVersion = ""
			st.HasUpdate = false
			st.Error = err.Error()
			return
		}
		st.LatestVersion = rel.Version
		st.Error = ""
		st.HasUpdate = target.Version != "" && compareVersion(rel.Version, target.Version) > 0
	})
}

// marketInfo 返回供 /api/market/info 使用的诊断快照（只读，不触发网络）。
func (m *UpdateManager) marketInfo() map[string]interface{} {
	target := m.resolveMarketTarget()
	st := m.getStatus(updateKindMarket)
	updatable := target.Scope == marketScopeServer && target.Version != "" &&
		st.LatestVersion != "" && compareVersion(st.LatestVersion, target.Version) > 0
	return map[string]interface{}{
		"ok":        true,
		"scope":     string(target.Scope),
		"dir":       target.Dir,
		"version":   target.Version,
		"latest":    st.LatestVersion,
		"updatable": updatable,
		"reason":    target.Reason,
		"error":     st.Error,
	}
}

// --- 下载与完整性校验 ---

// downloadMarketTarball 下载 registry 上的市场 tarball 并校验完整性，返回文件大小。
//
// 市场走**独立下载策略**（`downloadPlanFor(updateKindMarket)`）：只直连、不走代理，
// 失败重试，且不支持断点续传与暂停 —— 包只有几百 KB，registry 通常也不需要代理，
// 续传/暂停带来的状态复杂度不值得。
func (m *UpdateManager) downloadMarketTarball(rel *marketRelease, dest string,
	progress func(downloaded, total int64), ctrl *downloadControl, plan downloadPlan) (int64, error) {
	if rel.Integrity == "" && rel.Shasum == "" {
		return 0, fmt.Errorf("registry（%s）未提供 integrity/shasum，无法校验下载字节，拒绝安装", rel.Registry)
	}
	n, err := m.downloadToFile(rel.Tarball, dest, progress, ctrl, plan)
	if err != nil {
		return n, err
	}
	if err := verifyFileIntegrity(dest, rel.Integrity, rel.Shasum); err != nil {
		// 字节与元数据不符：这份文件没有任何保留价值，删掉让下次从零开始。
		os.Remove(dest)
		return 0, fmt.Errorf("市场包完整性校验失败（来源 %s）: %w", rel.Registry, err)
	}
	return n, nil
}

// verifyFileIntegrity 校验文件哈希。优先 dist.integrity（SRI，形如 sha512-<base64>），
// 退 dist.shasum（sha1 十六进制）。两者都为空视为校验不通过（由调用方决定是否拒绝）。
func verifyFileIntegrity(path, integrity, shasum string) error {
	if integrity != "" {
		algo, encoded, ok := strings.Cut(integrity, "-")
		if !ok {
			algo, encoded = "sha512", integrity
		}
		sum, err := fileHash(path, strings.ToLower(algo))
		if err != nil {
			return err
		}
		if base64.StdEncoding.EncodeToString(sum) != encoded {
			return fmt.Errorf("%s 摘要不匹配", algo)
		}
		return nil
	}
	if shasum != "" {
		sum, err := fileHash(path, "sha1")
		if err != nil {
			return err
		}
		if !strings.EqualFold(hex.EncodeToString(sum), shasum) {
			return fmt.Errorf("sha1 摘要不匹配")
		}
	}
	return nil
}

// fileHash 计算文件的指定算法摘要。
func fileHash(path, algo string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var h hash.Hash
	switch algo {
	case "sha1":
		h = sha1.New()
	case "sha256":
		h = sha256.New()
	case "sha512":
		h = sha512.New()
	default:
		return nil, fmt.Errorf("不支持的摘要算法 %q", algo)
	}
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// --- 包内容校验 ---

// marketPackageDir 从解压目录里定位 npm 包的根（npm tarball 恒有 package/ 前缀，
// 但保留「内容直接在根」的兼容分支，避免上游打包方式变化后整条链路失效）。
func marketPackageDir(extractDir string) (string, error) {
	nested := filepath.Join(extractDir, "package")
	if _, ok := readMarketManifest(nested); ok {
		return nested, nil
	}
	if _, ok := readMarketManifest(extractDir); ok {
		return extractDir, nil
	}
	// 兜底：递归一层找 package.json（解压目录结构异常时仍给一次机会）。
	var found string
	entries, err := os.ReadDir(extractDir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			cand := filepath.Join(extractDir, e.Name())
			if _, ok := readMarketManifest(cand); ok {
				found = cand
				break
			}
		}
	}
	if found == "" {
		return "", fmt.Errorf("更新包里未找到 %s 的 package.json", marketPackageName)
	}
	return found, nil
}

// marketManifest 是校验阶段需要的 package.json 字段子集。
type marketManifest struct {
	Name         string                     `json:"name"`
	Version      string                     `json:"version"`
	Main         string                     `json:"main"`
	Dependencies map[string]string          `json:"dependencies"`
	Exports      map[string]json.RawMessage `json:"exports"`
	DSH          struct {
		Bundle struct {
			Patch string `json:"patch"`
		} `json:"bundle"`
	} `json:"dsh"`
}

// validateMarketPackage 校验解压出来的新版本确实是「一份可作为 web bundle 装载的
// dshmarket」。任何一条不满足都拒绝安装 —— 换掉的是 dsh 启动时要装载的 bundle，
// 坏文件会让 dsh 起不来（虽然还有自动回滚兜底，但不该等到那一步）。
func validateMarketPackage(dir, wantVersion string) (*marketManifest, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return nil, fmt.Errorf("读取新版本 package.json 失败: %w", err)
	}
	var doc marketManifest
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("解析新版本 package.json 失败: %w", err)
	}
	if doc.Name != marketPackageName {
		return nil, fmt.Errorf("更新包名不符：期望 %s，实际 %q", marketPackageName, doc.Name)
	}
	if wantVersion != "" && doc.Version != wantVersion {
		return nil, fmt.Errorf("更新包版本不符：期望 %s，实际 %s", wantVersion, doc.Version)
	}
	if _, err := os.Stat(filepath.Join(dir, "lib")); err != nil {
		// lib/ 是 dshmarket 的发布产物目录（package.json 的 main 指向 lib/index.js）；
		// 缺失说明拿到的是源码包或残缺包。
		return nil, fmt.Errorf("更新包缺少 lib/ 目录（疑似源码包或残缺包）")
	}
	if doc.Main == "" {
		return nil, fmt.Errorf("更新包 package.json 缺少 main 字段")
	}
	if doc.DSH.Bundle.Patch == "" {
		return nil, fmt.Errorf("更新包未声明 dsh.bundle.patch，无法作为 web bundle 装载")
	}
	clientExport, ok := doc.Exports["./client"]
	if !ok || len(clientExport) == 0 || string(clientExport) == "null" {
		return nil, fmt.Errorf("更新包未声明 exports[\"./client\"]，面板将无法加载")
	}
	return &doc, nil
}

// marketUnresolvedDeps 检查新版本声明的运行时依赖能否在目标位置解析。
//
// 只检查非 @deepseek-ai/* 的依赖：那一段命名空间由宿主（dsh 自己）提供，不是要下载
// 的包。查找链与 Node 一致 —— 从目标目录自身逐级向上找 node_modules。
// 返回未解析到的依赖名（按名字排序，便于报错信息稳定）。
func marketUnresolvedDeps(doc *marketManifest, targetDir string) []string {
	if doc == nil || len(doc.Dependencies) == 0 {
		return nil
	}
	var missing []string
	for name := range doc.Dependencies {
		if strings.HasPrefix(name, "@deepseek-ai/") {
			continue
		}
		found := false
		for dir := filepath.Clean(targetDir); ; {
			if _, err := os.Stat(filepath.Join(dir, "node_modules", name, "package.json")); err == nil {
				found = true
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
		if !found {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

// --- 安装（停 dsh → 备份 → 原子替换 → 拉起 dsh → 就绪判定/回滚） ---

// installMarket 执行市场就地更新。extractDir 是 installUpdate 已解压好的更新包目录。
//
// 顺序（用户可见的语义：先提示，安装阶段停 dsh，更新完自动拉起并换 token）：
//  1. 校验新包 + 依赖可解析（不改盘，失败不留停机时间）
//  2. 停 dsh 并等端口释放（运行中替换会让两半版本错配）
//  3. 备份当前目录（market-<旧版本>-<时间戳>.tar.gz，可手动回滚）
//  4. staging + rename 原子替换
//  5. 拉起 dsh（startDshCaptured：异步 WaitToken + ExchangeToken）
//  6. 等就绪；起不来就换回旧目录、重新拉起，并把错误抛给前端
func (m *UpdateManager) installMarket(p *PendingUpdate, extractDir string) error {
	// 先复核下载阶段校验过的完整性值：两步之间文件可能被替换或截断。
	if p.Integrity != "" || p.Shasum != "" {
		if err := verifyFileIntegrity(p.PkgPath, p.Integrity, p.Shasum); err != nil {
			return fmt.Errorf("待安装的市场包完整性校验失败: %w", err)
		}
	}

	target := m.resolveMarketTarget()
	if target.Scope != marketScopeServer {
		return fmt.Errorf("当前市场由 %s 提供（%s），控制台不管理这份安装", target.Scope, target.Reason)
	}
	if fi, err := os.Stat(target.Dir); err != nil || !fi.IsDir() {
		return fmt.Errorf("市场安装目录不可用: %s", target.Dir)
	}

	srcDir, err := marketPackageDir(extractDir)
	if err != nil {
		return err
	}
	doc, err := validateMarketPackage(srcDir, p.Version)
	if err != nil {
		return err
	}
	if missing := marketUnresolvedDeps(doc, target.Dir); len(missing) > 0 {
		return fmt.Errorf("新版本依赖在目标位置无法解析：%s（拒绝安装，避免 dsh 装载失败）",
			strings.Join(missing, ", "))
	}

	oldVersion := target.Version
	logger().Printf("[market] 开始更新市场 %s → %s（目录 %s）", oldVersion, p.Version, target.Dir)

	// 2) 停 dsh。
	if err := marketStopDshFn(m); err != nil {
		// 与 applyServer 一致：停止失败也继续尝试，但后面必须确认它真的停了。
		logger().Printf("[market] 停止 dsh 服务失败（继续尝试替换）: %v", err)
	}
	marketPortFreeFn(m, 30*time.Second)

	// 3)(4) 备份 + 原子替换。
	rollback, cleanup, err := m.swapMarketDir(target.Dir, srcDir, oldVersion)
	if err != nil {
		m.marketRestartDsh()
		return err
	}

	// 5) 拉起 dsh + 换 token。
	m.marketRestartDsh()

	// 6) 等就绪（上限 marketReadyTimeout，进程退出立即判定），失败即回滚。
	switch result := marketReadyFn(m, marketReadyTimeout); result {
	case marketWaitReady:
		// 成功：走下面的收尾。
	case marketWaitExited:
		logger().Printf("[market] 新版本 %s 启动后进程退出，回滚到 %s", p.Version, oldVersion)
		if rbErr := rollback(); rbErr != nil {
			return fmt.Errorf("市场更新失败且回滚失败（目录已损坏，请手动处理 %s）: %v", target.Dir, rbErr)
		}
		m.marketRestartDsh()
		return fmt.Errorf("新版本 %s 启动后立即退出，已回滚到 %s；请从市场面板或控制台日志确认具体原因", p.Version, oldVersion)
	default:
		logger().Printf("[market] 新版本 %s 启动后 %s 内未监听端口，回滚到 %s", p.Version, marketReadyTimeout, oldVersion)
		if rbErr := rollback(); rbErr != nil {
			return fmt.Errorf("市场更新失败且回滚失败（目录已损坏，请手动处理 %s）: %v", target.Dir, rbErr)
		}
		m.marketRestartDsh()
		return fmt.Errorf("新版本 %s 在 %s 内未就绪，已回滚到 %s；请从市场面板或控制台日志确认具体原因", p.Version, marketReadyTimeout, oldVersion)
	}

	cleanup()
	m.clearPending()
	logger().Printf("[market] 市场已更新到 %s（旧版本 %s，备份在 %s/）", p.Version, oldVersion, m.backupDir())
	return nil
}

// waitMarketDsh 等新版本 dsh 起来并返回三种结果（见 marketWaitResult）。
//
// 与「一路轮询端口直到超时」的差别：
//   - 进程已经退出就立刻返回 marketWaitExited：坏 bundle 引起的启动失败通常 1 秒内
//     就能判定，不该让用户对着弹窗等满上限；
//   - 端口开放后再等 marketReadySettle 复核：dsh 是「先监听、再装配插件树」，
//     「先开放端口、再装配失败退出」是真实现象，不复核就会把失败当成功。
//
// 实测参考：本机 dsh 从进程启动到插件树装配完成约 2.4 秒（端口开放更早）。
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

// marketRestartDsh 拉起 dsh 服务并（异步）换取会话 token。
// 先确认没有残留进程：dsh.Stop 失败时 Start 会以「already running」直接报错。
func (m *UpdateManager) marketRestartDsh() {
	if m.dsh.Running() {
		if err := marketStopDshFn(m); err != nil {
			logger().Printf("[market] 拉起前停止残留 dsh 失败: %v", err)
		}
		marketPortFreeFn(m, 30*time.Second)
	}
	if err := marketStartDshFn(m); err != nil {
		logger().Printf("[market] 启动 dsh 服务失败: %v", err)
	}
}

// sweepMarketDebris 清理上次安装可能留下的中间目录（进程在两次 rename 之间被杀的
// 情况）。安全性：
//   - staging（`.staging-*`）只是替换过程的中间产物，任何情况下都可以删；
//   - old（`.old-*`）只在**目标目录存在且确实是一份 dshmarket** 时才删 —— 否则它
//     可能是这份安装唯一剩下的副本。
func sweepMarketDebris(targetDir string) {
	parent := filepath.Dir(targetDir)
	base := filepath.Base(targetDir)
	targetOK := false
	if _, ok := readMarketManifest(targetDir); ok {
		targetOK = true
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		switch {
		case strings.HasPrefix(name, base+marketStagingSuffix):
			os.RemoveAll(filepath.Join(parent, name))
		case targetOK && strings.HasPrefix(name, base+marketOldSuffix):
			os.RemoveAll(filepath.Join(parent, name))
		}
	}
}

// swapMarketDir 备份当前目录，并用 srcDir 原子替换它。
//
// 返回的 rollback 把旧目录换回去（rename 回退，不重新解包，快且精确）；
// 仅当旧目录已不在时才退化为从备份包解压。cleanup 删除旧目录，安装成功后调用。
// staging 建在目标同级目录：跨文件系统的 rename 会 EXDEV，/tmp 不可用。
func (m *UpdateManager) swapMarketDir(targetDir, srcDir, oldVersion string) (func() error, func(), error) {
	stamp := time.Now().Format("20060102150405")

	// 备份文件名必须是 market- 前缀（见文件头第 5 条）。版本号可能为空时兜一手，
	// 避免出现 "market--<ts>.tar.gz" 这种读不出类型的名字。
	if oldVersion == "" {
		oldVersion = "unknown"
	}
	backupPath := filepath.Join(m.backupDir(),
		fmt.Sprintf("%s%s-%s.tar.gz", marketBackupPrefix, oldVersion, stamp))
	if err := tgzDir(targetDir, backupPath); err != nil {
		return nil, nil, fmt.Errorf("备份当前市场目录失败: %w", err)
	}

	staging := targetDir + marketStagingSuffix + stamp
	oldDir := targetDir + marketOldSuffix + stamp
	sweepMarketDebris(targetDir)
	if err := copyDir(srcDir, staging); err != nil {
		os.RemoveAll(staging)
		return nil, nil, fmt.Errorf("准备新版本目录失败: %w", err)
	}
	// copyDir 保留源权限；npm tarball 的目录权限通常是 0755，但保险起见显式放开：
	// 目录不可读会让 dsh 在下一轮启动时直接失败。
	if err := chmodTreeDirs(staging); err != nil {
		os.RemoveAll(staging)
		return nil, nil, err
	}

	if err := os.Rename(targetDir, oldDir); err != nil {
		os.RemoveAll(staging)
		return nil, nil, fmt.Errorf("移出旧版本目录失败: %w", err)
	}
	if err := os.Rename(staging, targetDir); err != nil {
		// 放回旧目录，保持现场不变。
		if rbErr := os.Rename(oldDir, targetDir); rbErr != nil {
			logger().Printf("[market] 严重：旧目录放回失败 %v（旧目录仍在 %s）", rbErr, oldDir)
		}
		os.RemoveAll(staging)
		return nil, nil, fmt.Errorf("放入新版本目录失败: %w", err)
	}
	logger().Printf("[market] 目录已替换：%s（备份 %s，旧目录 %s）", targetDir, backupPath, oldDir)

	restored := false
	rollback := func() error {
		if restored {
			return nil
		}
		restored = true
		if err := os.RemoveAll(targetDir); err != nil {
			return fmt.Errorf("删除新版本目录失败: %w", err)
		}
		if _, err := os.Stat(oldDir); err == nil {
			if err := os.Rename(oldDir, targetDir); err == nil {
				return nil
			}
		}
		// 旧目录不在了（被清理或 rename 失败）：从备份包解压回去。
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return err
		}
		if err := extractTarGz(backupPath, targetDir); err != nil {
			return fmt.Errorf("从备份恢复失败: %w", err)
		}
		return nil
	}
	cleanup := func() {
		os.RemoveAll(oldDir)
	}
	return rollback, cleanup, nil
}

// chmodTreeDirs 把目录树里的目录权限放开到 0755（文件不动），避免 copyDir 从
// 权限异常的源（如 0700 的 staging、tar 里少见的收紧权限）带出不可读目录。
func chmodTreeDirs(root string) error {
	return filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := os.Chmod(p, 0755); err != nil {
				return err
			}
		}
		return nil
	})
}
