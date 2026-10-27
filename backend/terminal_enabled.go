//go:build !noterminal

// terminal_enabled.go —— 包含终端功能的构建（默认）在这里给出编译期开关与默认路径。

package main

import "path/filepath"

// terminalFeature 表示本次构建是否包含终端功能。
//
// 生产路径不靠它分叉：会话目录/快捷指令路径分别由本文件的 sessionDirFor /
// quickCmdsFileFor 给出（无终端构建返回空串），路由由 terminalRoute 的空实现兜住。
// 它留给测试断言两种构建的默认值差异（见 data_dir_test.go）；无终端构建里同名常量
// 为 false（见 terminal_disabled.go）。
const terminalFeature = true

// sessionDirFor 返回终端会话的临时镜像目录（DataDir/terminal-sessions）。
func sessionDirFor(dataDir string) string {
	return filepath.Join(dataDir, "terminal-sessions")
}

// quickCmdsFileFor 返回快捷指令表的持久化路径（DataDir/quickcmds.json）。
func quickCmdsFileFor(dataDir string) string {
	return filepath.Join(dataDir, "quickcmds.json")
}
