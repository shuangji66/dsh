package main

// 本文件覆盖「反代监听端口是持久化配置项」这条语义（见 AppConfig.ProxyPort）：
//   - 旧配置文件没有 proxyPort 字段时回退到默认 3079（旧版端口来自 PROXY_PORT
//     环境变量，配置里没有这个字段）；
//   - 端口取值范围校验；
//   - 设置页改端口时「先绑新、再关旧」：新端口绑定失败必须整次拒绝保存，且旧监听
//     继续服务、配置保持不变；成功时立即切换并落盘。

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// closeCurrentProxy 关闭当前进程持有的 TCP 反代监听，供用例收尾。
func closeCurrentProxy() {
	proxyMu.Lock()
	srv, ln := proxyServer, proxyLn
	proxyServer, proxyLn = nil, nil
	proxyMu.Unlock()
	closeProxyListener(srv, ln)
}

// proxyPortDialable 判断本机某端口是否可连（用于确认监听已建立/已关闭）。
func proxyPortDialable(port int) bool {
	c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// saveProxySettings 走真实的 handleSaveSettings，按 mutate 改动当前配置后提交，
// 返回 HTTP 状态码与响应体。
func saveProxySettings(t *testing.T, m *AdminMux, mutate func(*AppConfig)) (int, map[string]interface{}) {
	t.Helper()
	next := GetConfig()
	mutate(&next)
	body, err := json.Marshal(map[string]interface{}{"config": next})
	if err != nil {
		t.Fatalf("序列化配置失败: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	m.handleSaveSettings(rec, req)
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析响应失败: %v (%s)", err, rec.Body.String())
	}
	msg, _ := out["error"].(string)
	if msg != "" {
		out["error"] = msg
	}
	return rec.Code, out
}

// 旧配置文件（无 proxyPort）必须回退到默认 3079；显式配置的值必须原样保留。
func TestProxyPortConfigDefaultAndPersistence(t *testing.T) {
	if defaultProxyPort != 3079 {
		t.Fatalf("默认反代端口应为 3079，实际 %d", defaultProxyPort)
	}
	dir := t.TempDir()

	legacy := filepath.Join(dir, "legacy.json")
	if err := os.WriteFile(legacy, []byte(`{"dshPort":13080,"authEnabled":false}`), 0o600); err != nil {
		t.Fatalf("写入旧配置失败: %v", err)
	}
	if got := LoadConfig(&RuntimeEnv{ConfigFile: legacy}).ProxyPort; got != defaultProxyPort {
		t.Fatalf("旧配置回退端口 = %d, want %d", got, defaultProxyPort)
	}

	// 显式配置（含非法值 0 → 归一化为默认）两种情况
	explicit := filepath.Join(dir, "explicit.json")
	if err := os.WriteFile(explicit, []byte(`{"dshPort":13080,"proxyPort":13079}`), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	if got := LoadConfig(&RuntimeEnv{ConfigFile: explicit}).ProxyPort; got != 13079 {
		t.Fatalf("显式配置端口 = %d, want 13079", got)
	}
	zero := filepath.Join(dir, "zero.json")
	if err := os.WriteFile(zero, []byte(`{"dshPort":13080,"proxyPort":0}`), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	if got := LoadConfig(&RuntimeEnv{ConfigFile: zero}).ProxyPort; got != defaultProxyPort {
		t.Fatalf("非法端口 0 应归一化为 %d，实际 %d", defaultProxyPort, got)
	}
}

func TestProxyPortValidation(t *testing.T) {
	for _, tc := range []struct {
		port int
		ok   bool
	}{
		{0, false}, {-1, false}, {1, true}, {3079, true}, {65535, true}, {65536, false}, {70000, false},
	} {
		if got := validProxyPort(tc.port); got != tc.ok {
			t.Fatalf("validProxyPort(%d) = %v, want %v", tc.port, got, tc.ok)
		}
	}
	for _, tc := range []struct{ in, want int }{
		{0, defaultProxyPort}, {70000, defaultProxyPort}, {4096, 4096},
	} {
		if got := normalizeProxyPort(tc.in); got != tc.want {
			t.Fatalf("normalizeProxyPort(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// 改端口必须「先绑新、再关旧」：被占用时整次保存被拒（配置与旧监听不动），
// 可用时立即切换并落盘。
func TestSaveSettingsProxyPortRebind(t *testing.T) {
	prev := GetConfig()
	t.Cleanup(func() {
		closeCurrentProxy()
		initConfig(&prev)
	})

	dsh := newTestDshManager(t.TempDir(), "")
	// dsh 端口取一个无人监听的空闲端口，避免本机真实 dsh 干扰 Running() 判定。
	dshPort := closedPort(t)
	oldPort := closedPort(t)
	cfg := defaultConfig()
	cfg.DshPort = dshPort
	cfg.ProxyPort = oldPort
	initConfig(&cfg)

	renv := &RuntimeEnv{ConfigFile: filepath.Join(t.TempDir(), "config.json")}
	boot := newBootState()
	if err := startProxy(oldPort, NewAuth(), dsh, boot); err != nil {
		t.Fatalf("初始监听 :%d 失败: %v", oldPort, err)
	}
	if !proxyPortDialable(oldPort) {
		t.Fatalf("初始监听 :%d 不可连", oldPort)
	}
	m := &AdminMux{renv: renv, dsh: dsh, auth: NewAuth(), boot: boot}

	// 1) 新端口被占用：拒绝保存，旧监听继续服务、配置不变。
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("占用测试端口失败: %v", err)
	}
	defer blocker.Close()
	busyPort := blocker.Addr().(*net.TCPAddr).Port
	code, resp := saveProxySettings(t, m, func(c *AppConfig) { c.ProxyPort = busyPort })
	if code != http.StatusBadRequest {
		t.Fatalf("占用端口的保存状态 = %d, want 400 (%v)", code, resp)
	}
	if got := GetConfig().ProxyPort; got != oldPort {
		t.Fatalf("被拒后配置端口 = %d, want %d", got, oldPort)
	}
	if !proxyPortDialable(oldPort) {
		t.Fatalf("被拒后旧监听 :%d 不应被关闭", oldPort)
	}

	// 2) 与 dsh 端口相同：拒绝保存（两者会争抢同一个 TCP 端口）。
	code, _ = saveProxySettings(t, m, func(c *AppConfig) { c.ProxyPort = dshPort })
	if code != http.StatusBadRequest {
		t.Fatalf("与 dsh 端口相同时的状态 = %d, want 400", code)
	}

	// 3) 合法端口：保存成功、监听切换到新端口、旧端口释放、配置落盘。
	newPort := closedPort(t)
	code, resp = saveProxySettings(t, m, func(c *AppConfig) { c.ProxyPort = newPort })
	if code != http.StatusOK {
		t.Fatalf("切换端口的状态 = %d, want 200 (%v)", code, resp)
	}
	if got := GetConfig().ProxyPort; got != newPort {
		t.Fatalf("切换后配置端口 = %d, want %d", got, newPort)
	}
	if !proxyPortDialable(newPort) {
		t.Fatalf("切换后新监听 :%d 不可连", newPort)
	}
	if proxyPortDialable(oldPort) {
		t.Fatalf("切换后旧监听 :%d 应已关闭", oldPort)
	}
	data, err := os.ReadFile(renv.ConfigFile)
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	var onDisk AppConfig
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("解析配置失败: %v", err)
	}
	if onDisk.ProxyPort != newPort {
		t.Fatalf("落盘端口 = %d, want %d", onDisk.ProxyPort, newPort)
	}
}
