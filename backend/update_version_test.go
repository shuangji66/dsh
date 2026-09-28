package main

// update_version_test.go —— dsh 本地版本号刷新与「是否有更新」结论的一致性。
//
// 回归点一：dsh 服务回滚（RollbackServer）后本地版本变旧、LatestVersion 未变，
// 若只更新 LocalVersion 而沿用上一次检测留下的 HasUpdate=false，版本行/更新弹窗
// 会出现「本地 0.1.7 / 最新 0.1.9」却写着「已是最新」、右上角红点不亮。
// 前端红点只看 hasUpdate（UpdateSection.vue 的 hasUpdateDot），因此结论必须在这里重算。
//
// 回归点二：更新 dsh 服务与回滚 server 备份都是整目录替换 server 产物，包内自带的
// dshmarket 随之换版 —— 版本号必须连市场那份一起刷（refreshServerPackageVersions），
// 只刷 dsh 会让市场版本行停在旧值。

import (
	"os"
	"path/filepath"
	"testing"
)

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

// fakeDshVersion 造一个只回显版本号的假 `dsh`，并把 PATH 指向它：测试里不该真去执行
// 设备上的 dsh（要加载 node 环境、慢，而且会碰真实 profile 目录）。
func fakeDshVersion(t *testing.T, version string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\necho " + version + "\n"
	if err := os.WriteFile(filepath.Join(dir, "dsh"), []byte(script), 0o755); err != nil {
		t.Fatalf("写假 dsh 失败: %v", err)
	}
	t.Setenv("PATH", dir)
}

// TestRefreshServerPackageVersions 钉住「替换 server 目录后，dsh 与包内自带的
// dshmarket 两个版本号必须一起刷新」：更新 dsh 服务与回滚 server 备份都是整目录替换，
// 只刷 dsh 版本会让市场版本行停在旧值（红点也按旧版本算）。
func TestRefreshServerPackageVersions(t *testing.T) {
	home := t.TempDir()
	serverDir := t.TempDir()

	prevServerDir := serverDirFn
	serverDirFn = func(*UpdateManager) string { return serverDir }
	prevCfg := GetConfig()
	initConfig(&AppConfig{HomeDir: home, DshPort: 0})
	t.Cleanup(func() {
		serverDirFn = prevServerDir
		initConfig(&prevCfg)
	})

	// server 包自带的 dshmarket（兜底扫描那两个位置之一）。
	marketDir := filepath.Join(serverDir, "node_modules", marketPackageName)
	writeMarketPackage(t, marketDir, "1.0.0")
	fakeDshVersion(t, "1.0.7")

	m := &UpdateManager{
		renv:     &RuntimeEnv{Home: home},
		dsh:      &DshManager{renv: &RuntimeEnv{Home: home}},
		statuses: map[updateKind]*UpdateStatus{},
	}
	// 两个目标的「仓库最新版」都比本地新 → 刷完版本号后红点都该亮。
	m.statuses[updateKindDsh] = &UpdateStatus{Kind: updateKindDsh, LatestVersion: "1.1.0"}
	m.statuses[updateKindMarket] = &UpdateStatus{Kind: updateKindMarket, LatestVersion: "1.1.0"}

	m.refreshServerPackageVersions()

	if st := m.getStatus(updateKindDsh); st.LocalVersion != "1.0.7" || !st.HasUpdate {
		t.Errorf("dsh 版本 = %q, hasUpdate = %v, want 1.0.7 / true", st.LocalVersion, st.HasUpdate)
	}
	st := m.getStatus(updateKindMarket)
	if st.LocalVersion != "1.0.0" || st.MarketScope != string(marketScopeServer) || !st.HasUpdate {
		t.Errorf("市场版本 = %q, scope = %q, hasUpdate = %v, want 1.0.0 / server / true",
			st.LocalVersion, st.MarketScope, st.HasUpdate)
	}

	// 模拟「更新 dsh 服务 / 回滚 server 备份」：server 目录整目录被替换，dsh 本体与
	// 自带的 dshmarket 同时换版 → 再刷一次，两个版本号都必须跟着变、红点都该灭。
	writeMarketPackage(t, marketDir, "1.1.0")
	fakeDshVersion(t, "1.1.0")
	m.refreshServerPackageVersions()

	if st := m.getStatus(updateKindDsh); st.LocalVersion != "1.1.0" || st.HasUpdate {
		t.Errorf("替换后 dsh 版本 = %q, hasUpdate = %v, want 1.1.0 / false", st.LocalVersion, st.HasUpdate)
	}
	if st := m.getStatus(updateKindMarket); st.LocalVersion != "1.1.0" || st.HasUpdate {
		t.Errorf("替换后市场版本 = %q, hasUpdate = %v, want 1.1.0 / false", st.LocalVersion, st.HasUpdate)
	}
}
