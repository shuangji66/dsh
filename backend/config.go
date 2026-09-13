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
	DshPort      int    `json:"dshPort"`
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
}

// RuntimeEnv 添加 ProxyPort
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
	ProxyPort     int    // 新增
	QuickCmdsFile string // 终端快捷指令持久化文件路径（HARNESS_QUICK_CMDS_FILE）
	SessionDir    string // 终端会话临时镜像目录（HARNESS_SESSION_DIR，停止时整目录清除）
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
	// 从环境变量获取反代端口，默认 13079
	proxyPort := 13079
	if p := os.Getenv("PROXY_PORT"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			proxyPort = v
		}
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
		ProxyPort:     proxyPort,
		QuickCmdsFile: envOr("HARNESS_QUICK_CMDS_FILE", filepath.Join(os.Getenv("TRIM_PKGVAR"), "quickcmds.json")),
		SessionDir:    envOr("HARNESS_SESSION_DIR", filepath.Join(os.Getenv("TRIM_PKGVAR"), "terminal-sessions")),
	}
}

func defaultConfig() AppConfig {
	dshPort := atoi(envOr("dsh_port", envOr("TARGET_PORT", "13080")))
	authEnabled := envBool(envOr("auth_mode", envOr("PROXY_AUTH", "true")))
	proxyEnabled := os.Getenv("proxy_mode") == "1"
	return AppConfig{
		DshPort:      dshPort,
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
	return &v
}

// GetConfig returns a copy of the current config.
func GetConfig() AppConfig {
	cfgLock.RLock()
	defer cfgLock.RUnlock()
	return cfg
}

// SaveConfig persists the config to disk (atomic write) and updates memory.
// 注意：反代端口（ProxyPort）不再由配置保存，仅从环境变量读取。
func SaveConfig(renv *RuntimeEnv, next *AppConfig, lockedPorts bool) error {
	cfgLock.Lock()
	if lockedPorts {
		// 只锁定 dsh 端口，反代端口不可变
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
