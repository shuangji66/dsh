package main

import (
	"crypto/sha1"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 本文件覆盖 market.go 的纯逻辑部分：安装位置解析、包校验、完整性校验、
// 目录原子替换与回滚、以及「dsh 起不来就回滚」的完整安装流程。
// dsh 的启停与就绪探测通过 market*.Fn 钩子注入，测试不碰真实进程。

// --- 测试脚手架 ---

func writeJSONFile(t *testing.T, path string, v interface{}) {
	t.Helper()
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// marketManifestFixture 造一份最小可用的市场 package.json。
func marketManifestFixture(version string, deps map[string]string) map[string]interface{} {
	if deps == nil {
		deps = map[string]string{"js-yaml": "^4.1.0", "undici": "^7.29.0"}
	}
	return map[string]interface{}{
		"name":         marketPackageName,
		"version":      version,
		"main":         "lib/index.js",
		"exports":      map[string]interface{}{"./client": "./client/client.js"},
		"dsh":          map[string]interface{}{"bundle": map[string]string{"patch": "./cordis.patch.yml"}},
		"dependencies": deps,
	}
}

// writeMarketPackage 造一份「看起来像 dshmarket」的安装目录。
func writeMarketPackage(t *testing.T, dir, version string) {
	t.Helper()
	writeJSONFile(t, filepath.Join(dir, "package.json"), marketManifestFixture(version, nil))
	if err := os.MkdirAll(filepath.Join(dir, "lib"), 0o755); err != nil {
		t.Fatalf("mkdir lib: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lib", "index.js"), []byte("// "+version+"\n"), 0o644); err != nil {
		t.Fatalf("write index.js: %v", err)
	}
}

// setupMarketTest 准备一个自洽的假环境：临时 HOME、临时 server 目录、注入过的
// dsh 启停钩子，并在测试结束时恢复所有全局状态。
func setupMarketTest(t *testing.T) (m *UpdateManager, home, serverDir, targetDir string, calls *[]string) {
	t.Helper()
	home = t.TempDir()
	serverDir = t.TempDir()
	targetDir = filepath.Join(serverDir, "node_modules", "@deepseek-ai", "dsh", "node_modules", marketPackageName)
	writeMarketPackage(t, targetDir, "1.0.0")

	// 共享 fallback 软链：$DSH_HOME/profiles/node_modules/dshmarket → server 内那份。
	linkDir := filepath.Join(home, ".dsh", "profiles", "node_modules")
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		t.Fatalf("mkdir link dir: %v", err)
	}
	if err := os.Symlink(targetDir, filepath.Join(linkDir, marketPackageName)); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	prevCfg := GetConfig()
	initConfig(&AppConfig{HomeDir: home, DshPort: 0})
	prevServerDir := marketServerDirFn
	marketServerDirFn = func(*UpdateManager) string { return serverDir }
	prevStop, prevStart, prevReady, prevPortFree := marketStopDshFn, marketStartDshFn, marketReadyFn, marketPortFreeFn
	seq := []string{}
	calls = &seq
	marketStopDshFn = func(*UpdateManager) error { seq = append(seq, "stop"); return nil }
	marketStartDshFn = func(*UpdateManager) error { seq = append(seq, "start"); return nil }
	marketReadyFn = func(*UpdateManager, time.Duration) marketWaitResult {
		seq = append(seq, "ready")
		return marketWaitReady
	}
	marketPortFreeFn = func(*UpdateManager, time.Duration) { seq = append(seq, "portfree") }
	t.Setenv("TRIM_PKGVAR", t.TempDir())
	t.Cleanup(func() {
		initConfig(&prevCfg)
		marketServerDirFn = prevServerDir
		marketStopDshFn, marketStartDshFn, marketReadyFn, marketPortFreeFn = prevStop, prevStart, prevReady, prevPortFree
	})

	m = &UpdateManager{
		renv:     &RuntimeEnv{Home: home},
		dsh:      &DshManager{},
		statuses: make(map[updateKind]*UpdateStatus),
	}
	m.statuses[updateKindMarket] = &UpdateStatus{Kind: updateKindMarket}
	m.refreshMarketLocal()
	return m, home, serverDir, targetDir, calls
}

// stagedMarketPackage 造一份「已解压的更新包」目录（npm tarball 解压后形如 package/…）。
func stagedMarketPackage(t *testing.T, version string, deps map[string]string) (extractDir, srcDir string) {
	t.Helper()
	extractDir = t.TempDir()
	srcDir = filepath.Join(extractDir, "package")
	writeJSONFile(t, filepath.Join(srcDir, "package.json"), marketManifestFixture(version, deps))
	if err := os.MkdirAll(filepath.Join(srcDir, "lib"), 0o755); err != nil {
		t.Fatalf("mkdir lib: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "lib", "index.js"), []byte("// "+version+"\n"), 0o644); err != nil {
		t.Fatalf("write index.js: %v", err)
	}
	return extractDir, srcDir
}

// writeDep 在 dir 下造一个最小可解析的依赖包。
func writeDep(t *testing.T, dir, name, version string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"),
		[]byte(`{"name":"`+name+`","version":"`+version+`"}`), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// writeResolvableDeps 按真实布局补上 dshmarket 的两个运行时依赖：
// js-yaml 是 @deepseek-ai/dsh 的嵌套依赖（与 dshmarket 同级），
// undici 提升在 server 根目录下。
func writeResolvableDeps(t *testing.T, targetDir, serverDir string) {
	t.Helper()
	writeDep(t, filepath.Join(filepath.Dir(targetDir), "js-yaml"), "js-yaml", "4.3.2")
	writeDep(t, filepath.Join(serverDir, "node_modules", "undici"), "undici", "7.29.1")
}

// --- 安装位置解析 ---

func TestResolveMarketTargetServerViaSharedFallback(t *testing.T) {
	home := t.TempDir()
	serverDir := t.TempDir()
	pkg := filepath.Join(serverDir, "node_modules", "@deepseek-ai", "dsh", "node_modules", marketPackageName)
	writeMarketPackage(t, pkg, "1.47.0")
	linkDir := filepath.Join(home, ".dsh", "profiles", "node_modules")
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(pkg, filepath.Join(linkDir, marketPackageName)); err != nil {
		t.Fatal(err)
	}

	got := resolveMarketTargetIn(home, serverDir)
	if got.Scope != marketScopeServer {
		t.Fatalf("scope = %q, want server（reason=%s）", got.Scope, got.Reason)
	}
	if got.Dir != pkg {
		t.Fatalf("dir = %q, want %q", got.Dir, pkg)
	}
	if got.Version != "1.47.0" {
		t.Fatalf("version = %q, want 1.47.0", got.Version)
	}
}

func TestResolveMarketTargetProfileWins(t *testing.T) {
	home := t.TempDir()
	serverDir := t.TempDir()
	pkg := filepath.Join(serverDir, "node_modules", "@deepseek-ai", "dsh", "node_modules", marketPackageName)
	writeMarketPackage(t, pkg, "1.47.0")

	// profile 自己的 node_modules 里有一份真实目录 → 它才是生效的那份。
	profilePkg := filepath.Join(home, ".dsh", "profiles", "web", "node_modules", marketPackageName)
	writeMarketPackage(t, profilePkg, "9.9.9")

	got := resolveMarketTargetIn(home, serverDir)
	if got.Scope != marketScopeProfile {
		t.Fatalf("scope = %q, want profile", got.Scope)
	}
	if got.Version != "9.9.9" || got.Dir != profilePkg {
		t.Fatalf("target = %+v", got)
	}
}

func TestResolveMarketTargetProfileSymlinkIntoServer(t *testing.T) {
	home := t.TempDir()
	serverDir := t.TempDir()
	pkg := filepath.Join(serverDir, "node_modules", "@deepseek-ai", "dsh", "node_modules", marketPackageName)
	writeMarketPackage(t, pkg, "1.47.0")
	profileModules := filepath.Join(home, ".dsh", "profiles", "web", "node_modules")
	if err := os.MkdirAll(profileModules, 0o755); err != nil {
		t.Fatal(err)
	}
	// profile 内的条目是 dsh 自己维护的镜像软链（指向 server 内那份）→ 仍按 server 处理。
	if err := os.Symlink(pkg, filepath.Join(profileModules, marketPackageName)); err != nil {
		t.Fatal(err)
	}

	got := resolveMarketTargetIn(home, serverDir)
	if got.Scope != marketScopeServer || got.Dir != pkg {
		t.Fatalf("target = %+v, want server scope at %s", got, pkg)
	}
}

func TestResolveMarketTargetExternalAndMissing(t *testing.T) {
	home := t.TempDir()
	serverDir := t.TempDir()
	// 完全不存在 → missing
	if got := resolveMarketTargetIn(home, serverDir); got.Scope != marketScopeMissing {
		t.Fatalf("scope = %q, want missing", got.Scope)
	}

	// 指向 server 目录之外的软链（如 link: 本地开发安装）→ external
	external := filepath.Join(t.TempDir(), marketPackageName)
	writeMarketPackage(t, external, "2.0.0")
	linkDir := filepath.Join(home, ".dsh", "profiles", "node_modules")
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(linkDir, marketPackageName)); err != nil {
		t.Fatal(err)
	}
	got := resolveMarketTargetIn(home, serverDir)
	if got.Scope != marketScopeExternal || got.Dir != external {
		t.Fatalf("target = %+v, want external at %s", got, external)
	}
}

// 回归测试：平台把 /var/apps/<App>/target 做成指向 /vol1/@appcenter/<App> 的软链，
// 于是 serverDir() 给的是非规范路径，而 dsh 自己维护的 profile 镜像软链解析出来是
// 规范路径。早前实现拿两者直接做字符串前缀比较，会把「server 包自带的 dshmarket」
// 误判成 external，前端据此禁用更新按钮。
func TestResolveMarketTargetHandlesSymlinkedServerDir(t *testing.T) {
	base := t.TempDir()
	// 真实安装树：<base>/vol1/@appcenter/Harness/server/...
	realRoot := filepath.Join(base, "vol1", "@appcenter", "Harness")
	pkg := filepath.Join(realRoot, "server", "node_modules", "@deepseek-ai", "dsh", "node_modules", marketPackageName)
	writeMarketPackage(t, pkg, "1.47.0")

	// /var/apps/Harness/target -> <base>/vol1/@appcenter/Harness
	linkParent := filepath.Join(base, "apps", "Harness")
	if err := os.MkdirAll(linkParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realRoot, filepath.Join(linkParent, "target")); err != nil {
		t.Fatal(err)
	}

	// $DSH_HOME/profiles/node_modules/dshmarket -> server 内那份（dsh 自维护的镜像软链）
	home := filepath.Join(base, "appshare", "Harness")
	linkDir := filepath.Join(home, ".dsh", "profiles", "node_modules")
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(pkg, filepath.Join(linkDir, marketPackageName)); err != nil {
		t.Fatal(err)
	}

	// serverDir 走软链路径（与线上 serverDir() 返回的形态一致）
	serverDirViaLink := filepath.Join(linkParent, "target", "server")
	// 用例自身有效性检查：该路径必须真的经过软链，否则测不到这个 bug。
	if canonicalPath(serverDirViaLink) == serverDirViaLink {
		t.Fatalf("用例失效：%s 解析后未变化", serverDirViaLink)
	}

	got := resolveMarketTargetIn(home, serverDirViaLink)
	if got.Scope != marketScopeServer {
		t.Fatalf("scope = %q, want server（reason=%s）", got.Scope, got.Reason)
	}
	if got.Version != "1.47.0" {
		t.Fatalf("version = %q, want 1.47.0", got.Version)
	}
	// Dir 必须是规范路径：替换/备份用的就是它。
	if want := canonicalPath(pkg); got.Dir != want {
		t.Fatalf("dir = %q, want %q", got.Dir, want)
	}
}

// --- 包校验 ---

func TestValidateMarketPackage(t *testing.T) {
	extractDir, srcDir := stagedMarketPackage(t, "1.50.0", nil)
	if _, err := validateMarketPackage(srcDir, "1.50.0"); err != nil {
		t.Fatalf("合法包被拒: %v", err)
	}
	if _, err := validateMarketPackage(srcDir, "1.49.0"); err == nil {
		t.Fatal("版本不符应当被拒")
	}
	_ = extractDir

	cases := []struct {
		name   string
		mutate func(dir string)
	}{
		{"包名不符", func(dir string) {
			writeJSONFile(t, filepath.Join(dir, "package.json"),
				map[string]interface{}{"name": "not-dshmarket", "version": "1.50.0"})
		}},
		{"缺 lib 目录", func(dir string) {
			if err := os.RemoveAll(filepath.Join(dir, "lib")); err != nil {
				t.Fatal(err)
			}
		}},
		{"缺 dsh.bundle.patch", func(dir string) {
			m := marketManifestFixture("1.50.0", nil)
			delete(m, "dsh")
			writeJSONFile(t, filepath.Join(dir, "package.json"), m)
		}},
		{"缺 exports[./client]", func(dir string) {
			m := marketManifestFixture("1.50.0", nil)
			delete(m, "exports")
			writeJSONFile(t, filepath.Join(dir, "package.json"), m)
		}},
		{"缺 main", func(dir string) {
			m := marketManifestFixture("1.50.0", nil)
			delete(m, "main")
			writeJSONFile(t, filepath.Join(dir, "package.json"), m)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, dir := stagedMarketPackage(t, "1.50.0", nil)
			c.mutate(dir)
			if _, err := validateMarketPackage(dir, "1.50.0"); err == nil {
				t.Fatalf("应当被拒: %s", c.name)
			}
		})
	}
}

func TestMarketUnresolvedDeps(t *testing.T) {
	serverDir := t.TempDir()
	targetDir := filepath.Join(serverDir, "node_modules", "@deepseek-ai", "dsh", "node_modules", marketPackageName)
	writeMarketPackage(t, targetDir, "1.0.0")
	// 只提供 js-yaml（放在 @deepseek-ai/dsh 的 node_modules 下，真实布局即如此）。
	writeDep(t, filepath.Join(filepath.Dir(targetDir), "js-yaml"), "js-yaml", "4.3.2")

	doc := &marketManifest{Dependencies: map[string]string{
		"js-yaml": "^4.1.0", "undici": "^7.29.0", "@deepseek-ai/cordis": "^4.0.1",
	}}
	// undici 缺失，@deepseek-ai/* 由宿主提供、不检查。
	if missing := marketUnresolvedDeps(doc, targetDir); len(missing) != 1 || missing[0] != "undici" {
		t.Fatalf("missing = %v, want [undici]", missing)
	}

	// 把 undici 放到 server 根（Node 的父链查找会命中）→ 不再缺失。
	writeDep(t, filepath.Join(serverDir, "node_modules", "undici"), "undici", "7.29.1")
	if missing := marketUnresolvedDeps(doc, targetDir); len(missing) != 0 {
		t.Fatalf("missing = %v, want none", missing)
	}
}

// --- 完整性校验 ---

func TestVerifyFileIntegrity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pkg.tgz")
	payload := []byte("dshmarket payload")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	sha512sum := sha512.Sum512(payload)
	sha1sum := sha1.Sum(payload)
	good := "sha512-" + base64.StdEncoding.EncodeToString(sha512sum[:])
	bad := "sha512-" + base64.StdEncoding.EncodeToString(make([]byte, sha512.Size))

	if err := verifyFileIntegrity(path, good, ""); err != nil {
		t.Fatalf("正确 integrity 被拒: %v", err)
	}
	if err := verifyFileIntegrity(path, bad, ""); err == nil {
		t.Fatal("错误 integrity 应当被拒")
	}
	if err := verifyFileIntegrity(path, "", hex.EncodeToString(sha1sum[:])); err != nil {
		t.Fatalf("正确 shasum 被拒: %v", err)
	}
	if err := verifyFileIntegrity(path, "", strings.Repeat("0", 40)); err == nil {
		t.Fatal("错误 shasum 应当被拒")
	}
	if err := verifyFileIntegrity(path, "md5-whatever", ""); err == nil {
		t.Fatal("不支持的算法应当被拒")
	}
}

// --- 目录替换与回滚 ---

func TestSwapMarketDirReplaceAndRollback(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TRIM_PKGVAR", tmp)
	m := &UpdateManager{}

	targetDir := filepath.Join(tmp, "server", "node_modules", "@deepseek-ai", "dsh", "node_modules", marketPackageName)
	writeMarketPackage(t, targetDir, "1.0.0")
	_, srcDir := stagedMarketPackage(t, "2.0.0", nil)

	rollback, _, err := m.swapMarketDir(targetDir, srcDir, "1.0.0")
	if err != nil {
		t.Fatalf("swap 失败: %v", err)
	}
	if v, ok := readMarketManifest(targetDir); !ok || v != "2.0.0" {
		t.Fatalf("替换后版本 = %q ok=%v, want 2.0.0", v, ok)
	}
	// 备份文件名必须是 market-<版本>-<14位时间戳>.tar.gz
	entries, err := os.ReadDir(m.backupDir())
	if err != nil {
		t.Fatal(err)
	}
	var backup string
	for _, e := range entries {
		if isBackupFile(e.Name(), marketBackupPrefix) {
			backup = e.Name()
		}
	}
	if backup == "" {
		t.Fatalf("未生成 market- 前缀备份: %v", entries)
	}
	if !strings.HasPrefix(backup, "market-1.0.0-") {
		t.Fatalf("备份名 = %q, want market-1.0.0-<时间戳>.tar.gz", backup)
	}
	// 不能出现在 dsh 服务回滚列表里。
	backups, err := m.ListServerBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Fatalf("市场备份污染了 server 回滚列表: %+v", backups)
	}

	// 回滚 → 旧版本（含文件内容）必须回到原位。
	if err := rollback(); err != nil {
		t.Fatalf("rollback 失败: %v", err)
	}
	if v, ok := readMarketManifest(targetDir); !ok || v != "1.0.0" {
		t.Fatalf("回滚后版本 = %q ok=%v, want 1.0.0", v, ok)
	}
	raw, err := os.ReadFile(filepath.Join(targetDir, "lib", "index.js"))
	if err != nil || string(raw) != "// 1.0.0\n" {
		t.Fatalf("回滚后文件内容不对: %q err=%v", raw, err)
	}

	// 再换一次并正常收尾：不应留下 .old- / staging 残留。
	rollback2, cleanup2, err := m.swapMarketDir(targetDir, srcDir, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	_ = rollback2
	cleanup2()
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(targetDir), marketPackageName+".*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("残留目录: %v", leftovers)
	}
}

// --- 完整安装流程（含回滚） ---

func TestInstallMarketReplacesAndRestartsDsh(t *testing.T) {
	m, _, serverDir, targetDir, calls := setupMarketTest(t)
	extractDir, _ := stagedMarketPackage(t, "2.0.0", nil)
	writeResolvableDeps(t, targetDir, serverDir)

	p := &PendingUpdate{Kind: updateKindMarket, Version: "2.0.0"}
	if err := m.installMarket(p, extractDir); err != nil {
		t.Fatalf("installMarket 失败: %v", err)
	}
	if v, ok := readMarketManifest(targetDir); !ok || v != "2.0.0" {
		t.Fatalf("安装后版本 = %q ok=%v, want 2.0.0", v, ok)
	}
	// 顺序：先停 dsh，替换完再拉起（用户可见语义：提示 → 停服务 → 更新 → 自动拉起）。
	got := strings.Join(*calls, ",")
	if !strings.Contains(got, "stop") || !strings.Contains(got, "start") {
		t.Fatalf("未按预期启停 dsh: %v", *calls)
	}
	if idx := indexOf(*calls, "stop"); idx < 0 || indexOf(*calls, "start") < idx {
		t.Fatalf("start 必须发生在 stop 之后: %v", *calls)
	}
}

func TestInstallMarketRollsBackWhenDshNotReady(t *testing.T) {
	m, _, serverDir, targetDir, calls := setupMarketTest(t)
	marketReadyFn = func(*UpdateManager, time.Duration) marketWaitResult {
		*calls = append(*calls, "ready")
		return marketWaitTimeout
	}

	extractDir, _ := stagedMarketPackage(t, "2.0.0", nil)
	writeResolvableDeps(t, targetDir, serverDir)

	pkgPath := filepath.Join(m.pendingDir(), "market-2.0.0-test.tgz")
	if err := os.WriteFile(pkgPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &PendingUpdate{Kind: updateKindMarket, Version: "2.0.0", PkgPath: pkgPath}
	m.setPending(p)

	err := m.installMarket(p, extractDir)
	if err == nil {
		t.Fatal("dsh 未就绪时应当返回错误")
	}
	if !strings.Contains(err.Error(), "回滚") {
		t.Fatalf("错误信息未说明回滚: %v", err)
	}
	// 回滚后必须是旧版本，且旧文件内容原样。
	if v, ok := readMarketManifest(targetDir); !ok || v != "1.0.0" {
		t.Fatalf("回滚后版本 = %q ok=%v, want 1.0.0", v, ok)
	}
	raw, _ := os.ReadFile(filepath.Join(targetDir, "lib", "index.js"))
	if string(raw) != "// 1.0.0\n" {
		t.Fatalf("回滚后文件内容不对: %q", raw)
	}
	// 回滚后要重新拉起 dsh（两次 start）。
	starts := 0
	for _, c := range *calls {
		if c == "start" {
			starts++
		}
	}
	if starts != 2 {
		t.Fatalf("start 次数 = %d, want 2（替换后一次 + 回滚后一次）: %v", starts, *calls)
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(targetDir), marketPackageName+".*"))
	if len(leftovers) != 0 {
		t.Fatalf("回滚后残留目录: %v", leftovers)
	}
	// 失败时不清 pending（用户可重试/排查）。
	if m.getPending() == nil {
		t.Fatal("安装失败时不应清掉待安装包")
	}
}

func TestInstallMarketRefusesProfileScope(t *testing.T) {
	m, home, _, targetDir, calls := setupMarketTest(t)
	// profile 自己装了一份 → 控制台不该动 server 里那份。
	profilePkg := filepath.Join(home, ".dsh", "profiles", "web", "node_modules", marketPackageName)
	writeMarketPackage(t, profilePkg, "3.0.0")

	extractDir, _ := stagedMarketPackage(t, "2.0.0", nil)
	err := m.installMarket(&PendingUpdate{Kind: updateKindMarket, Version: "2.0.0"}, extractDir)
	if err == nil || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("应当拒绝并说明原因，err = %v", err)
	}
	if v, _ := readMarketManifest(targetDir); v != "1.0.0" {
		t.Fatalf("server 内那份被改动: %q", v)
	}
	if len(*calls) != 0 {
		t.Fatalf("校验阶段不应启停 dsh: %v", *calls)
	}
}

func TestInstallMarketRefusesUnresolvableDepBeforeStopping(t *testing.T) {
	m, _, _, targetDir, calls := setupMarketTest(t)
	// 新版本要求一个目标位置解析不到的依赖。
	deps := map[string]string{"undici": "^7.29.0", "brand-new-dep": "^1.0.0"}
	extractDir, _ := stagedMarketPackage(t, "2.0.0", deps)

	err := m.installMarket(&PendingUpdate{Kind: updateKindMarket, Version: "2.0.0"}, extractDir)
	if err == nil || !strings.Contains(err.Error(), "brand-new-dep") {
		t.Fatalf("应当因依赖缺失而拒绝，err = %v", err)
	}
	if v, _ := readMarketManifest(targetDir); v != "1.0.0" {
		t.Fatalf("拒绝后目录被改动: %q", v)
	}
	// 关键：校验失败必须发生在停 dsh 之前，不能白白制造停机。
	if len(*calls) != 0 {
		t.Fatalf("依赖校验失败时不应启停 dsh: %v", *calls)
	}
}

func TestInstallMarketRefusesIntegrityMismatch(t *testing.T) {
	m, _, _, targetDir, calls := setupMarketTest(t)
	extractDir, _ := stagedMarketPackage(t, "2.0.0", nil)
	pkgPath := filepath.Join(t.TempDir(), "market-2.0.0.tgz")
	if err := os.WriteFile(pkgPath, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &PendingUpdate{
		Kind: updateKindMarket, Version: "2.0.0", PkgPath: pkgPath,
		Integrity: "sha512-" + base64.StdEncoding.EncodeToString(make([]byte, sha512.Size)),
	}
	if err := m.installMarket(p, extractDir); err == nil {
		t.Fatal("完整性不符应当被拒")
	}
	if v, _ := readMarketManifest(targetDir); v != "1.0.0" {
		t.Fatalf("拒绝后目录被改动: %q", v)
	}
	if len(*calls) != 0 {
		t.Fatalf("完整性校验失败时不应启停 dsh: %v", *calls)
	}
}

func TestSweepMarketDebris(t *testing.T) {
	serverDir := t.TempDir()
	parent := filepath.Join(serverDir, "node_modules", "@deepseek-ai", "dsh", "node_modules")
	targetDir := filepath.Join(parent, marketPackageName)
	writeMarketPackage(t, targetDir, "1.0.0")
	stagingDebris := targetDir + marketStagingSuffix + "20260101000000"
	oldDebris := targetDir + marketOldSuffix + "20260101000000"
	for _, d := range []string{stagingDebris, oldDebris} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// 目标目录有效 → staging 与 old 都可清。
	sweepMarketDebris(targetDir)
	for _, d := range []string{stagingDebris, oldDebris} {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Fatalf("残留未清理: %s", d)
		}
	}

	// 目标目录缺失 → staging 仍可清，但 old 必须保留（它可能是唯一副本）。
	if err := os.RemoveAll(targetDir); err != nil {
		t.Fatal(err)
	}
	stagingDebris2 := targetDir + marketStagingSuffix + "20260102000000"
	oldDebris2 := targetDir + marketOldSuffix + "20260102000000"
	for _, d := range []string{stagingDebris2, oldDebris2} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sweepMarketDebris(targetDir)
	if _, err := os.Stat(stagingDebris2); !os.IsNotExist(err) {
		t.Fatal("staging 残留应当被清理")
	}
	if _, err := os.Stat(oldDebris2); err != nil {
		t.Fatalf("目标缺失时不应删除 old 目录: %v", err)
	}
}

// 就绪上限必须保持「秒级」：这个值直接决定失败时用户要盯着弹窗等多久。
// 实测本机 dsh 起进程 → 插件树装配完成约 2.4 秒，10 秒已有数倍余量。
func TestMarketReadyTimeoutStaysShort(t *testing.T) {
	if marketReadyTimeout > 15*time.Second {
		t.Fatalf("marketReadyTimeout = %s，失败等待过久（目标 ~10s）", marketReadyTimeout)
	}
	if marketReadySettle > 5*time.Second {
		t.Fatalf("marketReadySettle = %s，成功路径的额外确认过久", marketReadySettle)
	}
}

func TestInstallMarketRollsBackWhenDshExits(t *testing.T) {
	m, _, serverDir, targetDir, calls := setupMarketTest(t)
	marketReadyFn = func(*UpdateManager, time.Duration) marketWaitResult {
		*calls = append(*calls, "ready")
		return marketWaitExited
	}
	extractDir, _ := stagedMarketPackage(t, "2.0.0", nil)
	writeResolvableDeps(t, targetDir, serverDir)

	err := m.installMarket(&PendingUpdate{Kind: updateKindMarket, Version: "2.0.0"}, extractDir)
	if err == nil {
		t.Fatal("进程退出时应当返回错误")
	}
	if !strings.Contains(err.Error(), "退出") {
		t.Fatalf("错误信息应说明进程退出: %v", err)
	}
	if v, ok := readMarketManifest(targetDir); !ok || v != "1.0.0" {
		t.Fatalf("回滚后版本 = %q ok=%v, want 1.0.0", v, ok)
	}
}

// waitMarketDsh 的判定逻辑：端口无人监听时区分「进程还活着→超时」与「进程已退出
// →立即失败」。后者是这个超时改造的重点：坏版本通常在 1 秒内就能判定，不该让用户
// 盯着弹窗等满上限。
func TestWaitMarketDshDistinguishesTimeoutFromExit(t *testing.T) {
	prevCfg := GetConfig()
	initConfig(&AppConfig{DshPort: 0}) // 端口 0：不会有任何东西监听
	t.Cleanup(func() { initConfig(&prevCfg) })

	// 进程存活（用一个真实的长命子进程冒充受管 dsh）→ 应当报超时。
	alive := exec.Command("sleep", "30")
	if err := alive.Start(); err != nil {
		t.Skipf("无法启动用于测试的子进程: %v", err)
	}
	t.Cleanup(func() {
		_ = alive.Process.Kill()
		_, _ = alive.Process.Wait()
	})
	mAlive := &UpdateManager{dsh: &DshManager{cmd: alive}}
	started := time.Now()
	if got := waitMarketDsh(mAlive, 600*time.Millisecond); got != marketWaitTimeout {
		t.Fatalf("进程存活时应为超时，实际 %v", got)
	}
	if d := time.Since(started); d > 3*time.Second {
		t.Fatalf("超时判定耗时 %s，应接近给定上限", d)
	}

	// 进程已退出 → 必须立即判定，而不是等满 10 秒。
	dead := exec.Command("true")
	if err := dead.Start(); err != nil {
		t.Fatal(err)
	}
	_ = dead.Wait() // Wait 后 ProcessState 非空，stopped() 判死
	mDead := &UpdateManager{dsh: &DshManager{cmd: dead}}
	started = time.Now()
	if got := waitMarketDsh(mDead, 10*time.Second); got != marketWaitExited {
		t.Fatalf("进程已退出时应为 exited，实际 %v", got)
	}
	if d := time.Since(started); d > 2*time.Second {
		t.Fatalf("进程退出应立即判定，实际耗时 %s", d)
	}
}

func indexOf(list []string, want string) int {
	for i, v := range list {
		if v == want {
			return i
		}
	}
	return -1
}
