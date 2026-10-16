package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 凭据文件夹具：既有用户自己的 refs，也有一条 dsh 自己写的块式记录 —— 补丁式的写入
// 必须把它们逐字保住（这个文件里装着用户的其它密钥，写坏了是灾难）。
const accountCredsFixture = `version: 1
refs:
  SOME_API_KEY: sk-abc123
  QUOTED_KEY: "with spaces"
records:
  client-connection/browser-session:
    kind: grant
    payload:
      version: 1
      secret: UJxnEipPCJpcmTXsz250c4vcSGq1hxXGtUg5qlS41Kw
`

func writeAccountCreds(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".dsh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".credentials.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func newAccountTestMux(t *testing.T, home string) *AdminMux {
	t.Helper()
	renv := &RuntimeEnv{Home: home, AdminBaseURL: "/app/Harness", ProxySock: "/x/dsh.sock", ProxyBaseURL: "/app/Harness/dsh"}
	// 账号库是密文落盘（见 accountvault.go），所以测试里也得有一把主密钥
	// （临时目录里的一把，见 quickcmds_crypt_test.go 的 testCryptor）。
	return newAdminMux(renv, &DshManager{renv: renv, logf: func(logLevel, string, ...interface{}) {}}, nil, nil, nil, testCryptor(t))
}

// 记录写入：两条记录都要出现，且除它们之外的字节逐字不变；块式旧记录要能被删干净。
func TestAccountStoreGrantAndDevice(t *testing.T) {
	home := writeAccountCreds(t, accountCredsFixture)
	m := newAccountTestMux(t, home)

	deviceID, err := m.accountEnsureDeviceID()
	if err != nil {
		t.Fatalf("ensure device: %v", err)
	}
	if !accountUUIDRe.MatchString("id: " + deviceID) {
		t.Fatalf("device id 不是 uuid: %q", deviceID)
	}
	if err := m.accountStoreGrant("tok/en+with$dollar"); err != nil {
		t.Fatalf("store grant: %v", err)
	}
	path, _ := m.accountCredentialsPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{
		"  SOME_API_KEY: sk-abc123\n",
		"  QUOTED_KEY: \"with spaces\"\n",
		"  client-connection/browser-session:\n    kind: grant\n",
		"secret: UJxnEipPCJpcmTXsz250c4vcSGq1hxXGtUg5qlS41Kw\n",
		`  deepseek-account-platform/device: {kind: grant, payload: {id: "` + deviceID + `"}}`,
		`  deepseek-account-platform/default: {kind: grant, payload: {version: 1, token: "tok/en+with$dollar", issuer: "https://platform.deepseek.com"}}`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("凭据文件缺少 %q:\n%s", want, got)
		}
	}
	if id := accountParseDeviceID(got); id != deviceID {
		t.Fatalf("回读 device id = %q, want %q", id, deviceID)
	}
	if tok := accountParseGrantToken(got); tok != "tok/en+with$dollar" {
		t.Fatalf("回读 token = %q", tok)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("凭据文件权限应为 0600，实际 %v (%v)", fi.Mode().Perm(), err)
	}

	// 覆盖写：旧值被替换而不是叠加，其它内容照旧。
	if err := m.accountStoreGrant("second"); err != nil {
		t.Fatalf("store grant again: %v", err)
	}
	raw, _ = os.ReadFile(path)
	if strings.Count(string(raw), "deepseek-account-platform/default:") != 1 ||
		!strings.Contains(string(raw), `token: "second"`) {
		t.Fatalf("覆盖写入不正确:\n%s", raw)
	}
	if !strings.Contains(string(raw), "  SOME_API_KEY: sk-abc123\n") {
		t.Fatal("覆盖写入动了别的条目")
	}

	// 删除凭据（退出登录）：记录消失，其它内容保留。
	if err := m.accountRemoveGrant(); err != nil {
		t.Fatalf("remove grant: %v", err)
	}
	raw, _ = os.ReadFile(path)
	if strings.Contains(string(raw), "deepseek-account-platform/default") {
		t.Fatalf("凭据未被删除:\n%s", raw)
	}
	if !strings.Contains(string(raw), "client-connection/browser-session") {
		t.Fatal("删除凭据时误删了别的记录")
	}
	if id := accountParseDeviceID(string(raw)); id != deviceID {
		t.Fatal("删除凭据时误删了 device 记录")
	}
}

// 空/缺文件的凭据库：要能补出最小可用骨架（dsh 之后自己会补全其它字段）。
func TestAccountStoreCreatesMissingDocument(t *testing.T) {
	home := t.TempDir()
	m := newAccountTestMux(t, home)
	if _, err := m.accountEnsureDeviceID(); err != nil {
		t.Fatalf("ensure device on empty home: %v", err)
	}
	if err := m.accountStoreGrant("tok"); err != nil {
		t.Fatalf("store grant on empty home: %v", err)
	}
	path, _ := m.accountCredentialsPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "version: 1\n") || !strings.Contains(string(raw), "records:\n") {
		t.Fatalf("骨架不完整:\n%s", raw)
	}
}

// 回调地址解析：整条 URL、只有 query、带引号/空白、错地址。
func TestParseAccountCallback(t *testing.T) {
	cases := []struct {
		in          string
		code, state string
	}{
		{"http://127.0.0.1:3080/app/Harness/dsh/_account/callback?code=abc&state=xyz", "abc", "xyz"},
		{"code=abc&state=xyz", "abc", "xyz"},
		{"  \"http://localhost:1/cb?state=xyz&code=abc\"  ", "abc", "xyz"},
		{"https://fn.1kw.site/app/Harness/_account/callback?code=a-b_c&state=s-1", "a-b_c", "s-1"},
		{"http://127.0.0.1/cb", "", ""},
		{"", "", ""},
	}
	for _, tc := range cases {
		code, state := parseAccountCallback(tc.in)
		if code != tc.code || state != tc.state {
			t.Fatalf("parseAccountCallback(%q) = (%q, %q), want (%q, %q)", tc.in, code, state, tc.code, tc.state)
		}
	}
}

// 回调来源判定：只有 loopback 才「用浏览器自己的来源」，其余一律退回占位地址。
func TestAccountRedirectURI(t *testing.T) {
	home := writeAccountCreds(t, accountCredsFixture)
	m := newAccountTestMux(t, home)
	prev := GetConfig()
	initConfig(&AppConfig{ProxyPort: 3079})
	t.Cleanup(func() { initConfig(&prev) })

	cases := []struct {
		origin string
		want   string
	}{
		{"http://127.0.0.1:2298", "http://127.0.0.1:2298/app/Harness/oauth/callback"},
		{"http://localhost:3079", "http://localhost:3079/app/Harness/oauth/callback"},
		{"https://fn.1kw.site:3333", "http://127.0.0.1:3079/oauth/callback"},
		{"http://192.168.5.222:3079", "http://127.0.0.1:3079/oauth/callback"},
		// path/query 不干净的来源不算 loopback 来源（退回占位）。
		{"http://127.0.0.1:2298/other", "http://127.0.0.1:3079/oauth/callback"},
	}
	for _, tc := range cases {
		if got := m.accountRedirectURI(tc.origin); got != tc.want {
			t.Fatalf("accountRedirectURI(%q) = %q, want %q", tc.origin, got, tc.want)
		}
	}

	// 网关那条线关闭时前缀为空（占位地址落在根挂载上）。
	m.renv.ProxySock = "off"
	if got := m.accountRedirectURI("https://fn.1kw.site:3333"); got != "http://127.0.0.1:3079/oauth/callback" {
		t.Fatalf("无挂载前缀时 = %q", got)
	}
}

// accountFlowFixture 起一个假平台并返回一个可用的 AdminMux（假核验调用方自己设）。
func accountFlowFixture(t *testing.T) (string, *AdminMux, accountClientMeta, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]interface{}{"code": 0, "data": map[string]interface{}{"biz_code": 0, "biz_data": map[string]interface{}{
			"authorize_url": "https://platform.deepseek.com/dsh/authorize?authorize_id=abc", "authorize_id": "abc", "expires_in": 600,
		}}}
		if strings.HasSuffix(r.URL.Path, "auth_exchange") {
			body["data"] = map[string]interface{}{"biz_code": 0, "biz_data": map[string]interface{}{
				"token": "tok-from-platform", "authorized_url": "https://platform.deepseek.com/dsh/authorized",
				"user": map[string]interface{}{"mobile_number": "139******59"},
			}}
		}
		json.NewEncoder(w).Encode(body)
	}))
	prevOrigin := accountPlatformOrigin
	accountPlatformOrigin = srv.URL
	t.Cleanup(func() { accountPlatformOrigin = prevOrigin })
	prevCfg := GetConfig()
	initConfig(&AppConfig{ProxyPort: 3079})
	t.Cleanup(func() { initConfig(&prevCfg) })

	home := writeAccountCreds(t, accountCredsFixture)
	m := newAccountTestMux(t, home)
	accountAttempts.Lock()
	accountAttempts.cur = nil
	accountAttempts.Unlock()
	return home, m, accountClientMeta{Version: "test", WireLocale: "zh_CN", TimezoneOffsetSeconds: 28800}, srv
}

// 用假平台跑完整流程：start（挑 redirect_uri）→ complete（state 校验 + 换 token + 写凭据）。
func TestAccountLoginFlowWithFakePlatform(t *testing.T) {
	var gotInit, gotExchange map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth-api/v0/dsh/auth_init":
			json.NewDecoder(r.Body).Decode(&gotInit)
			for _, h := range []string{"x-client-platform", "x-client-version", "x-client-locale", "x-client-timezone-offset"} {
				if r.Header.Get(h) == "" {
					t.Errorf("auth_init 缺少请求头 %s", h)
				}
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 0,
				"data": map[string]interface{}{"biz_code": 0, "biz_data": map[string]interface{}{
					"authorize_url": "https://platform.deepseek.com/dsh/authorize?authorize_id=abc",
					"authorize_id":  "abc", "expires_in": 600,
				}},
			})
		case "/auth-api/v0/dsh/auth_exchange":
			json.NewDecoder(r.Body).Decode(&gotExchange)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 0,
				"data": map[string]interface{}{"biz_code": 0, "biz_data": map[string]interface{}{
					"token": "tok-from-platform", "authorized_url": "https://platform.deepseek.com/dsh/authorized",
					"user": map[string]interface{}{"mobile_number": "139******59"},
				}},
			})
		default:
			t.Errorf("未预期的平台请求 %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	prevOrigin := accountPlatformOrigin
	accountPlatformOrigin = srv.URL
	t.Cleanup(func() { accountPlatformOrigin = prevOrigin })

	// 核验那一步走假实现：dsh 认了这笔凭据。
	prevVerify := accountVerifyStoreFn
	accountVerifyStoreFn = func(*AdminMux) (string, error) { return "credential-stored", nil }
	t.Cleanup(func() { accountVerifyStoreFn = prevVerify })

	home := writeAccountCreds(t, accountCredsFixture)
	m := newAccountTestMux(t, home)
	prevCfg := GetConfig()
	initConfig(&AppConfig{ProxyPort: 3079})
	t.Cleanup(func() { initConfig(&prevCfg) })

	meta := accountClientMeta{Version: "test", WireLocale: "zh_CN", TimezoneOffsetSeconds: 28800}
	st, err := m.accountLoginStart("https://fn.1kw.site:3333", meta)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if st.Phase != accountPhaseWaiting || st.AuthorizeURL != "https://platform.deepseek.com/dsh/authorize?authorize_id=abc" {
		t.Fatalf("start 状态不对: %+v", st)
	}
	// 平台拿到的 redirect_uri 必须是 loopback（平台的硬规则）。
	redirect, _ := gotInit["redirect_uri"].(string)
	if redirect != "http://127.0.0.1:3079/oauth/callback" {
		t.Fatalf("auth_init redirect_uri = %q", redirect)
	}
	if gotInit["login_source"] != "web" || gotInit["code_challenge_method"] != "S256" {
		t.Fatalf("auth_init 参数不对: %v", gotInit)
	}

	// state 不对 → 拒绝，且不消耗本次尝试。
	if _, err := m.accountLoginComplete("http://127.0.0.1:3080/cb?code=x&state=wrong", "", "", meta); err == nil {
		t.Fatal("state 不匹配时应报错")
	}
	if m.accountLoginStateSnapshot().Phase != accountPhaseWaiting {
		t.Fatal("state 不匹配不该终止本次尝试")
	}

	// 正确 state → 换 token、写凭据、标记成功。
	callback := "http://127.0.0.1:3080/oauth/callback?code=the-code&state=" + currentAccountState(t)
	st, err = m.accountLoginComplete(callback, "", "", meta)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if st.Phase != accountPhaseSucceeded || st.Contact != "139******59" {
		t.Fatalf("complete 状态不对: %+v", st)
	}
	if gotExchange["code"] != "the-code" || gotExchange["redirect_uri"] != redirect {
		t.Fatalf("auth_exchange 参数不对: %v", gotExchange)
	}
	if _, ok := gotExchange["device_id"].(string); !ok {
		t.Fatalf("auth_exchange 缺少 device_id: %v", gotExchange)
	}
	path, _ := m.accountCredentialsPath()
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `token: "tok-from-platform"`) {
		t.Fatalf("凭据未写入:\n%s", raw)
	}
	// 同一笔授权不能重复兑换。
	if _, err := m.accountLoginComplete(callback, "", "", meta); err == nil {
		t.Fatal("已完成的尝试应拒绝再次兑换")
	}
}

// dsh 明确回答「没这笔凭据」时要回滚（不留半截状态）。
func TestAccountLoginRollsBackWhenDshRejects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]interface{}{"code": 0, "data": map[string]interface{}{"biz_code": 0, "biz_data": map[string]interface{}{
			"authorize_url": "https://platform.deepseek.com/dsh/authorize?authorize_id=abc", "authorize_id": "abc", "expires_in": 600,
		}}}
		if strings.HasSuffix(r.URL.Path, "auth_exchange") {
			body["data"] = map[string]interface{}{"biz_code": 0, "biz_data": map[string]interface{}{
				"token": "tok", "authorized_url": "https://platform.deepseek.com/dsh/authorized",
			}}
		}
		json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()
	prevOrigin := accountPlatformOrigin
	accountPlatformOrigin = srv.URL
	t.Cleanup(func() { accountPlatformOrigin = prevOrigin })

	prevVerify := accountVerifyStoreFn
	accountVerifyStoreFn = func(*AdminMux) (string, error) { return "signed-out", nil }
	t.Cleanup(func() { accountVerifyStoreFn = prevVerify })
	prevTimeout, prevInterval := accountVerifyPollTimeout, accountVerifyPollInterval
	accountVerifyPollTimeout, accountVerifyPollInterval = 5*time.Millisecond, time.Millisecond
	t.Cleanup(func() { accountVerifyPollTimeout, accountVerifyPollInterval = prevTimeout, prevInterval })

	home := writeAccountCreds(t, accountCredsFixture)
	m := newAccountTestMux(t, home)
	meta := accountClientMeta{Version: "test", WireLocale: "zh_CN"}
	if _, err := m.accountLoginStart("https://fn.1kw.site:3333", meta); err != nil {
		t.Fatalf("start: %v", err)
	}
	callback := "http://127.0.0.1:1/cb?code=c&state=" + currentAccountState(t)
	if _, err := m.accountLoginComplete(callback, "", "", meta); err == nil {
		t.Fatal("dsh 不认凭据时应报错")
	}
	// 回滚只针对账号凭据：device 记录是本地设备标识（dsh 自己也会建），保留是刻意的。
	path, _ := m.accountCredentialsPath()
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "deepseek-account-platform/default") {
		t.Fatalf("凭据未回滚:\n%s", raw)
	}
	for _, keep := range []string{"SOME_API_KEY: sk-abc123", "client-connection/browser-session", accountDeviceKey} {
		if !strings.Contains(string(raw), keep) {
			t.Fatalf("回滚误删了 %q:\n%s", keep, raw)
		}
	}
}

// 核验必须**轮询**：写完立刻问 dsh 会读到旧值（chokidar 还没刷到），照那个值判会把一次
// 成功的登录回滚掉 —— 这个回归测试守住「前几次 signed-out 之后认了」的情况。
func TestAccountLoginVerifyRetries(t *testing.T) {
	_, m, meta, srv := accountFlowFixture(t)
	defer srv.Close()

	prevTimeout, prevInterval := accountVerifyPollTimeout, accountVerifyPollInterval
	accountVerifyPollTimeout, accountVerifyPollInterval = 2*time.Second, time.Millisecond
	t.Cleanup(func() { accountVerifyPollTimeout, accountVerifyPollInterval = prevTimeout, prevInterval })

	calls := 0
	prevVerify := accountVerifyStoreFn
	accountVerifyStoreFn = func(*AdminMux) (string, error) {
		calls++
		if calls < 3 {
			return "signed-out", nil // dsh 还没看到新文档
		}
		return "credential-stored", nil
	}
	t.Cleanup(func() { accountVerifyStoreFn = prevVerify })

	if _, err := m.accountLoginStart("https://fn.1kw.site:3333", meta); err != nil {
		t.Fatalf("start: %v", err)
	}
	st, err := m.accountLoginComplete("http://127.0.0.1:1/cb?code=c&state="+currentAccountState(t), "", "", meta)
	if err != nil {
		t.Fatalf("轮询到 dsh 认账时应成功，却报错: %v", err)
	}
	if st.Phase != accountPhaseSucceeded {
		t.Fatalf("phase = %q", st.Phase)
	}
	if calls < 3 {
		t.Fatalf("只问了 %d 次，没走轮询", calls)
	}
	path, _ := m.accountCredentialsPath()
	if raw, _ := os.ReadFile(path); !strings.Contains(string(raw), "tok-from-platform") {
		t.Fatalf("凭据不该被回滚:\n%s", raw)
	}
}

// dsh 全程问不到（未就绪）时**不回滚**：凭据已经写好，它下次读到就会用。
func TestAccountLoginKeepsCredentialWhenDshUnavailable(t *testing.T) {
	_, m, meta, srv := accountFlowFixture(t)
	defer srv.Close()

	prevTimeout, prevInterval := accountVerifyPollTimeout, accountVerifyPollInterval
	accountVerifyPollTimeout, accountVerifyPollInterval = 5*time.Millisecond, time.Millisecond
	t.Cleanup(func() { accountVerifyPollTimeout, accountVerifyPollInterval = prevTimeout, prevInterval })

	prevVerify := accountVerifyStoreFn
	accountVerifyStoreFn = func(*AdminMux) (string, error) { return "", errors.New("dsh session is not ready") }
	t.Cleanup(func() { accountVerifyStoreFn = prevVerify })

	if _, err := m.accountLoginStart("https://fn.1kw.site:3333", meta); err != nil {
		t.Fatalf("start: %v", err)
	}
	st, err := m.accountLoginComplete("http://127.0.0.1:1/cb?code=c&state="+currentAccountState(t), "", "", meta)
	if err != nil {
		t.Fatalf("问不到 dsh 不该算失败: %v", err)
	}
	if st.Phase != accountPhaseSucceeded {
		t.Fatalf("phase = %q", st.Phase)
	}
	path, _ := m.accountCredentialsPath()
	if raw, _ := os.ReadFile(path); !strings.Contains(string(raw), "tok-from-platform") {
		t.Fatalf("凭据不该被回滚:\n%s", raw)
	}
}

// 超时的尝试不能再用。
func TestAccountLoginExpired(t *testing.T) {
	home := writeAccountCreds(t, accountCredsFixture)
	m := newAccountTestMux(t, home)
	accountAttempts.Lock()
	accountAttempts.cur = &accountAttempt{
		verifier: "v", state: "s", redirectURI: "http://127.0.0.1:1/cb",
		authorizeID: "a", authorize: "https://p/a", phase: accountPhaseWaiting,
		expiresAt: time.Now().Add(-time.Second),
	}
	accountAttempts.Unlock()
	meta := accountClientMeta{Version: "test"}
	if _, err := m.accountLoginComplete("http://127.0.0.1:1/cb?code=c&state=s", "", "", meta); err == nil {
		t.Fatal("过期的尝试应报错")
	}
	if st := m.accountLoginStateSnapshot(); st.Phase != accountPhaseExpired {
		t.Fatalf("过期后 phase = %q", st.Phase)
	}
	accountAttempts.Lock()
	accountAttempts.cur = nil
	accountAttempts.Unlock()
}

// 没有在途尝试时提交回调 → 明确报错（用户刷新后重试的常见路径）。
func TestAccountLoginCompleteWithoutAttempt(t *testing.T) {
	home := writeAccountCreds(t, accountCredsFixture)
	m := newAccountTestMux(t, home)
	accountAttempts.Lock()
	accountAttempts.cur = nil
	accountAttempts.Unlock()
	if _, err := m.accountLoginComplete("http://127.0.0.1:1/cb?code=c&state=s", "", "", accountClientMeta{}); err == nil {
		t.Fatal("没有在途尝试时应报错")
	}
}

// currentAccountState 读当前尝试的 state（测试里拿它拼回调地址）。
func currentAccountState(t *testing.T) string {
	t.Helper()
	accountAttempts.Lock()
	defer accountAttempts.Unlock()
	if accountAttempts.cur == nil {
		t.Fatal("没有在途尝试")
	}
	return accountAttempts.cur.state
}

// 回调入口：无论成败都要给浏览器一个明确的页面（用户是「顺着地址栏点进来」的，
// 不能是空白或 404），且失败时不影响在途尝试。
func TestAccountCallbackRoutePage(t *testing.T) {
	home := writeAccountCreds(t, accountCredsFixture)
	m := newAccountTestMux(t, home)

	// 没有在途尝试：失败页。
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, accountCallbackPath+"?code=c&state=s", nil)
	m.handleAccountCallback(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "登录没有完成") || !strings.Contains(body, "did not complete") {
		t.Fatalf("失败页不对: HTTP %d\n%s", rec.Code, body)
	}

	// 有在途尝试但 state 不对：同样是失败页，且尝试仍在等授权。
	accountAttempts.Lock()
	accountAttempts.cur = &accountAttempt{
		verifier: "v", state: "right", redirectURI: "http://127.0.0.1:1/cb",
		authorizeID: "a", authorize: "https://p/a", phase: accountPhaseWaiting,
		expiresAt: time.Now().Add(time.Minute),
	}
	accountAttempts.Unlock()
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, accountCallbackPath+"?code=c&state=wrong", nil)
	m.handleAccountCallback(rec, req)
	if !strings.Contains(rec.Body.String(), "登录没有完成") {
		t.Fatalf("state 不匹配应给失败页:\n%s", rec.Body.String())
	}
	if st := m.accountLoginStateSnapshot(); st.Phase != accountPhaseWaiting {
		t.Fatalf("回调失败不该终止尝试，phase=%q", st.Phase)
	}
	accountAttempts.Lock()
	accountAttempts.cur = nil
	accountAttempts.Unlock()
}

// 退出登录也要等 dsh 认账（删掉凭据后 dsh 要过一拍才更新内存里的文档）—— 不等的话
// 紧接着的状态查询还是「已登录」，界面只能靠用户手动刷新。
func TestAccountLogoutWaitsForDsh(t *testing.T) {
	home, m, meta, srv := accountFlowFixture(t)
	defer srv.Close()
	_ = meta
	prevTimeout, prevInterval := accountVerifyPollTimeout, accountVerifyPollInterval
	accountVerifyPollTimeout, accountVerifyPollInterval = 2*time.Second, time.Millisecond
	t.Cleanup(func() { accountVerifyPollTimeout, accountVerifyPollInterval = prevTimeout, prevInterval })

	// 文件里先有一笔凭据（退出登录要把它删掉）。
	if err := m.accountStoreGrant("tok-from-platform"); err != nil {
		t.Fatalf("pre-store grant: %v", err)
	}
	calls := 0
	prevVerify := accountVerifyStoreFn
	accountVerifyStoreFn = func(*AdminMux) (string, error) {
		calls++
		if calls < 3 {
			return "credential-stored", nil // dsh 还没看到记录被删
		}
		return "signed-out", nil
	}
	t.Cleanup(func() { accountVerifyStoreFn = prevVerify })

	rec := httptest.NewRecorder()
	m.handleAccountLogout(rec, httptest.NewRequest(http.MethodPost, "/api/account/logout", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("logout HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if calls < 3 {
		t.Fatalf("只问了 %d 次 dsh，没等它认账", calls)
	}
	// 响应直接给最新状态快照（前端据此刷新列表），dsh 已认账时 signedIn=false。
	if !strings.Contains(rec.Body.String(), `"signedIn": false`) {
		t.Fatalf("响应快照未反映已退出: %s", rec.Body.String())
	}
	path := filepath.Join(home, ".dsh", ".credentials.yaml")
	if raw, _ := os.ReadFile(path); strings.Contains(string(raw), "deepseek-account-platform/default") {
		t.Fatalf("凭据未被删除:\n%s", raw)
	}
}

// 回调路径的三种写法都要被反代线认出来（尤其别退化成只剩根路径那种）。
func TestAccountCallbackMountPaths(t *testing.T) {
	renv := &RuntimeEnv{AdminBaseURL: "/app/Harness", ProxySock: "/x/dsh.sock", ProxyBaseURL: "/app/Harness/dsh"}
	got := accountCallbackMountPaths(renv)
	want := []string{"/oauth/callback", "/app/Harness/dsh/oauth/callback", "/app/Harness/oauth/callback"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("accountCallbackMountPaths() = %v, want %v", got, want)
	}
	// 网关线关闭时只有根路径（不重复、不带挂载前缀）。
	renv.ProxySock = "off"
	if got := accountCallbackMountPaths(renv); strings.Join(got, ",") != "/oauth/callback,/app/Harness/oauth/callback" {
		t.Fatalf("无挂载前缀时 = %v", got)
	}
}

// 账号库：落盘/读回/权限/按 id 归并/删除/「当前生效」推导（密文形态的断言见
// accountvault_crypt_test.go）。
func TestAccountVaultRoundTrip(t *testing.T) {
	t.Setenv("HARNESS_DATA_DIR", t.TempDir())
	crypt := testCryptor(t)
	v, err := loadAccountVault(accountVaultDiskPath(), crypt)
	if err != nil {
		t.Fatalf("空库应可读: %v", err)
	}
	if len(v.Accounts) != 0 {
		t.Fatalf("新库应为空: %+v", v)
	}
	v.upsert(accountVaultEntry{ID: "a", Contact: "139******59", Token: "t1", AddedAt: "2026-01-01T00:00:00Z"})
	v.upsert(accountVaultEntry{ID: "b", Contact: "138******00", Token: "t2"})
	// 同一账号重新登录：只换 token，AddedAt 保留，不新增条目。
	v.upsert(accountVaultEntry{ID: "a", Contact: "139******59", Token: "t1-new"})
	if len(v.Accounts) != 2 {
		t.Fatalf("重复 id 应归并，实际 %d 条", len(v.Accounts))
	}
	if got := v.find("a"); got == nil || got.Token != "t1-new" || got.AddedAt != "2026-01-01T00:00:00Z" {
		t.Fatalf("归并结果不对: %+v", got)
	}
	if err := saveAccountVault(accountVaultDiskPath(), crypt, v); err != nil {
		t.Fatalf("save: %v", err)
	}
	path := dataPath(accountVaultFile)
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("账号库权限应为 0600: %v (%v)", fi.Mode().Perm(), err)
	}
	back, err := loadAccountVault(accountVaultDiskPath(), crypt)
	if err != nil || len(back.Accounts) != 2 || back.find("a").Token != "t1-new" {
		t.Fatalf("读回不一致: %+v (%v)", back, err)
	}
	// 当前生效靠 token 比对推导；列表不外传 token。
	if back.activeID("t2") != "b" || back.activeID("t1") != "" {
		t.Fatalf("activeID 推导不对: %q / %q", back.activeID("t2"), back.activeID("t1"))
	}
	views := accountVaultViews(back, "t2")
	if len(views) != 2 || !views[1].Active || views[0].Active {
		t.Fatalf("列表标记不对: %+v", views)
	}
	if !back.remove("b") || back.remove("b") {
		t.Fatal("remove 语义不对")
	}
	if len(back.Accounts) != 1 || back.find("b") != nil {
		t.Fatalf("删除后仍有: %+v", back.Accounts)
	}
	// 坏文件要报错（调用方转成「账号库读写失败」）。
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAccountVault(accountVaultDiskPath(), crypt); err == nil {
		t.Fatal("坏库应报错")
	}
}

// 多账号：列表、切换（写另一份 token + 等 dsh 认账）、退出登录（保留库里条目）、移除（作废）。
func TestAccountMultiAccountFlow(t *testing.T) {
	home := writeAccountCreds(t, accountCredsFixture)
	dataDir := t.TempDir()
	t.Setenv("HARNESS_DATA_DIR", dataDir)
	m := newAccountTestMux(t, home)
	prevCfg := GetConfig()
	initConfig(&AppConfig{ProxyPort: 3079})
	t.Cleanup(func() { initConfig(&prevCfg) })

	// 假平台（只用于「移除」时的 token 作废）。
	revoked := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		revoked = r.Header.Get("x-dsh-auth-token")
		w.WriteHeader(200)
	}))
	defer srv.Close()
	prevOrigin := accountPlatformOrigin
	accountPlatformOrigin = srv.URL
	t.Cleanup(func() { accountPlatformOrigin = prevOrigin })

	state := "credential-stored"
	prevState := accountDshStateFn
	accountDshStateFn = func(*AdminMux) (string, error) { return state, nil }
	t.Cleanup(func() { accountDshStateFn = prevState })
	prevVerify := accountVerifyStoreFn
	accountVerifyStoreFn = func(*AdminMux) (string, error) { return state, nil }
	t.Cleanup(func() { accountVerifyStoreFn = prevVerify })
	prevTimeout, prevInterval := accountVerifyPollTimeout, accountVerifyPollInterval
	accountVerifyPollTimeout, accountVerifyPollInterval = 20*time.Millisecond, time.Millisecond
	t.Cleanup(func() { accountVerifyPollTimeout, accountVerifyPollInterval = prevTimeout, prevInterval })

	// 库里两条账号，凭据文件里是第一条 → 快照应标出它 active。
	v, _ := loadAccountVault(accountVaultDiskPath(), m.crypt)
	v.upsert(accountVaultEntry{ID: "acc-1", Contact: "139******59", Token: "tok-1"})
	v.upsert(accountVaultEntry{ID: "acc-2", Contact: "138******00", Token: "tok-2"})
	if err := saveAccountVault(accountVaultDiskPath(), m.crypt, v); err != nil {
		t.Fatal(err)
	}
	if err := m.accountStoreGrant("tok-1"); err != nil {
		t.Fatal(err)
	}
	st := m.accountStatusSnapshot()
	if len(st.Accounts) != 2 || st.Accounts[0].ID != "acc-1" || !st.Accounts[0].Active || st.Accounts[1].Active {
		t.Fatalf("快照的账号列表不对: %+v", st.Accounts)
	}

	// 切换：文件里的 token 变成 tok-2，快照标 acc-2 active，LastUsedAt 记上。
	rec := httptest.NewRecorder()
	m.handleAccountSwitch(rec, httptest.NewRequest(http.MethodPost, "/api/account/switch",
		strings.NewReader(`{"id":"acc-2"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("switch HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if tok := m.accountActiveToken(); tok != "tok-2" {
		t.Fatalf("切换后凭据里的 token = %q", tok)
	}
	back, _ := loadAccountVault(accountVaultDiskPath(), m.crypt)
	if e := back.find("acc-2"); e == nil || e.LastUsedAt == "" {
		t.Fatalf("切换后未记录 LastUsedAt: %+v", e)
	}
	var switched accountStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &switched); err != nil {
		t.Fatal(err)
	}
	if len(switched.Accounts) != 2 || !switched.Accounts[1].Active {
		t.Fatalf("切换返回的快照不对: %+v", switched.Accounts)
	}
	// 已在用的账号再切一次：幂等、不写盘（凭据里还是 tok-2）。
	m.handleAccountSwitch(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/account/switch",
		strings.NewReader(`{"id":"acc-2"}`)))
	if tok := m.accountActiveToken(); tok != "tok-2" {
		t.Fatalf("幂等切换改了凭据: %q", tok)
	}

	// 切换失败（dsh 不认）：回滚到切换前的凭据，库里条目不动。
	state = "signed-out"
	rec = httptest.NewRecorder()
	m.handleAccountSwitch(rec, httptest.NewRequest(http.MethodPost, "/api/account/switch",
		strings.NewReader(`{"id":"acc-1"}`)))
	if rec.Code == http.StatusOK {
		t.Fatalf("dsh 不认时应报错: %s", rec.Body.String())
	}
	if tok := m.accountActiveToken(); tok != "tok-2" {
		t.Fatalf("切换失败后未回滚，凭据里是 %q", tok)
	}
	state = "credential-stored"

	// 退出登录：凭据记录删掉，但账号仍留在库里（可随时切回）。
	rec = httptest.NewRecorder()
	m.handleAccountLogout(rec, httptest.NewRequest(http.MethodPost, "/api/account/logout", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("logout HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if tok := m.accountActiveToken(); tok != "" {
		t.Fatalf("退出登录后凭据仍在: %q", tok)
	}
	if back, _ := loadAccountVault(accountVaultDiskPath(), m.crypt); len(back.Accounts) != 2 {
		t.Fatalf("退出登录不该删库里的账号: %+v", back.Accounts)
	}

	// 移除一个不在用的账号：请平台作废 token，库里少一条。
	rec = httptest.NewRecorder()
	m.handleAccountRemove(rec, httptest.NewRequest(http.MethodPost, "/api/account/remove",
		strings.NewReader(`{"id":"acc-1"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("remove HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if revoked != "tok-1" {
		t.Fatalf("未请平台作废 token，收到 %q", revoked)
	}
	if back, _ := loadAccountVault(accountVaultDiskPath(), m.crypt); len(back.Accounts) != 1 || back.find("acc-1") != nil {
		t.Fatalf("移除后库里不对: %+v", back.Accounts)
	}

	// 未知 id → 404（切换与移除都要）。
	for _, path := range []string{"/api/account/switch", "/api/account/remove"} {
		rec = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"id":"nope"}`))
		if path == "/api/account/switch" {
			m.handleAccountSwitch(rec, req)
		} else {
			m.handleAccountRemove(rec, req)
		}
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s 未知 id 应回 404，实际 %d", path, rec.Code)
		}
	}
}

// 自愈导入：dsh 正在用的 token 不在库里（老设备先登录后升级控制台）时补进库，
// 否则列表里看不到「正在用的那个账号」，切成别的就再也切不回来。
func TestAccountStatusImportsActiveAccount(t *testing.T) {
	home := writeAccountCreds(t, accountCredsFixture)
	t.Setenv("HARNESS_DATA_DIR", t.TempDir())
	m := newAccountTestMux(t, home)
	prevCfg := GetConfig()
	initConfig(&AppConfig{ProxyPort: 3079})
	t.Cleanup(func() { initConfig(&prevCfg) })
	prevState := accountDshStateFn
	accountDshStateFn = func(*AdminMux) (string, error) { return "credential-stored", nil }
	t.Cleanup(func() { accountDshStateFn = prevState })
	if err := m.accountStoreGrant("tok-existing"); err != nil {
		t.Fatal(err)
	}

	st := m.accountStatusSnapshot()
	if len(st.Accounts) != 1 || !st.Accounts[0].Active {
		t.Fatalf("应把在用的账号导入库: %+v", st.Accounts)
	}
	if back, _ := loadAccountVault(accountVaultDiskPath(), m.crypt); back.activeID("tok-existing") == "" {
		t.Fatalf("库未落盘: %+v", back)
	}
	// 再查一次不应重复导入。
	st = m.accountStatusSnapshot()
	if len(st.Accounts) != 1 {
		t.Fatalf("重复导入: %+v", st.Accounts)
	}
}

// errorCodeOf 从错误响应体里取 code（写法见 admin.go 的 writeErrBody）。
func errorCodeOf(t *testing.T, body []byte) string {
	t.Helper()
	var out struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("响应体不是 JSON: %v\n%s", err, body)
	}
	return out.Code
}

// 凭据文件按 dsh 自己的数据根定位：$DSH_HOME 优先（平台启动脚本会注入），拿不到时才退回
// <HOME>/.dsh；两者都没有时报错 —— 绝不拼出 ".dsh" 这种**相对**路径（那会落到进程 CWD）。
func TestAccountCredentialsPathFollowsDshHome(t *testing.T) {
	home := t.TempDir()

	// ① 显式 DshHome（= $DSH_HOME）优先：凭据写在它下面，而不是 <HOME>/.dsh。
	custom := filepath.Join(t.TempDir(), "dsh-home")
	m := newAccountTestMux(t, home)
	m.renv.DshHome = custom
	path, err := m.accountCredentialsPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(custom, ".credentials.yaml"); path != want {
		t.Fatalf("accountCredentialsPath = %q, want %q", path, want)
	}
	// 端到端：真的写在那里（设备记录会被创建）。
	if _, err := m.accountEnsureDeviceID(); err != nil {
		t.Fatalf("写设备记录失败: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("凭据没落在 $DSH_HOME 下: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".dsh", ".credentials.yaml")); !os.IsNotExist(err) {
		t.Fatalf("<HOME>/.dsh 下不该出现凭据文件: %v", err)
	}

	// ② 没有 DshHome：退回 <HOME>/.dsh（平台没注入 DSH_HOME 时的默认形态）。
	m2 := newAccountTestMux(t, home)
	m2.renv.DshHome = ""
	if p2, err := m2.accountCredentialsPath(); err != nil {
		t.Fatal(err)
	} else if want := filepath.Join(home, ".dsh", ".credentials.yaml"); p2 != want {
		t.Fatalf("回退路径 = %q, want %q", p2, want)
	}

	// ③ 两个都没有：报错，而不是返回 ".credentials.yaml" 这种相对路径。
	m3 := &AdminMux{renv: &RuntimeEnv{}}
	if p, err := m3.accountCredentialsPath(); err == nil {
		t.Fatalf("主目录与 DSH_HOME 都为空时应报错，实得 %q", p)
	}
}

// loadRuntimeEnv 里的 DshHome：$DSH_HOME 优先，否则 <HOME>/.dsh（与 dsh 的 resolveDshHome 一致）。
func TestLoadRuntimeEnvDshHome(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "custom-dsh")
	t.Setenv("DSH_HOME", custom)
	if got := loadRuntimeEnv().DshHome; got != custom {
		t.Fatalf("$DSH_HOME 注入时 DshHome = %q, want %q", got, custom)
	}
	t.Setenv("DSH_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got, want := loadRuntimeEnv().DshHome, filepath.Join(home, ".dsh"); got != want {
		t.Fatalf("没有 DSH_HOME 时 DshHome = %q, want %q", got, want)
	}
	// 主目录也没有：留空，交给调用方报错（不能拼出相对路径）。
	t.Setenv("HOME", "")
	if got := loadRuntimeEnv().DshHome; got != "" {
		t.Fatalf("HOME 与 DSH_HOME 都为空时 DshHome 应留空，实得 %q", got)
	}
}

// 「移除账号」在账号库写盘失败时必须**如实报错**（本机凭据已删、账号还在库里，用户可重试）：
// 返回 200 会让前端弹「已移除账号 X」，而列表里它还在（修复前就是这个表现）。
func TestAccountRemoveReportsVaultWriteFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("以 root 运行时目录权限不生效，构造不出「读得到、写不进」")
	}
	home := writeAccountCreds(t, accountCredsFixture)
	dataDir := t.TempDir()
	t.Setenv("HARNESS_DATA_DIR", dataDir)
	m := newAccountTestMux(t, home)
	prevCfg := GetConfig()
	initConfig(&AppConfig{ProxyPort: 3079})
	t.Cleanup(func() { initConfig(&prevCfg) })
	// 假平台：本用例不该真的去打 platform.deepseek.com（移除会请平台作废 token）。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	prevOrigin := accountPlatformOrigin
	accountPlatformOrigin = srv.URL
	t.Cleanup(func() { accountPlatformOrigin = prevOrigin })
	prevState := accountDshStateFn
	accountDshStateFn = func(*AdminMux) (string, error) { return "signed-out", nil }
	t.Cleanup(func() { accountDshStateFn = prevState })

	if err := accountVaultMutate(accountVaultDiskPath(), m.crypt, func(v *accountVault) error {
		v.upsert(accountVaultEntry{ID: "acc-keep", Token: "tok-keep"})
		v.upsert(accountVaultEntry{ID: "acc-drop", Token: "tok-drop", Contact: "139******02"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// 让**写**失败而**读**照常：数据目录只读（save 的 CreateTemp 必然 EACCES）。
	if err := os.Chmod(dataDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dataDir, 0o700) })

	rec := httptest.NewRecorder()
	m.handleAccountRemove(rec, httptest.NewRequest(http.MethodPost, "/api/account/remove",
		strings.NewReader(`{"id":"acc-drop"}`)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("库写失败时应报错（不能回 200），实得 %d: %s", rec.Code, rec.Body.String())
	}
	if code := errorCodeOf(t, rec.Body.Bytes()); code != "err_account_vault_failed" {
		t.Fatalf("应带 err_account_vault_failed（前端按它查译文），实得 %q", code)
	}

	// 账号仍在库里（没被半删）—— 前端据此让用户重试。
	if err := os.Chmod(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	back, err := loadAccountVault(accountVaultDiskPath(), m.crypt)
	if err != nil {
		t.Fatal(err)
	}
	if back.find("acc-drop") == nil || back.find("acc-keep") == nil {
		t.Fatalf("库内容不对（不该丢条目）: %+v", back.Accounts)
	}
}

// 「切换」时那次库写入（只更新最近使用时间）失败**不**让切换整体失败：凭据已经换好、dsh 也认了，
// 界面上的「使用中」是按凭据推导的。这个刻意的不对称在此钉住 —— 别把它改成报错。
func TestAccountSwitchKeepsWorkingWhenVaultWriteFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("以 root 运行时目录权限不生效，构造不出「读得到、写不进」")
	}
	home := writeAccountCreds(t, accountCredsFixture)
	dataDir := t.TempDir()
	t.Setenv("HARNESS_DATA_DIR", dataDir)
	m := newAccountTestMux(t, home)
	prevCfg := GetConfig()
	initConfig(&AppConfig{ProxyPort: 3079})
	t.Cleanup(func() { initConfig(&prevCfg) })
	prevState := accountDshStateFn
	accountDshStateFn = func(*AdminMux) (string, error) { return "credential-stored", nil }
	t.Cleanup(func() { accountDshStateFn = prevState })
	prevVerify := accountVerifyStoreFn
	accountVerifyStoreFn = func(*AdminMux) (string, error) { return "credential-stored", nil }
	t.Cleanup(func() { accountVerifyStoreFn = prevVerify })
	prevTimeout, prevInterval := accountVerifyPollTimeout, accountVerifyPollInterval
	accountVerifyPollTimeout, accountVerifyPollInterval = 20*time.Millisecond, time.Millisecond
	t.Cleanup(func() { accountVerifyPollTimeout, accountVerifyPollInterval = prevTimeout, prevInterval })

	if err := accountVaultMutate(accountVaultDiskPath(), m.crypt, func(v *accountVault) error {
		v.upsert(accountVaultEntry{ID: "acc-1", Contact: "139******59", Token: "tok-1"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dataDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dataDir, 0o700) })

	rec := httptest.NewRecorder()
	m.handleAccountSwitch(rec, httptest.NewRequest(http.MethodPost, "/api/account/switch",
		strings.NewReader(`{"id":"acc-1"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("库写失败不该让切换失败，实得 %d: %s", rec.Code, rec.Body.String())
	}
	if tok := m.accountActiveToken(); tok != "tok-1" {
		t.Fatalf("切换后凭据里的 token = %q，want tok-1", tok)
	}
	var st accountStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("响应不是 status 快照: %v\n%s", err, rec.Body.String())
	}
	if len(st.Accounts) != 1 || !st.Accounts[0].Active {
		t.Fatalf("切换后的快照应标出 acc-1 使用中: %+v", st.Accounts)
	}
}

// 平台请求的 client 必须复用（核验轮询 5 秒内最多 20 次请求、每次状态查询 2 次），
// 只有代理配置变化时才重建（并收掉旧连接池）。
func TestAccountHTTPClientReused(t *testing.T) {
	prevCfg := GetConfig()
	t.Cleanup(func() { initConfig(&prevCfg) })
	initConfig(&AppConfig{ProxyPort: 3079})

	first := accountHTTPClient()
	if again := accountHTTPClient(); again != first {
		t.Fatal("同一配置下应复用同一个 client")
	}

	proxied := GetConfig()
	proxied.ProxyEnabled = true
	proxied.ProxyAddr = "http://127.0.0.1:7890"
	initConfig(&proxied)
	withProxy := accountHTTPClient()
	if withProxy == first {
		t.Fatal("代理配置变化后应重建 client")
	}
	if tr, ok := withProxy.Transport.(*http.Transport); !ok || tr.Proxy == nil {
		t.Fatal("开了代理的 client 应装上 Proxy")
	}

	initConfig(&AppConfig{ProxyPort: 3079})
	off := accountHTTPClient()
	if off == withProxy {
		t.Fatal("代理关掉后应回到无代理的 client")
	}
	if tr, ok := off.Transport.(*http.Transport); !ok || tr.Proxy != nil {
		t.Fatal("关闭代理的 client 不该带 Proxy")
	}
}
