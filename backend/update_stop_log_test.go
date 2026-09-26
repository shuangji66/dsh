package main

// update_stop_log_test.go —— 共用停机入口的日志标签（审查 C9）。
//
// 旧实现固定打 "[update] failed to stop dsh …"，而更新日志是按目标分标签筛选的
// （[harness] / [dsh] / [market]，见 updateLogTag）—— 于是「更新 dsh 服务 / 更新
// 插件市场时停 dsh 失败」这条最需要看到的警告恰好过滤不出来（AGENTS 第 3 节）。

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestStopDshForReplacementLogsTargetTag(t *testing.T) {
	out, _ := captureLogs(t)

	prevStop, prevFree, prevBusy := dshStopFn, dshPortFreeFn, marketBusyFn
	t.Cleanup(func() { dshStopFn, dshPortFreeFn, marketBusyFn = prevStop, prevFree, prevBusy })
	marketBusyFn = func(*UpdateManager) (bool, string) { return false, "" }
	dshPortFreeFn = func(*UpdateManager, time.Duration) {}
	dshStopFn = func(*UpdateManager) error { return errors.New("boom") }

	upd := &UpdateManager{dsh: newTestDshManager(t.TempDir(), "")}

	// 更新市场：日志必须能被 [market] 过滤到。
	if err := upd.stopDshForReplacement("更新插件市场", updateKindMarket); err != nil {
		t.Fatalf("停止失败不应中断替换流程: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "[WARN] [market] failed to stop dsh") {
		t.Fatalf("更新市场的停机失败日志应带 [market] 标签，实际:\n%s", got)
	}
	if strings.Contains(got, "[update]") {
		t.Fatalf("不得再用通用 [update] 标签（按目标过滤时看不到）:\n%s", got)
	}

	// 更新 dsh 服务：必须带 [dsh] 标签。
	out.Reset()
	if err := upd.stopDshForReplacement("更新 dsh 服务", updateKindDsh); err != nil {
		t.Fatalf("停止失败不应中断替换流程: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "[WARN] [dsh] failed to stop dsh") {
		t.Fatalf("更新 dsh 服务的停机失败日志应带 [dsh] 标签，实际:\n%s", got)
	}
}
