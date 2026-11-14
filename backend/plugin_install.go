package main

// plugin_install.go —— 插件页的「安装插件」。
//
// 官方安装入口是 `dsh plugin --profile web add <规格>`（规格原样转给 pnpm），控制台因此
// 复用这条命令，只多做三件事：
//   1. 把用户能写出来的几种写法归一化成 pnpm 认得的安装规格：包名 / 包名@版本 /
//      Git 仓库地址 / 本地绝对路径（目录或 .tgz）/ 完整的 `dsh plugin add …` 命令；
//   2. 校验：规格不合法绝不进 argv（`-` 开头 = 选项注入），镜像源只用设置页选中的
//      那一个（与市场、dsh 版本安装同一口径，命令行里带来的 --registry 一律拒绝）；
//   3. 异步执行并回写状态 —— pnpm 安装可能跑几十秒到几分钟，同步请求会被平台网关掐断，
//      用户会看到「失败」而命令其实还在跑（与 dsh 数据备份同一条边界，见 update.go）。
//
// 安装成功后**不自动重启 dsh**：重启会掐掉用户正在跑的会话。与插件页的启停开关一致，
// 界面给一条「重启生效」提示（新装的 bundle 需要 dsh 重新装配）。

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// pluginInstallSpecMaxLen 是安装规格的长度上限：正常包名/路径远小于它，更长的输入
	// 基本是误粘贴（整段文档、HTML），提前拒掉比让 pnpm 猜更有用。
	pluginInstallSpecMaxLen = 512
	// pluginInstallTimeout 与市场的 `add` 同一档：pnpm 要下载 tarball、跑 prepare、
	// 解析依赖，给 10 分钟；同时它也是「卡死的安装最多占住 applying 锁多久」的上限。
	pluginInstallTimeout = 10 * time.Minute
)

// 安装规格的形状判定，对齐官方 @deepseek-ai/dsh-plugin-manager 的 install-spec.ts：
//   - 包名：小写 URL 安全段，可带 scope，首字符不能是 `.` / `_`；
//   - Git：`github:` 这类简写、`git(+xxx)://` / scp 形式、托管仓库的 https 地址；
//   - 本地：必须是绝对路径（相对路径对控制台没有意义 —— 它不在用户的 shell 里）；
//   - 网络包：只认 .tgz / .tar.gz。
var (
	installSpecNameRe     = regexp.MustCompile(`^(?:@[a-z0-9][a-z0-9._~-]*/)?[a-z0-9][a-z0-9._~-]*$`)
	installSpecGitShortRe = regexp.MustCompile(`^(?i:github|gitlab|bitbucket|gist):\S+$`)
	installSpecGitURLRe   = regexp.MustCompile(`^(?i:git(?:\+[a-z]+)?://\S+|git@[^:\s]+:\S+)$`)
	installSpecTarballRe  = regexp.MustCompile(`(?i)^\S+\.(?:tgz|tar\.gz)(?:#\S*)?$`)
	// 托管仓库的 https 地址：host/owner/repo，可带 .git 与 #片段。
	installSpecRepoRe = regexp.MustCompile(`(?i)^https?://[^/\s]+/[^/\s]+/[^/#\s]+(?:\.git)?(?:#\S*)?$`)
)

// PluginInstallStatus 是 GET /api/plugins/install/status 的载荷，也是弹窗里进度/结果的
// 唯一来源（前端关掉弹窗再打开、甚至刷新页面，都能接着这份状态显示）。
type PluginInstallStatus struct {
	// Running 表示后台任务仍在跑；由 tracker 的 active 派生，不落进 st。
	Running bool `json:"running"`
	// Phase：installing（进行中）/ done（成功）/ failed（失败）。
	Phase   string `json:"phase,omitempty"`
	Spec    string `json:"spec,omitempty"`
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	// Restart 表示需要重启 dsh 服务新装的插件才会生效（bundle / 客户端插件 / 原生依赖）。
	Restart    bool   `json:"restart,omitempty"`
	Message    string `json:"message,omitempty"`
	MessageRef *uiMsg `json:"messageRef,omitempty"`
	Error      string `json:"error,omitempty"`
	ErrorRef   *uiMsg `json:"errorRef,omitempty"`
}

// pluginInstallTracker 是「同一时刻只允许一次插件安装」的状态机。
//
// 状态放包级变量而不是 AdminMux 字段：控制台进程只有一个，且这样测试里裸构造的
// &AdminMux{} 也不会漏初始化（与 update.go 的 dshBackup 同一个理由）。
type pluginInstallTracker struct {
	mu     sync.Mutex
	st     PluginInstallStatus
	active bool
}

var pluginInstall pluginInstallTracker

// status 返回当前状态快照（Running 由 active 派生：开始前各字段都是零值，
// 不能拿 Phase 猜）。
func (t *pluginInstallTracker) status() PluginInstallStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.st
	st.Running = t.active
	return st
}

// begin 重置状态并进入「安装中」；已有任务在跑时回忙错误（HTTP 409）。
func (t *pluginInstallTracker) begin(spec, name string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.active {
		return busyf("err_plugin_install_running", "已有插件正在安装，请等它结束后再试")
	}
	t.active = true
	t.st = PluginInstallStatus{Phase: "installing", Spec: spec, Name: name}
	// 界面进度文案给包名/规格本身；日志另有英文行（见 runPluginInstall）。
	setMsgFields(&t.st.Message, &t.st.MessageRef, "msg_plugin_install_started",
		"正在安装 %s", "pkg", firstNonEmpty(name, spec))
	return nil
}

// succeed 记录安装成功并落定终态。
func (t *pluginInstallTracker) succeed(name, version string, restart bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active = false
	t.st.Phase = "done"
	t.st.Name = name
	t.st.Version = version
	t.st.Restart = restart
	setErrFields(&t.st.Error, &t.st.ErrorRef, nil)
	setMsgFields(&t.st.Message, &t.st.MessageRef, "msg_plugin_install_done",
		"已安装 %s", "pkg", firstNonEmpty(name, t.st.Spec))
}

// fail 记录安装失败（错误已带 code + 参数，前端按语言翻译）。
func (t *pluginInstallTracker) fail(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active = false
	t.st.Phase = "failed"
	setErrFields(&t.st.Error, &t.st.ErrorRef, err)
	setMsgFields(&t.st.Message, &t.st.MessageRef, "", "")
}

// reset 只给测试用：把状态清回「从没装过」，避免用例之间互相影响。
func (t *pluginInstallTracker) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active = false
	t.st = PluginInstallStatus{}
}

type installPluginReq struct {
	Spec string `json:"spec"`
}

// handleInstallPlugin 校验安装规格并把它交给后台任务执行。
func (m *AdminMux) handleInstallPlugin(w http.ResponseWriter, r *http.Request) {
	var body installPluginReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErrOperation(w, uiErr("err_plugin_spec_empty", "请输入要安装的插件"))
		return
	}
	spec, name, err := normalizePluginInstallInput(body.Spec)
	if err != nil {
		writeErrOperation(w, err)
		return
	}
	if m.dsh == nil {
		writeErrU(w, http.StatusInternalServerError, "err_plugin_install_failed",
			"安装失败（镜像源：%s）：%s", "mirror", selectedNpmMirror().Slug, "detail", "dsh manager is not available")
		return
	}
	// 先看是不是本功能自己已经有一次安装：这条提示比通用的「有插件操作在进行」准确。
	if pluginInstall.status().Running {
		writeErrOperation(w, busyf("err_plugin_install_running", "已有插件正在安装，请等它结束后再试"))
		return
	}
	// 有别的插件命令在跑 / profile 写锁被活进程持有：pnpm 正在重写同一个 package.json，
	// 这时再起一次 add 会静默丢掉一端的改动（与启停开关同一道忙守卫）。
	if m.pluginProfileWriteBusy() {
		writeErrOperation(w, busyf("err_plugin_install_busy", "有插件操作正在进行，请等它结束后再安装"))
		return
	}
	// 与「重置插件 / 备份恢复 / 更新 / 切换 dsh 版本」互斥：那些操作会删 profiles 或停
	// dsh，撞上就是安装白做、甚至留下陈旧写锁。拿不到锁就整次拒绝，不产生任何动作。
	if m.update != nil {
		if !m.update.applying.TryLock() {
			writeErrOperation(w, busyf("err_plugin_busy", "正在执行其它更新/插件操作，请等它结束后再重试"))
			return
		}
	}
	if err := pluginInstall.begin(spec, name); err != nil {
		if m.update != nil {
			m.update.applying.Unlock()
		}
		writeErrOperation(w, err)
		return
	}
	go m.runPluginInstall(spec, name)
	writeJSON(w, map[string]interface{}{"ok": true, "started": true, "spec": spec, "name": name})
}

// handlePluginInstallStatus 返回安装任务的当前状态（前端在安装期间轮询它）。
func (m *AdminMux) handlePluginInstallStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, struct {
		OK bool `json:"ok"`
		PluginInstallStatus
	}{true, pluginInstall.status()})
}

// runPluginInstall 是后台执行体：跑 `dsh plugin --profile web add <规格>`，成功后从
// profile 清单里认出真正装进来的包，算出要不要重启 dsh。
func (m *AdminMux) runPluginInstall(spec, specName string) {
	if m.update != nil {
		defer m.update.applying.Unlock()
	}
	profileWebDir := filepath.Join(m.dsh.effectiveHome(), ".dsh", "profiles", "web")
	// 安装前的依赖名快照：Git 仓库 / 本地目录这两种规格看不出包名，只能靠前后对比认出来。
	before, err := profileDependencyNames(profileWebDir)
	if err != nil {
		// 读不到不是致命错误（首次安装时清单可能还没建）：装完再读一次就是了。
		logWarn("[plugin] read profile dependencies before install failed: %v", err)
	}

	mirror := selectedNpmMirror()
	full := pluginInstallCommand(spec, mirror.URL)
	logInfo("[plugin] installing %s (via %s)", spec, mirror.mirrorLogName())
	out, err := m.dsh.RunPluginCommand(pluginInstallTimeout, full)
	if err != nil {
		// 没选中 dsh 版本时命令根本起不来，与「pnpm 装失败」不是一回事：给出可操作的提示
		// （概览页下载版本 + 切换），别让用户对着 "(no output)" 猜。
		if errors.Is(err, errNoDshVersion) {
			logWarn("[plugin] install %s refused: no dsh version selected", spec)
			pluginInstall.fail(uiErr("err_no_dsh_version",
				"未安装 dsh 服务：请先在控制台「概览」页的版本列表里下载一个版本，再点「切换」"))
			return
		}
		// 原始诊断（pnpm/dsh 的英文输出尾行）走 detail 参数，日志与界面都能看到。
		logError("[plugin] install %s failed: %s", spec, tailLines(out, 3))
		pluginInstall.fail(uiErr("err_plugin_install_failed",
			"安装失败（镜像源：%s）：%s；可在设置页切换 npm 镜像源后重试",
			"mirror", mirror.Slug, "detail", tailLines(out, 3)))
		return
	}

	name, version, restart := detectInstalledPlugin(profileWebDir, specName, before)
	pluginInstall.succeed(name, version, restart)
	logInfo("[plugin] installed %s (name=%s version=%s restart=%v)", spec, name, version, restart)
}

// pluginInstallCommand 组装安装命令：`dsh plugin --profile web add <规格>`。
//
// registry 只来自设置页选中的那一个源（调用方传 selectedNpmMirror().URL）；pnpm 的 add
// 支持 `--registry`，而 remove/list 不支持 —— 这里只有 add。
func pluginInstallCommand(spec, registryURL string) []string {
	full := []string{"plugin", "--profile", "web", "add", spec}
	if registryURL != "" {
		full = append(full, "--registry="+registryURL)
	}
	return full
}

// detectInstalledPlugin 认出这次安装真正落地的包名与版本，并判断要不要重启 dsh。
//
// specName 只在「规格本身就是 npm 包」时非空；Git / 本地目录只能靠 profile 依赖的
// 前后差异（新增项）来认。认不出来时返回空名字 —— 界面仍可提示「已安装」，只是不带包名。
func detectInstalledPlugin(profileWebDir, specName string, before []string) (name, version string, restart bool) {
	name = specName
	if name == "" {
		after, err := profileDependencyNames(profileWebDir)
		if err != nil {
			logWarn("[plugin] read profile dependencies after install failed: %v", err)
			return "", "", false
		}
		name = addedDependency(before, after)
	}
	if name == "" {
		return "", "", false
	}
	meta, err := readPluginManifest(profileWebDir, name)
	if err != nil {
		// 装上了但读不到 manifest（本地目录被删、权限不足等）：不必让整次安装变成失败，
		// 只是给不出准确的版本与重启建议。
		logWarn("[plugin] read manifest of %s after install failed: %v", name, err)
		return name, "", false
	}
	version = meta.Version
	// 新装的 bundle 需要 dsh 重新装配才生效；带客户端部分或原生依赖的包同理（与插件页
	// 启停开关的判定共用 needsRestart）。
	restart = meta.declaresBundle() || needsRestart(profileWebDir, name)
	return name, version, restart
}

// profileDependencyNames 读取 profile 清单里 dependencies 的键（按声明顺序）。
func profileDependencyNames(profileWebDir string) ([]string, error) {
	manifest, err := loadProfileManifest(profileWebDir)
	if err != nil {
		return nil, err
	}
	deps, ok, err := manifest.object("dependencies")
	if err != nil || !ok {
		return nil, err
	}
	return append([]string(nil), deps.keys...), nil
}

// addedDependency 返回 after 里新出现、且 before 没有的那一个依赖名（多于一个时返回空：
// 分不清哪个是用户这次装的，宁可不猜）。
func addedDependency(before, after []string) string {
	prev := make(map[string]bool, len(before))
	for _, name := range before {
		prev[name] = true
	}
	var added []string
	for _, name := range after {
		if !prev[name] {
			added = append(added, name)
		}
	}
	if len(added) == 1 {
		return added[0]
	}
	return ""
}

// normalizePluginInstallInput 把用户输入归一化成 pnpm 能接受的安装规格。
//
// 两种输入形态：
//   - 完整的安装命令（`dsh plugin [--profile web] add <规格>`）—— 支持用户从文档里
//     整段复制；命令里的选项一律拒绝（控制台的镜像源来自设置页，不能在这里被绕过）；
//   - 单个规格（包名 / 包名@版本 / 仓库地址 / 本地绝对路径）。
//
// 返回的 name 只有在「规格就是 npm 包」时才非空。
func normalizePluginInstallInput(raw string) (spec, name string, err error) {
	input := strings.TrimSpace(raw)
	if input == "" {
		return "", "", uiErr("err_plugin_spec_empty", "请输入要安装的插件")
	}
	if len(input) > pluginInstallSpecMaxLen {
		return "", "", uiErr("err_plugin_spec_too_long", "安装规格过长（最多 %s 个字符）",
			"max", strconv.Itoa(pluginInstallSpecMaxLen))
	}
	// 多行输入基本是误粘贴（整段文档 / 命令带输出）。这里只接一行。
	if strings.ContainsAny(input, "\r\n") {
		return "", "", uiErr("err_plugin_spec_invalid", "安装规格只能是一行（请去掉换行后重试）")
	}

	tokens := splitCommandTokens(input)
	if len(tokens) >= 2 && filepath.Base(tokens[0]) == "dsh" && tokens[1] == "plugin" {
		spec, err = parsePluginAddCommand(tokens[2:])
		if err != nil {
			return "", "", err
		}
	} else {
		spec = unquoteSpec(input)
	}
	if err := validatePluginInstallSpec(spec); err != nil {
		return "", "", err
	}
	return spec, registrySpecName(spec), nil
}

// parsePluginAddCommand 解析 `dsh plugin` 之后的参数：`[--profile web] add <规格>`。
func parsePluginAddCommand(tokens []string) (string, error) {
	profile := ""
	i := 0
parseProfile:
	for i < len(tokens) {
		tok := tokens[i]
		switch {
		case tok == "--profile":
			if i+1 >= len(tokens) {
				return "", uiErr("err_plugin_cmd_profile_missing", "安装命令里的 --profile 缺少 profile 名称")
			}
			profile = tokens[i+1]
			i += 2
		case strings.HasPrefix(tok, "--profile="):
			profile = strings.TrimPrefix(tok, "--profile=")
			i++
		default:
			break parseProfile
		}
	}
	// 控制台只管理 web profile（与列表/启停/卸载同一条边界）。
	if profile != "" && profile != "web" {
		return "", uiErr("err_plugin_cmd_profile", "控制台只管理 web profile，不能安装到 %s", "profile", profile)
	}
	if i >= len(tokens) {
		return "", uiErr("err_plugin_cmd_only_add", "只支持 `dsh plugin add <规格>` 形式的安装命令")
	}
	sub := tokens[i]
	// pnpm 的 add 有三个等价写法。
	if sub != "add" && sub != "install" && sub != "i" {
		return "", uiErr("err_plugin_cmd_only_add_got",
			"只支持 `dsh plugin add <规格>` 形式的安装命令（收到 %s）", "cmd", sub)
	}
	i++
	var positional []string
	for ; i < len(tokens); i++ {
		tok := tokens[i]
		// 选项一个都不透传：`--registry` 会绕过设置页选中的镜像源，`-D` 之类会改变依赖
		// 记录方式。静默丢掉比直接说清楚更糟。
		if strings.HasPrefix(tok, "-") {
			return "", uiErr("err_plugin_cmd_option",
				"安装命令里不支持选项 %s；镜像源请在设置页选择", "option", tok)
		}
		positional = append(positional, tok)
	}
	if len(positional) == 0 {
		return "", uiErr("err_plugin_spec_empty", "请输入要安装的插件")
	}
	if len(positional) > 1 {
		return "", uiErr("err_plugin_cmd_target", "一次只能安装一个插件（收到 %s 个）",
			"count", strconv.Itoa(len(positional)))
	}
	return positional[0], nil
}

// splitCommandTokens 按 shell 习惯切分一行命令：空白分隔，单/双引号包住的部分原样保留。
//
// 只用于读取用户粘贴的安装命令，**不执行任何 shell**：切出来的每个 token 之后只会作为
// 一个 argv 元素传给 dsh，引号只是为了支持「路径里有空格」这种正常输入。
func splitCommandTokens(s string) []string {
	var out []string
	var b strings.Builder
	var quote byte
	inToken := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
				continue
			}
			b.WriteByte(c)
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			inToken = true
		case ' ', '\t':
			if inToken {
				out = append(out, b.String())
				b.Reset()
				inToken = false
			}
		default:
			b.WriteByte(c)
			inToken = true
		}
	}
	if inToken {
		out = append(out, b.String())
	}
	return out
}

// unquoteSpec 去掉整段输入外层的一对引号（`"/path/with space"` 这种）。
func unquoteSpec(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// stripLocalPrefix 去掉 pnpm 的 file: / link: 前缀，返回可能为本地路径的部分。
func stripLocalPrefix(spec string) string {
	for _, prefix := range []string{"file:", "link:"} {
		if strings.HasPrefix(spec, prefix) {
			return strings.TrimPrefix(spec, prefix)
		}
	}
	return spec
}

// validatePluginInstallSpec 校验一个安装规格。通过即代表可以安全地放进 argv。
func validatePluginInstallSpec(spec string) error {
	if spec == "" {
		return uiErr("err_plugin_spec_empty", "请输入要安装的插件")
	}
	if len(spec) > pluginInstallSpecMaxLen {
		return uiErr("err_plugin_spec_too_long", "安装规格过长（最多 %s 个字符）",
			"max", strconv.Itoa(pluginInstallSpecMaxLen))
	}
	// 规格会原样成为 `dsh plugin add <规格>` 的 argv：`--foo` 会被 pnpm 当成选项。
	if strings.HasPrefix(spec, "-") {
		return uiErr("err_plugin_spec_option", "安装规格不能以 - 开头（会被当成命令选项）")
	}
	if strings.ContainsAny(spec, "\r\n") {
		return uiErr("err_plugin_spec_invalid", "安装规格只能是一行（请去掉换行后重试）")
	}

	// 1) 本地路径（file: / link: / 绝对路径）：必须是绝对路径，且存在性可判断。
	local := stripLocalPrefix(spec)
	if local != spec || filepath.IsAbs(local) {
		if !filepath.IsAbs(local) {
			return uiErr("err_plugin_spec_relative", "本地目录必须是绝对路径（控制台无法判断相对路径指向哪里）")
		}
		return checkLocalInstallPath(local)
	}
	// 2) 相对路径：pnpm 会按自己的 cwd 解析，而控制台的 cwd 对用户没有意义，直接拒绝。
	if spec == "." || spec == ".." || strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") || strings.HasPrefix(spec, "~") {
		return uiErr("err_plugin_spec_relative", "本地目录必须是绝对路径（控制台无法判断相对路径指向哪里）")
	}
	// 3) Git 仓库：简写、git 协议、托管仓库的 https 地址。
	if installSpecGitShortRe.MatchString(spec) || installSpecGitURLRe.MatchString(spec) {
		return nil
	}
	if strings.HasPrefix(strings.ToLower(spec), "http://") || strings.HasPrefix(strings.ToLower(spec), "https://") {
		if installSpecTarballRe.MatchString(spec) || installSpecRepoRe.MatchString(spec) {
			return nil
		}
		return uiErr("err_plugin_spec_url", "URL 必须指向 Git 仓库或 .tgz 安装包")
	}
	// 4) 其余按 npm 包名 / 包名@版本处理（含 scope）。
	name, rng, ok := splitRegistrySpec(spec)
	if !ok || len(name) > 214 || !installSpecNameRe.MatchString(name) {
		return uiErr("err_plugin_spec_name",
			"无法识别的安装规格：既不是 npm 包名（可带 @版本）、仓库地址，也不是绝对路径")
	}
	if rng != "" && strings.ContainsAny(rng, " \t") {
		return uiErr("err_plugin_spec_name",
			"无法识别的安装规格：既不是 npm 包名（可带 @版本）、仓库地址，也不是绝对路径")
	}
	return nil
}

// splitRegistrySpec 把 `name` / `name@range` 拆成包名与范围（`@scope/name` 的 scope @ 不算
// 版本分隔符 —— 与官方 install-spec.ts 一样从下标 1 开始找）。
func splitRegistrySpec(spec string) (name, rng string, ok bool) {
	if spec == "" {
		return "", "", false
	}
	at := strings.Index(spec[1:], "@")
	if at < 0 {
		return spec, "", true
	}
	at++ // 转成 spec 里的绝对下标
	name, rng = spec[:at], spec[at+1:]
	if name == "" || rng == "" {
		return "", "", false
	}
	return name, rng, true
}

// registrySpecName 返回规格里的 npm 包名（不是 npm 包时为空：仓库地址 / 本地路径 /
// tarball 的包名只能等装完从 profile 清单里认，见 detectInstalledPlugin）。
func registrySpecName(spec string) string {
	local := stripLocalPrefix(spec)
	switch {
	case local != spec, filepath.IsAbs(local),
		installSpecTarballRe.MatchString(spec),
		installSpecGitShortRe.MatchString(spec),
		installSpecGitURLRe.MatchString(spec),
		strings.Contains(spec, "://"):
		return ""
	}
	name, _, ok := splitRegistrySpec(spec)
	if !ok || !installSpecNameRe.MatchString(name) {
		return ""
	}
	return name
}

// checkLocalInstallPath 校验本地路径存在；目录与 .tgz 包之外的普通文件直接拒绝。
//
// 只对「确实不存在」报错：权限不足、网络文件系统等交给 pnpm 去报它自己的错 —— 控制台
// 抢答会把一个其实能装的路径挡住（例如 fnOS 上父目录只有穿越权限的情况）。
func checkLocalInstallPath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return uiErr("err_plugin_path_missing", "本地路径不存在：%s", "path", path)
		}
		return nil
	}
	if !info.IsDir() && !installSpecTarballRe.MatchString(path) {
		return uiErr("err_plugin_path_not_dir", "本地路径既不是目录，也不是 .tgz 安装包：%s", "path", path)
	}
	return nil
}
