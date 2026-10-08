package main

// plugin_cmd_entry_test.go —— 钉住「控制台侧 `dsh plugin …` 只有一个入口」这条约束。
//
// 背景（线上故障链）：插件市场改造后，市场的 add/remove/list 直接调了
// `runDshCmdTimeout`，绕开了 `runPluginCmd`，于是
//   - 执行前不清「持有者已死」的陈旧 profile 写锁（plugin-manager 的等待上限 120 秒，
//     陈旧锁会让命令白等后失败）；
//   - `PluginCmdRunning()` 看不到这次操作 —— `replaceBusyGuard`（切换 dsh 版本 /
//     更新 harness / 恢复数据前的统一前置检查）与概览页「停止/重启」的风险提示都会
//     误判成「没有插件操作在跑」，而此刻 pnpm 正在重写 profiles/web。
//
// 现在所有插件命令都走 `DshManager.RunPluginCommand`（清陈旧锁 + 登记忙守卫）。

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 假 dsh 的记事实：把 argv 与「陈旧锁还在不在」写进 $HARNESS_TEST_OUT，测试再读它。
// 用文件而不是 stdout，是因为市场那两个 runner 只回错误、不回输出。
const fakeDshBody = `
printf 'ARGV:%s\n' "$*" >> "$HARNESS_TEST_OUT"
if [ -f "$HARNESS_TEST_LOCK" ]; then echo LOCK=present >> "$HARNESS_TEST_OUT"; else echo LOCK=absent >> "$HARNESS_TEST_OUT"; fi
sleep 0.3
`

type pluginCmdFixture struct {
	dsh   *DshManager
	home  string
	out   string // 假 dsh 写下的记事实
	lock  string // 陈旧 profile 写锁的路径
	clean func()
}

// newPluginCmdFixture 造一个只碰临时目录的 DshManager：HOME 指向临时目录（便于观察陈旧锁），
// 当前选中版本指向一个假 dsh 脚本。
func newPluginCmdFixture(t *testing.T) *pluginCmdFixture {
	t.Helper()
	home := t.TempDir()
	dataDir := t.TempDir()
	outFile := filepath.Join(t.TempDir(), "calls.txt")
	lock := filepath.Join(home, ".dsh", "profiles", "web", "package.json.lock")
	if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
		t.Fatal(err)
	}
	// 「持有者已死」的陈旧锁：PID 取内核 pid_max 上限附近。
	if err := os.WriteFile(lock, []byte(strconv.Itoa(999999)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	const version = "0.2.0"
	bin := versionDshBinFor(&RuntimeEnv{DataDir: dataDir}, version)
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+fakeDshBody), 0o755); err != nil {
		t.Fatal(err)
	}

	prevCfg := GetConfig()
	cfg := GetConfig()
	cfg.DshVersion = version
	initConfig(&cfg)
	t.Cleanup(func() { initConfig(&prevCfg) })
	t.Setenv("HARNESS_TEST_OUT", outFile)
	t.Setenv("HARNESS_TEST_LOCK", lock)

	return &pluginCmdFixture{
		dsh: &DshManager{
			renv: &RuntimeEnv{DataDir: dataDir, Home: home},
			logf: func(logLevel, string, ...interface{}) {},
		},
		home: home,
		out:  outFile,
		lock: lock,
	}
}

// calls 返回假 dsh 到目前为止记下的内容。
func (f *pluginCmdFixture) calls(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(f.out)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(data)
}

// RunPluginCommand 必须在执行前清陈旧锁、并在执行期间登记「插件命令进行中」。
func TestRunPluginCommandHealsStaleLockAndRegistersBusy(t *testing.T) {
	f := newPluginCmdFixture(t)

	done := make(chan error, 1)
	go func() {
		_, err := f.dsh.RunPluginCommand(20*time.Second, marketCommand([]string{"list"}))
		done <- err
	}()

	// 命令执行期间：PluginCmdRunning() 必须为 true（忙守卫、概览页风险提示都靠它）。
	deadline := time.Now().Add(5 * time.Second)
	for !f.dsh.PluginCmdRunning() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !f.dsh.PluginCmdRunning() {
		t.Fatal("执行 dsh plugin 期间 PluginCmdRunning() 必须为 true，否则忙守卫看不见这次操作")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("命令不应失败: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("假 dsh 命令未在预期时间内结束")
	}
	if f.dsh.PluginCmdRunning() {
		t.Fatal("命令结束后必须把「进行中」复位，否则忙守卫会永久拦住其它操作")
	}
	got := f.calls(t)
	if !strings.Contains(got, "LOCK=absent") {
		t.Fatalf("执行前应清掉持有者已死的 profile 写锁，实际记事实:\n%s", got)
	}
}

// 市场的 add/remove 也必须走同一个入口，且各自的参数约定不变：
// add 带 --registry，remove 不带（pnpm 的 remove 不认这个选项）。
func TestMarketPluginCommandsGoThroughRunPluginCommand(t *testing.T) {
	f := newPluginCmdFixture(t)
	upd := &UpdateManager{
		renv:     f.dsh.renv,
		dsh:      f.dsh,
		statuses: map[updateKind]*UpdateStatus{updateKindMarket: {Kind: updateKindMarket}},
	}

	if err := upd.runMarketPluginCmdOnce([]string{"remove", marketPackageName}); err != nil {
		t.Fatalf("remove 不应失败: %v", err)
	}
	got := f.calls(t)
	if !strings.Contains(got, "plugin --profile web remove "+marketPackageName) {
		t.Fatalf("remove 的 argv 不对:\n%s", got)
	}
	if strings.Contains(got, "--registry") {
		t.Fatalf("remove 绝不能带 --registry（pnpm 不支持，会以 Unknown option 失败）:\n%s", got)
	}
	if !strings.Contains(got, "LOCK=absent") {
		t.Fatalf("市场命令必须经 RunPluginCommand（执行前清陈旧锁），实际记事实:\n%s", got)
	}

	// add 走「镜像源逐个尝试」的那条：第一个源成功即返回，argv 里带 registry。
	if err := upd.runMarketPluginCmd([]string{"add", marketPackageName + "@1.2.3"}); err != nil {
		t.Fatalf("add 不应失败: %v", err)
	}
	got = f.calls(t)
	if !strings.Contains(got, "add "+marketPackageName+"@1.2.3") || !strings.Contains(got, "--registry="+npmMirrors[0].URL) {
		t.Fatalf("add 的 argv 应带首个镜像源:\n%s", got)
	}
	if strings.Count(got, "LOCK=absent") < 2 {
		t.Fatalf("add 同样要经 RunPluginCommand（执行前清陈旧锁），实际记事实:\n%s", got)
	}
}

// 检测（list）也走同一个入口：它是只读命令，但同样会被陈旧锁拖 120 秒，
// 也需要被忙守卫登记（否则一次卡住的检测不会拦住其它操作）。
func TestMarketDetectGoesThroughRunPluginCommand(t *testing.T) {
	f := newPluginCmdFixture(t)
	upd := &UpdateManager{
		renv:     f.dsh.renv,
		dsh:      f.dsh,
		statuses: map[updateKind]*UpdateStatus{updateKindMarket: {Kind: updateKindMarket}},
	}

	// 假 dsh 不输出 dshmarket → 判定为「未安装」，但不该报错。
	if _, ok, err := upd.marketInstalled(); err != nil || ok {
		t.Fatalf("假 dsh 下应判定为未安装且不报错，实得 ok=%v err=%v", ok, err)
	}
	got := f.calls(t)
	if !strings.Contains(got, "LOCK=absent") {
		t.Fatalf("list 检测必须经 RunPluginCommand（执行前清陈旧锁），实际记事实:\n%s", got)
	}
}

// 源码守卫：插件命令不许再绕过入口直接调 runDshCmdTimeout。
func TestNoPluginCommandBypassesRunPluginCommand(t *testing.T) {
	for _, file := range []string{"market.go", "server.go", "update.go", "admin.go"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if strings.Contains(line, "runDshCmdTimeout(") {
				t.Errorf("%s:%d 直接调用了 runDshCmdTimeout —— 插件命令必须走 "+
					"DshManager.RunPluginCommand（否则不清陈旧锁、也不登记忙守卫）:\n\t%s",
					file, i+1, strings.TrimSpace(line))
			}
		}
	}
}
