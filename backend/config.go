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
}

// RuntimeEnv 添加 ProxyPort
type RuntimeEnv struct {
	DshBin       string
	ConfigFile   string
	AdminSock    string
	AdminBaseURL string
	DshWorkDir   string
	TRIMApiToken string
	TRIMAppDest  string
	TRIMAppName  string
	Path         string
	Home         string
	PnpmHome     string
	Lang         string
	ProxyPort    int // 新增
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
		DshBin:       envOr("HARNESS_DSH_BIN", ""),
		ConfigFile:   envOr("HARNESS_CONFIG_FILE", filepath.Join(os.Getenv("TRIM_PKGVAR"), "config.json")),
		AdminSock:    envOr("HARNESS_ADMIN_SOCK", filepath.Join(appDest, "app.sock")),
		AdminBaseURL: envOr("HARNESS_ADMIN_BASEURL", appDest),
		DshWorkDir:   envOr("HARNESS_DSH_WORKDIR", appDest),
		TRIMApiToken: os.Getenv("TRIM_API_TOKEN"),
		TRIMAppDest:  appDest,
		TRIMAppName:  appName,
		Path:         os.Getenv("PATH"),
		Home:         os.Getenv("HOME"),
		PnpmHome:     os.Getenv("PNPM_HOME"),
		Lang:         os.Getenv("TRIM_SYS_LANGUAGE"),
		ProxyPort:    proxyPort,
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
	}
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
	var v AppConfig
	if err := json.Unmarshal(data, &v); err != nil {
		return def
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