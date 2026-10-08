package main

// uimsg.go —— 「面向用户的文案」的 code + 参数机制。
//
// 背景：控制台的 i18n 字典在前端（`frontend/src/composables/useI18n.ts`，语言只存
// localStorage），后端既不知道用户选的是哪种语言，也没有理由把同一句话维护两份。
// 因此后端**只回稳定 code + 参数**，前端拿 code 查字典（key 就是 code 本身）；查不到
// 时回退到后端给的中文原文 —— 旧版前端遇到新 code 也不会白屏，只是显示原文。
//
// 约定（新增一条提示时照做）：
//   - code 用 snake_case，并自带命名空间：错误 `err_*`、进度/状态文案 `msg_*`；
//   - 中文原文（zh）与参数一起给：`uiErr("err_need_version", "缺少 %s", "field", "version")`；
//   - 参数是「k/v 成对展开」，同时用于格式化 zh 里的 %s 和前端 i18n 里的 {k}；
//   - **只有面向用户的文案才带 code**。会进日志的诊断错误保持英文原文（规则 7），
//     不要为了「顺手加个 code」把日志文案也搬进前端字典。
//   - 前后端一致性由 `backend/uimsg_test.go` 的 TestUserFacingCodesTranslatedInFrontend
//     守着：后端出现的每个 code 必须在前端 zh / en 两份字典里都有。

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// uiMsg 是一条界面文案的引用：code + 参数。前端 `t(code, params)` 直接消费它。
type uiMsg struct {
	Code   string            `json:"code,omitempty"`
	Params map[string]string `json:"params,omitempty"`
}

// kvMap 把「k, v, k, v…」展开成 map（参数为空时返回 nil，JSON 里不出现 params）。
func kvMap(kv []string) map[string]string {
	if len(kv) == 0 {
		return nil
	}
	m := make(map[string]string, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

// formatKV 用 k/v 对填充 %s（按出现顺序取 v）。没有参数时原样返回，避免把文案里的
// 字面量百分号当成格式符。
func formatKV(zh string, kv []string) string {
	if len(kv) == 0 {
		return zh
	}
	args := make([]interface{}, 0, len(kv)/2)
	for i := 1; i < len(kv); i += 2 {
		args = append(args, kv[i])
	}
	return fmt.Sprintf(zh, args...)
}

// userError 是「面向用户的错误」：带 code/参数，中文原文作为兜底文案。
type userError struct {
	msg  uiMsg
	text string
	// busy 表示这是「有别的操作正在进行」的并发拒绝：HTTP 层据此回 409。
	busy bool
}

func (e *userError) Error() string { return e.text }

// uiErr 构造一条带 code 的用户错误。zh 里的 %s 会按 kv 的 value 顺序填充。
func uiErr(code, zh string, kv ...string) error {
	return &userError{msg: uiMsg{Code: code, Params: kvMap(kv)}, text: formatKV(zh, kv)}
}

// uiMsgOf 取出错误里的 code/参数（普通错误返回零值与 false）。
func uiMsgOf(err error) (uiMsg, bool) {
	var ue *userError
	if errors.As(err, &ue) {
		return ue.msg, true
	}
	return uiMsg{}, false
}

// isBusyErr 报告错误是否为「并发拒绝」（HTTP 层据此回 409）。
func isBusyErr(err error) bool {
	var ue *userError
	if errors.As(err, &ue) {
		return ue.busy
	}
	return false
}

// uiMsgPair 返回（原文, 引用），供状态载荷成对赋值：
//
//	st.Message, st.MessageRef = uiMsgPair("msg_install_from_mirror", "正在从 %s 下载 %s@%s",
//	    "mirror", mirror.Name, "pkg", pkg, "version", v)
func uiMsgPair(code, zh string, kv ...string) (string, *uiMsg) {
	return formatKV(zh, kv), &uiMsg{Code: code, Params: kvMap(kv)}
}

// --- 状态载荷里的「文案 + 引用」成对赋值 ---
//
// 状态文案有两个字段：人类可读的原文（error / message）与 UI 引用（errorRef / messageRef）。
// 必须成对写：只改文本忘了清引用，前端会拿旧 code 显示上一轮的文案。

// setErrFields 把错误写进「文本 + 引用」（err 为 nil 表示清空两者）。
func setErrFields(text *string, ref **uiMsg, err error) {
	if err == nil {
		*text, *ref = "", nil
		return
	}
	*text = err.Error()
	if m, ok := uiMsgOf(err); ok {
		*ref = &m
	} else {
		*ref = nil
	}
}

// setMsgFields 写一条状态文案：
//   - code 非空 → 走 i18n（zh 是回退原文，kv 是参数）；
//   - code 为空但 zh 非空 → 原文直出（例如 npm 原始输出尾行，本就不该翻译）；
//   - 都为空 → 清空文本与引用。
func setMsgFields(text *string, ref **uiMsg, code, zh string, kv ...string) {
	switch {
	case code != "":
		*text, *ref = uiMsgPair(code, zh, kv...)
	case zh != "":
		*text, *ref = zh, nil
	default:
		*text, *ref = "", nil
	}
}

// userFacingCodes 是本文件之外出现的所有 code（供跨语言守卫测试与自查使用）。
//
// 用正则扫源码而不是维护一张手写清单：漏写清单比漏写文案更难发现。
// 扫描的是 `uiErr("` / `writeErrU(..., "` / `uiMsgPair("` / `msgRef("` 后面紧跟的字符串。
var uiCodeScanPatterns = []string{"uiErr(", "writeErrU(", "uiMsgPair(", "msgRef(", "busyf("}

// sortedParamKeys 只在测试里用：让 map 的比较有稳定顺序。
func sortedParamKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// codeIsWellFormed 校验 code 的形态：小写 snake_case + 命名空间前缀。
func codeIsWellFormed(code string) bool {
	if code == "" || !(strings.HasPrefix(code, "err_") || strings.HasPrefix(code, "msg_")) {
		return false
	}
	for i := 0; i < len(code); i++ {
		c := code[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			continue
		}
		return false
	}
	return true
}
