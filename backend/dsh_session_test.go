package main

// dsh_session_test.go —— dsh 会话凭据链路的回归测试（审查 C3 / C4）。
//
// C3：ExchangeToken 在「token 无效 / 响应里没有 dsh-auth-* cookie」时旧实现静默返回
// nil，与成功无法区分 —— 日志里没有任何线索，用户只能靠自己撞上 dsh 的 401 发现。
// C4：markSessionSettled 旧实现写的是**当前**启动代号，而不是发起等待的那一代：
// 上一代 captureDshSession 在旧版 dsh 上要空等 15 秒，期间 dsh 一旦重启，这次超时
// 就会把新一代标成「凭据已落定」，反代据此放行，用户被送进 dsh 的未授权页。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// tokenExchangeFixture 造一个假 dsh（httptest server 当 dsh 的 HTTP 端点）与一个
// 已捕获 token 的 DshManager。handler 决定假 dsh 如何响应 token 交换请求。
func tokenExchangeFixture(t *testing.T, handler http.HandlerFunc) *DshManager {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("无法解析测试服务器端口 %q: %v", u.Port(), err)
	}
	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })
	initConfig(&AppConfig{DshPort: port, HomeDir: t.TempDir()})

	dsh := newTestDshManager(t.TempDir(), "")
	dsh.tokenMu.Lock()
	dsh.token = "tok-abc123"
	dsh.tokenMu.Unlock()
	return dsh
}

// ExchangeToken 没换到 dsh-auth-* cookie 时必须报错（带状态码上下文），且不落凭据。
func TestExchangeTokenErrorsWithoutDshAuthCookie(t *testing.T) {
	var hits int
	dsh := tokenExchangeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		// 模拟「token 无效」：dsh 回 401 且不下发任何 cookie。
		w.WriteHeader(http.StatusUnauthorized)
	})

	err := dsh.ExchangeToken()
	if err == nil {
		t.Fatal("没换到 dsh-auth-* cookie 时必须返回错误（否则故障只能靠用户看到 401 发现）")
	}
	if !strings.Contains(err.Error(), "dsh-auth-") {
		t.Fatalf("错误信息应说明缺的是 dsh-auth-* cookie，实际: %v", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("错误信息应带 HTTP 状态码，实际: %v", err)
	}
	if ck := dsh.AuthCookie(); ck != "" {
		t.Fatalf("失败时不应写入会话 cookie，实际 %q", ck)
	}
	if hits != 1 {
		t.Fatalf("应恰好请求假 dsh 一次，实际 %d", hits)
	}
}

// 成功换取时返回 nil 并保存 cookie（新错误分支不能误伤正常路径）。
func TestExchangeTokenStoresDshAuthCookie(t *testing.T) {
	dsh := tokenExchangeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("token"); got != "tok-abc123" {
			t.Errorf("token 未按 query 传递: %q", got)
		}
		http.SetCookie(w, &http.Cookie{Name: "dsh-auth-session", Value: "v1", Path: "/"})
		w.WriteHeader(http.StatusOK)
	})

	if err := dsh.ExchangeToken(); err != nil {
		t.Fatalf("成功换取不应报错: %v", err)
	}
	if got := dsh.AuthCookie(); got != "dsh-auth-session=v1" {
		t.Fatalf("会话 cookie = %q, want dsh-auth-session=v1", got)
	}
}

// 没有 token（旧版 dsh）时是空操作：不请求、不报错 —— 这一代由 captureDshSession
// 的「超时也算落定」兜底，不能被 C3 改成错误。
func TestExchangeTokenWithoutTokenIsNoop(t *testing.T) {
	var hits int
	dsh := tokenExchangeFixture(t, func(w http.ResponseWriter, r *http.Request) { hits++ })
	dsh.tokenMu.Lock()
	dsh.token = ""
	dsh.tokenMu.Unlock()

	if err := dsh.ExchangeToken(); err != nil {
		t.Fatalf("无 token 时应是空操作: %v", err)
	}
	if hits != 0 {
		t.Fatalf("无 token 时不应发起请求，实际 %d 次", hits)
	}
}

// C4：落定是「按发起等待的那一代」生效的 compare-and-set。
func TestMarkSessionSettledIsGenerationScoped(t *testing.T) {
	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })
	initConfig(&AppConfig{DshPort: 65535, HomeDir: t.TempDir()})
	dsh := newTestDshManager(t.TempDir(), "")

	dsh.bumpSessionGen() // 第 1 代（模拟 Start）
	gen1 := dsh.SessionGen()
	if dsh.SessionSettled() {
		t.Fatal("本代凭据尚未落定")
	}
	dsh.markSessionSettledGen(gen1)
	if !dsh.SessionSettled() {
		t.Fatal("同一代标记后应落定")
	}

	// 上一代的等待快结束时 dsh 又重启了两代（旧版 dsh 不打印 token 时会空等 15 秒）。
	dsh.bumpSessionGen()
	dsh.bumpSessionGen()
	if dsh.SessionSettled() {
		t.Fatal("新一代凭据未落定，不得放行")
	}
	// 上一代的等待结束 → 用第一代的代号落定：绝不能把新一代标成已落定。
	dsh.markSessionSettledGen(gen1)
	if dsh.SessionSettled() {
		t.Fatal("上一代的落定不得作用于新一代（反代会据此把用户送进 dsh 未授权页）")
	}
	// 新一代自己的 captureDshSession 落定后才放行。
	dsh.markSessionSettledGen(dsh.SessionGen())
	if !dsh.SessionSettled() {
		t.Fatal("当前代落定后应放行")
	}
}

// 保留语义：无论是否拿到 token 都要落定（旧版 dsh 不打印 token 时等待会立即结束，
// 因为此时的 DshManager 判定进程未运行）。
func TestCaptureDshSessionSettlesWithoutToken(t *testing.T) {
	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })
	initConfig(&AppConfig{DshPort: 65535, HomeDir: t.TempDir()})
	dsh := newTestDshManager(t.TempDir(), "")
	dsh.bumpSessionGen()

	done := make(chan struct{})
	go func() {
		captureDshSession(dsh)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("拿不到 token 时也必须收尾落定，不能永久等待")
	}
	if !dsh.SessionSettled() {
		t.Fatal("未拿到 token 也必须标记本代落定（否则反代永远停在等待页）")
	}
}

// 上一代在等待期间被新一代取代时，captureDshSession 的收尾不得落定新一代。
// 这里直接按真实时序驱动：取 gen（第一代）→ 新一代 Start → 用第一代的代号收尾。
func TestCaptureDshSessionOutdatedGenerationCannotSettle(t *testing.T) {
	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })
	initConfig(&AppConfig{DshPort: 65535, HomeDir: t.TempDir()})
	dsh := newTestDshManager(t.TempDir(), "")

	dsh.bumpSessionGen()
	waitingGen := dsh.SessionGen() // captureDshSession 开头取的那一代
	dsh.bumpSessionGen()           // 等待期间 dsh 重启

	// captureDshSession 的 defer 收尾按 waitingGen 落定（等价于它的真实行为）。
	dsh.markSessionSettledGen(waitingGen)
	if dsh.SessionSettled() {
		t.Fatal("发起等待的那一代已被取代，收尾不得标记新一代")
	}

	// 同时确认「落定后不会因为代号比较方向而出现假放行」：新一代落定后必须为 true。
	dsh.markSessionSettledGen(dsh.SessionGen())
	if !dsh.SessionSettled() {
		t.Fatal("新一代落定后应放行")
	}
}
