package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv" // 新增导入
	"sync"
)

// AppConfig holds the settings editable from the frontend.
type AppConfig struct {
	DshPort int `json:"dshPort"`
	// ProxyPort 是反向代理（TCP 根挂载）的监听端口，默认 3079，随配置持久化。
	// 它只决定 harness 自身监听哪个端口对外提供 dsh 界面，不再是
	// 「dsh 端口」的对偶（dsh 只监听本机，由反代转发），因此改端口只影响反代自身：
	// 保存后立即重新绑定（见 rebindProxyPort），不需要重启 dsh 或控制台。
	ProxyPort    int    `json:"proxyPort"`
	ProxyEnabled bool   `json:"proxyEnabled"`
	ProxyAddr    string `json:"proxyAddr"`
	AuthEnabled  bool   `json:"authEnabled"`
	Password     string `json:"password,omitempty"`
	AuthTTLHours int    `json:"authTTLHours"` // 登录鉴权有效期（小时），默认 4
	// dsh 进程内存限制（MB），默认 2048；通过 NODE_OPTIONS 生效。
	// DshMemAuto 为 true（默认）时由系统 node 自动分配内存，不传 NODE_OPTIONS。
	DshMemLimit int  `json:"dshMemLimit"`
	DshMemAuto  bool `json:"dshMemAuto"`
	// NodeVersion 指定 dsh 启动时使用的 node 版本（"node24" / "node26"）。
	// 默认 "node24"（系统默认 node）。当宿主机存在 /var/apps/nodejs_v26/target/bin/node
	// 时，用户可切换到 "node26"，dsh 启动时会把对应版本的 bin 目录前置到 PATH。
	NodeVersion string `json:"nodeVersion"`
	// HomeDir 是 dsh 进程的 HOME 环境变量（实际系统目录）。空表示使用启动时的
	// 默认主目录（= /var/apps/Harness/shares/Harness 的实际路径 /vol1/@appshare/Harness）。
	// 资源页可把某个已授权目录设为新的主目录，保存后重启 dsh 生效。
	HomeDir string `json:"homeDir"`
	// AccessURLs 是用户配置的 dsh 访问地址列表，显示在概览页供快速访问。
	AccessURLs []string `json:"accessUrls,omitempty"`
	// BrowserCompat 为浏览器兼容模式开关，默认关闭。
	//
	// 一套开关，两处引擎兼容修复（都只影响受影响的引擎，Chromium 开启无副作用）：
	//  1. dsh 客户端 bundle 里只适配 V8 的原生函数格式判断（Firefox / Zen / Safari
	//     上「会话历史无法加载」）—— 改写 bundle 字节，运行时由 window.__DSH_BROWSER_COMPAT__
	//     决定是否归一化空白；
	//  2. iPhone（WKWebView）上「模型 / 推理等级菜单能打开、点选项却毫无反应」—— 见
	//     proxy.go 注入脚本第 6 段：iOS 在菜单内按钮之间搬家焦点时 focusout 的
	//     relatedTarget 为 null，而模型座位的 onBlur 守卫只接受 Node，会把菜单在
	//     mousedown 与 click 之间关掉。该段注入读同一个开关，且只在触屏设备上武装。
	//
	// 开启时反代会修正 dsh 客户端 bundle 中一处只适配 V8 的原生函数格式判断
	// （`Function.prototype.toString.call(c) === "function X() { [native code] }"`）。
	// SpiderMonkey（Firefox/Zen）与 JavaScriptCore（Safari/WebKit）把原生函数源码
	// 格式化为多行，使该判断恒为 false，进而让会话历史加载抛
	// TypeError（"Assistant stream raw chunk must be a lossless JSON object"），
	// 表现为历史区永久停留“载入历史…”。
	//
	// 修正方式是在比较前把空白折叠为单个空格。该变换对 V8（Chrome/Edge）是恒等
	// 变换，不影响其行为，因此 Chromium 用户开启也无副作用；默认关闭只是让未受
	// 影响的用户与官方 dsh 行为保持一致。
	//
	// 生效时机（重要，与缓存机制有关）：
	//   - 切换本开关：bundle 字节不随开关变化（替换结果是恒定形态，真正的判定发生在
	//     浏览器运行时，由 HTML 注入的 window.__DSH_BROWSER_COMPAT__ 提供），而 HTML
	//     每次刷新都回源，因此切换开关后普通刷新即可生效。
	//   - 但若浏览器缓存的 bundle 是“升级前”的旧字节（例如本次部署首次引入该修复时），
	//     dsh 的插件资源带 `Cache-Control: public, max-age=31536000, immutable` 且无
	//     ETag/Last-Modified，普通刷新不会回源，必须清除浏览器缓存或强制刷新
	//     （Ctrl+Shift+R）才能拿到新字节。详见 README「浏览器兼容模式」一节。
	BrowserCompat bool `json:"browserCompat"`
}

// RuntimeEnv 描述进程启动时的环境（环境变量来源，含路径与凭据）。
// 注意：反向代理端口不在其中 —— 它是可变的用户配置（AppConfig.ProxyPort）。
type RuntimeEnv struct {
	ConfigFile   string
	AdminSock    string
	AdminBaseURL string
	LogFile      string // 日志文件输出路径（HARNESS_LOG_FILE），为空则不落盘
	// PidFile 是 harness 控制台自身的 PID 文件路径（HARNESS_PID_FILE，为空则不
	// 维护）；DshPidFile 是 dsh 服务进程的 PID 文件路径（HARNESS_DSH_PID_FILE，
	// 为空则不维护）。二者用途不同：前者标识控制台进程，dsh 装插件自重启等场景
	// 下保持不变；后者跟随 dsh 实时 PID，dsh 停止时移除。
	PidFile       string
	DshPidFile    string
	TRIMApiToken  string
	TRIMAppDest   string
	TRIMAppName   string
	Path          string
	Home          string
	PnpmHome      string
	Lang          string
	QuickCmdsFile string // 终端快捷指令持久化文件路径（HARNESS_QUICK_CMDS_FILE）
	SessionDir    string // 终端会话临时镜像目录（HARNESS_SESSION_DIR，停止时整目录清除）
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
	return RuntimeEnv{
		ConfigFile:    envOr("HARNESS_CONFIG_FILE", filepath.Join(os.Getenv("TRIM_PKGVAR"), "config.json")),
		AdminSock:     envOr("HARNESS_ADMIN_SOCK", filepath.Join(os.Getenv("TRIM_APPDEST"), "app.sock")),
		AdminBaseURL:  envOr("HARNESS_ADMIN_BASEURL", "/app/Harness"),
		LogFile:       envOr("HARNESS_LOG_FILE", filepath.Join(os.Getenv("TRIM_PKGVAR"), "harness.log")),
		PidFile:       envOr("HARNESS_PID_FILE", filepath.Join(os.Getenv("TRIM_PKGVAR"), "harness.pid")),
		DshPidFile:    envOr("HARNESS_DSH_PID_FILE", filepath.Join(os.Getenv("TRIM_PKGVAR"), "dsh.pid")),
		TRIMApiToken:  os.Getenv("TRIM_API_TOKEN"),
		TRIMAppDest:   appDest,
		TRIMAppName:   appName,
		Path:          os.Getenv("PATH"),
		Home:          os.Getenv("HOME"),
		PnpmHome:      os.Getenv("PNPM_HOME"),
		Lang:          os.Getenv("TRIM_SYS_LANGUAGE"),
		QuickCmdsFile: envOr("HARNESS_QUICK_CMDS_FILE", filepath.Join(os.Getenv("TRIM_PKGVAR"), "quickcmds.json")),
		SessionDir:    envOr("HARNESS_SESSION_DIR", filepath.Join(os.Getenv("TRIM_PKGVAR"), "terminal-sessions")),
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
		AuthEnabled:  authEnabled,
		Password:     os.Getenv("password"),
		AuthTTLHours: envOrInt("auth_ttl_hours", 4),
		DshMemLimit:  2048,
		DshMemAuto:   true,
		NodeVersion:  "node24",
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

// validProxyPort 校验反代端口是否在可绑定范围内（1..65535）。
func validProxyPort(p int) bool { return p >= 1 && p <= 65535 }

// normalizeProxyPort 把非法/空值归一化为默认端口。
func normalizeProxyPort(p int) int {
	if !validProxyPort(p) {
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

// --- node 版本切换 ---

// nodeVersionBinDir 为每个可切换的 node 版本对应的 bin 目录路径。
// node24 使用系统默认 node（即启动时的 PATH），不需要额外 bin 目录。
const node26BinDir = "/var/apps/nodejs_v26/target/bin"

// validNodeVersions 返回当前合法的 node 版本标识集合（用于校验配置值）。
func validNodeVersions() map[string]bool {
	return map[string]bool{"node24": true, "node26": true}
}

// normalizeNodeVersion 把非法/空值归一化为默认的 "node24"。
func normalizeNodeVersion(v string) string {
	if validNodeVersions()[v] {
		return v
	}
	return "node24"
}

// node26Available 检测宿主机是否存在 node v26 的 node 二进制。
// 仅当 /var/apps/nodejs_v26/target/bin/node 存在时，node26 才可作为切换选项。
func node26Available() bool {
	fi, err := os.Stat(node26BinDir + "/node")
	return err == nil && fi.Mode().IsRegular()
}

// nodeVersionBinPrefix 返回指定 node 版本要前置到 PATH 的 bin 目录。
// node24 返回空串（使用系统默认 node）；node26 返回 node26 的 bin 目录
// （仅当该版本可用时才返回）。
func nodeVersionBinPrefix(v string) string {
	if normalizeNodeVersion(v) == "node26" && node26Available() {
		return node26BinDir
	}
	return ""
}

// ensureValidNodeVersion 检测当前持久化配置里的 node 版本是否仍可用。
// 若配置为 node26，但宿主机上 node v26 已被卸载/不存在，则主动回退到 node24
// 并把持久化配置改写为 node24（自动落盘）。返回 true 表示发生了回退。
// 这样用户此前选了 node26 后即便 node26 被卸载，下次启动或拉取设置时也能自愈。
func ensureValidNodeVersion(renv *RuntimeEnv) bool {
	cur := GetConfig()
	if cur.NodeVersion != "node26" {
		return false
	}
	if node26Available() {
		return false
	}
	next := cur
	next.NodeVersion = "node24"
	if err := SaveConfig(renv, &next, false); err != nil {
		return false
	}
	return true
}

// LoadConfig reads the JSON config file; falls back to defaults if missing.
func LoadConfig(renv *RuntimeEnv) AppConfig {
	def := defaultConfig()
	if c := loadJSONFile(renv.ConfigFile, &def); c != nil {
		return *c
	}
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
		return def
	}
	// 旧配置文件中可能没有这些字段，回退到默认值
	if v.AuthTTLHours <= 0 {
		v.AuthTTLHours = 4
	}
	if v.DshMemLimit <= 0 {
		v.DshMemLimit = 2048
	}
	// 旧配置文件可能没有 nodeVersion 字段，回退到默认 node24。
	if v.NodeVersion == "" {
		v.NodeVersion = "node24"
	}
	// 旧配置文件（反代端口还来自 PROXY_PORT 环境变量）没有 proxyPort 字段，
	// 回退到默认 3079 并随下次保存落盘。
	v.ProxyPort = normalizeProxyPort(v.ProxyPort)
	return &v
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
func SaveConfig(renv *RuntimeEnv, next *AppConfig, lockedPorts bool) error {
	cfgLock.Lock()
	if lockedPorts {
		// 只锁定 dsh 端口：dsh 运行中不可改（端口由 dsh 进程持有）；
		// 反代端口可随时改，保存后即时重绑。
		next.DshPort = cfg.DshPort
	}
	cfg = *next
	cfgLock.Unlock()

	tmp := renv.ConfigFile + ".tmp"
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, renv.ConfigFile)
}
