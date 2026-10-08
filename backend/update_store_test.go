package main

// update_store_test.go —— 「控制台只自更新小版本」的回归测试。
//
// 规则见 update.go 的「控制台只自更新小版本」一节：
//   - 同一 major.minor 线内的更高补丁版（1.4.3 → 1.4.9）才允许就地下载安装
//     （HasUpdate=true）；
//   - 跨主要/次要版本（1.4 → 1.5、1.x → 2.x）一律**拒绝下载与安装**，改为提示用户去
//     更新 fpk 安装包（StoreUpdate=true + StoreVersion）；
//   - 判定基准是**仓库最新版**，不是「本线最高补丁版」：最新版一跨线，就地自更新整体
//     停用（同一条旧线里还挂着补丁版也不再提示）。
//
// 三条链路都要挡：版本检测（checkOnce/状态字段）、下载接口（HTTP）、下载与安装实现。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withHarnessVersion 临时替换本地版本号。harnessVersion 是 -ldflags 注入的包级变量，
// 判定全部基于它，因此测试要能换个版本再看结论（用 Cleanup 还原，避免跨用例污染）。
func withHarnessVersion(t *testing.T, v string) {
	t.Helper()
	prev := harnessVersion
	harnessVersion = v
	t.Cleanup(func() { harnessVersion = prev })
}

// 允许与拒绝的分界线：只有「同 major.minor 且更高」才允许就地自更新。
func TestHarnessUpdateTargetOnlyAllowsSameMinorLine(t *testing.T) {
	cases := []struct {
		local, latest string
		self, store   bool
		why           string
	}{
		{"1.4.3", "1.4.9", true, false, "同线补丁版：允许"},
		{"1.4.3", "1.4.3", false, false, "同版本：没有更新"},
		{"1.4.3", "1.4.2", false, false, "更旧：没有更新（更不该提示去装旧版）"},
		{"1.4.0-rc.1", "1.4.0", true, false, "同线且更高（预发布 → 正式版）：允许"},
		{"1.4.3", "1.5.0", false, true, "次要版本：需要换包"},
		{"1.4.3", "1.5.0-beta.1", false, true, "次要版本的预发布：同样需要换包"},
		{"1.4.3", "2.0.0", false, true, "主要版本：需要换包"},
		{"1.0.0", "1.10.0", false, true, "1.10 不等于 1.1（按数字比较，不是字符串比较）"},
		{"2.0.0", "2.0.0-rc.1", false, false, "预发布比正式版旧：没有更新"},
		{"1.4.3", "", false, false, "未取到最新版：两条路都不走"},
		{"1", "1.5.0", false, true, "本地版本号解析不出 major.minor：判不了就不自更新"},
	}
	for _, tc := range cases {
		withHarnessVersion(t, tc.local)
		self, store := harnessUpdateTarget(tc.latest)
		if self != tc.self || store != tc.store {
			t.Errorf("local=%s latest=%s: selfUpdate=%v storeUpdate=%v, want %v/%v（%s）",
				tc.local, tc.latest, self, store, tc.self, tc.store, tc.why)
		}
		// HasUpdate 与 StoreUpdate 互斥：能下载就不会同时要用户换包。
		if self && store {
			t.Errorf("local=%s latest=%s: 两条结论不能同时成立", tc.local, tc.latest)
		}
	}
}

// 下载实现侧的硬拒绝：跨线版本连字节都不下（在触碰网络与盘之前就返回）。
func TestDownloadUpdateRefusesCrossMinorVersion(t *testing.T) {
	withHarnessVersion(t, "1.4.3")
	m := newChecksumTestManager(t)
	m.statuses[updateKindHarness].LatestVersion = "1.5.0"

	err := m.downloadUpdate(updateKindHarness)
	if err == nil {
		t.Fatal("跨次要版本的下载必须被拒绝")
	}
	msg, ok := uiMsgOf(err)
	if !ok || msg.Code != "err_update_store_required" {
		t.Fatalf("拒绝错误应带 code err_update_store_required，实得 %v (%+v)", err, msg)
	}
	if msg.Params["version"] != "1.5.0" {
		t.Fatalf("错误应带上要被拒绝的版本号，实得 %+v", msg.Params)
	}
	// 拒绝发生在任何状态改动之前：状态没被置成 downloading，也没有半成品落盘。
	if st := m.getStatus(updateKindHarness); st.Phase != "" || st.Downloading {
		t.Fatalf("被拒绝时不应进入下载态，实得 phase=%q downloading=%v", st.Phase, st.Downloading)
	}
	if entries, err := os.ReadDir(m.pendingDir()); err == nil && len(entries) > 0 {
		t.Fatalf("被拒绝时不应落下任何更新包，实得 %d 个文件", len(entries))
	}
}

// 主要版本同样拒绝（与次要版本走同一条判定）。
func TestDownloadUpdateRefusesMajorVersion(t *testing.T) {
	withHarnessVersion(t, "1.4.3")
	m := newChecksumTestManager(t)
	m.statuses[updateKindHarness].LatestVersion = "2.0.0"
	if err := m.downloadUpdate(updateKindHarness); err == nil {
		t.Fatal("跨主要版本的下载必须被拒绝")
	}
}

// HTTP 层：/api/update/download 同步回 400 + code + 参数（不下字节、不改状态），
// 前端据此走 i18n 弹「请更新 fpk 安装包」。
func TestUpdateDownloadHandlerRefusesCrossMinorVersion(t *testing.T) {
	withHarnessVersion(t, "1.4.3")
	m := newChecksumTestManager(t)
	m.statuses[updateKindHarness].LatestVersion = "2.0.0"
	mux := &AdminMux{update: m}

	rec := httptest.NewRecorder()
	mux.handleUpdateDownload(rec, httptest.NewRequest(http.MethodPost, "/api/update/download",
		strings.NewReader(`{"kind":"harness"}`)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d, want 400（不是 409：这不是并发拒绝）\n%s", rec.Code, rec.Body.String())
	}
	var body struct {
		OK     bool              `json:"ok"`
		Code   string            `json:"code"`
		Params map[string]string `json:"params"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是 JSON: %v (%s)", err, rec.Body.String())
	}
	if body.OK || body.Code != "err_update_store_required" || body.Params["version"] != "2.0.0" {
		t.Fatalf("响应应带 code + version 参数，实得 %+v", body)
	}
	if st := m.getStatus(updateKindHarness); st.Phase != "" || st.Downloading {
		t.Fatalf("被拒绝时不应进入下载态，实得 phase=%q downloading=%v", st.Phase, st.Downloading)
	}
}

// 安装侧兜底：待安装包是跨线版本时，即使它已在盘上也不许装上（会得到「二进制换了、
// 平台侧没换」的半升级状态）。
func TestInstallUpdateRefusesCrossMinorPendingPackage(t *testing.T) {
	withHarnessVersion(t, "1.4.3")
	m := newChecksumTestManager(t)
	m.setPending(&PendingUpdate{
		Kind:    updateKindHarness,
		Version: "1.5.0",
		PkgPath: filepath.Join(m.pendingDir(), pendingPkgName(updateKindHarness, "1.5.0")),
	})

	err := m.installUpdate(updateKindHarness)
	if err == nil {
		t.Fatal("跨线版本的待安装包必须被拒绝")
	}
	if msg, ok := uiMsgOf(err); !ok || msg.Code != "err_update_store_required" {
		t.Fatalf("拒绝错误应带 code err_update_store_required，实得 %v", err)
	}
}
