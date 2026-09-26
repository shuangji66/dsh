package main

// update_checksum_test.go —— 发布资产（harness / dsh server）sha256 校验的回归测试。
//
// 规则见 update.go 的「发布资产的 sha256 校验」一节：
//   - 自我更新时把 `.sha256` 与包一并取回，用**实际下载到的字节**复核摘要；
//   - 校验全程静默（不写更新状态、不进弹窗），**只有摘要不符**才失败并进弹窗；
//   - 校验文件缺失（老发布资产）/ 取不到 / 内容无法解析时只记 WARN，退化为不校验，
//     绝不因此阻塞更新 —— 这条是刻意的策略选择；
//   - 下载阶段校验过的摘要在安装前再复核一次（跨「下载 → 安装」两步）。
//
// 用 httptest 起真实 HTTP 服务：404、摘要不符、通退回退都能真实复现。

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// releaseAssetServer 起一个「release 资产」测试服务：非 `.sha256` 路径给包，
// `.sha256` 路径给校验文件正文；checksumBody 为空时 `.sha256` 返回 404
// （模拟「该版本发布在带校验文件之前」）。
func releaseAssetServer(t *testing.T, pkg []byte, checksumBody string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			if checksumBody == "" {
				http.NotFound(w, r)
				return
			}
			_, _ = io.WriteString(w, checksumBody)
			return
		}
		_, _ = w.Write(pkg)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newChecksumTestManager 造一个只碰临时目录、且「harness 最新版本号已就绪」的 UpdateManager。
func newChecksumTestManager(t *testing.T) *UpdateManager {
	t.Helper()
	t.Setenv("TRIM_PKGVAR", t.TempDir()) // pending/ 与 backup/ 落在临时目录
	t.Setenv("TRIM_SYS_ARCH", "x86")     // 资产名固定为 x86，不随宿主架构变化
	return &UpdateManager{
		renv: &RuntimeEnv{Home: t.TempDir(), Path: t.TempDir()},
		statuses: map[updateKind]*UpdateStatus{
			updateKindHarness: {Kind: updateKindHarness, LatestVersion: "1.0.2"},
		},
	}
}

// useReleaseAsset 把发布资产地址指向测试服务，并让下载走它的直连通路。
func useReleaseAsset(t *testing.T, srv *httptest.Server, assetName string) {
	t.Helper()
	prev := assetURLFn
	assetURLFn = func(*UpdateManager, updateKind, string, string) string {
		return srv.URL + "/" + assetName
	}
	t.Cleanup(func() { assetURLFn = prev })
	useRoutes(t, []updateRoute{directRoute(srv)})
}

// sha256Of 返回字节的十六进制摘要（小写）。
func sha256Of(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// --- 解析与比对 ---

func TestParseChecksumFile(t *testing.T) {
	good := strings.Repeat("a", 64)
	cases := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{name: "sha256sum 输出", body: good + "  harness-1.0.2-x86.tar.gz\n", want: good},
		{name: "大写摘要归一化", body: strings.ToUpper(good) + "  pkg\n", want: good},
		{name: "忽略前导空行", body: "\n\n" + good + "\n", want: good},
		{name: "空文件", body: "", wantErr: true},
		{name: "只有换行", body: "\n\n", wantErr: true},
		{name: "长度不对", body: "abc123  pkg\n", wantErr: true},
		{name: "不是十六进制", body: strings.Repeat("z", 64) + "  pkg\n", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseChecksumFile(tc.body)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("应报错，实际得到 %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if got != tc.want {
				t.Fatalf("摘要 = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestVerifyFileSHA256(t *testing.T) {
	data := []byte("harness package bytes")
	path := filepath.Join(t.TempDir(), "harness-1.0.2-x86.tar.gz")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyFileSHA256(path, sha256Of(data)); err != nil {
		t.Fatalf("摘要一致时应通过: %v", err)
	}
	// 期望值大小写不该影响判定（校验文件可能由别的工具生成）。
	if err := verifyFileSHA256(path, strings.ToUpper(sha256Of(data))); err != nil {
		t.Fatalf("期望值大小写不同仍应通过: %v", err)
	}
	err := verifyFileSHA256(path, sha256Of([]byte("被替换过的字节")))
	if err == nil {
		t.Fatal("摘要不符应报错")
	}
	if !strings.Contains(err.Error(), "不匹配") {
		t.Fatalf("错误应说明摘要不匹配，实际: %v", err)
	}
}

// --- 下载阶段校验 ---

// 摘要一致：进入「已下载待安装」，并把校验过的摘要记进 pending（安装前还要复核）。
func TestDownloadVerifiesReleaseChecksum(t *testing.T) {
	pkg := testPayload(32 << 10)
	want := sha256Of(pkg)
	srv := releaseAssetServer(t, pkg, want+"  harness-1.0.2-x86.tar.gz\n")
	m := newChecksumTestManager(t)
	useReleaseAsset(t, srv, "harness-1.0.2-x86.tar.gz")

	if err := m.downloadUpdate(updateKindHarness); err != nil {
		t.Fatalf("校验通过时下载应成功: %v", err)
	}
	p := m.getPending(updateKindHarness)
	if p == nil {
		t.Fatal("下载成功后应记录待安装包")
	}
	if p.SHA256 != want {
		t.Fatalf("待安装包应记下校验过的摘要 = %q, want %q", p.SHA256, want)
	}
	got, err := os.ReadFile(p.PkgPath)
	if err != nil {
		t.Fatal(err)
	}
	if sha256Of(got) != want {
		t.Fatal("盘上字节与源不一致")
	}
	st := m.getStatus(updateKindHarness)
	if st.Phase != "downloaded" || !st.ReadyToInstall {
		t.Fatalf("应进入已下载待安装，实际 phase=%q ready=%v", st.Phase, st.ReadyToInstall)
	}
	if st.Error != "" {
		t.Fatalf("校验成功时状态里不应有错误: %q", st.Error)
	}
}

// 摘要不符：下载报错（错误经下载流程推给弹窗）、坏包删除、不进入待安装。
func TestDownloadRejectsPackageWithBadChecksum(t *testing.T) {
	pkg := testPayload(32 << 10)
	srv := releaseAssetServer(t, pkg, sha256Of([]byte("别的字节"))+"  harness-1.0.2-x86.tar.gz\n")
	m := newChecksumTestManager(t)
	useReleaseAsset(t, srv, "harness-1.0.2-x86.tar.gz")

	err := m.downloadUpdate(updateKindHarness)
	if err == nil {
		t.Fatal("摘要不符应拒绝这次下载")
	}
	if !strings.Contains(err.Error(), "sha256 校验失败") {
		t.Fatalf("错误应说明校验失败（前端把它显示在更新弹窗里）: %v", err)
	}
	if m.getPending(updateKindHarness) != nil {
		t.Fatal("校验失败的包不应进入「已下载待安装」")
	}
	pkgPath := filepath.Join(m.pendingDir(), "harness-1.0.2.tar.gz")
	if _, statErr := os.Stat(pkgPath); !os.IsNotExist(statErr) {
		t.Fatal("校验失败的包必须删掉：留着会让下次续传从错误位置接")
	}
	st := m.getStatus(updateKindHarness)
	if st.Phase != "" || st.Downloading || st.Paused {
		t.Fatalf("失败后应退出下载中状态，实际 phase=%q downloading=%v paused=%v", st.Phase, st.Downloading, st.Paused)
	}
}

// 校验文件缺失（老发布资产）：只记 WARN，更新照常进行（不阻塞）。
func TestDownloadSkipsVerificationWhenChecksumMissing(t *testing.T) {
	pkg := testPayload(16 << 10)
	srv := releaseAssetServer(t, pkg, "") // .sha256 → 404
	m := newChecksumTestManager(t)
	useReleaseAsset(t, srv, "harness-1.0.2-x86.tar.gz")

	if err := m.downloadUpdate(updateKindHarness); err != nil {
		t.Fatalf("缺校验文件不应阻塞更新: %v", err)
	}
	p := m.getPending(updateKindHarness)
	if p == nil {
		t.Fatal("下载成功后应记录待安装包")
	}
	if p.SHA256 != "" {
		t.Fatalf("没有校验文件时不该记下摘要，实际 %q", p.SHA256)
	}
	if _, err := os.Stat(p.PkgPath); err != nil {
		t.Fatalf("包应已就位: %v", err)
	}
	if st := m.getStatus(updateKindHarness); st.Phase != "downloaded" {
		t.Fatalf("应进入已下载待安装，实际 phase=%q", st.Phase)
	}
}

// 取校验文件也走「代理 → 直连」的通路序列，且全程不改更新状态（静默）。
func TestReleaseChecksumFallsBackToDirectRoute(t *testing.T) {
	pkg := testPayload(1 << 10)
	want := sha256Of(pkg)
	srv := releaseAssetServer(t, pkg, want+"  harness-1.0.2-x86.tar.gz\n")
	m := newChecksumTestManager(t)

	var proxyCalls int32
	plan := releasePlan(proxyRouteForTest(&proxyCalls), directRoute(srv))
	got := m.releaseChecksum(updateKindHarness, srv.URL+"/harness-1.0.2-x86.tar.gz", plan, nil)
	if got != want {
		t.Fatalf("代理失败后应回退直连取到摘要 = %q, want %q", got, want)
	}
	if atomic.LoadInt32(&proxyCalls) == 0 {
		t.Fatal("代理通路应被尝试过（顺序：代理在前、直连兜底）")
	}
	st := m.getStatus(updateKindHarness)
	if st.Phase != "" || st.Downloading || st.Error != "" {
		t.Fatalf("取校验文件应静默：不该改动更新状态，实际 phase=%q downloading=%v error=%q", st.Phase, st.Downloading, st.Error)
	}
}

// 校验文件不存在时 releaseChecksum 返回空串（不报错），由调用方退化为不校验。
func TestReleaseChecksumMissingFileReturnsEmpty(t *testing.T) {
	srv := releaseAssetServer(t, testPayload(1<<10), "")
	m := newChecksumTestManager(t)

	got := m.releaseChecksum(updateKindHarness, srv.URL+"/harness-1.0.2-x86.tar.gz", releasePlan(directRoute(srv)), nil)
	if got != "" {
		t.Fatalf("缺校验文件应返回空串（退化为不校验），实际 %q", got)
	}
}

// --- 安装阶段复核 ---

// 安装前复核摘要：不符则拒绝安装（错误进弹窗），且保留包由用户决定是否重下。
func TestInstallRejectsTamperedPendingPackage(t *testing.T) {
	m := newChecksumTestManager(t)
	pkgPath := filepath.Join(m.pendingDir(), "harness-1.0.2.tar.gz")
	if err := os.WriteFile(pkgPath, []byte("下载后被替换过的字节"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.setPending(&PendingUpdate{
		Kind:    updateKindHarness,
		Version: "1.0.2",
		PkgPath: pkgPath,
		SHA256:  sha256Of([]byte("原始的字节")),
	})

	err := m.installUpdate(updateKindHarness)
	if err == nil {
		t.Fatal("待安装包摘要不符应拒绝安装")
	}
	if !strings.Contains(err.Error(), "sha256 校验失败") {
		t.Fatalf("错误应说明校验失败（前端把它显示在更新弹窗里）: %v", err)
	}
	if _, statErr := os.Stat(pkgPath); statErr != nil {
		t.Fatalf("校验失败时应保留待安装包（用户可删除后重新下载）: %v", statErr)
	}
}

// 摘要一致时复核放行：继续走到解压（内容不是 tar.gz，因此在解压阶段失败 ——
// 恰好证明校验没有把它拦下）。
func TestInstallPassesChecksumThenExtracts(t *testing.T) {
	m := newChecksumTestManager(t)
	content := []byte("这不是一个 tar.gz")
	pkgPath := filepath.Join(m.pendingDir(), "harness-1.0.2.tar.gz")
	if err := os.WriteFile(pkgPath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	m.setPending(&PendingUpdate{
		Kind:    updateKindHarness,
		Version: "1.0.2",
		PkgPath: pkgPath,
		SHA256:  sha256Of(content),
	})

	err := m.installUpdate(updateKindHarness)
	if err == nil || !strings.Contains(err.Error(), "解压") {
		t.Fatalf("摘要一致时应继续到解压阶段，实际 err=%v", err)
	}
}

// 没有摘要（缺校验文件的那次下载）时安装不做复核，行为与改动前一致。
func TestInstallSkipsChecksumWhenAbsent(t *testing.T) {
	m := newChecksumTestManager(t)
	pkgPath := filepath.Join(m.pendingDir(), "harness-1.0.2.tar.gz")
	if err := os.WriteFile(pkgPath, []byte("not a tarball"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.setPending(&PendingUpdate{Kind: updateKindHarness, Version: "1.0.2", PkgPath: pkgPath})

	err := m.installUpdate(updateKindHarness)
	if err == nil || !strings.Contains(err.Error(), "解压") {
		t.Fatalf("无摘要时不该因校验失败，应继续到解压阶段，实际 err=%v", err)
	}
}
