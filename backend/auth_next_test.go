package main

// auth_next_test.go —— 登录回跳链路（next）的回归测试。
//
// 旧实现有两个断点：
//  1. 登录页表单 action 只写 mount.join(authLogin)，把 next 丢了：反代把未登录请求
//     302 到 `<mount>/_login?next=/api/rpc`，而表单 POST 到不带 query 的地址，
//     于是 r.URL.Query().Get("next") 恒为空、safeNext("") 返回 "/" —— 深链接全部
//     落回挂载根，next 是个死参数；
//  2. safeNext 只挡 `//` 开头的协议相对地址，没挡含 `\` 的值：浏览器把 `\` 归一化成
//     `/`，`/\evil.com` 与 `//evil.com` 等价，仍是开放重定向。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

const authNextPassword = "Next-Demo-1!"

// authNextFixture 准备一份开启鉴权的配置与 Auth 实例（会话密钥写临时文件，避免污染
// 真实路径）。不依赖其它测试文件的 fixture，避免相互耦合。
func authNextFixture(t *testing.T) *Auth {
	t.Helper()
	t.Setenv("HARNESS_SESSION_KEY_FILE", filepath.Join(t.TempDir(), "session.key"))
	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })
	cfg := defaultConfig()
	cfg.AuthEnabled = true
	cfg.Password = authNextPassword
	initConfig(&cfg)
	return NewAuth()
}

// loginFormAction 从登录页 HTML 里取出表单 action 属性的值。
func loginFormAction(t *testing.T, body string) string {
	t.Helper()
	const marker = `<form method="POST" action="`
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("登录页缺少表单 action: %q", body)
	}
	rest := body[i+len(marker):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		t.Fatalf("登录页表单 action 属性未闭合: %q", body)
	}
	return rest[:j]
}

// mountInternal 把「浏览器可见地址」换算成 handleAuthRoutes 实际收到的挂载内路径：
// 反代在 stripMount 里先剥掉挂载前缀，r.URL 因此只剩挂载内路径（见 proxy.go）。
func mountInternal(t *testing.T, base, browserURI string) string {
	t.Helper()
	if base == "" {
		return browserURI
	}
	if !strings.HasPrefix(browserURI, base) {
		t.Fatalf("浏览器地址 %q 不在挂载 %q 下", browserURI, base)
	}
	p := strings.TrimPrefix(browserURI, base)
	if p == "" {
		p = "/"
	}
	return p
}

// getLoginPage 请求登录页并返回表单 action（浏览器可见地址）。
func getLoginPage(t *testing.T, a *Auth, base, next string) string {
	t.Helper()
	internal := authLogin
	if next != "" {
		internal += "?next=" + url.QueryEscape(next)
	}
	rec := httptest.NewRecorder()
	if handled := a.handleAuthRoutes(rec, httptest.NewRequest(http.MethodGet, internal, nil), newProxyMount(base)); !handled {
		t.Fatalf("GET %s 应被鉴权路由消费", internal)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s code=%d", internal, rec.Code)
	}
	return loginFormAction(t, rec.Body.String())
}

// postLogin 向给定 action 地址提交密码（模拟浏览器提交登录页表单；action 是浏览器
// 可见地址，反代会先剥前缀再交给鉴权路由）。
func postLogin(t *testing.T, a *Auth, base, action, password string) *httptest.ResponseRecorder {
	t.Helper()
	form := strings.NewReader("password=" + url.QueryEscape(password))
	req := httptest.NewRequest(http.MethodPost, mountInternal(t, base, action), form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	if handled := a.handleAuthRoutes(rec, req, newProxyMount(base)); !handled {
		t.Fatalf("POST %s 应被鉴权路由消费", action)
	}
	return rec
}

// 登录页表单 action 必须携带 next（根挂载与子路径挂载都要），并且把 next 原样编码。
func TestLoginPageActionCarriesNext(t *testing.T) {
	a := authNextFixture(t)
	for _, base := range []string{"", "/app/Harness/dsh"} {
		action := getLoginPage(t, a, base, "/api/rpc?x=1")
		want := newProxyMount(base).join(authLogin) + "?next=%2Fapi%2Frpc%3Fx%3D1"
		if action != want {
			t.Fatalf("base=%q 表单 action = %q, want %q", base, action, want)
		}
	}
}

// 「取到 action → 提交表单」这条真实链路：登录成功后必须落回 next（而不是挂载根）。
// 这条就是旧实现断裂的地方：action 不带 next 时，POST 的 next 为空 → 落回 "/"。
func TestLoginFormActionRoundTripKeepsDeepLink(t *testing.T) {
	a := authNextFixture(t)
	for _, base := range []string{"", "/app/Harness/dsh"} {
		action := getLoginPage(t, a, base, "/api/rpc?x=1")
		rec := postLogin(t, a, base, action, authNextPassword)
		if rec.Code != http.StatusFound {
			t.Fatalf("登录成功应 302，实际 %d", rec.Code)
		}
		want := newProxyMount(base).join("/api/rpc?x=1")
		if got := rec.Header().Get("Location"); got != want {
			t.Fatalf("base=%q 登录后跳转 = %q, want %q（深链接丢失）", base, got, want)
		}
	}
}

// 密码错误重试时 next 也不能丢：否则重试一次就被打回挂载根。
func TestLoginRetryKeepsNext(t *testing.T) {
	a := authNextFixture(t)
	action := getLoginPage(t, a, "", "/api/rpc")
	rec := postLogin(t, a, "", action, "wrong-password")
	if rec.Code != http.StatusOK {
		t.Fatalf("密码错误应回登录页 200，实际 %d", rec.Code)
	}
	if got := loginFormAction(t, rec.Body.String()); got != action {
		t.Fatalf("重试表单 action = %q, want %q", got, action)
	}
}

// next 的放行/拒绝规则：只接受挂载内绝对路径，且任何含反斜杠的值都拒绝。
func TestSafeNextRejectsOpenRedirectShapes(t *testing.T) {
	allow := map[string]string{
		"/":                "/",
		"/api/rpc":         "/api/rpc",
		"/api/rpc?x=1&y=2": "/api/rpc?x=1&y=2",
	}
	for in, want := range allow {
		if got := safeNext(in); got != want {
			t.Fatalf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
	for _, bad := range []string{
		"",                    // 空：落回挂载根
		"http://x",            // 绝对 URL
		"//evil.com",          // 协议相对地址
		`/\evil.com`,          // 浏览器把 \ 归一化成 / → 同样是协议相对地址
		`/a\evil.com`,         // 反斜杠不在第二个字符，同样必须拒绝
		authLogin,             // 回到登录页自身（循环）
		authLogin + "?next=/", // 带 query 的登录页同样是循环
	} {
		if got := safeNext(bad); got != "/" {
			t.Fatalf("safeNext(%q) = %q, want %q", bad, got, "/")
		}
	}
}

// 恶意 next 不能让登录页 action 变成跳板，POST 后也必须落回挂载根。
func TestLoginPageIgnoresUnsafeNext(t *testing.T) {
	a := authNextFixture(t)
	for _, base := range []string{"", "/app/Harness/dsh"} {
		for _, bad := range []string{"//evil.com", `/\evil.com`, "http://x"} {
			action := getLoginPage(t, a, base, bad)
			if want := newProxyMount(base).join(authLogin); action != want {
				t.Fatalf("base=%q next=%q 时 action 应为 %q（不带 next），实得 %q", base, bad, want, action)
			}
			if strings.Contains(action, "evil.com") {
				t.Fatalf("base=%q next=%q 被写进了表单 action: %q", base, bad, action)
			}
			// 即便请求方手工拼一个带恶意 next 的 POST 地址，落点也只能是挂载根。
			rec := postLogin(t, a, base, newProxyMount(base).join(authLogin)+"?next="+url.QueryEscape(bad), authNextPassword)
			if got := rec.Header().Get("Location"); got != newProxyMount(base).join("/") {
				t.Fatalf("base=%q next=%q 登录后跳转 = %q, want 挂载根", base, bad, got)
			}
		}
	}
}

// next 里带引号之类的字符必须被编码后再嵌进 HTML 属性（不能截断/注入属性）。
func TestLoginActionEscapesNextIntoAttribute(t *testing.T) {
	a := authNextFixture(t)
	rec := httptest.NewRecorder()
	target := authLogin + "?next=" + url.QueryEscape(`/a"onmouseover=1`)
	if handled := a.handleAuthRoutes(rec, httptest.NewRequest(http.MethodGet, target, nil), newProxyMount("")); !handled {
		t.Fatal("GET /_login 应被鉴权路由消费")
	}
	body := rec.Body.String()

	if !strings.Contains(body, "%22onmouseover") {
		t.Fatalf("next 应被 QueryEscape 编码后嵌入 action: %q", body)
	}
	if strings.Contains(body, `"onmouseover`) {
		t.Fatalf("next 中的引号逃出了属性: %q", body)
	}
}
