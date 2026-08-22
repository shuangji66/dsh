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
	authTTL      = 2 * 60 * 60 // 2 hours
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
	return subtle.ConstantTimeCompare([]byte(expected), []byte(raw[dot+1:])) == 1
}

type Auth struct {
	validateErr string
}

func NewAuth() *Auth {
	return &Auth{validateErr: ""}
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
<title>登录 - Harness Proxy</title>
<style>
  *{box-sizing:border-box}body{margin:0;min-height:100vh;display:flex;
    align-items:center;justify-content:center;font-family:-apple-system,
    "Segoe UI",Roboto,"PingFang SC","Microsoft YaHei",sans-serif;
    background:#0f172a;color:#e2e8f0}
  .card{width:340px;padding:32px 28px;background:#1e293b;border:1px solid #334155;
    border-radius:12px;box-shadow:0 10px 40px rgba(0,0,0,.4)}
  h1{margin:0 0 8px;font-size:20px;font-weight:600;text-align:center;color:#f8fafc}
  .sub{margin:0 0 24px;font-size:13px;color:#94a3b8;text-align:center}
  label{display:block;margin:0 0 6px;font-size:13px;color:#cbd5e1}
  input{width:100%;padding:10px 12px;border:1px solid #475569;border-radius:8px;
    background:#0f172a;color:#f8fafc;font-size:14px;outline:none}
  input:focus{border-color:#6366f1;box-shadow:0 0 0 3px rgba(99,102,241,.25)}
  button{width:100%;margin-top:18px;padding:11px;border:none;border-radius:8px;
    background:#6366f1;color:#fff;font-size:14px;font-weight:600;cursor:pointer}
  button:hover{background:#4f46e5}
  .err{margin-top:14px;padding:10px 12px;background:#7f1d1d;border:1px solid #991b1b;
    border-radius:8px;font-size:13px;color:#fecaca;text-align:center;word-break:break-all}
</style></head><body><div class="card">
  <h1>登录</h1><p class="sub">访问受密码保护</p>
  <form method="POST" action="/_login">
    <label for="pw">密码</label>
    <input id="pw" name="password" type="password" autofocus required
           autocomplete="current-password">
    <button type="submit">登录</button>
  </form>
  __ERROR_SLOT__
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
			expire := time.Now().Unix() + authTTL
			token := hmacToken(c.Password, expire)
			next := safeNext(r.URL.Query().Get("next"))
			w.Header().Set("Set-Cookie", fmt.Sprintf("%s=%d.%s; Path=/; HttpOnly; SameSite=Lax; Max-Age=%d", authCookie, expire, token, authTTL))
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