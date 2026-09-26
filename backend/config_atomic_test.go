package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// 落盘过非法 dsh 端口的配置（历史版本可能写入 0/超范围的值）必须在读取时被兜回：
// 否则 dsh 会以 `--port 0` 启动，反代永远停在等待页。
func TestLoadConfigNormalizesPersistedDshPort(t *testing.T) {
	def := defaultConfig()
	for _, bad := range []int{0, -1, 80, 1024, 70000} {
		path := filepath.Join(t.TempDir(), "config.json")
		body := `{"dshPort":` + strconv.Itoa(bad) + `,"proxyPort":3079}`
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		got := LoadConfig(&RuntimeEnv{ConfigFile: path})
		if got.DshPort != def.DshPort {
			t.Fatalf("落盘 dshPort=%d 应回落到 %d，实际 %d", bad, def.DshPort, got.DshPort)
		}
	}
}

// 但「配置文件里没有 dshPort 字段」时必须保留平台播种的值（dsh_port/TARGET_PORT），
// 不能在读取时改写平台的决定。
func TestLoadConfigKeepsPlatformDshPortWhenFieldAbsent(t *testing.T) {
	t.Setenv("dsh_port", "3000")
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"proxyPort":3079}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadConfig(&RuntimeEnv{ConfigFile: path}).DshPort; got != 3000 {
		t.Fatalf("字段缺失时应保留平台值 3000，实际 %d", got)
	}

	// 平台播种了特权端口（绑不上）时也不在读取时改写：交由设置页校验/用户处理。
	t.Setenv("dsh_port", "80")
	if got := LoadConfig(&RuntimeEnv{ConfigFile: path}).DshPort; got != 80 {
		t.Fatalf("平台播种的端口不应被读取路径改写，实际 %d", got)
	}
}

// SaveConfig 的并发正确性：序列化必须在锁内针对本次提交完成，临时文件必须唯一。
// 旧实现「解锁后再 marshal 全局 cfg + 共用 <config>.tmp」在并发保存下可能把对方的
// 配置写盘、或把写到一半的字节 rename 成正式配置（解析失败即等于设置丢失）。
// 这里用 -race 跑并发保存，断言最终文件始终可解析、且不留临时文件。
func TestSaveConfigConcurrentWrites(t *testing.T) {
	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })
	initConfig(&AppConfig{DshPort: 13080, ProxyPort: 3079})

	dir := t.TempDir()
	renv := &RuntimeEnv{ConfigFile: filepath.Join(dir, "config.json")}

	const writers = 16
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			next := GetConfig()
			next.AuthTTLHours = i + 1
			next.Password = "pwd-" + strconv.Itoa(i)
			if err := SaveConfig(renv, &next, false); err != nil {
				t.Errorf("并发保存失败: %v", err)
			}
		}(i)
	}
	wg.Wait()

	data, err := os.ReadFile(renv.ConfigFile)
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	var onDisk AppConfig
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("并发保存后配置无法解析（可能 rename 了半写文件）: %v\n%s", err, data)
	}
	if onDisk.AuthTTLHours < 1 || onDisk.AuthTTLHours > writers {
		t.Fatalf("落盘的 AuthTTLHours = %d，不属于任何一次提交", onDisk.AuthTTLHours)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("保存后残留临时文件: %s", e.Name())
		}
	}
}

// 配置损坏时必须留下明确日志（不能静默回落默认值），且仍返回默认配置保证可启动。
func TestLoadConfigCorruptFileFallsBackToDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"dshPort":`), 0o600); err != nil {
		t.Fatal(err)
	}
	def := defaultConfig()
	got := LoadConfig(&RuntimeEnv{ConfigFile: path})
	if got.DshPort != def.DshPort {
		t.Fatalf("损坏配置应回落默认 dsh 端口 %d，实际 %d", def.DshPort, got.DshPort)
	}
}
