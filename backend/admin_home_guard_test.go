package main

// admin_home_guard_test.go —— 「设置主目录」的授权范围校验（审查 C6）。
//
// 旧实现的函数注释写着「把某个已授权目录设为 dsh 的 HOME」，但只校验了「路径是绝对
// 路径、存在、是目录」：任意目录（/etc、/tmp/x）都能被设成 dsh 的 HOME，而且
// migrate=true 时会先 RemoveAll(dest/.dsh) 再覆盖拷贝 —— 一个请求就能删掉别人的
// 配置目录。现在目标必须落在「当前请求用户的飞牛授权目录 + 默认主目录」之内，
// 且在 RemoveAll 之前再确认一次。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// homeDirAllowed 是纯函数：判「是否在白名单目录之内」，必须用 filepath.Rel 而不是
// 字符串前缀（前缀比较会把 .../Harness-evil 误判成 .../Harness 的子目录）。
func TestHomeDirAllowed(t *testing.T) {
	roots := []string{"/vol1/@appshare/Harness", "/vol1/Shared/My"}

	allow := []string{
		"/vol1/@appshare/Harness",
		"/vol1/@appshare/Harness/",
		"/vol1/@appshare/Harness/sub/deep",
		"/vol1/Shared/My",
		"/vol1/@appshare/Harness/../Harness/data", // Clean 之后仍在白名单内
	}
	for _, p := range allow {
		if !homeDirAllowed(p, roots) {
			t.Fatalf("homeDirAllowed(%q) 应为 true", p)
		}
	}

	deny := []string{
		"",
		"/",
		"/etc",
		"/tmp/whatever",
		"/vol1/@appshare/Harness-evil",      // 字符串前缀相同、实际是兄弟目录
		"/vol1/@appshare/Harness2",          // 同上
		"/vol1/Shared/My-bak",               // 同上
		"/vol1/@appshare/Harness/../../etc", // Clean 之后落在白名单外
		"/vol1/@appshare/HarnessX/data",
	}
	for _, p := range deny {
		if homeDirAllowed(p, roots) {
			t.Fatalf("homeDirAllowed(%q) 应为 false", p)
		}
	}

	// 白名单里的符号链接不能成为逃逸通道：授权目录下的 link → /etc 必须判为不允许。
	allowedRoot := t.TempDir()
	link := filepath.Join(allowedRoot, "escape")
	if err := os.Symlink("/etc", link); err != nil {
		t.Skipf("无法创建符号链接（环境限制）: %v", err)
	}
	if homeDirAllowed(link, []string{allowedRoot}) {
		t.Fatal("指向白名单外的符号链接必须判为不允许")
	}
	if !homeDirAllowed(filepath.Join(allowedRoot, "real-sub"), []string{allowedRoot}) {
		t.Fatal("白名单目录内的子目录应允许")
	}
}

// setHomeFixture 造一个「默认主目录 + 当前 HOME + 不忙的市场」的 AdminMux。
// m.fnos 保持 nil：即「拿不到授权目录」的环境，此时策略是 fail-closed（只允许默认
// 主目录），本测试正是要验证这一点。
func setHomeFixture(t *testing.T) (m *AdminMux, cfgFile, defaultHome, currentHome string) {
	t.Helper()
	defaultHome = t.TempDir()
	currentHome = t.TempDir()
	if err := os.MkdirAll(filepath.Join(currentHome, ".dsh"), 0o755); err != nil {
		t.Fatal(err)
	}

	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })
	cfg := defaultConfig()
	cfg.HomeDir = currentHome
	cfg.DshPort = 65535
	initConfig(&cfg)

	prevBusy := marketBusyFn
	marketBusyFn = func(*UpdateManager) (bool, string) { return false, "" }
	t.Cleanup(func() { marketBusyFn = prevBusy })

	cfgFile = filepath.Join(t.TempDir(), "config.json")
	dsh := newTestDshManager(currentHome, "")
	m = &AdminMux{
		renv:   &RuntimeEnv{Home: defaultHome, ConfigFile: cfgFile},
		dsh:    dsh,
		update: &UpdateManager{dsh: dsh},
	}
	return m, cfgFile, defaultHome, currentHome
}

func postSetHome(t *testing.T, m *AdminMux, path string, migrate bool) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(map[string]interface{}{"path": path, "migrate": migrate})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.handleSetHome(rec, httptest.NewRequest(http.MethodPost, "/api/dsh/set-home", strings.NewReader(string(raw))))
	return rec
}

// 非授权目录（/etc、任意临时目录）必须 400：不改配置、不删任何东西。
func TestSetHomeRejectsNonAuthorizedDir(t *testing.T) {
	m, cfgFile, _, currentHome := setHomeFixture(t)

	// 目标目录里放一份 ~/.dsh 哨兵：migrate=true 时旧实现会把它整目录删掉。
	outside := t.TempDir()
	dstDsh := filepath.Join(outside, ".dsh")
	if err := os.MkdirAll(dstDsh, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dstDsh, "keep-me")
	if err := os.WriteFile(keep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, target := range []string{"/etc", outside} {
		rec := postSetHome(t, m, target, true)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("目标 %q 不在授权范围时应 400，实际 %d (%s)", target, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "授权目录") {
			t.Fatalf("错误文案应说明授权范围: %s", rec.Body.String())
		}
		if got := GetConfig().HomeDir; got != currentHome {
			t.Fatalf("被拒时配置不应改变，HomeDir = %s, want %s", got, currentHome)
		}
		if _, err := os.Stat(cfgFile); err == nil {
			t.Fatal("被拒时不应写配置文件")
		}
		if _, err := os.Stat(keep); err != nil {
			t.Fatalf("被拒时不得删除目标 ~/.dsh: %v", err)
		}
	}
}

// 默认主目录（renv.Home）本身必须始终可设为 HOME：否则在非飞牛环境里连「切回默认
// 目录」都做不了（fail-closed 策略的边界）。
func TestSetHomeAllowsDefaultHomeDir(t *testing.T) {
	m, _, defaultHome, currentHome := setHomeFixture(t)

	// 避免真的去拉起 dsh：把 PATH 指向空目录，Start 会立刻失败（后台 goroutine 的
	// 日志不影响本测试的断言）。
	t.Setenv("PATH", t.TempDir())
	rec := postSetHome(t, m, defaultHome, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("默认主目录应被接受，实际 %d (%s)", rec.Code, rec.Body.String())
	}
	if got := GetConfig().HomeDir; got != defaultHome {
		t.Fatalf("配置 HomeDir = %s, want %s", got, defaultHome)
	}
	if _, err := os.Stat(filepath.Join(currentHome, ".dsh")); err != nil {
		t.Fatalf("未开启 migrate 时不应改动原 HOME: %v", err)
	}
}
