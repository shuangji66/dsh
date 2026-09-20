package main

import "sync"

// 控制台启动阶段与「反代能否放行」的状态。
//
// 背景：反代从控制台启动的第一刻起就在监听（早于 dsh 启动，见 main.go 的
// startProxy 调用位置），因此必然存在一段“反代在监听、dsh 还没起来 / 会话凭据
// 还没换取 / 运行依赖还在安装”的窗口。这段时间里转发到 dsh 只会失败，反代改为
// 对外呈现等待页（见 proxy.go serveWaitingPage），由本文件的状态决定等待页显示
// 什么、以及什么时候自动跳转到真正的 dsh 界面。
//
// 放行的完整条件只有一个入口：reverseProxy.state() ——
//
//	boot 流水线已收尾（不是 starting/auth/deps）
//	+ dsh 端口已监听
//	+ 本代 dsh 的会话凭据已落定（DshManager.SessionSettled）
const (
	// 下面 starting/auth/deps 三个阶段都属于「启动流水线进行中」。它们只用于门禁与
	// 内部判断：等待页对用户统一显示「正在等待 DeepSeek Harness 服务就绪」，不展示
	// 这些内部细节（见 proxy.go waitingPageHTML）。
	//
	// phaseStarting：正在拉起 dsh 进程（首次启动）。
	phaseStarting = "starting"
	// phaseAuth：dsh 已启动，正在等待一次性 token 并换取 dsh 会话凭据。
	phaseAuth = "auth"
	// phaseDeps：正在安装/校正运行依赖（node-pty），首次启动可能较慢。
	phaseDeps = "deps"
	// phaseReady：启动流水线已收尾。此后能否转发仍由端口与凭据决定。
	phaseReady = "ready"
	// phaseStopped：dsh 未在运行（用户手动停止，或启动流水线之外进程已退出）。
	// 它不由 main 显式设置，而是由 reverseProxy.state() 在“端口未监听且进程不在”
	// 时推导出来，用于把“等待启动”和“已经停了”区分开；与下面的 failed/disabled
	// 一样是需要用户动手的状态，等待页会显示对应指引。
	phaseStopped = "stopped"
	// phaseFailed：dsh 启动失败，detail 为失败原因。
	phaseFailed = "failed"
	// phaseDisabled：未自动启动 dsh（HARNESS_AUTOSTART=0）。
	phaseDisabled = "disabled"
)

// proxyState 是反代对外呈现的就绪状态：phase 供等待页显示文案，ready 表示可以
// 放行（转发给 dsh）。同一份值同时用于等待页的首屏渲染与 /_ready 轮询响应，
// 两者必须一致，否则会出现“轮询说就绪 → 跳转 → 又是等待页”的抖动。
type proxyState struct {
	Phase  string `json:"phase"`
	Ready  bool   `json:"ready"`
	Detail string `json:"detail,omitempty"`
}

// bootState 保存控制台启动流水线的当前阶段（main.go 推进）。
type bootState struct {
	mu     sync.Mutex
	phase  string
	detail string
}

// newBootState 构造启动状态：控制台刚启动时流水线尚未推进，视为 starting。
func newBootState() *bootState {
	return &bootState{phase: phaseStarting}
}

// set 推进启动阶段；detail 是可选的补充说明（失败原因等），会显示在等待页上。
func (b *bootState) set(phase, detail string) {
	b.mu.Lock()
	b.phase, b.detail = phase, detail
	b.mu.Unlock()
}

// get 返回当前阶段与补充说明。
func (b *bootState) get() (string, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.phase, b.detail
}

// booting 报告启动流水线是否仍在执行。此时即使 dsh 端口已通也不放行：流水线
// 收尾（例如 node-pty 的 pnpm install 之后重启 dsh）会让刚跳转过去的界面随即
// 失效，用户只能再手动刷新一次。
func (b *bootState) booting() bool {
	p, _ := b.get()
	return p == phaseStarting || p == phaseAuth || p == phaseDeps
}
