package main

// update_version_test.go —— dsh 本地版本号刷新与「是否有更新」结论的一致性。
//
// 回归点：dsh 服务回滚（RollbackServer）后本地版本变旧、LatestVersion 未变，
// 若只更新 LocalVersion 而沿用上一次检测留下的 HasUpdate=false，版本行/更新弹窗
// 会出现「本地 0.1.7 / 最新 0.1.9」却写着「已是最新」、右上角红点不亮。
// 前端红点只看 hasUpdate（UpdateSection.vue 的 hasUpdateDot），因此结论必须在这里重算。

import "testing"

func newDshStatusForTest(local, latest string, hasUpdate bool) *UpdateManager {
	m := &UpdateManager{statuses: map[updateKind]*UpdateStatus{}}
	m.statuses[updateKindDsh] = &UpdateStatus{
		Kind:          updateKindDsh,
		LocalVersion:  local,
		LatestVersion: latest,
		HasUpdate:     hasUpdate,
	}
	return m
}

func TestSetDshLocalVersionRecomputesHasUpdate(t *testing.T) {
	// 回滚场景：本在最新版（hasUpdate=false），回滚到旧版本后必须重新亮起红点。
	m := newDshStatusForTest("0.1.9", "0.1.9", false)
	m.setDshLocalVersion("0.1.7")
	st := m.getStatus(updateKindDsh)
	if st.LocalVersion != "0.1.7" {
		t.Fatalf("LocalVersion = %q, want 0.1.7", st.LocalVersion)
	}
	if !st.HasUpdate {
		t.Errorf("回滚到 0.1.7（最新 0.1.9）后 HasUpdate = false, want true")
	}
	if st.LatestVersion != "0.1.9" {
		t.Errorf("LatestVersion = %q, want 0.1.9（不应被清空）", st.LatestVersion)
	}

	// 更新安装场景：本地版本追平最新版后红点必须熄灭。
	m = newDshStatusForTest("0.1.7", "0.1.9", true)
	m.setDshLocalVersion("0.1.9")
	if st := m.getStatus(updateKindDsh); st.HasUpdate {
		t.Errorf("升级到 0.1.9 后 HasUpdate = true, want false")
	}

	// 预发布后缀按 compareVersion 的语义比较：本地 alpha.2 落后于最新 alpha.5。
	m = newDshStatusForTest("0.1.7-alpha.2", "0.1.7-alpha.5", false)
	m.setDshLocalVersion("0.1.7-alpha.2")
	if st := m.getStatus(updateKindDsh); !st.HasUpdate {
		t.Errorf("0.1.7-alpha.2 相对最新 0.1.7-alpha.5 应判定为有更新")
	}
}

func TestSetDshLocalVersionKeepsConclusionWhenLatestUnknown(t *testing.T) {
	// 从未成功检测到仓库版本（LatestVersion 为空）时不臆造结论，保持原状。
	m := newDshStatusForTest("0.1.7", "", false)
	m.setDshLocalVersion("0.1.6")
	st := m.getStatus(updateKindDsh)
	if st.LocalVersion != "0.1.6" {
		t.Fatalf("LocalVersion = %q, want 0.1.6", st.LocalVersion)
	}
	if st.HasUpdate {
		t.Errorf("LatestVersion 未知时 HasUpdate = true, want false")
	}
}
