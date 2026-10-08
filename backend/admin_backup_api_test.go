package main

// admin_backup_api_test.go —— dsh 数据备份三个接口的契约测试。
//
// 前端弹窗完全依赖这三个接口：启动（POST /api/dsh/backup）、进度（GET …/status）、
// 取消（POST …/cancel）。这里只测「状态码 + JSON 形状 + 幂等性」，真正的并发/互斥
// 语义在 update_backup_progress_test.go 里对着 UpdateManager 测。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// backupAPIResponse 是三个接口的公共响应形状（status 快照）。
type backupAPIResponse struct {
	OK      bool            `json:"ok"`
	Started bool            `json:"started"`
	Error   string          `json:"error"`
	Status  DshBackupStatus `json:"status"`
}

func callBackupAPI(t *testing.T, h http.HandlerFunc, method, target string) (*httptest.ResponseRecorder, backupAPIResponse) {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(method, target, nil))
	var body backupAPIResponse
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
	}
	return rec, body
}

func TestDshBackupAPIEndToEnd(t *testing.T) {
	t.Setenv("TRIM_PKGVAR", t.TempDir())
	home := t.TempDir()
	upd := newBackupTestManager(t, home, t.TempDir())
	resetDshBackupTracker(t)
	m := &AdminMux{update: upd}

	// 1) .dsh 不存在 → 400（不是 500，也不是静默成功）
	rec, body := callBackupAPI(t, m.handleDshBackup, http.MethodPost, "/api/dsh/backup")
	if rec.Code != http.StatusBadRequest || body.OK {
		t.Fatalf(".dsh 不存在时应 400，实际 %d %s", rec.Code, rec.Body.String())
	}

	// 2) 正常启动：200 + started + 快照（running）
	dshDir := filepath.Join(home, ".dsh")
	if err := os.MkdirAll(dshDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dshDir, "settings.yaml"), make([]byte, 1<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	rec, body = callBackupAPI(t, m.handleDshBackup, http.MethodPost, "/api/dsh/backup")
	if rec.Code != http.StatusOK || !body.OK || !body.Started {
		t.Fatalf("启动备份失败: %d %s", rec.Code, rec.Body.String())
	}

	// 3) 进度接口：直到落定，字段自洽（字节数不超过总量，文件数不超过总数）
	deadline := time.Now().Add(30 * time.Second)
	var st DshBackupStatus
	for time.Now().Before(deadline) {
		rec, body = callBackupAPI(t, m.handleDshBackupStatus, http.MethodGet, "/api/dsh/backup/status")
		if rec.Code != http.StatusOK || !body.OK {
			t.Fatalf("进度接口应 200: %d %s", rec.Code, rec.Body.String())
		}
		st = body.Status
		if st.Bytes > st.TotalBytes || st.Files > st.TotalFiles {
			t.Fatalf("进度字段越界: %+v", st)
		}
		if st.Done {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !st.Done || !st.Ok {
		t.Fatalf("备份应成功结束: %+v", st)
	}
	// seq 是「本次备份」的标识：前端靠它给同一份终态快照去重提示，必须非零且稳定。
	if st.Seq <= 0 {
		t.Fatalf("结束快照应带非零 seq: %+v", st)
	}
	if st.TotalBytes != 1<<20 || st.Bytes != 1<<20 {
		t.Fatalf("进度应覆盖整个目录: %+v", st)
	}

	// 4) 取消接口在「没有备份在跑」时是幂等的空操作（前端可能重复点/晚到）
	rec, body = callBackupAPI(t, m.handleDshBackupCancel, http.MethodPost, "/api/dsh/backup/cancel")
	if rec.Code != http.StatusOK || !body.OK || body.Status.Running {
		t.Fatalf("空闲时取消除应为 200 + ok（幂等）: %d %s", rec.Code, rec.Body.String())
	}
	// 上一步的成功结论必须保留（取消不能把「已完成」改写成「已取消」）
	if !body.Status.Done || !body.Status.Ok || body.Status.Cancelled {
		t.Fatalf("空闲取消不应改写上一轮结论: %+v", body.Status)
	}
	// 备份产物仍在（取消没有误删已完成的那份）
	if names := listBackupNames(t, upd, "dsh-data-"); len(names) != 1 {
		t.Fatalf("已完成的备份不应被取消动作删掉: %v", names)
	}
}
