package main

// account.go —— 「DeepSeek 官方账号」在控制台侧完成授权。
//
// 为什么放在控制台：dsh 自带的账号登录要 OAuth 回调落在 loopback（**平台侧的硬规则**：
// auth_init 只接受 http:// + {localhost,127.0.0.1,[::1]} 的 redirect_uri，路径任意，其它
// 形态一律 50005 PAYLOAD_SCHEMA_MISMATCH —— 公网 https/http、局域网 IP、甚至
// https://127.0.0.1:… 都被拒；平台只有 auth_init/auth_exchange/auth_cancel 三个端点，
// 没有设备码轮询）。本部署的浏览器经平台网关访问，来源必然是公网地址，那条路走不通。
//
// 但整条链路里只有两件事必须在 dsh 这一侧发生：拿授权地址（auth_init）、换 token
// （auth_exchange）；「在浏览器里点授权」谁做都一样。于是控制台自己实现：
//
//	① POST auth_init（自持 PKCE verifier/challenge 与 state）→ 浏览器打开返回的授权地址；
//	② 用户授权后浏览器被重定向到 <loopback>/…/_account/callback?code=&state=：
//	   · 来源本身是 loopback（同机 / SSH 本地转发 / 飞牛客户端把 NAS 网页代理到
//	     127.0.0.1:<port>）时，回调直接落到控制台（本文件同时挂 admin 与反代两条线）；
//	   · 否则把地址栏那条（打不开的）地址粘回控制台，由控制台解析 code+state；
//	③ POST auth_exchange 换 token（校 state、单次有效、10 分钟过期，与平台 expires_in 一致）；
//	④ 按 dsh 的格式把凭据写进 <dsh home>/.dsh/.credentials.yaml：
//	     deepseek-account-platform/default → grant{version,token,issuer}
//	     deepseek-account-platform/device  → grant{id}（auth_exchange 需要，缺则生成）
//	⑤ 用 dsh 自己的 RPC（account/getState、getProfile、getBalance）核验与展示状态。
//
// 真机上已逐环验证：dsh 的 credentials-local 用 chokidar 监听该文件，外部写入**立刻生效**
// （getState 从 signed-out 变 credential-stored，不需要重启 dsh），且模型侧每次请求现读
// 凭据（dsh-llm-deepseek-account 的 resolveToken → readRecord），登录后即可用。
//
// 这一版刻意**不改 dsh 的产物**：dsh 自己的账号登录按钮在该部署下仍然不可用（平台规则
// 使然），但登录/退出都由控制台完成，凭据写的是 dsh 认的那份。

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// accountPlatformOrigin 是官方账号平台的来源（与 dsh 的 platformOrigin 默认值一致）。
// 用变量而不是常量：单测把它指向假平台（见 account_test.go）。
var accountPlatformOrigin = "https://platform.deepseek.com"

// accountDshStateFn 是「读一次 dsh 账号状态」的出口（状态快照用，不轮询）；单测换成假实现。
var accountDshStateFn = func(m *AdminMux) (string, error) { return m.accountDshState() }

// accountVerifyStoreFn 是「写完凭据后向 dsh 核验」的出口：默认问 dsh 自己
// （account/getState）；单测替换成假实现（与 npmInstallFn / dshStopFn 同一套路）。
var accountVerifyStoreFn = func(m *AdminMux) (string, error) { return m.accountDshState() }

const (
	// accountCallbackPath 是控制台自留的回调路径。**必须是 /oauth/callback**：平台只在
	// auth_init 那一步宽松地接受任意路径，真正授权（用户点「登录」）时会拒掉别的路径，
	// 页面显示「授权失败」（实测对照：http://127.0.0.1:3080/oauth/callback 成功，
	// http://127.0.0.1:3080/app/Harness/dsh/_account/callback 授权失败；请求头无关）。
	// 这也是 dsh 自己「固定回调路径」的由来。
	//
	// 注意：反代线上自留这个路径等于**遮蔽 dsh 自己的账号回调路由**（本部署里 dsh 自己的
	// 那条路走不通，见文件头），后续若要恢复 dsh 的原生流程，得先撤掉这处自留。
	accountCallbackPath = "/oauth/callback"
	// accountAttemptTTL 是「等浏览器授权」的时限：与平台返回的 expires_in（600s）一致。
	accountAttemptTTL = 10 * time.Minute
	// accountHTTPTimeout 单次平台请求超时。
	accountHTTPTimeout = 30 * time.Second
)

// accountVerifyPollTimeout / interval 是写盘后向 dsh 轮询核验的窗口：dsh 的
// credentials-local 用 chokidar 监听凭据文件（去抖 ~100ms），写完**立刻**问会读到旧值 →
// 必须轮询。用变量而不是常量：单测把窗口缩短。
var (
	// 实测：写盘后 dsh 在 1~2 秒内认账（chokidar 去抖 + 文档解析），5 秒留足余量。
	accountVerifyPollTimeout  = 5 * time.Second
	accountVerifyPollInterval = 250 * time.Millisecond
)

// --- 授权尝试（内存态，单条，与 dsh 的 startSignIn 同语义） ---

type accountAttemptPhase string

const (
	accountPhaseIdle      accountAttemptPhase = "idle"
	accountPhaseWaiting   accountAttemptPhase = "waiting"
	accountPhaseSucceeded accountAttemptPhase = "succeeded"
	accountPhaseFailed    accountAttemptPhase = "failed"
	accountPhaseCancelled accountAttemptPhase = "cancelled"
	accountPhaseExpired   accountAttemptPhase = "expired"
)

// accountAttempt 是一次「等浏览器授权」的过程。
type accountAttempt struct {
	verifier    string
	state       string
	redirectURI string
	authorizeID string
	authorize   string
	expiresAt   time.Time
	phase       accountAttemptPhase
	msg         accountMessage
	contact     string
}

// accountLoginState 是给前端的登录过程快照。
type accountLoginState struct {
	Phase        accountAttemptPhase `json:"phase"`
	AuthorizeURL string              `json:"authorizeUrl,omitempty"`
	RedirectURI  string              `json:"redirectUri,omitempty"`
	ExpiresAt    int64               `json:"expiresAt,omitempty"`
	Contact      string              `json:"contact,omitempty"`
	Message      string              `json:"message,omitempty"`
	MessageRef   *uiMsg              `json:"messageRef,omitempty"`
}

// accountMessage 是「文案 + code」的一对，前端用 uiText(text, ref) 渲染。
type accountMessage struct {
	Text string `json:"text,omitempty"`
	Ref  *uiMsg `json:"ref,omitempty"`
}

// accountStatus 是账号总览（控制台上要显示的东西全部来自 dsh 自己）。
type accountStatus struct {
	Available bool            `json:"available"`
	SignedIn  bool            `json:"signedIn"`
	DshStatus string          `json:"dshStatus,omitempty"`
	Contact   string          `json:"contact,omitempty"`
	Name      string          `json:"name,omitempty"`
	AvatarURL string          `json:"avatarUrl,omitempty"`
	Balance   []accountWallet `json:"balance,omitempty"`
	Bonus     []accountWallet `json:"bonus,omitempty"`
	// Accounts 是控制台账号库里的账号（多账号：切换 = 把它那份 token 重新写进 dsh 的
	// 凭据记录，见 accountvault.go）。Active 标出正在生效的那条。
	Accounts   []accountVaultView `json:"accounts,omitempty"`
	Message    string             `json:"message,omitempty"`
	MessageRef *uiMsg             `json:"messageRef,omitempty"`
}

type accountWallet struct {
	Currency string `json:"currency"`
	Balance  string `json:"balance"`
}

// accountAttempts 是唯一的在途尝试（同时只允许一次登录，与 dsh 的语义一致）。
var accountAttempts struct {
	sync.Mutex
	cur *accountAttempt
}

// --- 平台调用 ---

// accountHTTPClient 返回平台请求用的客户端。
//
// 出网策略与 dsh 对齐：走设置页的「代理dsh」（换来的 token 就是给 dsh 用的，dsh 自己也是
// 经那套环境变量出网）；关闭时直连（探针实测本机可直连 platform.deepseek.com）。
func accountHTTPClient() *http.Client {
	tr := &http.Transport{MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second}
	if cfg := GetConfig(); cfg.ProxyEnabled && strings.TrimSpace(cfg.ProxyAddr) != "" {
		if u, err := url.Parse(strings.TrimSpace(cfg.ProxyAddr)); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	return &http.Client{Timeout: accountHTTPTimeout, Transport: tr}
}

// accountPlatformPost 调一个平台端点，并按 dsh 的 envelope 约定取 biz_data。
//
// 平台的成功报文是 {code:0, data:{biz_code:0, biz_data:{…}}}；其余形态（含
// 50005 PAYLOAD_SCHEMA_MISMATCH 这类业务码）都算失败 —— 错误里带上平台的 code/msg，
// 便于对日志排查（英文；面向用户的文案走 uiMsg code）。
func accountPlatformPost(path string, payload interface{}, clientMeta accountClientMeta) (map[string]interface{}, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, accountPlatformOrigin+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	// 平台的 WAF 与业务层都要这几个头：缺了会被拦成 "Request Blocked" 页面。
	req.Header.Set("x-client-bundle-id", "")
	req.Header.Set("x-client-platform", "web")
	req.Header.Set("x-client-version", clientMeta.Version)
	req.Header.Set("x-client-locale", clientMeta.WireLocale)
	req.Header.Set("x-client-timezone-offset", strconv.Itoa(clientMeta.TimezoneOffsetSeconds))
	req.Header.Set("origin", accountPlatformOrigin)
	req.Header.Set("referer", accountPlatformOrigin+"/dsh/authorize")
	req.Header.Set("accept", "*/*")
	req.Header.Set("user-agent", "Mozilla/5.0 (compatible; harness-console)")

	resp, err := accountHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return nil, err
	}
	var env struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data *struct {
			BizCode int                    `json:"biz_code"`
			BizMsg  string                 `json:"biz_msg"`
			BizData map[string]interface{} `json:"biz_data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("platform %s: HTTP %d, unparsable body", path, resp.StatusCode)
	}
	if env.Code != 0 || env.Data == nil {
		return nil, &platformError{path: path, status: resp.StatusCode, code: env.Code, msg: env.Msg}
	}
	if env.Data.BizCode != 0 {
		return nil, &platformError{path: path, status: resp.StatusCode, code: env.Data.BizCode, msg: env.Data.BizMsg}
	}
	return env.Data.BizData, nil
}

// platformError 是平台侧的业务/协议拒绝（用于日志与界面上的 code 参数）。
type platformError struct {
	path   string
	status int
	code   int
	msg    string
}

func (e *platformError) Error() string {
	return fmt.Sprintf("platform %s rejected: HTTP %d, code %d %s", e.path, e.status, e.code, e.msg)
}

// accountClientMeta 是发给平台的「发起方界面」身份（与 dsh 客户端送的字段同义）。
type accountClientMeta struct {
	Version               string
	WireLocale            string
	TimezoneOffsetSeconds int
}

// accountWireLocale 把界面语言折成平台认的 wire locale（zh_CN / en_US）。
func accountWireLocale(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return "zh_CN"
	}
	return "en_US"
}

// --- 授权过程 ---

// accountLoginStart 发起一次登录：生成 PKCE、调 auth_init、记住本次尝试。
func (m *AdminMux) accountLoginStart(callbackOrigin string, meta accountClientMeta) (*accountLoginState, error) {
	verifier, err := accountRandomB64(32)
	if err != nil {
		return nil, err
	}
	state, err := accountRandomB64(32)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	redirectURI := m.accountRedirectURI(callbackOrigin)
	data, err := accountPlatformPost("/auth-api/v0/dsh/auth_init", map[string]interface{}{
		"code_challenge":        challenge,
		"code_challenge_method": "S256",
		"state":                 state,
		"redirect_uri":          redirectURI,
		"locale":                meta.WireLocale,
		"login_source":          "web",
	}, meta)
	if err != nil {
		return nil, err
	}
	authorize, _ := data["authorize_url"].(string)
	authorizeID, _ := data["authorize_id"].(string)
	if authorize == "" || authorizeID == "" {
		return nil, fmt.Errorf("platform auth_init returned an incomplete payload")
	}
	expiresAt := time.Now().Add(accountAttemptTTL)
	if secs, ok := data["expires_in"].(float64); ok && secs > 0 && time.Duration(secs)*time.Second < accountAttemptTTL {
		expiresAt = time.Now().Add(time.Duration(secs) * time.Second)
	}

	att := &accountAttempt{
		verifier:    verifier,
		state:       state,
		redirectURI: redirectURI,
		authorizeID: authorizeID,
		authorize:   authorize,
		expiresAt:   expiresAt,
		phase:       accountPhaseWaiting,
	}
	accountAttempts.Lock()
	prev := accountAttempts.cur
	accountAttempts.cur = att
	accountAttempts.Unlock()
	// 上一次尝试还没结束就换新的话，顺手取消它（平台侧也要收回）。
	if prev != nil && prev.authorizeID != "" && prev.authorizeID != authorizeID &&
		(prev.phase == accountPhaseWaiting || prev.phase == accountPhaseIdle) {
		go accountCancelRequest(prev.authorizeID, prev.verifier, meta)
	}
	logInfo("[account] login started redirect_uri=%s", redirectURI)
	return att.stateView(), nil
}

// accountLoginState 返回当前登录过程（含过期判定）。
func (m *AdminMux) accountLoginStateSnapshot() *accountLoginState {
	accountAttempts.Lock()
	defer accountAttempts.Unlock()
	att := accountAttempts.cur
	if att == nil {
		return &accountLoginState{Phase: accountPhaseIdle}
	}
	if att.phase == accountPhaseWaiting && time.Now().After(att.expiresAt) {
		att.phase = accountPhaseExpired
		setMsgFields(&att.msg.Text, &att.msg.Ref, "err_account_login_expired", "授权超时（10 分钟），请重新发起登录")
	}
	return att.stateView()
}

func (a *accountAttempt) stateView() *accountLoginState {
	st := &accountLoginState{
		Phase:       a.phase,
		RedirectURI: a.redirectURI,
		ExpiresAt:   a.expiresAt.UnixMilli(),
		Contact:     a.contact,
		Message:     a.msg.Text,
		MessageRef:  a.msg.Ref,
	}
	if a.phase == accountPhaseWaiting {
		st.AuthorizeURL = a.authorize
	}
	return st
}

// accountCancelLogin 取消在途尝试（用户点「取消」）。
func (m *AdminMux) accountCancelLogin() *accountLoginState {
	accountAttempts.Lock()
	att := accountAttempts.cur
	if att != nil && att.phase == accountPhaseWaiting {
		att.phase = accountPhaseCancelled
		setMsgFields(&att.msg.Text, &att.msg.Ref, "msg_account_login_cancelled", "已取消登录")
	}
	accountAttempts.Unlock()
	if att != nil && att.authorizeID != "" {
		go accountCancelRequest(att.authorizeID, att.verifier, accountClientMeta{Version: harnessVersion})
	}
	return m.accountLoginStateSnapshot()
}

// accountCancelRequest 尽力通知平台收回这次授权请求（失败无所谓，本地状态才是权威）。
func accountCancelRequest(authorizeID, verifier string, meta accountClientMeta) {
	_, err := accountPlatformPost("/auth-api/v0/dsh/auth_cancel", map[string]interface{}{
		"authorize_id":  authorizeID,
		"code_verifier": verifier,
	}, meta)
	if err != nil {
		logWarn("[account] auth_cancel failed: %v", err)
	}
}

// accountLoginComplete 用回调里的 code+state 完成登录：换 token → 写 dsh 凭据 → 核验。
//
// callback 可以是整条回调地址（用户从地址栏粘回来的那条），也可以只有 code 与 state。
func (m *AdminMux) accountLoginComplete(callback, code, state string, meta accountClientMeta) (*accountLoginState, error) {
	code = strings.TrimSpace(code)
	state = strings.TrimSpace(state)
	if code == "" && callback != "" {
		code, state = parseAccountCallback(callback)
	}
	if code == "" || state == "" {
		return nil, uiErr("err_account_callback_invalid", "没能在回调地址里找到 code 与 state，请粘贴完整地址")
	}

	accountAttempts.Lock()
	att := accountAttempts.cur
	if att == nil || att.phase != accountPhaseWaiting {
		accountAttempts.Unlock()
		return nil, uiErr("err_account_login_not_pending", "当前没有等待授权的登录（可能已过期或已完成），请重新点「登录」")
	}
	if time.Now().After(att.expiresAt) {
		att.phase = accountPhaseExpired
		setMsgFields(&att.msg.Text, &att.msg.Ref, "err_account_login_expired", "授权超时（10 分钟），请重新发起登录")
		view := att.stateView()
		accountAttempts.Unlock()
		return view, uiErr("err_account_login_expired", "授权超时（10 分钟），请重新发起登录")
	}
	if subtle.ConstantTimeCompare([]byte(state), []byte(att.state)) != 1 {
		accountAttempts.Unlock()
		return nil, uiErr("err_account_callback_invalid", "回调地址与本次登录不匹配（state 校验失败），请重新发起登录")
	}
	accountAttempts.Unlock()

	// 设备标识：auth_exchange 要用，缺就生成一条（与 dsh 自己的记录同格式）。
	deviceID, err := m.accountEnsureDeviceID()
	if err != nil {
		logWarn("[account] device record write failed: %v", err)
		m.accountFail(att, "err_account_store_failed", "写入账号凭据失败：%s", "detail", err.Error())
		return nil, uiErr("err_account_store_failed", "写入账号凭据失败：%s", "detail", err.Error())
	}

	data, err := accountPlatformPost("/auth-api/v0/dsh/auth_exchange", map[string]interface{}{
		"code":          code,
		"code_verifier": att.verifier,
		"redirect_uri":  att.redirectURI,
		"device_id":     deviceID,
		"device_model":  accountDeviceModel(),
		"os_version":    accountOSVersion(),
	}, meta)
	if err != nil {
		m.accountFail(att, "err_account_exchange_failed", "换取账号凭据失败（平台拒绝：%s）", "detail", accountPlatformDetail(err))
		return nil, uiErr("err_account_exchange_failed", "换取账号凭据失败（平台拒绝：%s）", "detail", accountPlatformDetail(err))
	}
	token, _ := data["token"].(string)
	if token == "" {
		m.accountFail(att, "err_account_exchange_failed", "换取账号凭据失败（平台未返回 token）")
		return nil, uiErr("err_account_exchange_failed", "换取账号凭据失败（平台未返回 token）")
	}

	if err := m.accountStoreGrant(token); err != nil {
		logWarn("[account] credential write failed: %v", err)
		m.accountFail(att, "err_account_store_failed", "写入账号凭据失败：%s", "detail", err.Error())
		return nil, uiErr("err_account_store_failed", "写入账号凭据失败：%s", "detail", err.Error())
	}
	// 核验：dsh 认了才算成功。拿不到 dsh 状态（未就绪）不算失败 —— 凭据已落盘，
	// 下次启动/刷新就会读到；只有 dsh **在窗口内持续**回答「没这笔凭据」才回滚。
	status, verified := m.accountVerifyStored()
	switch {
	case verified && status == "signed-out":
		logWarn("[account] dsh did not accept the stored credential within %s, rolling back", accountVerifyPollTimeout)
		if rbErr := m.accountRemoveGrant(); rbErr != nil {
			logWarn("[account] rollback after failed verification: %v", rbErr)
		}
		m.accountFail(att, "err_account_store_failed", "凭据写入后 dsh 未识别（已回滚），请重试登录")
		return nil, uiErr("err_account_store_failed", "凭据写入后 dsh 未识别（已回滚），请重试登录")
	case !verified:
		// dsh 没就绪/没答上来：凭据已经落盘，它下次读到就会用，不算失败。
		logInfo("[account] credential stored but dsh state could not be verified (dsh not ready?)")
	}

	userID, contact, name, avatar := accountUserFields(data["user"])
	now := time.Now().Format(time.RFC3339)
	if vault, vaultErr := loadAccountVault(); vaultErr != nil {
		logWarn("[account] account vault unreadable: %v", vaultErr)
	} else {
		vault.upsert(accountVaultEntry{
			ID: accountVaultID(userID, token), Contact: contact, Name: name, AvatarURL: avatar,
			Token: token, Issuer: accountPlatformOrigin, AddedAt: now, LastUsedAt: now,
		})
		if err := saveAccountVault(vault); err != nil {
			logWarn("[account] account vault save failed: %v", err)
		} else {
			logInfo("[account] account saved to the vault (%s)", accountVaultDebug(vault))
		}
	}
	accountAttempts.Lock()
	att.contact = contact
	att.phase = accountPhaseSucceeded
	setMsgFields(&att.msg.Text, &att.msg.Ref, "msg_account_login_done", "已登录 DeepSeek 账号 %s", "contact", contact)
	view := att.stateView()
	accountAttempts.Unlock()
	logInfo("[account] login succeeded (contact=%s, dsh state=%s)", contact, firstNonEmpty(status, "unverified"))
	return view, nil
}

// accountFail 把尝试标记为失败并留下 code 文案。
func (m *AdminMux) accountFail(att *accountAttempt, code, zh string, kv ...string) {
	accountAttempts.Lock()
	defer accountAttempts.Unlock()
	if att == nil {
		return
	}
	att.phase = accountPhaseFailed
	setMsgFields(&att.msg.Text, &att.msg.Ref, code, zh, kv...)
}

// accountPlatformDetail 提炼平台错误的可读细节（给界面上的 %s 参数）。
func accountPlatformDetail(err error) string {
	if pe, ok := err.(*platformError); ok {
		if pe.msg != "" {
			return fmt.Sprintf("%d %s", pe.code, pe.msg)
		}
		return strconv.Itoa(pe.code)
	}
	return err.Error()
}

// accountContactOf 从 auth_exchange 的 user 字段里取一个可显示的账号标识。
func accountContactOf(user interface{}) string {
	_, contact, _, _ := accountUserFields(user)
	return contact
}

// parseAccountCallback 从用户粘贴的（或浏览器地址栏里的）回调地址里取 code 与 state。
//
// 用户拿到的往往是平台重定向后的完整 URL（形如
// http://127.0.0.1:3080/app/Harness/dsh/_account/callback?code=…&state=…），也可能是
// 只复制了 query 片段，甚至带了多余空白/引号 —— 都尽量认。
func parseAccountCallback(raw string) (code, state string) {
	s := strings.TrimSpace(raw)
	s = strings.Trim(s, "\"'<>")
	if s == "" {
		return "", ""
	}
	query := s
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		query = s[i+1:]
	}
	if q, err := url.ParseQuery(strings.TrimPrefix(query, "?")); err == nil {
		return q.Get("code"), q.Get("state")
	}
	return "", ""
}

// --- 回调入口（admin 线与反代线共用） ---

// handleAccountCallback 是浏览器/隧道把授权码送回控制台的那条路。
func (m *AdminMux) handleAccountCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	meta := accountClientMeta{Version: harnessVersion, WireLocale: "zh_CN"}
	_, err := m.accountLoginComplete("", code, state, meta)
	st := m.accountLoginStateSnapshot()
	serveAccountCallbackPage(w, err == nil, st)
}

// serveAccountCallbackPage 渲染回调结果页：只有结论，没有可点的东西（用户关掉即可）。
func serveAccountCallbackPage(w http.ResponseWriter, ok bool, st *accountLoginState) {
	zhBody, enBody := "登录成功，可以关闭这个页面了。", "Signed in. You can close this page."
	if !ok {
		zhBody, enBody = "登录没有完成。请回到控制台的「DeepSeek 账号」里重新发起，或粘贴回调地址。",
			"Sign-in did not complete. Start it again from the console (DeepSeek account), or paste the callback URL there."
	}
	if st != nil && st.Contact != "" && ok {
		zhBody = fmt.Sprintf("已登录 DeepSeek 账号 %s，可以关闭这个页面了。", st.Contact)
		enBody = fmt.Sprintf("Signed in as %s. You can close this page.", st.Contact)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	io.WriteString(w, `<!DOCTYPE html><html><head><meta charset="utf-8">`+
		`<meta name="viewport" content="width=device-width,initial-scale=1">`+
		`<title>DeepSeek Harness</title><style>`+
		`body{margin:0;height:100dvh;display:flex;align-items:center;justify-content:center;`+
		`background:#0b0b0f;color:#e8e8ea;font:15px/1.7 system-ui,-apple-system,"PingFang SC",sans-serif}`+
		`main{max-width:34rem;padding:2rem;text-align:center}`+
		`p{margin:.4rem 0;color:#a6a6ad}</style></head><body><main>`+
		`<p>`+htmlEscape(zhBody)+`</p><p>`+htmlEscape(enBody)+`</p>`+
		`</main></body></html>`)
}

// --- 回调地址的挑选 ---

// accountRedirectURI 按平台规则挑一个它认的 redirect_uri（只认 http + loopback）。
//
// 路径固定为 /oauth/callback（平台在授权那一步只认它，见 accountCallbackPath）。三种写法
// 都由 accountCallbackMountPaths 自留：
//
//   - loopback 来源（同机 / 飞牛客户端把 NAS 网页代理到 127.0.0.1:<port>）→ 用它的来源 +
//     控制台 baseurl（本地代理念理通常按 NAS 根映射，必须带 baseurl 才落得到控制台）；
//   - 否则（公网/局域网来源）→ 用反代端口的 loopback 地址作占位（最短、且 `ssh -L 反代端口`
//     时能自动完成）；授权后浏览器停在 127.0.0.1 那条打不开的地址上，地址栏里的 code/state
//     就是完成这次登录所需的一切 —— 粘回控制台，或把 host 换成设备后打开（两种写法都自留）。
func (m *AdminMux) accountRedirectURI(callbackOrigin string) string {
	if origin, ok := loopbackOriginOf(callbackOrigin); ok {
		adminBase := ""
		if m.renv != nil {
			adminBase = strings.TrimRight(m.renv.AdminBaseURL, "/")
		}
		return origin + adminBase + accountCallbackPath
	}
	return fmt.Sprintf("http://127.0.0.1:%d%s", GetConfig().ProxyPort, accountCallbackPath)
}

// loopbackOriginOf 判断并归一化一个 loopback 来源（http(s)://host[:port]，无路径）：
// 平台只认 localhost / 127.0.0.0/8 / ::1 这类主机名。
func loopbackOriginOf(origin string) (string, bool) {
	s := strings.TrimSpace(origin)
	if s == "" {
		return "", false
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		u.User != nil || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "::1" {
		return u.Scheme + "://" + u.Host, true
	}
	if ip := strings.Split(host, "."); len(ip) == 4 && ip[0] == "127" {
		return u.Scheme + "://" + u.Host, true
	}
	return "", false
}

// proxyMountPrefixFor 返回反代的挂载前缀（网关那条 Unix Socket 线开启时非空）。
// 与 startProxySocket 的判定保持一致。
func proxyMountPrefixFor(renv *RuntimeEnv) string {
	if renv == nil {
		return ""
	}
	if s := strings.TrimSpace(renv.ProxySock); s == "" || s == "off" {
		return ""
	}
	return newProxyMount(renv.ProxyBaseURL).prefix
}

// accountCallbackMountPaths 返回反代线上要自留的回调路径。
//
// 反代有两条监听、两种挂载，浏览器可能用三种写法打到同一条线上：
//   - 根挂载（端口线 / 本地转发）：<origin>/oauth/callback；
//   - 网关线：网关把 <dsh 挂载前缀>/… 整段转过来（stripMount 会剥掉），落回 /oauth/callback；
//   - 本地转发指向反代端口、但地址里带了控制台 baseurl（飞牛客户端那种按 NAS 根映射的
//     代理会这么拼）：<AdminBaseURL>/oauth/callback，根挂载线不剥前缀，得原样认。
func accountCallbackMountPaths(renv *RuntimeEnv) []string {
	paths := []string{accountCallbackPath}
	add := func(p string) {
		for _, existing := range paths {
			if existing == p {
				return
			}
		}
		paths = append(paths, p)
	}
	if prefix := proxyMountPrefixFor(renv); prefix != "" {
		add(prefix + accountCallbackPath)
	}
	if renv != nil {
		if admin := strings.TrimRight(renv.AdminBaseURL, "/"); admin != "" {
			add(admin + accountCallbackPath)
		}
	}
	return paths
}

// accountRandomB64 生成 PKCE/state 用的 URL-safe 随机串（无填充）。
func accountRandomB64(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// --- 凭据文件（dsh 的 credentials-local 格式） ---

// accountCredentialsPath 返回 dsh 的凭据文件（$DSH_HOME/.credentials.yaml）。
func (m *AdminMux) accountCredentialsPath() (string, error) {
	home := ""
	if m.renv != nil {
		home = m.renv.Home
	}
	if strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("dsh home is unknown")
	}
	return filepath.Join(home, ".dsh", ".credentials.yaml"), nil
}

const (
	accountGrantKey  = "deepseek-account-platform/default"
	accountDeviceKey = "deepseek-account-platform/device"
)

// accountRecordLineRe 匹配「二级键」开头的那一行（records 下的记录名）。
func accountRecordLineRe(key string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(key) + `:`)
}

// accountRemoveRecordText 从凭据文档里删掉一条记录（块式与 flow 式都能删）。
func accountRemoveRecordText(text, key string) string {
	lines := strings.SplitAfter(text, "\n")
	out := make([]string, 0, len(lines))
	re := accountRecordLineRe(key)
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if re.MatchString(strings.TrimSuffix(line, "\n")) {
			i++
			// 块式记录：吃掉所有比它更深的行（4 空格及以上）以及尾随空行。
			for i < len(lines) && (strings.TrimSpace(lines[i]) == "" || strings.HasPrefix(lines[i], "    ")) {
				i++
			}
			i--
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "")
}

// accountInsertRecords 在 records: 下插入若干条 flow 风格记录（幂等由调用方保证）。
//
// 刻意用 flow 风格（`{kind: grant, payload: {…}}`）：插入只需改一行，不动文档其它任何
// 字节 —— 这个文件里还有用户自己的其它凭据，逐字节保真比排版好看重要得多。dsh 的
// credentials-local 用 yaml 的 Document API 解析，flow/block 两种风格等价（真机已验证：
// 写入后 getState 立刻变成 credential-stored）。
func accountInsertRecords(text string, records map[string]string) string {
	if !strings.HasSuffix(text, "\n") && text != "" {
		text += "\n"
	}
	keys := make([]string, 0, len(records))
	for k := range records {
		keys = append(keys, k)
	}
	sortStrings(keys)
	var ins strings.Builder
	for _, k := range keys {
		ins.WriteString("  " + k + ": " + records[k] + "\n")
	}
	if loc := regexp.MustCompile(`(?m)^records:[ \t]*\n`).FindStringIndex(text); loc != nil {
		// 手工拼接而不是 ReplaceAllString：替换文本里可能出现 token 里的 `$`，
		// 那是 Go 替换串的展开语法，会把凭据写坏。
		return text[:loc[1]] + ins.String() + text[loc[1]:]
	}
	if text == "" {
		text = "version: 1\n"
	}
	return text + "records:\n" + ins.String()
}

// sortStrings 是给 map 键排序用的小工具（保持输出稳定，便于测试与 diff）。
func sortStrings(v []string) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j] < v[j-1]; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}

// accountQuoteYAML 把字符串包成 YAML 双引号标量（token 是 \x21-\x7e 的可打印 ASCII，
// 但仍按最保守的方式转义）。
func accountQuoteYAML(v string) string {
	r := strings.NewReplacer("\\", "\\\\", `"`, `\"`)
	return `"` + r.Replace(v) + `"`
}

// accountStoreGrant 把账号凭据写进 dsh 的凭据文件（原子替换 + 0600）。
//
// 步骤：读原文 → 删掉同名旧记录（块式/flow 式都能删）→ 在 records: 下插入新记录 →
// 写临时文件 → rename。原文档里除这两条记录之外的字节保持不变。
func (m *AdminMux) accountStoreGrant(token string) error {
	path, err := m.accountCredentialsPath()
	if err != nil {
		return err
	}
	text, err := accountReadFileText(path)
	if err != nil {
		return err
	}
	text = accountRemoveRecordText(text, accountGrantKey)
	text = accountInsertRecords(text, map[string]string{
		accountGrantKey: "{kind: grant, payload: {version: 1, token: " + accountQuoteYAML(token) +
			", issuer: " + accountQuoteYAML(accountPlatformOrigin) + "}}",
	})
	return accountWriteFileText(path, text)
}

// accountEnsureDeviceID 读取（或创建）设备标识记录，返回其 uuid。
func (m *AdminMux) accountEnsureDeviceID() (string, error) {
	path, err := m.accountCredentialsPath()
	if err != nil {
		return "", err
	}
	text, err := accountReadFileText(path)
	if err != nil {
		return "", err
	}
	if id := accountParseDeviceID(text); id != "" {
		return id, nil
	}
	id, err := accountNewUUID()
	if err != nil {
		return "", err
	}
	text = accountRemoveRecordText(text, accountDeviceKey)
	text = accountInsertRecords(text, map[string]string{
		accountDeviceKey: "{kind: grant, payload: {id: " + accountQuoteYAML(id) + "}}",
	})
	if err := accountWriteFileText(path, text); err != nil {
		return "", err
	}
	return id, nil
}

// accountDeviceIDRe / accountTokenRe 从文档文本里取值（flow 与块式两种写法都认）。
var (
	accountDeviceIDRe = regexp.MustCompile(`(?m)^  deepseek-account-platform/device:[^\n]*\n(?:    [^\n]*\n)*`)
	accountGrantRe    = regexp.MustCompile(`(?m)^  deepseek-account-platform/default:[^\n]*\n(?:    [^\n]*\n)*`)
	accountUUIDRe     = regexp.MustCompile(`id:\s*"?([0-9a-fA-F-]{36})"?`)
	// token 取两种写法：我们自己写的是双引号标量（见 accountQuoteYAML），dsh 自己写的
	// 是裸标量（块式）。引号里允许转义，取出后再反转义。
	accountTokenQuotedRe = regexp.MustCompile(`token:[ \t]*"((?:[^"\\]|\\.)*)"`)
	accountTokenPlainRe  = regexp.MustCompile(`(?m)token:[ \t]*([^"\n,]+)`)
)

// accountParseDeviceID 取设备标识记录里的 id。
func accountParseDeviceID(text string) string {
	block := accountDeviceIDRe.FindString(text)
	if block == "" {
		return ""
	}
	if m := accountUUIDRe.FindStringSubmatch(block); m != nil {
		return m[1]
	}
	return ""
}

// accountParseGrantToken 取凭据记录里的 token。
func accountParseGrantToken(text string) string {
	block := accountGrantRe.FindString(text)
	if block == "" {
		return ""
	}
	if m := accountTokenQuotedRe.FindStringSubmatch(block); m != nil {
		return strings.NewReplacer(`\\`, `\`, `\"`, `"`).Replace(m[1])
	}
	if m := accountTokenPlainRe.FindStringSubmatch(block); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// accountRemoveGrant 删除账号凭据记录（退出登录用）。
func (m *AdminMux) accountRemoveGrant() error {
	path, err := m.accountCredentialsPath()
	if err != nil {
		return err
	}
	text, err := accountReadFileText(path)
	if err != nil {
		return err
	}
	next := accountRemoveRecordText(text, accountGrantKey)
	if next == text {
		return nil
	}
	return accountWriteFileText(path, next)
}

// accountReadFileText 读凭据文件；不存在时返回空串（后续插入会补出最小文档骨架）。
func accountReadFileText(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// accountWriteFileText 原子写凭据文件：同目录临时文件 → 0600 → rename。
//
// 权限必须是 0600：credentials-local 会拒绝「属主之外可读」的文档并拒绝启动（见其
// validatePermissions），所以这里显式收紧，绝不沿用旧文件的宽松位。
func accountWriteFileText(path, text string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".harness-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// accountNewUUID 生成一个 v4 uuid（与 dsh 的 device 记录同格式）。
func accountNewUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// accountDeviceModel / accountOSVersion 复刻 dsh 送平台的两个环境字段
// （platform()-arch() 与 platform()+release()），只是用于平台侧记录设备。
func accountDeviceModel() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

func accountOSVersion() string {
	release := ""
	if raw, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		release = strings.TrimSpace(string(raw))
	}
	return strings.TrimSpace(runtime.GOOS + " " + firstNonEmpty(release, "unknown"))
}

// --- 与 dsh 的 RPC 桥 ---

// accountWaitDshState 在核验窗口内轮询 dsh，直到它给出的状态满足 want。
//
// 返回 (最后读到的状态, 是否拿到过明确答案)。dsh 全程问不到（未就绪）时 ok=false ——
// 调用方按「无法核验」处理（凭据本身已经落盘/删掉了，不该因为问不到就报失败）。
func (m *AdminMux) accountWaitDshState(want func(string) bool) (string, bool) {
	deadline := time.Now().Add(accountVerifyPollTimeout)
	status := ""
	for {
		s, err := accountVerifyStoreFn(m)
		if err != nil {
			status = ""
		} else {
			status = s
			if want(s) {
				return s, true
			}
		}
		if !time.Now().Before(deadline) {
			return status, status != ""
		}
		time.Sleep(accountVerifyPollInterval)
	}
}

// accountVerifyStored 在 accountVerifyPollTimeout 内轮询 dsh，直到它认这笔凭据
// （状态不再是 signed-out）。
//
// 必须轮询：dsh 的 credentials-local 用 chokidar 监听凭据文件（debounce 100ms，
// awaitWriteFinish），而 getState 读的是它内存里的文档 —— 写完立刻问会拿到
// 「还没看到」的旧值（`signed-out`），照那个值判会**把一次成功的登录回滚掉**。
//
// 返回 (state, 是否拿到明确答案)：dsh 全程问不到（未就绪）时 ok=false，调用方按
// 「无法核验」处理（不回滚 —— 凭据已经写好了）。
func (m *AdminMux) accountVerifyStored() (string, bool) {
	return m.accountWaitDshState(func(s string) bool { return s != "signed-out" })
}

// accountWaitSignedOut 轮询 dsh 直到它确认「没有凭据了」。
//
// 退出登录必须等这一步：dsh 的 credentials-local 用 chokidar 监听凭据文件，删掉 grant
// 之后它要过一拍才更新内存里的文档 —— 立刻把状态回给前端，界面会继续显示「已登录」，
// 用户只能手动刷新（这正是踩过的坑）。
func (m *AdminMux) accountWaitSignedOut() (string, bool) {
	return m.accountWaitDshState(func(s string) bool { return s == "signed-out" })
}

// accountDshState 问 dsh「认不认这笔凭据」（account/getState → signed-out | credential-stored）。
// dsh 未就绪（端口/凭据缺失）时返回错误，调用方按「无法核验」处理。
func (m *AdminMux) accountDshState() (string, error) {
	var out struct {
		Status string `json:"status"`
	}
	if err := m.accountDshRPC("account/getState", map[string]interface{}{}, &out); err != nil {
		return "", err
	}
	return out.Status, nil
}

// accountDshRPC 以控制台身份调 dsh 的 RPC（带 dsh 会话 Cookie，走它自己的端口）。
func (m *AdminMux) accountDshRPC(method string, args map[string]interface{}, out interface{}) error {
	if m.dsh == nil {
		return fmt.Errorf("dsh manager unavailable")
	}
	cookie := m.dsh.AuthCookie()
	if cookie == "" {
		return fmt.Errorf("dsh session is not ready")
	}
	body, err := json.Marshal(map[string]interface{}{
		"type":    "client-request",
		"rpcId":   fmt.Sprintf("console-%d", time.Now().UnixNano()),
		"method":  method,
		"payload": map[string]interface{}{"args": args},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/api/%s", GetConfig().DshPort, method), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	resp, err := accountHTTPClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	var env struct {
		Result struct {
			OK    bool            `json:"ok"`
			Value json.RawMessage `json:"value"`
			Error *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("dsh rpc %s: HTTP %d, unparsable body", method, resp.StatusCode)
	}
	if !env.Result.OK {
		if env.Result.Error != nil {
			return fmt.Errorf("dsh rpc %s: %s %s", method, env.Result.Error.Code, env.Result.Error.Message)
		}
		return fmt.Errorf("dsh rpc %s: rejected", method)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(env.Result.Value, out)
}

// --- HTTP handlers（admin 线；见 admin.go 的路由表） ---

// accountClientMetaFromRequest 取前端送来的「发起方身份」；缺失时给合理缺省
// （版本用控制台版本、时区用进程所在时区）。
func accountClientMetaFromRequest(version, locale string, tzOffset *int) accountClientMeta {
	meta := accountClientMeta{Version: firstNonEmpty(version, harnessVersion), WireLocale: accountWireLocale(locale)}
	if tzOffset != nil {
		meta.TimezoneOffsetSeconds = *tzOffset
	} else {
		_, offset := time.Now().Zone()
		meta.TimezoneOffsetSeconds = offset
	}
	return meta
}

// handleAccountStatus 返回账号总览（状态 + 资料 + 余额，全部来自 dsh 自己）。
func (m *AdminMux) handleAccountStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, m.accountStatusSnapshot())
}

// accountStatusSnapshot 汇总账号总览：dsh 侧状态 + 当前账号资料/余额 + 账号库列表。
//
// 顺带做一次**自愈导入**：如果 dsh 现在用的是某个 token、而账号库里没有它（老设备上先登录
// 后升级控制台，或凭据是别的途径写进去的），就把它补进库里 —— 否则列表里看不到「正在用的
// 那个账号」，切换过去以后就再也切不回来了。
func (m *AdminMux) accountStatusSnapshot() accountStatus {
	st := accountStatus{}
	activeToken := m.accountActiveToken()
	vault, vaultErr := loadAccountVault()
	if vaultErr != nil {
		logWarn("[account] account vault unreadable: %v", vaultErr)
	}

	state, err := accountDshStateFn(m)
	if err != nil {
		st.Available = false
		setMsgFields(&st.Message, &st.MessageRef, "err_account_dsh_unavailable", "dsh 服务尚未就绪或已停止，无法查询账号状态")
		st.Accounts = accountVaultViews(vault, "")
		return st
	}
	st.Available = true
	st.DshStatus = state
	st.SignedIn = state == "credential-stored"
	if !st.SignedIn {
		st.Accounts = accountVaultViews(vault, "")
		return st
	}

	_, offset := time.Now().Zone()
	client := map[string]interface{}{
		"version": harnessVersion, "locale": "zh-CN", "timezoneOffsetSeconds": offset,
	}
	var prof struct {
		Status string `json:"status"`
		Value  *struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Contact   string `json:"contact"`
			AvatarURL string `json:"avatarUrl"`
		} `json:"value"`
	}
	if err := m.accountDshRPC("account/getProfile", map[string]interface{}{"client": client}, &prof); err == nil && prof.Value != nil {
		st.Name, st.Contact, st.AvatarURL = prof.Value.Name, prof.Value.Contact, prof.Value.AvatarURL
	}
	var bal struct {
		Status string          `json:"status"`
		Value  []accountWallet `json:"value"`
		Bonus  []accountWallet `json:"bonusWallets"`
	}
	if err := m.accountDshRPC("account/getBalance", map[string]interface{}{"client": client}, &bal); err == nil {
		st.Balance, st.Bonus = bal.Value, bal.Bonus
	}

	// 自愈导入：dsh 正在用的 token 不在库里 → 补一条（id 优先取平台 user id）。
	if vaultErr == nil && activeToken != "" && vault.activeID(activeToken) == "" {
		profileID := ""
		if prof.Value != nil {
			profileID = prof.Value.ID
		}
		now := time.Now().Format(time.RFC3339)
		vault.upsert(accountVaultEntry{
			ID: accountVaultID(profileID, activeToken), Contact: st.Contact, Name: st.Name,
			AvatarURL: st.AvatarURL, Token: activeToken, Issuer: accountPlatformOrigin,
			AddedAt: now, LastUsedAt: now,
		})
		if err := saveAccountVault(vault); err != nil {
			logWarn("[account] account vault save failed: %v", err)
		} else {
			logInfo("[account] imported the account in use into the vault (%s)", accountVaultDebug(vault))
		}
	}
	st.Accounts = accountVaultViews(vault, activeToken)
	// 列表里正在生效的那条用实时资料覆盖（库里存的联系方式可能已经变了）。
	for i := range st.Accounts {
		if st.Accounts[i].Active {
			if st.Contact != "" {
				st.Accounts[i].Contact = st.Contact
			}
			if st.Name != "" {
				st.Accounts[i].Name = st.Name
			}
			if st.AvatarURL != "" {
				st.Accounts[i].AvatarURL = st.AvatarURL
			}
		}
	}
	return st
}

// accountActiveToken 读凭据文件里当前生效的 token（读不到返回空串）。
func (m *AdminMux) accountActiveToken() string {
	path, err := m.accountCredentialsPath()
	if err != nil {
		return ""
	}
	text, err := accountReadFileText(path)
	if err != nil {
		return ""
	}
	return accountParseGrantToken(text)
}

// handleAccountLoginStart 发起登录：返回授权地址（前端用预开标签页打开它）。
func (m *AdminMux) handleAccountLoginStart(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		CallbackOrigin        string `json:"callbackOrigin"`
		Version               string `json:"version"`
		Locale                string `json:"locale"`
		TimezoneOffsetSeconds *int   `json:"timezoneOffsetSeconds"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&payload); err != nil && err != io.EOF {
		writeErrU(w, http.StatusBadRequest, "err_bad_request", "请求格式不正确")
		return
	}
	st, err := m.accountLoginStart(payload.CallbackOrigin,
		accountClientMetaFromRequest(payload.Version, payload.Locale, payload.TimezoneOffsetSeconds))
	if err != nil {
		logWarn("[account] auth_init failed: %v", err)
		writeErrU(w, http.StatusBadGateway, "err_account_init_failed", "无法向 DeepSeek 平台发起授权：%s", "detail", accountPlatformDetail(err))
		return
	}
	writeJSON(w, st)
}

// handleAccountLoginStatus 轮询登录过程。
func (m *AdminMux) handleAccountLoginStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, m.accountLoginStateSnapshot())
}

// handleAccountLoginComplete 用粘贴回来的回调地址完成登录。
func (m *AdminMux) handleAccountLoginComplete(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		URL            string `json:"url"`
		Code           string `json:"code"`
		State          string `json:"state"`
		Version        string `json:"version"`
		Locale         string `json:"locale"`
		TimezoneOffset *int   `json:"timezoneOffsetSeconds"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&payload); err != nil && err != io.EOF {
		writeErrU(w, http.StatusBadRequest, "err_bad_request", "请求格式不正确")
		return
	}
	meta := accountClientMetaFromRequest(payload.Version, payload.Locale, payload.TimezoneOffset)
	st, err := m.accountLoginComplete(payload.URL, payload.Code, payload.State, meta)
	if err != nil {
		writeErrFrom(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, st)
}

// handleAccountLoginCancel 取消在途登录。
func (m *AdminMux) handleAccountLoginCancel(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, m.accountCancelLogin())
}

// handleAccountLogout 退出登录：让 dsh 不再使用当前凭据，但**账号仍留在账号库里**
// （可以随时切回，不需要重新授权）。彻底删除一个账号走「移除」（handleAccountRemove，
// 那时才会请平台作废 token）。
func (m *AdminMux) handleAccountLogout(w http.ResponseWriter, r *http.Request) {
	if err := m.accountRemoveGrant(); err != nil {
		logWarn("[account] credential removal failed: %v", err)
		writeErrU(w, http.StatusInternalServerError, "err_account_store_failed", "写入账号凭据失败：%s", "detail", err.Error())
		return
	}
	// 等 dsh 认账（它监听凭据文件，删完要过一拍）：否则紧接着的状态查询还是「已登录」，
	// 界面只能等用户手动刷新。超时不报错（凭据确实删掉了），只记一笔 WARN。
	status, ok := m.accountWaitSignedOut()
	switch {
	case !ok:
		logWarn("[account] signed out locally, but dsh state could not be verified (dsh not ready?)")
	case status != "signed-out":
		logWarn("[account] dsh still reports %q after removing the credential; state may be stale", status)
	}
	logInfo("[account] signed out")
	writeJSON(w, m.accountStatusSnapshot())
}

// handleAccountSwitch 切换当前生效的账号：把该账号的 token 重新写进 dsh 的凭据记录。
//
// 写失败要回滚到切换前的凭据 —— 否则会留下一个 dsh 不认、用户也切不回来的状态。
func (m *AdminMux) handleAccountSwitch(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&payload); err != nil && err != io.EOF {
		writeErrU(w, http.StatusBadRequest, "err_bad_request", "请求格式不正确")
		return
	}
	vault, err := loadAccountVault()
	if err != nil {
		logWarn("[account] account vault unreadable: %v", err)
		writeErrFrom(w, accountVaultError(err), http.StatusInternalServerError)
		return
	}
	entry := vault.find(strings.TrimSpace(payload.ID))
	if entry == nil || entry.Token == "" {
		writeErrU(w, http.StatusNotFound, "err_account_not_found", "账号库里没有这个账号")
		return
	}
	if entry.Token == m.accountActiveToken() {
		writeJSON(w, m.accountStatusSnapshot()) // 已经在用了：不写盘，直接回最新状态
		return
	}
	prevToken := m.accountActiveToken()
	if _, err := m.accountEnsureDeviceID(); err != nil {
		logWarn("[account] device record write failed: %v", err)
		writeErrU(w, http.StatusInternalServerError, "err_account_store_failed", "写入账号凭据失败：%s", "detail", err.Error())
		return
	}
	if err := m.accountStoreGrant(entry.Token); err != nil {
		logWarn("[account] credential write failed: %v", err)
		writeErrU(w, http.StatusInternalServerError, "err_account_store_failed", "写入账号凭据失败：%s", "detail", err.Error())
		return
	}
	status, verified := m.accountVerifyStored()
	if verified && status == "signed-out" {
		if rbErr := m.accountRestoreGrant(prevToken); rbErr != nil {
			logWarn("[account] rollback after failed switch: %v", rbErr)
		}
		logWarn("[account] dsh did not accept the switched credential, rolled back")
		writeErrU(w, http.StatusBadGateway, "err_account_store_failed", "切换账号失败：dsh 未识别新凭据（已回滚）")
		return
	}
	entry.LastUsedAt = time.Now().Format(time.RFC3339)
	vault.upsert(*entry)
	if err := saveAccountVault(vault); err != nil {
		logWarn("[account] account vault save failed: %v", err)
	}
	logInfo("[account] switched to account %s (dsh state=%s)", entry.ID, firstNonEmpty(status, "unverified"))
	writeJSON(w, m.accountStatusSnapshot())
}

// handleAccountRemove 从账号库里彻底删除一个账号：正在使用的先让 dsh 忘掉它，再请平台
// 作废 token，最后从库里删掉。
func (m *AdminMux) handleAccountRemove(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&payload); err != nil && err != io.EOF {
		writeErrU(w, http.StatusBadRequest, "err_bad_request", "请求格式不正确")
		return
	}
	vault, err := loadAccountVault()
	if err != nil {
		logWarn("[account] account vault unreadable: %v", err)
		writeErrFrom(w, accountVaultError(err), http.StatusInternalServerError)
		return
	}
	entry := vault.find(strings.TrimSpace(payload.ID))
	if entry == nil {
		writeErrU(w, http.StatusNotFound, "err_account_not_found", "账号库里没有这个账号")
		return
	}
	if entry.Token != "" && entry.Token == m.accountActiveToken() {
		if err := m.accountRemoveGrant(); err != nil {
			logWarn("[account] credential removal failed: %v", err)
			writeErrU(w, http.StatusInternalServerError, "err_account_store_failed", "写入账号凭据失败：%s", "detail", err.Error())
			return
		}
		if status, ok := m.accountWaitSignedOut(); !ok || status != "signed-out" {
			logWarn("[account] dsh state not settled after removing the active credential")
		}
	}
	if entry.Token != "" {
		if err := accountPlatformLogout(entry.Token); err != nil {
			// 平台侧作废失败不阻塞本机删除：本地凭据已经删掉/替换，不会再用了。
			logWarn("[account] platform logout failed: %v", err)
		}
	}
	vault.remove(entry.ID)
	if err := saveAccountVault(vault); err != nil {
		logWarn("[account] account vault save failed: %v", err)
	}
	logInfo("[account] removed account %s (%s)", entry.ID, accountVaultDebug(vault))
	writeJSON(w, m.accountStatusSnapshot())
}

// accountRestoreGrant 把凭据记录恢复成 token（空串 = 删掉这条记录）。
func (m *AdminMux) accountRestoreGrant(token string) error {
	if token == "" {
		return m.accountRemoveGrant()
	}
	return m.accountStoreGrant(token)
}

// accountPlatformLogout 请平台作废 token（dsh 的 logoutAccount 同一端点、同一头）。
func accountPlatformLogout(token string) error {
	req, err := http.NewRequest(http.MethodPost, accountPlatformOrigin+"/auth-api/v0/users/logout", nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-dsh-auth-token", token)
	req.Header.Set("origin", accountPlatformOrigin)
	req.Header.Set("user-agent", "Mozilla/5.0 (compatible; harness-console)")
	resp, err := accountHTTPClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("platform logout: HTTP %d", resp.StatusCode)
	}
	return nil
}

// firstNonEmpty 返回第一个非空串。
func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
