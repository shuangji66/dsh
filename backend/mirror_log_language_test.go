package main

// mirror_log_language_test.go —— 「npm 镜像源在日志里不许出现中文」的回归测试。
//
// 背景：日志一律英文（AGENTS 第 4 节规则 7），但镜像源的中文显示名（阿里云/腾讯云/华为云）
// 过去既进界面也进日志：`logWarn("[dsh] installing %s from %s", version, mirror.Name)`，
// 以及被 `logError(..., err)` 原样打印的错误链（错误里也枚举了中文镜像名）。
//
// 现在的分工（见 server.go 的 npmMirror）：
//   - Name（中文）只用于**界面**：进度文案、安装状态里的源标签；
//   - Slug（aliyun/tencent/huawei/npmjs）走日志与错误链，因此错误链里也不许出现中文镜像名。
//
// 两条测试各守一边：① 日志标识本身是英文；② 源码里的日志调用不再引用中文显示名。

import (
	"os"
	"strings"
	"testing"
	"unicode"
)

// TestNpmMirrorLogIdentityIsEnglish 真实镜像源的日志标识必须是英文、且与中文显示名不同。
func TestNpmMirrorLogIdentityIsEnglish(t *testing.T) {
	if len(npmMirrors) != 4 {
		t.Fatalf("镜像源应恰好四个（阿里云 / 腾讯云 / 华为云 / 官方源），实得 %d", len(npmMirrors))
	}
	for _, m := range npmMirrors {
		logID := m.mirrorLogName()
		if logID == "" {
			t.Fatalf("镜像源 %q 缺日志标识", m.Name)
		}
		if logID == m.Name {
			t.Fatalf("日志标识不能直接用中文显示名: %q", logID)
		}
		if strings.ContainsFunc(logID, func(r rune) bool { return r > unicode.MaxASCII }) {
			t.Fatalf("日志标识必须是 ASCII: %q", logID)
		}
	}
	joined := mirrorLogNames()
	if strings.ContainsFunc(joined, func(r rune) bool { return r > unicode.MaxASCII }) {
		t.Fatalf("镜像源日志列表必须是纯英文: %q", joined)
	}
	for _, m := range npmMirrors {
		if !strings.Contains(joined, m.Slug) {
			t.Fatalf("日志列表 %q 缺少 %q", joined, m.Slug)
		}
	}

	// Slug 为空的镜像源（单测里临时构造的）退回 URL 主机名，日志里也不会是空的。
	fallback := npmMirror{Name: "临时源", URL: "https://mirror.example.test/npm"}.mirrorLogName()
	if fallback != "mirror.example.test" {
		t.Fatalf("Slug 为空时应退回 URL 主机名，实得 %q", fallback)
	}
}

// TestNoChineseMirrorNameInLogCalls 源码守卫：日志调用不得引用中文标识。
//
// 两个已知的中文标识来源（都只该进界面，不该进日志）：
//   - server.go / market.go 的镜像源显示名 mirror.Name（用 mirror.mirrorLogName()）；
//   - update.go 的下载通路名 route.label（用 route.logName()）。
//
// 错误链同样受约束：这些错误会被 logError 原样打印（插件市场失败那条就是）。这里扫描的是
// 「日志调用所在行」，够精确 —— 界面文案（st.Message / DshInstallState.Mirror /
// 「下载失败（…）」错误文案）不受影响。
func TestNoChineseMirrorNameInLogCalls(t *testing.T) {
	logCalls := []string{"logInfo(", "logWarn(", "logError("}
	// 日志里禁止出现的标识表达式（都是中文来源）
	bannedExpr := []string{"mirror.Name", "route.label", "actionLog == \"\""}
	for _, file := range []string{"server.go", "market.go", "update.go"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			isLog := false
			for _, call := range logCalls {
				if strings.Contains(line, call) {
					isLog = true
					break
				}
			}
			if !isLog {
				continue
			}
			for _, expr := range bannedExpr {
				if strings.Contains(line, expr) {
					t.Errorf("%s:%d 日志里引用了中文标识 %s（镜像源用 mirror.mirrorLogName()，下载通路用 route.logName()）:\n\t%s", file, i+1, expr, strings.TrimSpace(line))
				}
			}
			for _, m := range npmMirrors {
				if strings.Contains(line, m.Name) {
					t.Errorf("%s:%d 日志里出现了中文镜像名 %q:\n\t%s", file, i+1, m.Name, strings.TrimSpace(line))
				}
			}
		}
	}
}
