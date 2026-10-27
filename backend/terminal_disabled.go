//go:build noterminal

// terminal_disabled.go —— 无终端构建（`-tags noterminal`）的占位实现。
//
// 终端功能的实际代码全在带 `//go:build !noterminal` 的文件里（terminal.go 的 PTY 会话、
// terminal_routes.go 的 HTTP 入口、quickcmds.go 的快捷指令）。本文件只提供其它文件会
// 引用到的符号，好让 admin.go / main.go / config.go 不必按构建变体分叉；它不做任何事，
// 也不引入 net/http 之外的依赖。

package main

import "net/http"

// terminalFeature 为 false：无终端构建不创建会话目录、也不执行快捷指令迁移。
const terminalFeature = false

// SessionManager 是空壳：AdminMux.sessions 字段在两种构建下面目一致，但这里没有任何
// 会话可管理（NewSessionManager 返回 nil，CloseAll 在 nil 接收者上安全空转）。
type SessionManager struct{}

// NewSessionManager 在无终端构建里不分配任何东西。
func NewSessionManager(*RuntimeEnv) *SessionManager { return nil }

// CloseAll 与有终端构建同名，进程退出时照常调用；这里无事可做。
func (m *SessionManager) CloseAll() {}

// migrateQuickCmdsAtStartup 在无终端构建里无事可做（快捷指令随终端一起被剔除）。
func migrateQuickCmdsAtStartup(*RuntimeEnv, *cryptor) error { return nil }

// sessionDirFor 返回空串：main.go 因此不会创建 terminal-sessions 目录。
func sessionDirFor(string) string { return "" }

// quickCmdsFileFor 返回空串：没有快捷指令可持久化。
func quickCmdsFileFor(string) string { return "" }

// terminalRoute 恒返回 false：终端相关接口在这个构建里不存在，请求会落到 SPA/404。
func (m *AdminMux) terminalRoute(http.ResponseWriter, *http.Request, string) bool { return false }
