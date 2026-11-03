package main

// dsh_session_test.go —— dsh 会话凭据链路的回归测试（审查 C3 / C4）。
//
// C3：ExchangeToken 在「token 无效 / 响应里没有 dsh-auth-* cookie」时旧实现静默返回
// nil，与成功无法区分 —— 日志里没有任何线索，用户只能靠自己撞上 dsh 的 401 发现。
// C4：markSessionSettled 旧实现写的是**当前**启动代号，而不是发起等待的那一代：
// 上一代 captureDshSession 要等满 20 秒，期间 dsh 一旦重启，这次超时就会把新一代
// 标成「凭据已落定」，反代据此放行，用户被送进 dsh 的未授权页。

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// initDshPortForTest 把测试用 httptest 服务器的端口写进全局配置（ProbeAuth 与
// ExchangeToken 都从这里取 dsh 端口）。
func initDshPortForTest(t *testing.T, srv *httptest.Server) {
	t.Helper()
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
	initConfig(&AppConfig{DshPort: port})
}

// tokenExchangeFixture 造一个假 dsh（httptest server 当 dsh 的 HTTP 端点）与一个
// 已捕获 token 的 DshManager。handler 决定假 dsh 如何响应 token 交换请求。
func tokenExchangeFixture(t *testing.T, handler http.HandlerFunc) *DshManager {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	initDshPortForTest(t, srv)

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
	initConfig(&AppConfig{DshPort: 65535})
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

	// 上一代的等待快结束时 dsh 又重启了两代（每次等待最长 20 秒）。
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

// 新语义：拿不到 token、也没有可用的旧 cookie 时**不落定**（旧实现无条件落定，
// 把用户放行到没有 cookie 的 dsh，只见 401）。
// 这里的 DshManager 没有受管进程：WaitToken 会立即结束、Running() 为 false，
// 属于「进程已退出」分支 —— 既不落定，也不该标成凭据失败（等待页会显示 stopped）。
func TestCaptureDshSessionDoesNotSettleWithoutCredentials(t *testing.T) {
	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })
	initConfig(&AppConfig{DshPort: closedPort(t)})
	dsh := newTestDshManager(t.TempDir(), "")
	dsh.bumpSessionGen()

	done := make(chan struct{})
	go func() {
		captureDshSessionWithin(dsh, 50*time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("拿不到凭据时也必须收尾，不能永久等待")
	}
	if dsh.SessionSettled() {
		t.Fatal("没有可用凭据时不得落定（否则反代把用户放行到 401）")
	}
	if dsh.SessionAuthFailed() {
		t.Fatal("进程已不在时不该标成凭据失败（等待页应显示已停止）")
	}
}

// 拿到 token 且兑换成功 → 落定。
func TestCaptureDshSessionSettlesWithToken(t *testing.T) {
	dsh := tokenExchangeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "dsh-auth-session", Value: "v1", Path: "/"})
		w.WriteHeader(http.StatusOK)
	})
	dsh.bumpSessionGen()

	captureDshSessionWithin(dsh, 50*time.Millisecond)

	if !dsh.SessionSettled() {
		t.Fatal("token 兑换成功后应落定")
	}
	if got := dsh.AuthCookie(); got != "dsh-auth-session=v1" {
		t.Fatalf("会话 cookie = %q, want dsh-auth-session=v1", got)
	}
}

// 没有新 token，但上一代留下的 cookie 仍然有效 → 直接落定（dsh 的 dsh-auth-*
// 由持久签名密钥签发，插件市场自重启、node-pty 重启都属于这一类）。
func TestCaptureDshSessionSettlesWithExistingValidCookie(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Cookie"), "dsh-auth-old=keep") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// 真实 dsh：有效 cookie + 不带 token 的 GET / 直接给首页（200）。
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	initDshPortForTest(t, srv)

	dsh := newTestDshManager(t.TempDir(), "")
	dsh.setAuthCookie("dsh-auth-old=keep")
	dsh.bumpSessionGen()

	captureDshSessionWithin(dsh, 50*time.Millisecond)

	if !dsh.SessionSettled() {
		t.Fatal("旧 cookie 仍被 dsh 接受时应直接落定")
	}
	if dsh.SessionAuthFailed() {
		t.Fatal("探测有效时不该标成凭据失败")
	}
}

// 进程还活着、没有 token、旧 cookie 被 401 拒绝 → 不落定 + 标成凭据失败
// （等待页停在 auth-failed，后台补偿继续重试）。
func TestCaptureDshSessionMarksAuthFailedWhenCookieRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	initDshPortForTest(t, srv)

	dsh := newTestDshManager(t.TempDir(), "")
	// Running() 需要有受管子进程：用 sleep 占位（不启动真正的 dsh）。
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("无法启动占位进程: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	dsh.cmd = cmd
	dsh.setAuthCookie("dsh-auth-stale=1")
	dsh.bumpSessionGen()

	captureDshSessionWithin(dsh, 50*time.Millisecond)

	if dsh.SessionSettled() {
		t.Fatal("没有凭据时不得落定")
	}
	if !dsh.SessionAuthFailed() {
		t.Fatal("进程还活着但拿不到凭据时应标成 auth-failed（等待页要给出可操作提示）")
	}
	// 收尾：别让补偿 goroutine 在测试结束后继续跑。
	dsh.cancelAuthRepair()
}

// 后台补偿的核心承诺：首次等待窗口里没拿到 token 不代表彻底放弃 ——
// repairSession 必须在退避后重新读 token、兑换成功并落定（慢设备上 token
// 往往晚于 dshTokenWait 才打印）。
func TestStartAuthRepairSettlesWhenTokenArrivesLater(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != "late-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "dsh-auth-late", Value: "1", Path: "/"})
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	initDshPortForTest(t, srv)

	dsh := newTestDshManager(t.TempDir(), "")
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("无法启动占位进程: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	dsh.cmd = cmd
	dsh.bumpSessionGen()
	gen := dsh.SessionGen()
	t.Cleanup(dsh.cancelAuthRepair)

	dsh.markAuthFailedGen(gen)
	dsh.StartAuthRepair(gen)
	// token 在首次等待窗口之后才出现（由 WaitToken 的超时分支交给补偿）。
	dsh.tokenMu.Lock()
	dsh.token = "late-token"
	dsh.tokenMu.Unlock()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if dsh.SessionSettled() {
			if got := dsh.AuthCookie(); got != "dsh-auth-late=1" {
				t.Fatalf("补偿换到的 cookie = %q, want dsh-auth-late=1", got)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("迟到的 token 应被后台补偿兑换并落定")
}

// 探测三态：401 = 未授权；303/200 = 有效；404/5xx/连接错误 = 未知（慢设备上
// 插件还没加载完、路由还没挂上，不能据此判失败）。
func TestProbeAuthClassifiesResponses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   authProbeResult
	}{
		{"401", http.StatusUnauthorized, authProbeInvalid},
		{"303", http.StatusSeeOther, authProbeValid},
		{"200", http.StatusOK, authProbeValid},
		{"404", http.StatusNotFound, authProbeUnknown},
		{"503", http.StatusServiceUnavailable, authProbeUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			initDshPortForTest(t, srv)

			dsh := newTestDshManager(t.TempDir(), "")
			dsh.setAuthCookie("dsh-auth-x=y")
			if got := dsh.ProbeAuth(1500 * time.Millisecond); got != tc.want {
				t.Fatalf("ProbeAuth(%d) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

// 没有 cookie 时探测直接判未授权，不发请求（dsh 从未启动过的常见路径）。
func TestProbeAuthWithoutCookieSkipsRequest(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	defer srv.Close()
	initDshPortForTest(t, srv)

	dsh := newTestDshManager(t.TempDir(), "")
	if got := dsh.ProbeAuth(time.Second); got != authProbeInvalid {
		t.Fatalf("无 cookie 时应判未授权，实际 %v", got)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("无 cookie 时不应发起请求，实际 %d 次", got)
	}
}

// EnsureAuthProbe 是反代热路径的入口：有效 cookie 探测成功后必须自行落定。
func TestEnsureAuthProbeSettlesWhenCookieValid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "./", http.StatusSeeOther)
	}))
	defer srv.Close()
	initDshPortForTest(t, srv)

	dsh := newTestDshManager(t.TempDir(), "")
	dsh.setAuthCookie("dsh-auth-x=y")
	dsh.bumpSessionGen()

	dsh.EnsureAuthProbe()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if dsh.SessionSettled() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("有效 cookie 的异步探测应把本代标记为已落定")
}

// 失败标记同样按启动代号 compare-and-set：旧代失败不得作用于新代；
// 一旦落定，本代也不再算失败。
func TestSessionAuthFailedScopedToGeneration(t *testing.T) {
	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })
	initConfig(&AppConfig{DshPort: 65535})
	dsh := newTestDshManager(t.TempDir(), "")

	dsh.bumpSessionGen()
	gen1 := dsh.SessionGen()
	dsh.markAuthFailedGen(gen1)
	if !dsh.SessionAuthFailed() {
		t.Fatal("本代失败标记应生效")
	}
	dsh.markSessionSettledGen(gen1)
	if dsh.SessionAuthFailed() {
		t.Fatal("落定后不该再算失败")
	}

	dsh.bumpSessionGen()
	if dsh.SessionAuthFailed() {
		t.Fatal("新一代未标失败，旧代的失败不得作用过来")
	}
	dsh.markAuthFailedGen(gen1)
	if dsh.SessionAuthFailed() {
		t.Fatal("旧代的失败标记不得写到新一代")
	}
	dsh.markAuthFailedGen(dsh.SessionGen())
	if !dsh.SessionAuthFailed() {
		t.Fatal("当前代标记后应生效")
	}
}

// tokenScanner 必须按行扫描：一次写入远超旧实现的 8 KiB 窗口、且 token 行在前的
// 输出，旧实现会在匹配之前把 token 行裁掉（静默漏扫），新实现必须命中。
func TestTokenScannerFindsTokenInLargeWrite(t *testing.T) {
	var got []string
	var out bytes.Buffer
	s := &tokenScanner{dst: &out, cb: func(tok string) { got = append(got, tok) }}
	payload := "dsh web: http://127.0.0.1:3080/?token=abcdefgh1234\n" +
		strings.Repeat("plugin loading output\n", 1200)
	if _, err := s.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "abcdefgh1234" {
		t.Fatalf("大段输出里的 token 必须被捕获，实际 %v", got)
	}
	if out.String() != payload {
		t.Fatal("dsh 输出必须原样透传")
	}
}

// 子进程输出不保证一次写完一行：token 行被拆成多次 Write 也要能识别。
func TestTokenScannerHandlesSplitLine(t *testing.T) {
	var got []string
	var out bytes.Buffer
	s := &tokenScanner{dst: &out, cb: func(tok string) { got = append(got, tok) }}
	_, _ = s.Write([]byte("dsh web: http://127.0.0.1:3080/?tok"))
	_, _ = s.Write([]byte("en=abcdefgh1234\n"))
	if len(got) != 1 || got[0] != "abcdefgh1234" {
		t.Fatalf("跨 Write 的 token 行必须被捕获，实际 %v", got)
	}
}

// 超长无换行输出（进度条 / 全屏 TUI）不能把缓冲撑爆，也不能影响之后的 token 行。
func TestTokenScannerHandlesHugeLine(t *testing.T) {
	var got []string
	var out bytes.Buffer
	s := &tokenScanner{dst: &out, cb: func(tok string) { got = append(got, tok) }}
	_, _ = s.Write([]byte(strings.Repeat("x", maxTokenLineBytes*2)))
	_, _ = s.Write([]byte("\ndsh web: http://127.0.0.1:3080/?token=abcdefgh1234\n"))
	if len(got) != 1 || got[0] != "abcdefgh1234" {
		t.Fatalf("超长行之后的 token 行必须被捕获，实际 %v", got)
	}
}

// 上一代在等待期间被新一代取代时，captureDshSession 的收尾不得落定新一代。
// 这里直接按真实时序驱动：取 gen（第一代）→ 新一代 Start → 用第一代的代号收尾。
func TestCaptureDshSessionOutdatedGenerationCannotSettle(t *testing.T) {
	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })
	initConfig(&AppConfig{DshPort: 65535})
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

// WaitToken 在「本代已被反代探测落定」时必须提前结束：否则慢设备上会白等满
// 20 秒，capture 还会打一条「没等到 token」的误导日志。
func TestWaitTokenEndsWhenSessionAlreadySettled(t *testing.T) {
	dsh := newTestDshManager(t.TempDir(), "")
	dsh.bumpSessionGen()
	dsh.markSessionSettledGen(dsh.SessionGen())

	start := time.Now()
	if tok := dsh.WaitToken(2 * time.Second); tok != "" {
		t.Fatalf("已落定时不应再返回 token，实际 %q", tok)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("已落定时 WaitToken 应立即返回，实际耗时 %s", elapsed)
	}
}
