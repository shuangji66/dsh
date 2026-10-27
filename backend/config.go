package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv" // 新增导入
	"strings"
	"sync"
	"time"
)

// AppConfig holds the settings editable from the frontend.
type AppConfig struct {
	DshPort int `json:"dshPort"`
	// ProxyPort 是反向代理（TCP 根挂载）的监听端口，默认 3079，随配置持久化。
	// 它只决定 harness 自身监听哪个端口对外提供 dsh 界面，不再是
	// 「dsh 端口」的对偶（dsh 只监听本机，由反代转发），因此改端口只影响反代自身：
	// 保存后立即重新绑定（见 rebindProxyPort），不需要重启 dsh 或控制台。
	ProxyPort int `json:"proxyPort"`
	// ProxyEnabled 是设置页「代理dsh」开关：只影响 dsh 进程自身的出网环境变量
	// （见 dsh.go 的 buildEnv）。控制台自身的版本检测与 dsh 版本安装、插件安装都
	// 直连各自的数据源，不走这个代理。
	ProxyEnabled bool   `json:"proxyEnabled"`
	ProxyAddr    string `json:"proxyAddr"`
	// NpmRegistry 是设置页「npm 镜像源」选中的源，取值是 npmMirrors 里的 Slug
	// （aliyun / tencent / huawei / npmjs），默认 aliyun（阿里云）。
	//
	// 它决定 dsh 服务与两个受管插件（市场 / 增强插件）安装、更新、版本检测时使用
	// 哪个 registry：**只用所选源、失败即报错**，不再自动换源。空串（旧配置 / 旧版
	// 前端没提交该字段）按默认阿里云处理，非法值在保存时拒绝、加载时回退默认。
	NpmRegistry  string `json:"npmRegistry"`
	AuthEnabled  bool   `json:"authEnabled"`
	Password     string `json:"password,omitempty"`
	AuthTTLHours int    `json:"authTTLHours"` // 登录鉴权有效期（小时），默认 4
	// dsh 进程内存限制（MB），通过 NODE_OPTIONS 生效。
	// 未设置（0，首次启动或旧配置缺字段）时由 detectNodeHeapLimitMB 取当前 node
	// 自身的堆上限（`v8.getHeapStatistics().heap_size_limit` 折算为 MB）兜底，
	// 不再写死一个与机器无关的 2048。
	// DshMemAuto 为 true（默认）时由系统 node 自动分配内存，不传 NODE_OPTIONS。
	DshMemLimit int  `json:"dshMemLimit"`
	DshMemAuto  bool `json:"dshMemAuto"`
	// DshVersion 是**当前选中的 dsh 服务版本**，即 `${TRIM_PKGVAR}/server/<版本>`
	// 的那个目录名（如 "0.2.0-rc.2"）。空串表示未选中任何版本：控制台版本行显示
	// 「未安装」，dsh 也无法启动（见 server.go）。
	//
	// 这是本应用唯一的 dsh 版本来源 —— 不再有「server 包内置在 fpk 里」这一形态，
	// 也没有压缩包备份/回退：换版本 = 换一个已安装的版本目录（见 SwitchVersion）。
	DshVersion string `json:"dshVersion,omitempty"`
	// 主目录（dsh 的 HOME）**不可配置**：它恒为本应用的 shares 目录
	// （/var/apps/<AppName>/shares/<AppName> 的实际路径），见 DshManager.effectiveHome。
	// 曾经存在的 homeDir 配置项与资源页的「设置为主目录」功能已整体移除 —— 目标目录的
	// 平台权限模型（飞牛共享目录的 ACL / `system.trim_acl` 扩展属性）会让 dsh 的 profile
	// 文件对应用用户不可读，插件安装直接崩，风险不可控。
	// BrowserCompat 为浏览器兼容模式开关，默认关闭。
	//
	// 现在只管一处引擎兼容修复（只影响触屏 iOS / WebKit，桌面与 Chromium 开启无副作用）：
	// iPhone（WKWebView）上「模型 / 推理等级菜单能打开、点选项却毫无反应」—— 见 proxy.go
	// 注入脚本第 6 段：iOS 在菜单内按钮之间搬家焦点时 focusout 的 relatedTarget 为 null，
	// 而模型座位的 onBlur 守卫只接受 Node，会把菜单在 mousedown 与 click 之间关掉。该段
	// 注入读同一个开关，且只在触屏设备上武装。
	//
	// 历史：它曾经还管着一处 bundle 字节改写（非 V8 引擎上「会话历史无法加载」，修法是把
	// 原生函数格式判断里的空白折叠后再比较）。dsh 上游后来把那处判断改成了引擎无关的形式，
	// 锚点在 0.2.x 的客户端产物中零命中，改写已整体移除 —— 来龙去脉见 proxy.go 中
	// 「转发路径上不再改写 JS 产物」那段说明。
	//
	// 生效时机：开关状态由 HTML 注入（window.__DSH_BROWSER_COMPAT__，见
	// browserCompatFlagScript），而 HTML 每次刷新都回源，因此「切换开关 → 普通刷新」即可
	// 生效 —— 不再涉及 dsh 插件 bundle 的一年期 immutable 缓存（那只是当年改写 bundle
	// 字节才有的约束）。详见 README「浏览器兼容模式」一节。
	BrowserCompat bool `json:"browserCompat"`
}

// RuntimeEnv 描述进程启动时的环境（环境变量来源，含路径与凭据）。
// 注意：反向代理端口不在其中 —— 它是可变的用户配置（AppConfig.ProxyPort）。
//
// 所有「后端自己产生的文件」都统一落在 DataDir 下（见 dataDirFromEnv）：
// 日志、PID、配置文件、会话密钥、终端会话镜像与 dsh 数据备份。单独设置这些
// 路径的环境变量（HARNESS_CONFIG_FILE / HARNESS_LOG_FILE / HARNESS_PID_FILE /
// HARNESS_DSH_PID_FILE / HARNESS_QUICK_CMDS_FILE / HARNESS_SESSION_DIR /
// HARNESS_SESSION_KEY_FILE）已移除，要换位置就换 DataDir。
// 例外：pnpm 目录由 PNPM_HOME 单独指定，不在数据目录下。
type RuntimeEnv struct {
	// DataDir 是统一的后端数据目录（HARNESS_DATA_DIR，默认取平台变量 TRIM_PKGVAR）。
	DataDir      string
	ConfigFile   string // DataDir/config.json
	AdminSock    string
	AdminBaseURL string
	LogFile      string // DataDir/harness.log
	// PidFile 是 harness 控制台自身的 PID 文件路径（DataDir/harness.pid）；
	// DshPidFile 是 dsh 服务进程的 PID 文件路径（DataDir/dsh.pid）。二者用途不同：
	// 前者标识控制台进程，dsh 装插件自重启等场景下保持不变；后者跟随 dsh 实时
	// PID，dsh 停止时移除。
	PidFile      string
	DshPidFile   string
	TRIMApiToken string
	TRIMAppDest  string
	TRIMAppName  string
	Path         string
	Home         string
	// DshHome 是 dsh 的**数据根**（`$DSH_HOME`，dsh 自己解析为「显式配置 > $DSH_HOME >
	// ~/.dsh」，见 @deepseek-ai/dsh-home-paths 的 resolveDshHome）。平台启动脚本会注入它
	// （实测本机为 $HOME/.dsh），控制台里只有凭据文件按它定位（见 accountCredentialsPath）——
	// 凭据必须写在 dsh 真正读的那个文件上，否则登录会「成功但 dsh 说没登录」。
	// 其余 `<home>/.dsh/...`（profiles、备份包）仍按 Home 拼：备份包的条目名就是 `.dsh`，
	// 改它等于改备份格式，目前没有理由动。为空时调用方回退 `<Home>/.dsh`。
	DshHome       string
	PnpmHome      string
	Lang          string
	QuickCmdsFile string // 终端快捷指令持久化文件路径（DataDir/quickcmds.json）
	SessionDir    string // 终端会话临时镜像目录（DataDir/terminal-sessions，停止时整目录清除）
	// MasterKey / MasterKeyFile：快捷指令「命令内容」加密用的主密钥（见 secret.go）。
	// MasterKey 来自 HARNESS_MASTER_KEY（外部注入，优先），它不是路径而是密钥本身——
	// 「不让密钥与密文同放」靠它；MasterKeyFile 恒为 DataDir/master.key（缺失时自动生成 0600），
	// 与其它后端自产文件一样只跟着 DataDir 走（AGENTS.md 规则 2）。
	MasterKey     string
	MasterKeyFile string
	// ProxySock/ProxyBaseURL 是反代的「子路径挂载」监听：平台网关（fnOS
	// open-gateway）把 http://<fnip>:<port>/app/Harness/dsh 整段转发到该 unix
	// socket，反代剥掉前缀后再转发给 dsh，于是 dsh 前端按文档相对路径发起的
	// "<prefix>/api"、"<prefix>/plugins/..." 都能正确落回挂载目录
	// （dsh 0.1.7-alpha.1 起其前端产物全部使用文档相对路径，无需 baseurl 配置）。
	// ProxySock 取值为空或 "off" 时不开这条监听，见 startProxySocket。
	ProxySock    string
	ProxyBaseURL string
}

var (
	cfgLock sync.RWMutex
	cfg     AppConfig
)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// dataDirFromEnv 解析统一的后端数据目录：HARNESS_DATA_DIR 优先，其次平台变量
// TRIM_PKGVAR；两者都没有（开发/测试环境）时退回当前工作目录 —— 派生出的文件名
// 于是仍是相对路径，与历史行为一致。
//
// 每个调用都会重读环境变量（单测用 t.Setenv 注入临时目录），因此不要在进程中途
// 改这两个变量：路径会跟着漂移。
func dataDirFromEnv() string {
	if v := os.Getenv("HARNESS_DATA_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("TRIM_PKGVAR"); v != "" {
		return v
	}
	return "."
}

// dataPath 返回数据目录下的文件/子目录路径。给「拿不到 RuntimeEnv」的路径解析用
// （包级默认值、单测构造的空 RuntimeEnv）；正常路径请直接用 RuntimeEnv 里已解析
// 好的字段。
func dataPath(elem ...string) string {
	return filepath.Join(append([]string{dataDirFromEnv()}, elem...)...)
}

func loadRuntimeEnv() RuntimeEnv {
	appDest := os.Getenv("TRIM_APPDEST")
	appName := os.Getenv("TRIM_APPNAME")
	// 反代 unix socket 的默认路径：平台应用目录下的 dsh.sock（即
	// /var/apps/Harness/target/dsh.sock）。非平台环境（TRIM_APPDEST 缺失）沿用
	// 同一绝对默认值，目录建不出来时 startProxySocket 只记日志并跳过这条监听。
	proxySock := "/var/apps/Harness/target/dsh.sock"
	if appDest != "" {
		proxySock = filepath.Join(appDest, "dsh.sock")
	}
	dataDir := dataDirFromEnv()
	home := os.Getenv("HOME")
	// dsh 数据根：$DSH_HOME 优先，否则 <HOME>/.dsh；主目录也拿不到时留空（由调用方处理，
	// 绝不能拼出 ".dsh" 这种**相对**路径 —— 那会落到进程当前工作目录）。
	dshHome := os.Getenv("DSH_HOME")
	if dshHome == "" && home != "" {
		dshHome = filepath.Join(home, ".dsh")
	}
	// 快捷指令与终端会话目录都属于终端功能：无终端构建（-tags noterminal）里两者都是
	// 空串，启动/退出流程因此不会创建或删除会话目录（见 terminal_enabled.go /
	// terminal_disabled.go）。
	quickCmdsFile := quickCmdsFileFor(dataDir)
	sessionDir := sessionDirFor(dataDir)
	return RuntimeEnv{
		DataDir:       dataDir,
		ConfigFile:    filepath.Join(dataDir, "config.json"),
		AdminSock:     envOr("HARNESS_ADMIN_SOCK", filepath.Join(appDest, "app.sock")),
		AdminBaseURL:  envOr("HARNESS_ADMIN_BASEURL", "/app/Harness"),
		LogFile:       filepath.Join(dataDir, "harness.log"),
		PidFile:       filepath.Join(dataDir, "harness.pid"),
		DshPidFile:    filepath.Join(dataDir, "dsh.pid"),
		TRIMApiToken:  os.Getenv("TRIM_API_TOKEN"),
		TRIMAppDest:   appDest,
		TRIMAppName:   appName,
		Path:          os.Getenv("PATH"),
		Home:          home,
		DshHome:       dshHome,
		PnpmHome:      os.Getenv("PNPM_HOME"),
		Lang:          os.Getenv("TRIM_SYS_LANGUAGE"),
		QuickCmdsFile: quickCmdsFile,
		SessionDir:    sessionDir,
		// 主密钥：优先 HARNESS_MASTER_KEY（外部注入的 32 字节 base64/hex），
		// 否则用数据目录里的 master.key（缺失时自动生成 0600）。
		MasterKey:     os.Getenv("HARNESS_MASTER_KEY"),
		MasterKeyFile: filepath.Join(dataDir, masterKeyFileName),
		ProxySock:     envOr("HARNESS_PROXY_SOCK", proxySock),
		// 默认挂在控制台 baseurl（/app/Harness）之下的一层：控制台 SPA 走 admin
		// socket 占据 /app/Harness，dsh GUI 走 dsh.sock 占据 /app/Harness/dsh，
		// 两者互不抢路径。网关需按更长的前缀优先匹配，否则 dsh 的流量会被控制台
		// 那条规则截走。
		ProxyBaseURL: envOr("HARNESS_PROXY_BASEURL", "/app/Harness/dsh"),
	}
}

func defaultConfig() AppConfig {
	dshPort := atoi(envOr("dsh_port", envOr("TARGET_PORT", "13080")))
	authEnabled := envBool(envOr("auth_mode", envOr("PROXY_AUTH", "true")))
	proxyEnabled := os.Getenv("proxy_mode") == "1"
	return AppConfig{
		DshPort:      dshPort,
		ProxyPort:    defaultProxyPort,
		ProxyEnabled: proxyEnabled,
		ProxyAddr:    envOr("proxy_addr", "http://127.0.0.1:7890"),
		// npm 镜像源默认阿里云（见 server.go 的 npmMirror / npmRegistryDefault）。
		NpmRegistry:  npmRegistryDefault,
		AuthEnabled:  authEnabled,
		Password:     os.Getenv("password"),
		AuthTTLHours: envOrInt("auth_ttl_hours", 4),
		// DshMemLimit 不在此写死：留 0 表示「未设置」，由 LoadConfig 按当前 node
		// 的实际堆上限补齐（见 detectNodeHeapLimitMB）。
		DshMemAuto: true,
	}
}

// envOrInt reads an integer env var, falling back to def if empty/invalid.
func envOrInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

// defaultProxyPort 是反代 TCP 监听端口的默认值（设置页可改，见 AppConfig.ProxyPort）。
// 它刻意不再读 PROXY_PORT 环境变量：端口属于用户配置，随 config.json 持久化，
// 旧环境变量（以及旧默认 13079）不再生效。
const defaultProxyPort = 3079

// 用户可配置端口（反代端口 / dsh 端口）的取值范围：1025..65535。
// 下限 1025 是刻意的 —— 应用以自身 uid（非 root）运行，绑定 <1025 的特权端口需要
// CAP_NET_BIND_SERVICE，注定失败；上限 65535 是 TCP 端口上限。两个端口必须落在
// 同一范围内并彼此不同（相同会争抢同一个 TCP 端口），校验入口见 handleSaveSettings。
const (
	minListenPort = 1025
	maxListenPort = 65535
)

// validListenPort 校验端口是否落在可绑定的用户端口范围内（1025..65535）。
func validListenPort(p int) bool { return p >= minListenPort && p <= maxListenPort }

// normalizeProxyPort 把非法/空值归一化为默认端口。
func normalizeProxyPort(p int) int {
	if !validListenPort(p) {
		return defaultProxyPort
	}
	return p
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func envBool(s string) bool {
	switch s {
	case "", "false", "0", "no", "off":
		return false
	}
	return true
}

// --- node 堆上限探测（栈内存默认值 / 「自动设置」展示值） ---

// nodeHeapLimitProbe 打印 node 在没有任何 --max-old-space-size 覆盖时的堆上限（字节）。
// 等价于 `node -e "console.log(v8.getHeapStatistics().heap_size_limit)"`；
// 这里写 require('v8') 而不是裸的 v8 全局，兼容更老的 node（裸全局只在 REPL 与较新
// 版本的 -e 里存在）。
const nodeHeapLimitProbe = "console.log(require('v8').getHeapStatistics().heap_size_limit)"

var (
	nodeHeapMu       sync.Mutex
	nodeHeapCachedMB int
	nodeHeapCachedOK bool
)

// detectNodeHeapLimitMB 探测 PATH 中 node 的默认堆上限并折算为 MB（四舍五入）。
// 控制台只使用平台通过环境变量（PATH）提供的 node24，不再有版本切换，因此结果只需
// 缓存一份：同一台机器上 node 的默认上限只取决于该 node 与可用内存，不必每次拉设置
// 都起一个 node 进程。探测失败（node 不存在 / 输出无法解析 / 超时）返回 0，由调用方
// 决定回退；失败结果不缓存，留待下次重试。
func detectNodeHeapLimitMB() int {
	nodeHeapMu.Lock()
	if nodeHeapCachedOK {
		mb := nodeHeapCachedMB
		nodeHeapMu.Unlock()
		return mb
	}
	nodeHeapMu.Unlock()
	mb := probeNodeHeapLimitMB()
	if mb > 0 {
		nodeHeapMu.Lock()
		nodeHeapCachedMB = mb
		nodeHeapCachedOK = true
		nodeHeapMu.Unlock()
	}
	return mb
}

// probeNodeHeapLimitMB 起一个 node 进程做探测，5 秒超时。
func probeNodeHeapLimitMB() int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "-e", nodeHeapLimitProbe)
	out, err := cmd.Output()
	if err != nil {
		logWarn("[node] failed to probe default heap limit: %v", err)
		return 0
	}
	s := strings.TrimSpace(string(out))
	bytes, err := strconv.ParseInt(s, 10, 64)
	if err != nil || bytes <= 0 {
		logWarn("[node] failed to parse default heap limit: %q", s)
		return 0
	}
	return bytesToMB(bytes)
}

// bytesToMB 把字节数折算为 MB（四舍五入）。node 的 heap_size_limit 通常不是整数 MB
// （如 4344760320 B ≈ 4143.5 MB），取最接近的整数便于展示，也让「关闭自动设置」时
// 回填的值尽量贴近 node 默认。
func bytesToMB(b int64) int {
	const mb = 1 << 20
	return int((b + mb/2) / mb)
}

// defaultDshMemLimit 在内存限制未设置（cur <= 0）时返回探测到的 node 堆上限（MB）；
// 已设置则原样返回。探测不到（宿主机没有 node）时同样原样返回 0，语义是「交给 node
// 自己决定」，buildEnv 会因此不下发 NODE_OPTIONS。
func defaultDshMemLimit(cur int) int {
	if cur > 0 {
		return cur
	}
	if mb := detectNodeHeapLimitMB(); mb > 0 {
		return mb
	}
	return cur
}

// minDshMemLimitMB 是「手动设置」时 node 堆内存上限（dshMemLimit）的下限（MB）：
// 低于它整次保存被拒绝（handleSaveSettings），前端同阈值给红字提示并阻止提交
// （见 frontend/src/stores/settings.ts 的 MEM_LIMIT_MIN_MB）。这个量级下 dsh 起来
// 就会频繁 GC、随时 OOM。仅在「自动设置」（DshMemAuto）关闭时校验 —— 自动模式不
// 使用这个持久化值，它可能是历史遗留的小数值。
const minDshMemLimitMB = 500

// LoadConfig reads the JSON config file; falls back to defaults if missing.
func LoadConfig(renv *RuntimeEnv) AppConfig {
	def := defaultConfig()
	if c := loadJSONFile(renv.ConfigFile, &def); c != nil {
		return *c
	}
	// 没有配置文件（首次启动）：内存限制同样按当前 node 的堆上限补齐。
	def.DshMemLimit = defaultDshMemLimit(def.DshMemLimit)
	return def
}

func loadJSONFile(path string, def *AppConfig) *AppConfig {
	data, err := os.ReadFile(path)
	if err != nil {
		return def
	}
	// 先用默认值播种，再反序列化覆盖：文件里缺失的字段保留默认值。
	// 例如旧配置没有 dshMemAuto 时仍保持默认 true（由系统 node 自动分配内存）。
	v := *def
	if err := json.Unmarshal(data, &v); err != nil {
		// 解析失败不能静默：调用方会退回默认配置（AuthEnabled 默认 true 但密码通常
		// 来自环境变量、可能为空），用户看到的是「设置全没了」。至少让日志里有现场。
		logError("[config] failed to parse %s, falling back to defaults: %v", path, err)
		return def
	}
	// 旧配置文件中可能没有这些字段，回退到默认值
	if v.AuthTTLHours <= 0 {
		v.AuthTTLHours = 4
	}
	// 内存限制缺失/为 0（旧配置没有该字段）时，用 PATH 中 node 自身的堆上限兜底，
	// 不再写死 2048 —— 不同机器上的默认上限并不相同。
	v.DshMemLimit = defaultDshMemLimit(v.DshMemLimit)
	// 旧配置文件（反代端口还来自 PROXY_PORT 环境变量）没有 proxyPort 字段，
	// 回退到默认 3079 并随下次保存落盘。
	v.ProxyPort = normalizeProxyPort(v.ProxyPort)
	// npm 镜像源：字段缺失（旧配置）时保留默认的阿里云；存了未知值则回退默认并留一条
	// WARN（用户多半是手改过 config.json 或从旧版本降级）。
	if v.NpmRegistry == "" {
		v.NpmRegistry = npmRegistryDefault
	} else if !validNpmRegistry(v.NpmRegistry) {
		logWarn("[config] persisted npmRegistry %q is invalid, falling back to %s",
			v.NpmRegistry, npmRegistryDefault)
		v.NpmRegistry = npmRegistryDefault
	}
	// dsh 端口：只在**配置文件里显式写了非法值**时回退（历史上保存过 0/超范围的值会
	// 让 dsh 起不来、反代常驻等待页）。字段缺失时保留 def 的值 —— 平台可能通过
	// dsh_port/TARGET_PORT 环境变量播种它，不能在这里改写平台的决定。
	if !validListenPort(v.DshPort) {
		if hasJSONField(data, "dshPort") {
			logWarn("[config] persisted dshPort %d is out of range (%d-%d), falling back to %d",
				v.DshPort, minListenPort, maxListenPort, def.DshPort)
			v.DshPort = def.DshPort
		}
	}
	return &v
}

// hasJSONField 判断 JSON 对象里是否存在某个顶层字段（用于区分「显式写了 0」与
// 「字段缺失」—— 前者要归一化，后者要保留平台播种的值）。
func hasJSONField(data []byte, key string) bool {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return false
	}
	_, ok := raw[key]
	return ok
}

// GetConfig returns a copy of the current config.
func GetConfig() AppConfig {
	cfgLock.RLock()
	defer cfgLock.RUnlock()
	return cfg
}

// SaveConfig persists the config to disk (atomic write) and updates memory.
// 注意：反代端口（ProxyPort）由本配置保存，但它不是 dsh 自己绑定的端口 ——
// 端口值写完内存后需要调用方重新绑定监听（见 admin.go 的 rebindProxyPort）。
//
// 并发正确性（两个调用点：保存设置 / 切换 dsh 版本，彼此可能重叠）：
//   - 序列化必须在 cfgLock 内、针对**本次的 next** 完成，不能解锁后再读全局 cfg ——
//     否则 A 的请求可能把 B 的配置写盘（自己的改动静默丢失）；
//   - 临时文件必须唯一（os.CreateTemp），不能共用一个 <config>.tmp —— 否则两个保存
//     交错时，后一个 rename 会把前一个写到一半的字节改名成正式配置。
func SaveConfig(renv *RuntimeEnv, next *AppConfig, lockedPorts bool) error {
	cfgLock.Lock()
	if lockedPorts {
		// 只锁定 dsh 端口：dsh 运行中不可改（端口由 dsh 进程持有）；
		// 反代端口可随时改，保存后即时重绑。
		next.DshPort = cfg.DshPort
	}
	cfg = *next
	data, err := json.MarshalIndent(*next, "", "  ")
	cfgLock.Unlock()
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	dir := filepath.Dir(renv.ConfigFile)
	tmp, err := os.CreateTemp(dir, filepath.Base(renv.ConfigFile)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// 失败路径上清掉临时文件，避免在配置目录里堆 .tmp（成功时已被 rename 走）。
	defer func() {
		if _, statErr := os.Stat(tmpName); statErr == nil {
			os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	// 先把内容刷到盘再 rename：否则断电/崩溃后可能拿到「文件在但内容为空」的
	// 配置（loadJSONFile 会解析失败并回落默认值，等于设置丢失）。
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, renv.ConfigFile)
}
