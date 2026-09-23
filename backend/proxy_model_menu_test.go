package main

// 本文件锁定「dsh 客户端注入」里与移动端模型 / 推理等级菜单直接相关的那一段
// （bootstrapScript 第 6 段：触屏模型菜单的失焦守卫）。
//
// 背景：dsh 0.1.7 的模型座位（conversation.input.model）在根节点上用 onBlur 关菜单，
// 且写成 `if (event.relatedTarget instanceof Node && (root 或 menu 包含它)) return; close()`。
// 桌面成立；iOS WebKit 在「菜单内按钮之间焦点搬家」时 relatedTarget 是 null（真机实测），
// 守卫落到 close()，菜单在 mousedown 之后、click 之前被卸载 —— 选项的 click 没有目标、
// 不发 session/selectModel，表现为「菜单能开、根面板点得动、一进二级面板点选项毫无反应」。
// 注入的守卫把该菜单内部的 focusout 拦在捕获阶段，React 的委托监听收不到，菜单不被误关。
//
// 契约（改注入时必须同步改这里）：必须随 HTML 下发、只在触屏分支内、只认模型座位自己的
// 菜单（aria-expanded + aria-controls），并且只拦该菜单内部的 focusout。

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// composerGuardFixture 提供一个最小 index 文档，用来观察反代注入后的 HTML。
func modelMenuGuardFixture(t *testing.T) *proxyFixture {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><html><head><title>dsh</title></head><body><div id=\"root\"></div></body></html>")
	}))
	t.Cleanup(upstream.Close)

	f := newMountedFixture(t, portOfURL(t, upstream.URL), proxyMountTestBase)
	f.boot.set(phaseReady, "")
	return f
}

func TestInjectIntoHTMLCarriesModelMenuFocusGuard(t *testing.T) {
	f := modelMenuGuardFixture(t)
	rec := proxyGet(t, f.proxy, proxyMountTestBase+"/", sessionCookieHeader())
	if rec.Code != http.StatusOK {
		t.Fatalf("index 应 200, 实际 code=%d", rec.Code)
	}
	body := rec.Body.String()

	headOpen := strings.Index(body, "<head>")
	guardAt := strings.Index(body, "__DSH_MODEL_MENU_FOCUS_GUARD__")
	if guardAt < 0 {
		t.Fatalf("注入里缺少触屏模型菜单失焦守卫标记")
	}
	if headOpen < 0 || guardAt < headOpen {
		t.Fatalf("守卫必须注入在 <head> 之后（head=%d guard=%d）", headOpen, guardAt)
	}

	for _, want := range []string{
		`document.addEventListener("focusout"`, // 监听失焦
		`}, true);`,                            // 捕获阶段（React 委托监听在更低层）
		`[data-slot="conversation.input.model"] button[aria-haspopup="menu"]`, // 只认模型座位
		`seat.getAttribute("aria-expanded") !== "true"`,                       // 菜单确实开着
		`document.getElementById(seat.getAttribute("aria-controls") || "")`,   // 用规范属性定位菜单
		`menu.contains(node)`,      // 只拦菜单内部的 focusout
		`event.stopPropagation();`, // 拦掉传播
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("注入的守卫缺少 %q", want)
		}
	}
}

// 守卫只在触屏设备上武装：无触摸的桌面窗口里那段脚本必须整条不生效（行为零变化）。
func TestModelMenuFocusGuardIsTouchOnly(t *testing.T) {
	gate := `var touchPrimaryForMenu = (navigator.maxTouchPoints || 0) > 0 || "ontouchstart" in window;`
	if !strings.Contains(bootstrapScript, gate) {
		t.Fatalf("守卫的触屏判定必须同时看 maxTouchPoints 与 ontouchstart")
	}
	branch := "if (touchPrimaryForMenu) {"
	i := strings.Index(bootstrapScript, branch)
	if i < 0 {
		t.Fatalf("找不到触屏分支 %s", branch)
	}
	// 监听必须在触屏分支之内（否则桌面也会被拦 focusout）。
	tail := bootstrapScript[i:]
	listener := strings.Index(tail, `document.addEventListener("focusout"`)
	branchEnd := strings.Index(tail, "\n  } catch (_e) {}")
	if listener < 0 || branchEnd < 0 || listener > branchEnd {
		t.Fatalf("focusout 监听必须写在触屏分支内（listener=%d branchEnd=%d）", listener, branchEnd)
	}
}

// 守卫与「浏览器兼容模式」开关（browserCompat）共用一个开关：守卫只在
// window.__DSH_BROWSER_COMPAT__ === true 时武装，开关关闭时整段不生效。
func TestModelMenuFocusGuardFollowsBrowserCompatFlag(t *testing.T) {
	// 开关在监听内现读：与注入顺序解耦，也保证开关关闭时整段放行。
	want := `if (window.__DSH_BROWSER_COMPAT__ !== true) return;`
	if !strings.Contains(bootstrapScript, want) {
		t.Fatalf("守卫必须受「浏览器兼容模式」开关约束（缺少 %s）", want)
	}
	if !strings.Contains(bootstrapScript, `window.__DSH_MODEL_MENU_FOCUS_GUARD__ = window.__DSH_BROWSER_COMPAT__ === true;`) {
		t.Fatal("守卫武装后应留下与开关一致的调试标记")
	}
}

// 诊断打点默认关闭：只有 HARNESS_DSH_DIAG 或页面 URL 的 ?dsh-diag=1 才注入。
func TestDshDiagnosticInjectionIsOptIn(t *testing.T) {
	f := modelMenuGuardFixture(t)

	// 默认：不下发诊断脚本。
	rec := proxyGet(t, f.proxy, proxyMountTestBase+"/", sessionCookieHeader())
	if body := rec.Body.String(); strings.Contains(body, "dsh-diag/") {
		t.Fatal("默认不应注入诊断打点")
	}

	// 访问级：页面 URL 带 ?dsh-diag=1。
	rec = proxyGet(t, f.proxy, proxyMountTestBase+"/?dsh-diag=1", sessionCookieHeader())
	if body := rec.Body.String(); !strings.Contains(body, "dsh-diag/") {
		t.Fatal("?dsh-diag=1 应注入诊断打点")
	}

	// 进程级：HARNESS_DSH_DIAG。
	t.Setenv("HARNESS_DSH_DIAG", "1")
	rec = proxyGet(t, f.proxy, proxyMountTestBase+"/", sessionCookieHeader())
	if body := rec.Body.String(); !strings.Contains(body, "dsh-diag/") {
		t.Fatal("HARNESS_DSH_DIAG 应注入诊断打点")
	}

	// 诊断只随 dsh 页面下发，且不改变守卫本身的行为。
	if got := dshDiagEnabled(nil); !got {
		t.Fatal("环境变量开启时 dshDiagEnabled(nil) 应为 true")
	}
}
