package main

// auth_session_test.go —— 会话 cookie 的强度回归测试（审查第 10 条）。
//
// 旧实现：`cookie = <过期秒>.<HMAC(访问密码, 过期秒)>`。三个问题：
//  1. Cookie 明文带过期秒 → 拿到一次 Cookie 即可**离线爆破访问密码**；
//  2. 签名只依赖 (密码, 秒) → 同一秒登录的两台设备令牌完全相同（访客列表合并、
//     踢一台等于踢两台）；
//  3. 无 per-session 随机量。
// 新实现：`<过期秒>.<每会话随机 nonce>.<HMAC(密码 + 进程外随机密钥, 过期秒.nonce)>`，
// 并且带 nonce 的整段 cookie 仍是访客列表/吊销的键。

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// authFixture 准备一份开启鉴权的配置 + Auth 实例（会话密钥写进临时文件，可跨实例共享）。
func authFixture(t *testing.T) (*Auth, string) {
	t.Helper()
	keyFile := filepath.Join(t.TempDir(), "session.key")
	t.Setenv("HARNESS_SESSION_KEY_FILE", keyFile)

	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })
	cfg := defaultConfig()
	cfg.AuthEnabled = true
	cfg.Password = proxyTestPassword
	initConfig(&cfg)
	return NewAuth(), keyFile
}

// authedReq 用指定 cookie 值构造一个请求。
func authedReq(cookieValue string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Cookie", authCookie+"="+cookieValue)
	return r
}

func TestSessionCookieIsUniquePerLoginAndVerifiable(t *testing.T) {
	a, _ := authFixture(t)

	expire := time.Now().Add(time.Hour).Unix()
	c1 := a.newSessionCookie(proxyTestPassword, expire)
	c2 := a.newSessionCookie(proxyTestPassword, expire)

	if c1 == c2 {
		t.Fatal("同一秒内的两次登录必须得到不同令牌（含 per-session nonce）")
	}
	for _, c := range []string{c1, c2} {
		if parts := strings.Split(c, "."); len(parts) != 3 {
			t.Fatalf("令牌应为三段式 <过期秒>.<nonce>.<mac>，实际 %q", c)
		}
		if !a.isAuthed(authedReq(c)) {
			t.Fatalf("新签发的令牌必须能通过校验: %q", c)
		}
	}
}

// 上一版的两段式令牌（mac 只用密码）必须被拒绝：它的 HMAC 可离线爆破密码，
// 不能为了兼容继续放行。
func TestLegacyCookieAndPasswordOnlyMacRejected(t *testing.T) {
	a, _ := authFixture(t)
	expire := time.Now().Add(time.Hour).Unix()

	// 1) 旧格式：<过期秒>.<HMAC(密码, 过期秒)>
	legacyKey := []byte(proxyTestPassword)
	legacy := strconv.FormatInt(expire, 10) + "." + sessionSign(legacyKey, expire, "")
	if a.isAuthed(authedReq(legacy)) {
		t.Fatal("旧版两段式令牌必须被拒绝（否则离线爆破通路仍然存在）")
	}

	// 2) 三段式外形，但 mac 只用密码签名（即攻击者能离线重算的那种）
	nonce := newSessionNonce()
	weak := strconv.FormatInt(expire, 10) + "." + nonce + "." + sessionSign(legacyKey, expire, nonce)
	if a.isAuthed(authedReq(weak)) {
		t.Fatal("仅用密码签名的令牌必须被拒绝（现在还要掺入进程外随机密钥）")
	}
}

func TestSessionCookieRejectsExpiredAndTampered(t *testing.T) {
	a, _ := authFixture(t)

	// 过期
	expired := a.newSessionCookie(proxyTestPassword, time.Now().Add(-time.Minute).Unix())
	if a.isAuthed(authedReq(expired)) {
		t.Fatal("过期令牌必须被拒绝")
	}

	// 篡改 nonce（签名随之失配）
	cur := a.newSessionCookie(proxyTestPassword, time.Now().Add(time.Hour).Unix())
	parts := strings.Split(cur, ".")
	parts[1] = newSessionNonce()
	if a.isAuthed(authedReq(strings.Join(parts, "."))) {
		t.Fatal("篡改 nonce 的令牌必须被拒绝")
	}

	// 篡改过期时间（延长有效期）同样必须失败
	parts = strings.Split(cur, ".")
	parts[0] = strconv.FormatInt(time.Now().Add(72*time.Hour).Unix(), 10)
	if a.isAuthed(authedReq(strings.Join(parts, "."))) {
		t.Fatal("篡改过期时间的令牌必须被拒绝")
	}

	// 畸形（段数不足/为空）不应放行
	for _, bad := range []string{"", "123", "abc.def.ghi", "123..abc"} {
		if a.isAuthed(authedReq(bad)) {
			t.Fatalf("畸形令牌 %q 不应放行", bad)
		}
	}
}

// 吊销按整段 cookie 生效：踢掉一台设备不影响另一台（旧实现里同秒登录的两台共用令牌，
// 踢一台等于两台都被踢）。
func TestRevokeIsolatesSessions(t *testing.T) {
	a, _ := authFixture(t)
	expire := time.Now().Add(time.Hour).Unix()
	c1 := a.newSessionCookie(proxyTestPassword, expire)
	c2 := a.newSessionCookie(proxyTestPassword, expire)
	// 访客列表先登记（吊销只对「列在表里」的会话生效，与生产路径一致：请求经过反代
	// 时由 recordVisitor 登记）。
	a.recordVisitor(authedReq(c1))
	a.recordVisitor(authedReq(c2))

	if !a.RevokeVisitor(c1) {
		t.Fatal("吊销一个存在的会话应成功")
	}
	if a.isAuthed(authedReq(c1)) {
		t.Fatal("被吊销的会话必须失效")
	}
	if !a.isAuthed(authedReq(c2)) {
		t.Fatal("吊销一台设备不应影响另一台")
	}
}

// 会话密钥落盘后必须跨实例（≈跨控制台重启）一致：否则每次重启都会把所有已登录的
// 浏览器踢出登录。
func TestSessionSecretPersistsAcrossInstances(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "session.key")
	t.Setenv("HARNESS_SESSION_KEY_FILE", keyFile)

	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })
	cfg := defaultConfig()
	cfg.AuthEnabled = true
	cfg.Password = proxyTestPassword
	initConfig(&cfg)

	// 先放一份「上次运行留下的」密钥文件：新实例必须优先用它（而不是另生成一份）。
	const known = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	if err := os.WriteFile(keyFile, []byte(known+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := NewAuth()
	if got := a.sessionSecret(); got != known {
		t.Fatalf("应复用密钥文件里的密钥，实际 %q", got)
	}
	expire := time.Now().Add(time.Hour).Unix()
	cookie := a.newSessionCookie(proxyTestPassword, expire)
	if !a.isAuthed(authedReq(cookie)) {
		t.Fatal("签发实例自身应能校验")
	}

	// 新实例（模拟控制台重启后）：读同一份密钥文件，必须仍认可既有登录态。
	fresh := NewAuth()
	if !fresh.isAuthed(authedReq(cookie)) {
		t.Fatal("重启后的新实例必须继续认可既有会话（密钥来自同一文件）")
	}
}

// 登录处理器必须签发「新式」令牌（三段式且能通过校验），避免只改校验、漏改签发。
func TestLoginHandlerIssuesVerifiableCookie(t *testing.T) {
	a, _ := authFixture(t)
	form := strings.NewReader("password=" + proxyTestPassword)
	req := httptest.NewRequest(http.MethodPost, authLogin, form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	if handled := a.handleAuthRoutes(rec, req, newProxyMount("")); !handled {
		t.Fatal("POST /_login 应被鉴权路由消费")
	}
	if rec.Code != http.StatusFound {
		t.Fatalf("登录成功应 302，实际 %d", rec.Code)
	}
	raw := rec.Header().Get("Set-Cookie")
	value := ""
	if i := strings.Index(raw, authCookie+"="); i >= 0 {
		value = raw[i+len(authCookie)+1:]
		if j := strings.IndexByte(value, ';'); j >= 0 {
			value = value[:j]
		}
	}
	if len(strings.Split(value, ".")) != 3 {
		t.Fatalf("登录下发的令牌应为三段式，实际 Set-Cookie=%q", raw)
	}
	if !a.isAuthed(authedReq(value)) {
		t.Fatalf("登录下发的令牌必须能通过校验: %q", value)
	}

	// 密码错误时不发令牌
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, authLogin, strings.NewReader("password=wrong"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	a.handleAuthRoutes(rec, req, newProxyMount(""))
	if strings.Contains(rec.Header().Get("Set-Cookie"), authCookie+"=") {
		t.Fatalf("密码错误不应下发令牌: %q", rec.Header().Get("Set-Cookie"))
	}
}
