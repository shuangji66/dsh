package main

// uimsg_test.go —— 「面向用户的文案 = code + 参数」这套机制的守卫。
//
// 三条契约（都是「错了不会报错、只有用户看得到」的那类）：
//  1. 后端出现的每个 code 必须在前端 i18n 字典（zh / en 两份）里都有条目 —— 漏一条，
//     英文界面下那句提示会静默退回中文原文；
//  2. 带参文案的占位符数量必须与参数个数一致 —— 否则界面上会出现 %!s(MISSING)；
//  3. 状态载荷里的原文与引用必须成对写（setErrFields / setMsgFields），只改文本不清引用
//     会让前端拿旧 code 显示上一轮的文案。
//
// code / 文案 / 参数都从**语法树**里取（不是正则）：文案跨行拼接、参数是函数调用都很常见，
// 正则数不准。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// uiCall 是一处「用户文案」调用点。
type uiCall struct {
	File string
	Line int
	Code string
	// Zh 是中文原文（跨行/拼接的字面量会拼起来）；取不到时为空。
	Zh string
	// Params 是 k/v 参数个数（uiErr("c", "…", "k", "v") 记 2）。
	Params int
}

// callShape 描述各入口里 code / 原文的位置：
//   - uiErr / busyf / uiMsgPair：code 在 0、原文在 1；
//   - msgRef：只有 code；
//   - writeErrU(w, status, code, zh, kv…)：code 在 2、原文在 3。
type callShape struct {
	codeIdx int
	zhIdx   int // -1 表示没有原文参数
}

var uiCallShapes = map[string]callShape{
	"uiErr":     {0, 1},
	"busyf":     {0, 1},
	"uiMsgPair": {0, 1},
	"msgRef":    {0, -1},
	"writeErrU": {2, 3},
}

// stringValue 取出字符串字面量的值（支持 `"a" + "b"` 形式的拼接）。
func stringValue(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return "", false
		}
		return s, true
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, ok1 := stringValue(v.X)
		r, ok2 := stringValue(v.Y)
		if !ok1 || !ok2 {
			return "", false
		}
		return l + r, true
	}
	return "", false
}

// scanUICallSites 解析 backend 下的非测试源码，挑出所有用户文案调用点。
func scanUICallSites(t *testing.T) []uiCall {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var out []uiCall
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			ident, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			shape, ok := uiCallShapes[ident.Name]
			if !ok || len(call.Args) <= shape.codeIdx {
				return true
			}
			code, ok := stringValue(call.Args[shape.codeIdx])
			if !ok {
				return true
			}
			site := uiCall{File: name, Line: fset.Position(call.Pos()).Line, Code: code}
			if shape.zhIdx >= 0 {
				if len(call.Args) > shape.zhIdx {
					site.Zh, _ = stringValue(call.Args[shape.zhIdx])
					site.Params = len(call.Args) - shape.zhIdx - 1
				}
			} else {
				site.Params = len(call.Args) - shape.codeIdx - 1
			}
			out = append(out, site)
			return true
		})
	}
	return out
}

// parseFrontendDict 从 useI18n.ts 里取出某个语言字典的 key 集合。
func parseFrontendDict(t *testing.T, src, startMarker string) map[string]bool {
	t.Helper()
	i := strings.Index(src, startMarker)
	if i < 0 {
		t.Fatalf("useI18n.ts 里找不到 %q（字典结构变了？）", startMarker)
	}
	rest := src[i+len(startMarker):]
	j := strings.Index(rest, "\n}\n")
	if j < 0 {
		t.Fatalf("useI18n.ts 里找不到 %q 的结束标记", startMarker)
	}
	keys := map[string]bool{}
	keyRe := regexp.MustCompile(`(?m)^\s{2}([A-Za-z0-9_]+):`)
	for _, m := range keyRe.FindAllStringSubmatch(rest[:j], -1) {
		keys[m[1]] = true
	}
	return keys
}

// 后端用到的每个 code，前端 zh / en 两份字典都必须有 —— 否则切到英文时这句提示
// 会静默退回中文（不会报错，只有用户看得到）。
func TestUserFacingCodesTranslatedInFrontend(t *testing.T) {
	sites := scanUICallSites(t)
	if len(sites) < 50 {
		t.Fatalf("只扫到 %d 处用户文案调用，扫描规则恐怕失效了（应覆盖 writeErrU/uiErr/busyf/uiMsgPair）", len(sites))
	}

	data, err := os.ReadFile("../frontend/src/composables/useI18n.ts")
	if err != nil {
		t.Fatalf("读取前端 i18n 字典失败: %v", err)
	}
	src := string(data)
	zh := parseFrontendDict(t, src, "const zh: Record<string, string> = {")
	en := parseFrontendDict(t, src, "const en: Record<string, string> = {")

	seen := map[string]bool{}
	for _, s := range sites {
		if seen[s.Code] {
			continue
		}
		seen[s.Code] = true
		if !codeIsWellFormed(s.Code) {
			t.Errorf("%s:%d code %q 形态不对（必须是 err_* / msg_* 的小写 snake_case）", s.File, s.Line, s.Code)
		}
		if !zh[s.Code] {
			t.Errorf("%s:%d code %q 缺少中文条目（前端 useI18n.ts 的 zh 字典）", s.File, s.Line, s.Code)
		}
		if !en[s.Code] {
			t.Errorf("%s:%d code %q 缺少英文条目（前端 useI18n.ts 的 en 字典）—— 英文界面会退回中文原文",
				s.File, s.Line, s.Code)
		}
	}
}

// 带参文案必须「占位符数量 == 参数个数」：多一个少一个都会在界面上打出
// %!s(MISSING) / %!(EXTRA …)，只有用户看得到。
func TestUserFacingCodePlaceholdersMatchParams(t *testing.T) {
	verbRe := regexp.MustCompile(`%[sdvq]`)
	checked := 0
	for _, s := range scanUICallSites(t) {
		if s.Zh == "" || s.Params == 0 {
			continue
		}
		if s.Params%2 != 0 {
			t.Errorf("%s:%d %s 的参数不是 k/v 成对（实得 %d 个）", s.File, s.Line, s.Code, s.Params)
			continue
		}
		want := len(verbRe.FindAllString(s.Zh, -1))
		if got := s.Params / 2; got != want {
			t.Errorf("%s:%d %s 的占位符 %d 个、参数 %d 对（原文 %q）—— 界面上会出现 %%!s(MISSING)",
				s.File, s.Line, s.Code, want, got, s.Zh)
		}
		checked++
	}
	if checked < 15 {
		t.Fatalf("只校验了 %d 处带参文案，扫描规则恐怕失效了", checked)
	}
}

// 错误响应必须带 code + params（前端 apiErrorFrom 靠它们走 i18n）。
func TestErrorResponseCarriesCode(t *testing.T) {
	u := uiErr("err_need_version", "缺少 version")
	m, ok := uiMsgOf(u)
	if !ok || m.Code != "err_need_version" {
		t.Fatalf("uiErr 应该产出带 code 的错误，实得 %+v ok=%v", m, ok)
	}
	if u.Error() != "缺少 version" {
		t.Fatalf("原文应保留（未知 code 时的兜底），实得 %q", u.Error())
	}

	// 带参数：中文原文按 kv 的 value 顺序填充，params 里 k/v 成对保留给前端 i18n。
	u = uiErr("err_port_proxy_range", "反代端口必须在 %s-%s 之间", "min", "1025", "max", "65535")
	if u.Error() != "反代端口必须在 1025-65535 之间" {
		t.Fatalf("参数填充不对: %q", u.Error())
	}
	m, _ = uiMsgOf(u)
	if m.Params["min"] != "1025" || m.Params["max"] != "65535" {
		t.Fatalf("params 应保留 k/v 对，实得 %+v", m.Params)
	}

	// busy 类会被 HTTP 层映射成 409。
	if !isBusyErr(busyf("err_backup_running", "已有备份任务正在进行")) {
		t.Fatal("busyf 产出的错误必须被识别为 busy")
	}
	if isBusyErr(u) {
		t.Fatal("普通 uiErr 不该被当成 busy")
	}
}

// 状态载荷里的 error / message 必须成对写：只改文本不清引用，前端会拿旧 code 显示
// 上一轮的文案。
func TestStatusFieldsCarryRefs(t *testing.T) {
	var text string
	var ref *uiMsg
	setErrFields(&text, &ref, uiErr("err_dsh_running", "dsh 已在运行"))
	if text != "dsh 已在运行" || ref == nil || ref.Code != "err_dsh_running" {
		t.Fatalf("setErrFields 应同时写原文与引用，实得 text=%q ref=%+v", text, ref)
	}
	// 清空时两者都要清，否则旧 code 会留在快照里。
	setErrFields(&text, &ref, nil)
	if text != "" || ref != nil {
		t.Fatalf("setErrFields(nil) 必须清空两者，实得 text=%q ref=%+v", text, ref)
	}

	setMsgFields(&text, &ref, "msg_install_done", "dsh %s 安装完成", "version", "0.2.0")
	if text != "dsh 0.2.0 安装完成" || ref == nil || ref.Code != "msg_install_done" || ref.Params["version"] != "0.2.0" {
		t.Fatalf("setMsgFields 应写原文 + 引用 + 参数，实得 text=%q ref=%+v", text, ref)
	}
	// 诊断原文（npm 尾行）只写文本，不带引用。
	setMsgFields(&text, &ref, "", "added 12 packages in 3s")
	if text != "added 12 packages in 3s" || ref != nil {
		t.Fatalf("原文直出时不该带引用，实得 text=%q ref=%+v", text, ref)
	}
	setMsgFields(&text, &ref, "", "")
	if text != "" || ref != nil {
		t.Fatalf("清空时两者都要清，实得 text=%q ref=%+v", text, ref)
	}
}

// HTTP 层：带 code 的错误响应体里必须有 code/params（前端据此走 i18n）。
func TestHTTPErrorBodyCarriesCodeAndParams(t *testing.T) {
	rec := httptest.NewRecorder()
	writeErrU(rec, 400, "err_port_proxy_range", "反代端口必须在 %s-%s 之间", "min", "1025", "max", "65535")
	if rec.Code != 400 {
		t.Fatalf("状态码 = %d, want 400", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`"code":"err_port_proxy_range"`, `"min":"1025"`, `"max":"65535"`, "反代端口必须在 1025-65535 之间"} {
		if !strings.Contains(body, want) {
			t.Fatalf("错误响应体缺少 %s:\n%s", want, body)
		}
	}

	// busy 错误：writeErrFrom 自动改成 409。
	rec = httptest.NewRecorder()
	writeErrFrom(rec, busyf("err_data_op_busy", "正在执行其它更新/备份/恢复操作，请等它结束后再重试"), 400)
	if rec.Code != 409 {
		t.Fatalf("busy 错误应回 409，实得 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"err_data_op_busy"`) {
		t.Fatalf("busy 错误也应带 code:\n%s", rec.Body.String())
	}
}

// 登录页是后端渲染的独立页面（用不了前端 i18n），文案靠页内脚本按 localStorage 的语言
// 换 data-en；这里钉住「两语言都在页面上 + 脚本确实读了语言偏好」。
func TestLoginPageCarriesBothLanguages(t *testing.T) {
	page := loginPageHTML
	for _, want := range []string{
		`data-zh="欢迎回来" data-en="Welcome back"`,
		`data-zh="密码" data-en="Password"`,
		`data-ph-en="Enter the access password"`,
		`localStorage.getItem('console-language')`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("登录页缺少 %s", want)
		}
	}
	// 错误提示槽：两种语言都要在里面（页面脚本按语言取其中一个）。
	zh, en, ok := loginErrText("err_login_bad_password")
	if !ok || zh == "" || en == "" {
		t.Fatalf("登录失败提示应中英都有，实得 zh=%q en=%q ok=%v", zh, en, ok)
	}
	// 认不出的 code 不能静默吞掉（排查时能看到原文）。
	if _, _, ok := loginErrText("err_not_a_code"); ok {
		t.Fatal("未登记的 code 不该被当成已知文案")
	}
}
