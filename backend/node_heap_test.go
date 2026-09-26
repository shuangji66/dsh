package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// bytesToMB：字节数折算为最接近的整数 MB。
func TestBytesToMB(t *testing.T) {
	const mb = 1 << 20
	cases := []struct {
		in   int64
		want int
	}{
		{0, 0},
		{1, 0},
		{mb/2 - 1, 0}, // 0.499… MB → 0
		{mb / 2, 1},   // 0.5 MB → 1
		{mb - 1, 1},   // 0.999… MB → 1
		{mb, 1},
		{mb + mb/2 - 1, 1}, // 1.499… MB → 1
		{mb + mb/2, 2},     // 1.5 MB → 2
		{2248146944, 2144}, // node v26 实测默认堆上限
		{4344760320, 4143}, // ≈4143.49 MB → 4143
	}
	for _, tc := range cases {
		if got := bytesToMB(tc.in); got != tc.want {
			t.Fatalf("bytesToMB(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// nodeHeapLimitProbe 的输出必须是可解析的字节数（宿主机没有 node 时跳过）。
func TestNodeHeapLimitProbeOutput(t *testing.T) {
	out, err := exec.Command("node", "-e", nodeHeapLimitProbe).Output()
	if err != nil {
		t.Skipf("宿主机没有可用的 node: %v", err)
	}
	if _, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64); err != nil {
		t.Fatalf("node 输出无法解析为字节数: %q (%v)", strings.TrimSpace(string(out)), err)
	}
}

// detectNodeHeapLimitMB 的结果必须与直接运行 node 得到的一致，且二次调用走缓存。
func TestDetectNodeHeapLimitMBMatchesNode(t *testing.T) {
	out, err := exec.Command("node", "-e", nodeHeapLimitProbe).Output()
	if err != nil {
		t.Skipf("宿主机没有可用的 node: %v", err)
	}
	bytes, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		t.Fatalf("node 输出无法解析为字节数: %q", strings.TrimSpace(string(out)))
	}
	want := bytesToMB(bytes)
	for i := 0; i < 2; i++ {
		if got := detectNodeHeapLimitMB("node24"); got != want {
			t.Fatalf("第 %d 次 detectNodeHeapLimitMB(node24) = %d, want %d", i+1, got, want)
		}
	}
	// 非法/空版本号归一化为 node24，取到同一份缓存。
	if got := detectNodeHeapLimitMB(""); got != want {
		t.Fatalf("detectNodeHeapLimitMB(\"\") = %d, want %d", got, want)
	}
}

// defaultDshMemLimit：已设置时原样保留；未设置（0）时用探测值兜底。
func TestDefaultDshMemLimit(t *testing.T) {
	if got := defaultDshMemLimit(4096, "node24"); got != 4096 {
		t.Fatalf("已设置的内存限制被改写: %d", got)
	}
	got := defaultDshMemLimit(0, "node24")
	if _, err := exec.LookPath("node"); err != nil {
		if got != 0 {
			t.Fatalf("没有 node 时 defaultDshMemLimit(0) = %d, want 0", got)
		}
		return
	}
	if want := detectNodeHeapLimitMB("node24"); got != want || got <= 0 {
		t.Fatalf("defaultDshMemLimit(0) = %d, want %d", got, want)
	}
}

// 设置接口必须下发 runtime.nodeHeapLimitMB（前端在「自动设置」打开时展示它）。
func TestGetSettingsExposesNodeHeapLimitMB(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skipf("宿主机没有可用的 node: %v", err)
	}
	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })
	// 走真实的默认值路径：配置里没有 dshMemLimit，由 LoadConfig 按 node 堆上限补齐。
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"dshPort":13080,"nodeVersion":"node24"}`), 0o644); err != nil {
		t.Fatalf("写测试配置失败: %v", err)
	}
	cfg := LoadConfig(&RuntimeEnv{ConfigFile: cfgPath})
	initConfig(&cfg)

	m := &AdminMux{
		renv: &RuntimeEnv{ProxyBaseURL: "/app/Harness/dsh"},
		dsh:  newTestDshManager(t.TempDir(), ""),
	}
	rec := httptest.NewRecorder()
	m.handleGetSettings(rec, httptest.NewRequest(http.MethodGet, "/app/Harness/api/settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("handleGetSettings 状态码 = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Config  AppConfig              `json:"config"`
		Runtime map[string]interface{} `json:"runtime"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析设置响应失败: %v", err)
	}
	raw, ok := resp.Runtime["nodeHeapLimitMB"]
	if !ok {
		t.Fatalf("响应缺少 runtime.nodeHeapLimitMB: %s", rec.Body.String())
	}
	got, _ := raw.(float64)
	want := detectNodeHeapLimitMB(cfg.NodeVersion)
	if int(got) != want || want <= 0 {
		t.Fatalf("runtime.nodeHeapLimitMB = %v, want %d", raw, want)
	}
	// 配置里的内存限制同样是 node 的实际堆上限（不再是写死的 2048）。
	if resp.Config.DshMemLimit != want {
		t.Fatalf("config.dshMemLimit = %d, want %d", resp.Config.DshMemLimit, want)
	}
}

// 探测二进制必须与 node 版本对应：node26 用绝对路径（PATH 里的 node 可能是 node24，
// 也可能是别的版本），node24 用 PATH 里的 node。
func TestNodeHeapProbeBin(t *testing.T) {
	if got := nodeHeapProbeBin("node24"); got != "node" {
		t.Fatalf("nodeHeapProbeBin(node24) = %q, want %q", got, "node")
	}
	want26 := "node"
	if node26Available() {
		want26 = filepath.Join(node26BinDir, "node")
	}
	if got := nodeHeapProbeBin("node26"); got != want26 {
		t.Fatalf("nodeHeapProbeBin(node26) = %q, want %q", got, want26)
	}
}

// node26 可用时，必须探到 node26 自己的上限（而不是 PATH 里第一个 node 的）：
// 机器上两个版本的默认上限可以不同（如 v24=2240 MB、v26=2144 MB）。
func TestDetectNodeHeapLimitMBNode26(t *testing.T) {
	if !node26Available() {
		t.Skip("宿主机没有 node v26")
	}
	out, err := exec.Command(filepath.Join(node26BinDir, "node"), "-e", nodeHeapLimitProbe).Output()
	if err != nil {
		t.Fatalf("运行 node26 失败: %v", err)
	}
	bytes, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		t.Fatalf("node26 输出无法解析: %q", strings.TrimSpace(string(out)))
	}
	if got, want := detectNodeHeapLimitMB("node26"), bytesToMB(bytes); got != want {
		t.Fatalf("detectNodeHeapLimitMB(node26) = %d, want %d", got, want)
	}
}

// 旧配置缺 dshMemLimit 字段时，默认值取当前 node 的堆上限，而不是写死的 2048。
func TestLoadConfigFillsMemLimitFromNode(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skipf("宿主机没有可用的 node: %v", err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"dshPort":13080,"nodeVersion":"node24"}`), 0o644); err != nil {
		t.Fatalf("写测试配置失败: %v", err)
	}
	cfg := LoadConfig(&RuntimeEnv{ConfigFile: path})
	if want := detectNodeHeapLimitMB("node24"); cfg.DshMemLimit != want {
		t.Fatalf("缺 dshMemLimit 时 LoadConfig 得到 %d, want %d", cfg.DshMemLimit, want)
	}
	// 已显式配置的值必须原样保留。
	path2 := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path2, []byte(`{"dshMemLimit":1234,"nodeVersion":"node24"}`), 0o644); err != nil {
		t.Fatalf("写测试配置失败: %v", err)
	}
	if cfg := LoadConfig(&RuntimeEnv{ConfigFile: path2}); cfg.DshMemLimit != 1234 {
		t.Fatalf("已配置的 dshMemLimit 被改写: %d", cfg.DshMemLimit)
	}
}
