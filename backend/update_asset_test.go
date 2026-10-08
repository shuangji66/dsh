package main

// update_asset_test.go —— 发布资产与待安装包的命名契约。
//
// 命名是跨仓库的约定：`.github/workflows/harness-build.yaml` 按这里的规则生成并上传
// 资产，控制台按同一规则拼地址下载（`assetURL` / `checksumURL`）。命名改了一侧而漏改
// 另一侧**不会报错**，只会让下载 404 或让 sha256 校验静默退化成「不校验」，因此在这里钉住。
//
// 现在只有 harness 控制台走这条通路：dsh 服务由控制台 `npm install` 官方 npm 包
// （见 server.go），插件市场由 `dsh plugin add` 安装（见 market.go），两者都不再有
// 「下载压缩包 → 解压安装」这一步。

import (
	"strings"
	"testing"
)

// harness 资产的地址与校验文件地址（`.sha256` 必须与包同名）。
func TestAssetURLHarnessOnly(t *testing.T) {
	m := &UpdateManager{}

	harness := m.assetURL(updateKindHarness, "1.2.6", "arm")
	if !strings.HasSuffix(harness, "/harness-1.2.6/harness-1.2.6-arm.tar.gz") {
		t.Errorf("harness 资产地址 = %q, want 以 harness-<版本>-arm.tar.gz 结尾", harness)
	}
	if got := checksumURL(harness); got != harness+".sha256" || !strings.HasSuffix(got, ".tar.gz.sha256") {
		t.Errorf("harness 校验文件地址 = %q", got)
	}
	// x86 架构后缀同样固定。
	if got := m.assetURL(updateKindHarness, "1.2.6", "x86"); !strings.HasSuffix(got, "harness-1.2.6-x86.tar.gz") {
		t.Errorf("x86 资产地址 = %q", got)
	}
}

// 待安装包名恒为 `<kind>-<版本>.tar.gz`，且必须能被 pendingKind 反推回同一个 kind
// （清残留、日志标签都靠它）。
func TestPendingPkgNameUsesTarGz(t *testing.T) {
	cases := []struct {
		kind    updateKind
		version string
	}{
		{updateKindHarness, "1.2.6"},
		{updateKindDsh, "0.1.7-alpha.1"},
		{updateKindMarket, "0.19.1"},
	}
	for _, tc := range cases {
		name := pendingPkgName(tc.kind, tc.version)
		if want := string(tc.kind) + "-" + tc.version + ".tar.gz"; name != want {
			t.Errorf("pendingPkgName(%s, %s) = %q, want %q", tc.kind, tc.version, name, want)
		}
		if !strings.HasSuffix(name, ".tar.gz") {
			t.Errorf("待安装包 %q 必须是 .tar.gz（extractArchive 只认这一种）", name)
		}
		if got := pendingKind(name); got != tc.kind {
			t.Errorf("pendingKind(%q) = %q, want %q", name, got, tc.kind)
		}
	}
}

// 旧控制台留下的 `dsh-<版本>.tar.xz` 半成品也要能被认出归属（清理时不至于漏掉）。
func TestPendingKindAcceptsLegacyXzDshPackage(t *testing.T) {
	if got := pendingKind("dsh-0.1.6-alpha.1.tar.xz"); got != updateKindDsh {
		t.Errorf("pendingKind(旧 .tar.xz dsh 包) = %q, want %q", got, updateKindDsh)
	}
}

// dsh 与市场都不再支持「下载更新包」：请求必须被明确拒绝（而不是下出一份没人装的包）。
func TestDownloadUpdateRejectsDshAndMarket(t *testing.T) {
	m := newChecksumTestManager(t)
	m.statuses[updateKindDsh] = &UpdateStatus{Kind: updateKindDsh, LatestVersion: "0.1.7-alpha.1"}
	m.statuses[updateKindMarket] = &UpdateStatus{Kind: updateKindMarket, LatestVersion: "9.9.9"}
	for _, k := range []updateKind{updateKindDsh, updateKindMarket} {
		if err := m.downloadUpdate(k); err == nil {
			t.Errorf("%s 走下载压缩包通路应当被拒绝", k)
		}
	}
	if err := m.installUpdate(updateKindDsh); err == nil {
		t.Error("安装 dsh 更新包应当被拒绝")
	}
	if err := m.installUpdate(updateKindMarket); err == nil {
		t.Error("安装市场更新包应当被拒绝")
	}
}
