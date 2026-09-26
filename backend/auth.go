package main

import (
	"crypto/hmac"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	authCookie   = "harness_session"
	authLogin    = "/_login"
	authLogout   = "/_logout"
	safePunctStr = ".,-_:/@%^=+~"
)

var safePunct = map[rune]bool{}

func init() {
	for _, r := range safePunctStr {
		safePunct[r] = true
	}
}

func classifyChar(ch rune) string {
	if ch >= '0' && ch <= '9' {
		return "digit"
	}
	if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
		return "letter"
	}
	if safePunct[ch] {
		return "safe_punct"
	}
	if ch < 128 {
		return "unsafe"
	}
	return "other"
}

func validatePassword(pwd string) string {
	if len(pwd) < 8 {
		return "长度不足 8 位"
	}
	var hasLetter, hasDigit, hasPunct bool
	for _, ch := range pwd {
		switch classifyChar(ch) {
		case "letter":
			hasLetter = true
		case "digit":
			hasDigit = true
		case "safe_punct":
			hasPunct = true
		case "unsafe":
			return fmt.Sprintf("含危险标点 %q（仅允许 . , - _ : / @ %% ^ = + ~）", string(ch))
		}
	}
	if !hasLetter {
		return "缺少字母"
	}
	if !hasDigit {
		return "缺少数字"
	}
	if !hasPunct {
		return "缺少标点（仅允许 . , - _ : / @ %% ^ = + ~）"
	}
	return ""
}

// authTTLSeconds 返回登录有效期（秒）：取配置中的 AuthTTLHours，未配置或非法
// （<=0）时回退 4 小时。登录 Cookie 的 Max-Age 与网关访问条目的闲置清除时长
// （见 visitors.go 的 gatewayVisitorIdleTTL）共用这一份取值。
func authTTLSeconds(c AppConfig) int {
	if c.AuthTTLHours <= 0 {
		return 4 * 60 * 60
	}
	return c.AuthTTLHours * 3600
}

// --- 会话 cookie 的签名 ---
//
// Cookie 形态：`<过期秒>.<每会话随机 nonce>.<mac>`，其中
//
//	mac = HMAC-SHA256(密码 + "\x00" + 进程外随机密钥, "<过期秒>.<nonce>")。
//
// 为什么不能只用密码当 HMAC key（旧实现 `mac = HMAC(密码, 过期秒)` 的问题）：
//  1. Cookie 里明文带着过期秒，于是任何拿到一次 Cookie 的人都能**离线爆破访问密码**
//     （猜测口令 → 重算 HMAC 比对即可）；访问常走明文 http，抓包/日志泄漏都有可能。
//  2. 签名只依赖 (密码, 秒)，同一秒内登录的两台设备得到**完全相同**的令牌：访客列表
//     把它们合并成一条，「踢掉一台」等于两台都被踢。
//
// 现在：nonce 每会话随机（不同设备/不同次登录互不相同），密钥里掺了一段与密码无关的
// 32 字节随机量 —— 爆破必须同时猜中 128 位随机量，该通路被堵死。密码变更会让密钥随之
// 变化，所有旧会话自动失效（与旧行为一致）。
func sessionSign(key []byte, expireTs int64, nonce string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(strconv.FormatInt(expireTs, 10)))
	m.Write([]byte("."))
	m.Write([]byte(nonce))
	return hex.EncodeToString(m.Sum(nil))
}

// newSessionNonce 生成一次性会话随机量（16 字节 hex）。
func newSessionNonce() string {
	var b [16]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		// 极端情况（无可用随机源）退化为纳秒时间戳：仍然每次不同，不会让同秒登录
		// 共用同一个令牌。
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b[:])
}

// parseSessionCookie 解析三段式会话 cookie，返回过期时刻、nonce 与 mac。
// 旧版两段式（过期秒.mac）在这里判为不成形 —— 升级后需要重新登录一次。
func parseSessionCookie(raw string) (int64, string, string, bool) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || parts[1] == "" {
		return 0, "", "", false
	}
	expireTs, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, "", "", false
	}
	return expireTs, parts[1], parts[2], true
}

// sessionKeyFileFn 返回「会话随机密钥」的存放路径。变量而非常量：单测注入临时路径。
// 默认放在应用数据目录（与 config.json 同级），0600。
var sessionKeyFileFn = func() string {
	if p := os.Getenv("HARNESS_SESSION_KEY_FILE"); p != "" {
		return p
	}
	if pkgvar := os.Getenv("TRIM_PKGVAR"); pkgvar != "" {
		return filepath.Join(pkgvar, "session.key")
	}
	// 开发/测试环境没有 TRIM_PKGVAR：退到系统临时目录（重启后可能丢失，届时只需重登）。
	return filepath.Join(os.TempDir(), "harness-session.key")
}

// processSecret 是「密钥文件不可用」时的进程内兜底密钥：同一进程内的所有 Auth 实例
// 共用同一份（否则同一进程里签发与校验会用到不同的密钥，登录必然失效）。
// 代价仅是「重启后需要重新登录」。
var processSecret struct {
	mu    sync.Mutex
	value string
}

func cachedProcessSecret() string {
	processSecret.mu.Lock()
	defer processSecret.mu.Unlock()
	return processSecret.value
}

func setProcessSecret(v string) {
	processSecret.mu.Lock()
	processSecret.value = v
	processSecret.mu.Unlock()
}

// sessionSecret 读取（首次调用则生成并落盘）与访问密码无关的随机会话密钥。
// 落盘保证控制台重启/自更新后已登录的浏览器不会被踢出登录；密钥文件不可写/不可读时
// 退化为进程内共享的随机密钥（功能一致，重启后需重新登录一次）。
func (a *Auth) sessionSecret() string {
	a.secretMu.Lock()
	defer a.secretMu.Unlock()
	if a.secret != "" {
		return a.secret
	}
	path := sessionKeyFileFn()
	if b, err := os.ReadFile(path); err == nil {
		if sec := strings.TrimSpace(string(b)); len(sec) >= 32 {
			a.secret = sec
			return sec
		}
	}
	// 已有进程内兜底就不再各自生成（多实例必须共用同一份，否则登录永远校验不过）。
	if sec := cachedProcessSecret(); sec != "" {
		a.secret = sec
		return sec
	}
	secret := newSessionNonce() + newSessionNonce() // 32 字节 hex
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0o700)
	}
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		logWarn("[auth] cannot persist session key to %s (%v) - sessions will not survive a console restart", path, err)
	} else {
		logInfo("[auth] generated session key at %s", path)
	}
	setProcessSecret(secret)
	a.secret = secret
	return secret
}

// sessionKey 组合出会话 cookie 的 HMAC 密钥（访问密码 + 与密码无关的随机密钥）。
func (a *Auth) sessionKey(password string) []byte {
	return []byte(password + "\x00" + a.sessionSecret())
}

// newSessionCookie 打包一份会话 cookie 值。
func (a *Auth) newSessionCookie(password string, expireTs int64) string {
	nonce := newSessionNonce()
	return strconv.FormatInt(expireTs, 10) + "." + nonce + "." + sessionSign(a.sessionKey(password), expireTs, nonce)
}

func parseCookies(header string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(header, ";") {
		eq := strings.Index(part, "=")
		if eq == -1 {
			continue
		}
		k := strings.TrimSpace(part[:eq])
		v := strings.TrimSpace(part[eq+1:])
		if k != "" {
			out[k] = v
		}
	}
	return out
}

// isAuthed reports whether the request carries a valid session cookie.
//
// 「放行」的判据只有两条：未启用鉴权（AuthEnabled=false），或启用了鉴权但访问密码为
// 空（无密码即无门可守，历史行为如此）。**不要**再为「密码强度不合规」放行：
// 密码强度只影响提示，被接受的密码始终按原值参与校验（见 main.go 的日志，
// 以及 validatePassword 的调用点）。历史上这里还有一个 `a.validateErr != ""`
// 短路分支，而 validateErr 全仓从未被赋值 —— 等于一个永不生效、却让读者以为
// 「强度不合规就放行」的死开关，已删除。
func (a *Auth) isAuthed(r *http.Request) bool {
	c := GetConfig()
	if !c.AuthEnabled {
		return true
	}
	if c.Password == "" {
		return true // no-password passthrough mode
	}
	raw := parseCookies(r.Header.Get("Cookie"))[authCookie]
	expireTs, nonce, mac, ok := parseSessionCookie(raw)
	if !ok || expireTs <= time.Now().Unix() {
		return false
	}
	expected := sessionSign(a.sessionKey(c.Password), expireTs, nonce)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(mac)) != 1 {
		return false
	}
	// 访客被删除（需重新登录）后，其会话令牌被吊销，视为未鉴权
	if a.visitors.isRevokedToken(raw) {
		return false
	}
	return true
}

// cookieSession parses the session cookie into (expireTs, rawToken). Returns
// (0, "") when no valid session cookie is present.
// rawToken 是整段 cookie 值（含 nonce），作为访客列表的键与吊销键 —— 因为 nonce 每会话
// 不同，同一秒登录的两台设备现在是两条独立记录（旧实现里它们完全相同、会被合并）。
func (a *Auth) cookieSession(r *http.Request) (int64, string) {
	raw := parseCookies(r.Header.Get("Cookie"))[authCookie]
	dot := strings.Index(raw, ".")
	if dot == -1 {
		return 0, ""
	}
	et, err := strconv.ParseInt(raw[:dot], 10, 64)
	if err != nil {
		return 0, ""
	}
	return et, raw
}

// recordVisitor updates the reverse-proxy visitor registry for the request.
func (a *Auth) recordVisitor(r *http.Request) {
	et, token := a.cookieSession(r)
	if token == "" {
		return
	}
	a.visitors.record(token, realIP(r), et)
}

// --- 飞牛网关访问（网关注入的身份头） ---
//
// 平台网关（fnOS open-gateway）转发应用请求时会注入这三个头，代表请求已经过飞牛
// OS 的登录认证。反代只在「网关那条线」（Unix Socket 监听，见 startProxySocket 与
// reverseProxy.gatewayLine）上认可它们：那条 socket 不在网络上暴露，只有本机网关
// 进程能连。TCP 端口在局域网内可达，认这三个头等于把端口鉴权交给客户端自己声明
// （任何客户端加三个头就能绕过），故端口线一概不认。
const (
	headerTrimUsername = "X-Trim-Username"
	headerTrimIsAdmin  = "X-Trim-Isadmin"
	headerTrimUserID   = "X-Trim-Userid"
)

// gatewayVisitor 是从网关请求头解析出的访问者身份。网关请求没有 harness 会话
// cookie，登录列表因此按「飞牛用户 + 客户端 IP」标识：同一个人从不同环境（网络）
// 访问飞牛时 IP 不同，各占一条记录，而不是反复刷新同一条。
type gatewayVisitor struct {
	ID       string // gatewayVisitorID(UserID, IP)，登录列表里的稳定键
	UserID   int
	Username string
	Admin    bool
	IP       string // 客户端 IP，取不到时为空
}

// parseGatewayVisitor 解析网关注入的身份头。判定门槛取「X-Trim-Userid 是有效正
// 整数 + X-Trim-Username 非空」：这两项才构成可标识的身份；X-Trim-Isadmin 只作为
// 附加标记（取值可能是 1/true/yes），不参与判定，以免网关版本差异让请求退回登录页。
func parseGatewayVisitor(r *http.Request) (gatewayVisitor, bool) {
	uid, err := strconv.Atoi(strings.TrimSpace(r.Header.Get(headerTrimUserID)))
	if err != nil || uid <= 0 {
		return gatewayVisitor{}, false
	}
	username := strings.TrimSpace(r.Header.Get(headerTrimUsername))
	if username == "" {
		return gatewayVisitor{}, false
	}
	ip := gatewayClientIP(r)
	return gatewayVisitor{
		ID:       gatewayVisitorID(uid, ip),
		UserID:   uid,
		Username: username,
		Admin:    truthyHeader(r.Header.Get(headerTrimIsAdmin)),
		IP:       ip,
	}, true
}

// gatewayClientIP 取网关请求的客户端 IP，用于在登录列表里区分不同访问环境。
// 平台网关（nginx 风格）通常带 X-Forwarded-For / X-Real-Ip，realIP 优先取它们；
// 请求经 Unix Socket 到达时 RemoteAddr 不是可用地址（"@" 或 socket 路径），此时
// 返回空串，记录退化为只按飞牛用户标识。
func gatewayClientIP(r *http.Request) string {
	raw := strings.TrimSpace(realIP(r))
	if raw == "" || strings.HasPrefix(raw, "@") || strings.HasPrefix(raw, "/") {
		return ""
	}
	return raw
}

// truthyHeader 判断网关注入的布尔标记头是否为真。
func truthyHeader(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// recordGatewayVisitor 记录一次网关访问（登录列表里的「网关访问」条目）。
func (a *Auth) recordGatewayVisitor(gv gatewayVisitor) {
	a.visitors.recordGateway(gv.ID, gv.IP, gv.Username, gv.Admin)
}

// Visitors returns a snapshot of the reverse-proxy visitors.
func (a *Auth) Visitors() []Visitor {
	return a.visitors.List()
}

// RevokeVisitor logs out the visitor identified by session token.
func (a *Auth) RevokeVisitor(token string) bool {
	return a.visitors.Revoke(token)
}

// realIP returns the client IP, honoring an upstream reverse proxy that sets
// X-Forwarded-For. The leftmost (original client) entry is used when present,
// otherwise the direct connection address is used.
func realIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for _, part := range strings.Split(xff, ",") {
			if s := strings.TrimSpace(part); s != "" {
				return strings.TrimPrefix(s, "::ffff:")
			}
		}
	}
	if xr := r.Header.Get("X-Real-Ip"); xr != "" {
		return strings.TrimPrefix(strings.TrimSpace(xr), "::ffff:")
	}
	return ip(r)
}

type Auth struct {
	visitors *VisitorTracker
	// secretMu 保护 secret：进程外随机会话密钥（懒加载，见 sessionSecret）。
	secretMu sync.Mutex
	secret   string
}

func NewAuth() *Auth {
	return &Auth{visitors: NewVisitorTracker()}
}

// safeNext 校验「登录成功后的跳转目标」（登录页 query 里的 next）。
// 返回 "/" 表示「不允许作为跳转目标」，此时调用方一律落回挂载根。
//
// 拒绝三类值：
//   - 不以单个 "/" 开头：绝对 URL（http://x）或站外相对路径；
//   - 第二个字符又是 "/"：`//evil.com` 是协议相对地址，浏览器会跳去外站；
//   - 任何位置含 "\"：浏览器把 "\" 归一化成 "/"，`/\evil.com` 与 `//evil.com`
//     等效（开放重定向）。收紧到「只要含反斜杠就拒绝」，覆盖 `\` 出现在第二个
//     字符之外的其它形态（例如 `/\evil.com`、`/a\..\..\x`）。
func safeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "/"
	}
	if strings.Contains(next, `\`) {
		return "/"
	}
	if next == authLogin || strings.HasPrefix(next, authLogin+"?") {
		return "/"
	}
	return next
}

const loginPageHTML = `<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>登录 - DeepSeek Harness</title>
<style>
  :root{
    --brand:#6366F1;
    --brand-hover:#4F46E5;
    --brand-soft:rgba(99,102,241,.12);
    --bg:#FAFAFA;
    --surface:#FFFFFF;
    --line:#E8E8EC;
    --ink:#0A0A0A;
    --ink-soft:#6B6B6B;
    --ink-faint:#9C9C9C;
    --shadow:0 2px 10px rgba(0,0,0,.04);
    --err-bg:rgba(239,68,68,.08);
    --err-border:rgba(239,68,68,.35);
    --err-color:#dc2626;
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --bg:#0B0B0F;
      --surface:#111115;
      --line:#2A2A32;
      --ink:#EDEDF0;
      --ink-soft:#A6A6AD;
      --ink-faint:#8A8A92;
      --shadow:0 2px 10px rgba(0,0,0,.5);
      --err-bg:rgba(248,113,113,.12);
      --err-border:rgba(248,113,113,.35);
      --err-color:#f87171;
    }
  }
  *{box-sizing:border-box}
  html,body{height:100%}
  body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;
    font-family:'DM Sans',ui-sans-serif,system-ui,-apple-system,'Segoe UI',Roboto,
      'PingFang SC','Microsoft YaHei',sans-serif;
    background:var(--bg);color:var(--ink);-webkit-font-smoothing:antialiased;
    padding:24px}
  .wrap{width:100%;max-width:380px}
  .card{background:var(--surface);border:1px solid var(--line);border-radius:12px;
    padding:36px 32px;box-shadow:var(--shadow);overflow:hidden}
  .logo{width:44px;height:44px;margin:0 auto 20px;border-radius:12px;
    background:var(--brand);display:flex;align-items:center;justify-content:center;
    box-shadow:0 4px 12px rgba(99,102,241,.3)}
  .logo svg{width:22px;height:22px;display:block}
  h1{margin:0 0 6px;font-size:24px;font-weight:600;text-align:center;
    font-family:'General Sans','DM Sans',ui-sans-serif,system-ui,sans-serif;
    letter-spacing:-.03em;color:var(--ink)}
  .sub{margin:0 0 28px;font-size:13px;color:var(--ink-soft);text-align:center}
  label{display:block;margin:0 0 8px;font-size:13px;font-weight:500;color:var(--ink)}
  input{width:100%;padding:10px 14px;border:1px solid var(--line);border-radius:6px;
    background:var(--surface);color:var(--ink);font-size:14px;outline:none;
    transition:border-color .15s ease,box-shadow .15s ease}
  input::placeholder{color:var(--ink-faint)}
  input:focus{border-color:var(--brand);box-shadow:0 0 0 3px var(--brand-soft)}
  button{width:100%;margin-top:20px;padding:11px;border:none;border-radius:6px;
    background:var(--brand);color:#fff;font-size:14px;font-weight:600;cursor:pointer;
    transition:background .15s ease,transform .15s ease,box-shadow .15s ease}
  button:hover{background:var(--brand-hover);transform:translateY(-1px);
    box-shadow:0 4px 12px rgba(99,102,241,.35)}
  button:active{transform:translateY(0)}
  .err{margin-top:18px;padding:10px 12px;background:var(--err-bg);
    border:1px solid var(--err-border);border-radius:6px;font-size:13px;
    color:var(--err-color);text-align:center;word-break:break-all}
  .foot{margin-top:16px;text-align:center;font-size:12px;color:var(--ink-faint)}
</style></head><body><div class="wrap">
  <div class="card">
    <div class="logo"><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#fff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/></svg></div>
    <h1>欢迎回来</h1><p class="sub">访问受密码保护，请输入登录密码</p>
    <form method="POST" action="__LOGIN_ACTION__">
      <label for="pw">密码</label>
      <input id="pw" name="password" type="password" autofocus required
             autocomplete="current-password" placeholder="请输入访问密码">
      <button type="submit">登 录</button>
    </form>
    __ERROR_SLOT__
  </div>
  <div class="foot">DeepSeek Harness</div>
</div></body></html>`

// htmlEscape 转义要嵌入 HTML 属性的文本（与错误提示槽用同一套替换规则）。
func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;").Replace(s)
}

// serveLoginPage 输出登录页。表单 action 必须带挂载前缀（子路径部署下 POST 要
// 回到 "<prefix>/_login"，否则会落到站点根），并**把 next 一并带上**：
// 未登录访问深链接时反代会 302 到 `/_login?next=<挂载内路径>`，若表单 action 丢掉
// 这个 query，POST 时 r.URL.Query().Get("next") 恒为空，登录成功后只能落回挂载根
// —— next 就成了死参数（深链接全部丢失）。
//
// next 是**已剥掉挂载前缀的挂载内路径**（反代记录的就是剥前缀后的路径），
// 这里再经 safeNext 校验（挡协议相对地址/反斜杠/回到登录页自身的循环），
// 只有校验后确实是一个有意义的、非挂载根的路径才写进 query；否则保持
// `<prefix>/_login` 原样（少一个无意义的 `?next=%2F`）。
func serveLoginPage(w http.ResponseWriter, errMsg, next string, mount proxyMount) {
	slot := ""
	if errMsg != "" {
		slot = `<div class="err">` + htmlEscape(errMsg) + `</div>`
	}
	action := mount.join(authLogin)
	if safe := safeNext(next); safe != "/" {
		// QueryEscape 会把 "/" 编码成 %2F：POST 时 r.URL.Query().Get("next") 解码后
		// 仍是挂载内路径，随后由 handleAuthRoutes 用 mount.join 补回前缀。
		action += "?next=" + url.QueryEscape(safe)
	}
	body := strings.Replace(loginPageHTML, "__ERROR_SLOT__", slot, 1)
	body = strings.Replace(body, "__LOGIN_ACTION__", htmlEscape(action), 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(200)
	w.Write([]byte(body))
}

// handleAuthRoutes returns true if it consumed the request. mount 是这条监听的
// 挂载点：登录页表单 action、登录成功后的跳转目标与登出跳转都要带上它的前缀，
// 否则子路径部署下浏览器会被送出挂载目录（r.URL 已由反代剥掉前缀，见 stripMount）。
func (a *Auth) handleAuthRoutes(w http.ResponseWriter, r *http.Request, mount proxyMount) bool {
	c := GetConfig()
	if !c.AuthEnabled {
		return false
	}
	u := r.URL
	// next 是挂载内路径（反代门禁按剥前缀后的路径记录）：GET 时带进表单 action，
	// 登录失败重试时也不能丢（否则重试一次就回到挂载根）。
	next := u.Query().Get("next")
	switch u.Path {
	case authLogin:
		if r.Method == http.MethodGet {
			serveLoginPage(w, "", next, mount)
			return true
		}
		if r.Method == http.MethodPost {
			r.ParseForm()
			pwd := r.FormValue("password")
			if c.Password == "" {
				serveLoginPage(w, "鉴权未启用（未设置密码），无需登录。", next, mount)
				return true
			}
			if pwd != c.Password {
				serveLoginPage(w, "密码错误", next, mount)
				return true
			}
			// 登录有效期取配置中的 AuthTTLHours（小时）；未配置或非法时回退到 4 小时
			ttlSeconds := authTTLSeconds(c)
			expire := time.Now().Unix() + int64(ttlSeconds)
			token := a.newSessionCookie(c.Password, expire)
			// next 是挂载内路径（反代门禁按剥前缀后的路径记录），补回前缀才是
			// 浏览器可用的地址；safeNext 已挡掉 // 与含 \ 的协议相对地址。
			target := mount.join(safeNext(next))
			w.Header().Set("Set-Cookie", fmt.Sprintf("%s=%s; Path=/; HttpOnly; SameSite=Lax; Max-Age=%d", authCookie, token, ttlSeconds))
			http.Redirect(w, r, target, http.StatusFound)
			return true
		}
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return true
	case authLogout:
		http.SetCookie(w, &http.Cookie{Name: authCookie, Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
		http.Redirect(w, r, mount.join(authLogin), http.StatusFound)
		return true
	}
	return false
}

func randomReqID() string {
	return strconv.FormatInt(rand.Int63(), 10)
}
