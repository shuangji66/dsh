package main

// auth_passthrough_test.go —— 鉴权「放行判据」的回归测试（审查 C2）。
//
// 历史问题：Auth 有个 validateErr 字段，isAuthed 里写着
// `if a.validateErr != "" || c.Password == "" { return true }`，而全仓从未给它赋值 ——
// 一个永不生效的死开关，却让人误以为「密码强度不合规就放行所有人」；同时 main.go
// 打印的是 "login auth disabled"，把「鉴权其实开着」说成「已关闭」。
//
// 现在的语义：放行只有两条判据 —— 未启用鉴权（authEnabled=false），或启用了鉴权但
// 密码为空；密码强度不合规只是日志提醒，被接受的密码始终按原值参与校验/签发。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// 用反射锁住「死开关不再存在」：这条测试在旧代码上失败（字段存在），
// 防止将来又加回一个永不复位、却写着「放行」的字段。
func TestAuthHasNoDeadValidateErrSwitch(t *testing.T) {
	if _, ok := reflect.TypeOf(Auth{}).FieldByName("validateErr"); ok {
		t.Fatal("Auth.validateErr 应已删除：isAuthed 的放行判据只有「未启用鉴权」与「密码为空」")
	}
}

// 强度不合规的密码仍然照常参与鉴权：未带 cookie 必须拒绝，用该密码登录后必须放行。
func TestWeakPasswordStillEnforced(t *testing.T) {
	t.Setenv("HARNESS_SESSION_KEY_FILE", filepath.Join(t.TempDir(), "session.key"))
	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })
	cfg := defaultConfig()
	cfg.AuthEnabled = true
	cfg.Password = "abc" // 明显不合规（长度不足、缺数字/标点），但仍然是唯一的口令
	initConfig(&cfg)
	if v := validatePassword(cfg.Password); v == "" {
		t.Fatal("测试前提：这个密码应被 validatePassword 判为不合规")
	}

	a := NewAuth()
	if a.isAuthed(httptest.NewRequest(http.MethodGet, "/", nil)) {
		t.Fatal("未带会话 cookie 的请求不得放行（强度不合规 ≠ 放行所有人）")
	}

	// 用该密码登录并带上签发的 cookie：必须放行（密码原样参与校验）。
	form := strings.NewReader("password=" + url.QueryEscape(cfg.Password))
	req := httptest.NewRequest(http.MethodPost, authLogin, form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	if handled := a.handleAuthRoutes(rec, req, newProxyMount("")); !handled {
		t.Fatal("POST /_login 应被鉴权路由消费")
	}
	if rec.Code != http.StatusFound {
		t.Fatalf("用配置里的密码登录应成功 302，实际 %d", rec.Code)
	}
	raw := rec.Header().Get("Set-Cookie")
	i := strings.Index(raw, authCookie+"=")
	if i < 0 {
		t.Fatalf("登录成功应下发会话 cookie，实际 %q", raw)
	}
	value := raw[i+len(authCookie)+1:]
	if j := strings.IndexByte(value, ';'); j >= 0 {
		value = value[:j]
	}
	authed := httptest.NewRequest(http.MethodGet, "/", nil)
	authed.Header.Set("Cookie", authCookie+"="+value)
	if !a.isAuthed(authed) {
		t.Fatal("用配置里的密码登录后必须放行")
	}
}

// 未启用鉴权 / 密码为空：这两条才是「放行」的判据。
func TestPassthroughOnlyWhenAuthDisabledOrPasswordEmpty(t *testing.T) {
	prev := GetConfig()
	t.Cleanup(func() { initConfig(&prev) })
	a := NewAuth()

	cfg := defaultConfig()
	cfg.AuthEnabled = false
	initConfig(&cfg)
	if !a.isAuthed(httptest.NewRequest(http.MethodGet, "/", nil)) {
		t.Fatal("未启用鉴权时不应要求登录")
	}

	cfg = defaultConfig()
	cfg.AuthEnabled = true
	cfg.Password = ""
	initConfig(&cfg)
	if !a.isAuthed(httptest.NewRequest(http.MethodGet, "/", nil)) {
		t.Fatal("启用了鉴权但密码为空时应放行（无密码即无门可守）")
	}
}
