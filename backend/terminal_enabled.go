//go:build !noterminal

// terminal_enabled.go —— 包含终端功能的构建（默认）在这里给出编译期开关与默认路径。

package main

import "path/filepath"

// terminalFeature 表示本次构建包含终端功能。跨文件判断（config.go 的默认会话目录、
// main.go 的启动/退出流程、测试）都读它；无终端构建里同名常量为 false，见
// terminal_disabled.go。
const terminalFeature = true

// sessionDirFor 返回终端会话的临时镜像目录（DataDir/terminal-sessions）。
func sessionDirFor(dataDir string) string {
	return filepath.Join(dataDir, "terminal-sessions")
}

// quickCmdsFileFor 返回快捷指令表的持久化路径（DataDir/quickcmds.json）。
func quickCmdsFileFor(dataDir string) string {
	return filepath.Join(dataDir, "quickcmds.json")
}
