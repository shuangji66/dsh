package main

// update_asset_test.go —— 发布资产与待安装包的命名契约。
//
// 命名是跨仓库的约定：`.github/workflows/server-build.yaml` 按这里的规则生成并上传资产，
// 控制台按同一规则拼地址下载（`assetURL` / `checksumURL`）。命名改了一侧而漏改另一侧
// **不会报错**，只会让下载 404 或让 sha256 校验静默退化成「不校验」，因此在这里钉住：
//   - dsh 服务：同时发布 .tar.gz 与 .tar.xz，新版控制台**只下载 .tar.xz**；
//     .tar.gz 只为兼容旧版控制台（它们只会拼 .tar.gz）而保留；
//   - harness 控制台：仍是 .tar.gz（发布与自更新都不变）；
//   - 本地备份：恒为 .tar.gz，与下载格式无关。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssetURLCompressionByKind(t *testing.T) {
	m := &UpdateManager{}

	dsh := m.assetURL(updateKindDsh, "0.1.7-alpha.1", "x86")
	if !strings.HasSuffix(dsh, "/dsh-0.1.7-alpha.1/server-x86-0.1.7-alpha.1.tar.xz") {
		t.Errorf("dsh 资产地址 = %q, want 以 /dsh-<版本>/server-x86-<版本>.tar.xz 结尾", dsh)
	}
	// 校验文件与包同名 + .sha256（发布侧必须生成 server-<arch>-<ver>.tar.xz.sha256）。
	if got := checksumURL(dsh); got != dsh+".sha256" || !strings.HasSuffix(got, ".tar.xz.sha256") {
		t.Errorf("dsh 校验文件地址 = %q", got)
	}

	harness := m.assetURL(updateKindHarness, "1.2.6", "arm")
	if !strings.HasSuffix(harness, "/harness-1.2.6/harness-1.2.6-arm.tar.gz") {
		t.Errorf("harness 资产地址 = %q, want 以 harness-<版本>-arm.tar.gz 结尾", harness)
	}
	if got := checksumURL(harness); got != harness+".sha256" || !strings.HasSuffix(got, ".tar.gz.sha256") {
		t.Errorf("harness 校验文件地址 = %q", got)
	}
}

// 待安装包名的扩展名必须与下载格式一致（安装时按扩展名选解压器）。
func TestPendingPkgNameByKind(t *testing.T) {
	cases := []struct {
		kind    updateKind
		version string
		want    string
	}{
		{updateKindDsh, "0.1.7-alpha.1", "dsh-0.1.7-alpha.1.tar.xz"},
		{updateKindHarness, "1.2.6", "harness-1.2.6.tar.gz"},
		{updateKindMarket, "0.19.1", "market-0.19.1.tar.gz"},
	}
	for _, tc := range cases {
		if got := pendingPkgName(tc.kind, tc.version); got != tc.want {
			t.Errorf("pendingPkgName(%s, %s) = %q, want %q", tc.kind, tc.version, got, tc.want)
		}
		// 文件名必须能被 pendingKind 反推回同一个 kind（清残留、日志标签都靠它）。
		if got := pendingKind(pendingPkgName(tc.kind, tc.version)); got != tc.kind {
			t.Errorf("pendingKind(%q) = %q, want %q", pendingPkgName(tc.kind, tc.version), got, tc.kind)
		}
	}
}

// 旧控制台留下的 .tar.gz 待安装包（dsh）仍要认得出来并照常解压安装。
func TestPendingKindAcceptsLegacyGzDshPackage(t *testing.T) {
	if got := pendingKind("dsh-0.1.6-alpha.1.tar.gz"); got != updateKindDsh {
		t.Errorf("pendingKind(旧 .tar.gz dsh 包) = %q, want %q", got, updateKindDsh)
	}
}

// dsh 服务端到端（下载 → 解压）：只取 `.tar.xz`，校验文件同名，待安装包名带 `.xz`，
// 且解压按扩展名走 xz 通路 —— 这条链路跨了 assetURL / pendingPkgName / extractArchive
// 三处，任一处漏改都会在这里红。
func TestDownloadDshServerPackageUsesXz(t *testing.T) {
	dir := t.TempDir()
	// 造一个真的 .tar.xz（与发布资产同构：顶层 server/ 目录）。
	src := filepath.Join(dir, "server-x86-0.1.7.tar.xz")
	writeTarXz(t, src, []tarEntry{
		{name: "server/", isDir: true},
		{name: "server/package.json", body: `{"name":"server","version":"0.1.7"}`},
	})
	pkg, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256Of(pkg)
	srv := releaseAssetServer(t, pkg, want+"  server-x86-0.1.7.tar.xz\n")

	m := newChecksumTestManager(t)
	m.statuses[updateKindDsh] = &UpdateStatus{Kind: updateKindDsh, LatestVersion: "0.1.7"}
	useReleaseAsset(t, srv, "server-x86-0.1.7.tar.xz")

	if err := m.downloadUpdate(updateKindDsh); err != nil {
		t.Fatalf("dsh 包下载应成功: %v", err)
	}
	p := m.getPending(updateKindDsh)
	if p == nil {
		t.Fatal("下载成功后应记录待安装包")
	}
	if base := filepath.Base(p.PkgPath); base != "dsh-0.1.7.tar.xz" {
		t.Fatalf("待安装包名 = %q, want dsh-0.1.7.tar.xz（扩展名必须与下载格式一致）", base)
	}
	if p.SHA256 != want {
		t.Fatalf("待安装包应记下校验过的摘要 = %q, want %q", p.SHA256, want)
	}

	// 安装阶段（installUpdate）按扩展名解压，这里直接验证同一条路径可用。
	out := filepath.Join(dir, "out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := extractArchive(p.PkgPath, out); err != nil {
		t.Fatalf("下载到的 .tar.xz 必须能解压: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(out, "server/package.json"))
	if err != nil || !strings.Contains(string(body), "0.1.7") {
		t.Fatalf("解压内容不符（body=%q err=%v）", body, err)
	}
}
