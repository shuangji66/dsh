package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
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

func hmacToken(pwd string, expireTs int64) string {
	m := hmac.New(sha256.New, []byte(pwd))
	m.Write([]byte(strconv.FormatInt(expireTs, 10)))
	return hex.EncodeToString(m.Sum(nil))
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
func (a *Auth) isAuthed(r *http.Request) bool {
	c := GetConfig()
	if !c.AuthEnabled {
		return true
	}
	if a.validateErr != "" || c.Password == "" {
		return true // no-password passthrough mode
	}
	raw := parseCookies(r.Header.Get("Cookie"))[authCookie]
	dot := strings.Index(raw, ".")
	if dot == -1 {
		return false
	}
	expireTs, err := strconv.ParseInt(raw[:dot], 10, 64)
	if err != nil || expireTs <= time.Now().Unix() {
		return false
	}
	expected := hmacToken(c.Password, expireTs)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(raw[dot+1:])) != 1 {
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
	validateErr string
	visitors    *VisitorTracker
}

func NewAuth() *Auth {
	return &Auth{validateErr: "", visitors: NewVisitorTracker()}
}

func safeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
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
    <form method="POST" action="/_login">
      <label for="pw">密码</label>
      <input id="pw" name="password" type="password" autofocus required
             autocomplete="current-password" placeholder="请输入访问密码">
      <button type="submit">登 录</button>
    </form>
    __ERROR_SLOT__
  </div>
  <div class="foot">DeepSeek Harness</div>
</div></body></html>`

func serveLoginPage(w http.ResponseWriter, errMsg string) {
	slot := ""
	if errMsg != "" {
		esc := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;").Replace(errMsg)
		slot = `<div class="err">` + esc + `</div>`
	}
	body := strings.Replace(loginPageHTML, "__ERROR_SLOT__", slot, 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(200)
	w.Write([]byte(body))
}

// handleAuthRoutes returns true if it consumed the request.
func (a *Auth) handleAuthRoutes(w http.ResponseWriter, r *http.Request) bool {
	c := GetConfig()
	if !c.AuthEnabled {
		return false
	}
	u := r.URL
	switch u.Path {
	case authLogin:
		if r.Method == http.MethodGet {
			serveLoginPage(w, "")
			return true
		}
		if r.Method == http.MethodPost {
			r.ParseForm()
			pwd := r.FormValue("password")
			if c.Password == "" {
				serveLoginPage(w, "鉴权未启用（未设置密码），无需登录。")
				return true
			}
			if pwd != c.Password {
				serveLoginPage(w, "密码错误")
				return true
			}
			// 登录有效期取配置中的 AuthTTLHours（小时）；未配置或非法时回退到 2 小时
			ttlSeconds := c.AuthTTLHours * 3600
			if ttlSeconds <= 0 {
				ttlSeconds = 2 * 60 * 60
			}
			expire := time.Now().Unix() + int64(ttlSeconds)
			token := hmacToken(c.Password, expire)
			next := safeNext(r.URL.Query().Get("next"))
			w.Header().Set("Set-Cookie", fmt.Sprintf("%s=%d.%s; Path=/; HttpOnly; SameSite=Lax; Max-Age=%d", authCookie, expire, token, ttlSeconds))
			http.Redirect(w, r, next, http.StatusFound)
			return true
		}
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return true
	case authLogout:
		http.SetCookie(w, &http.Cookie{Name: authCookie, Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
		http.Redirect(w, r, authLogin, http.StatusFound)
		return true
	}
	return false
}

func randomReqID() string {
	return strconv.FormatInt(rand.Int63(), 10)
}