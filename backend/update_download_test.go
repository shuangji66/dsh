package main

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 本文件覆盖 update.go 的下载链路改造（用户要求）：
//   - 移除 GitHub 加速源分支，只剩「代理」与「直连」两条通路；
//   - 每条通路各 2 次机会，失败后退避重试；
//   - 断点续传（Range）与暂停/取消（pause 留半成品、cancel 删半成品）；
//   - 全部失败时报 errUpdateNetworkFailed（前端据此提示检查网络/代理）。
//
// 用 httptest 起真实 HTTP 服务来测：Range、206/200/416、断流、慢速都能真实复现，
// 比给下载器打桩更有意义。

// pkgServer 是一个「像 release 资产那样」的测试服务。
type pkgServer struct {
	data       []byte
	failFirst  int           // 前 N 次请求直接 500
	shortAfter int           // >0：非 Range 请求只发这么多字节就断（模拟断流）
	chunkSize  int           // 分块大小（慢发用）
	chunkDelay time.Duration // 每块之间的延迟

	mu       sync.Mutex
	attempts int
	ranges   []string
}

func (s *pkgServer) handler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.attempts++
	attempt := s.attempts
	rng := r.Header.Get("Range")
	if rng != "" {
		s.ranges = append(s.ranges, rng)
	}
	s.mu.Unlock()

	if attempt <= s.failFirst {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	// 带 Range 的请求交给 ServeContent：它正确实现 206 / 416 与 Content-Range。
	if rng != "" {
		http.ServeContent(w, r, "pkg.tar.gz", time.Time{}, bytes.NewReader(s.data))
		return
	}
	total := len(s.data)
	limit := total
	if s.shortAfter > 0 {
		limit = s.shortAfter
	}
	w.Header().Set("Content-Length", strconv.Itoa(total))
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	chunk := s.chunkSize
	if chunk <= 0 {
		chunk = 16 << 10
	}
	for written := 0; written < limit; {
		n := chunk
		if limit-written < n {
			n = limit - written
		}
		if _, err := w.Write(s.data[written : written+n]); err != nil {
			return
		}
		written += n
		if flusher != nil {
			flusher.Flush()
		}
		if s.chunkDelay > 0 {
			time.Sleep(s.chunkDelay)
		}
	}
	// shortAfter>0 时这里直接返回：声明的 Content-Length 大于实际发送量，
	// 连接被服务端关闭，客户端会看到 unexpected EOF。
}

func (s *pkgServer) stats() (attempts int, ranges []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempts, append([]string(nil), s.ranges...)
}

// releasePlan 造一份「发布资产」（harness/dsh）下载策略：代理/直连各 2 次机会，
// 支持 Range 续传与暂停。通路由调用方给（测试里指向 httptest 服务）。
func releasePlan(routes ...updateRoute) downloadPlan {
	return downloadPlan{routes: routes, resume: true, pausable: true}
}

// marketPlan 造一份「插件市场」下载策略：只直连、失败重试，不续传、不可暂停。
func marketPlan(routes ...updateRoute) downloadPlan {
	return downloadPlan{routes: routes, resume: false, pausable: false}
}

// useRoutes 注入下载通路（绕开真实代理探测）并缩短重试退避。
func useRoutes(t *testing.T, routes []updateRoute) {
	t.Helper()
	prev := updateRoutesFn
	updateRoutesFn = func(*UpdateManager) []updateRoute { return routes }
	prevBackoff := updateRetryBackoff
	updateRetryBackoff = 10 * time.Millisecond // 测试里不需要真退避
	t.Cleanup(func() {
		updateRoutesFn = prev
		updateRetryBackoff = prevBackoff
	})
}

// directRoute 把「直连」指向测试服务。
func directRoute(srv *httptest.Server) updateRoute {
	return updateRoute{label: "直连", client: srv.Client()}
}

// failingTransport 永远失败，用来占住一条通路并统计尝试次数。
type failingTransport struct{ calls *int32 }

func (f *failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	atomic.AddInt32(f.calls, 1)
	return nil, errors.New("dial tcp 127.0.0.1:7890: connect: connection refused")
}

// proxyRouteForTest / directRouteForTest 是两条「必定失败」的通路，用来验证
// 重试次数与回退顺序。
func proxyRouteForTest(calls *int32) updateRoute {
	return updateRoute{label: "代理 127.0.0.1:7890", client: &http.Client{Transport: &failingTransport{calls: calls}}}
}

func directRouteForTest(calls *int32) updateRoute {
	return updateRoute{label: "直连", client: &http.Client{Transport: &failingTransport{calls: calls}}}
}

func testPayload(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 251)
	}
	return data
}

// --- 断点续传 ---

func TestDownloadResumesFromPartialFile(t *testing.T) {
	data := testPayload(256 << 10)
	stats := &pkgServer{data: data}
	srv := httptest.NewServer(http.HandlerFunc(stats.handler))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "pkg.tar.gz")
	offset := len(data) / 3
	if err := os.WriteFile(dest, data[:offset], 0o644); err != nil {
		t.Fatal(err)
	}
	useRoutes(t, []updateRoute{directRoute(srv)})

	var lastTotal int64
	n, err := (&UpdateManager{}).downloadToFile(srv.URL+"/pkg.tar.gz", dest,
		func(downloaded, total int64) { lastTotal = total }, nil, releasePlan(directRoute(srv)))
	if err != nil {
		t.Fatalf("续传失败: %v", err)
	}
	if n != int64(len(data)) {
		t.Fatalf("下载字节 = %d, want %d", n, len(data))
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("续传后内容与源不一致（半成品没被正确接上）")
	}
	if lastTotal != int64(len(data)) {
		t.Fatalf("进度上报的总量 = %d, want %d", lastTotal, len(data))
	}
	attempts, ranges := stats.stats()
	if attempts != 1 {
		t.Fatalf("本地已有 1/3 数据时应一次请求搞定，实际请求 %d 次", attempts)
	}
	if len(ranges) != 1 || ranges[0] != fmt.Sprintf("bytes=%d-", offset) {
		t.Fatalf("首个请求的 Range = %v, want bytes=%d-", ranges, offset)
	}
}

// --- 通路重试策略：代理 2 次 → 直连 2 次 → 全部失败 ---

func TestDownloadFailsAllRoutesWithNetworkHint(t *testing.T) {
	var proxyCalls, directCalls int32
	useRoutes(t, []updateRoute{proxyRouteForTest(&proxyCalls), directRouteForTest(&directCalls)})

	dest := filepath.Join(t.TempDir(), "pkg.tar.gz")
	_, err := (&UpdateManager{}).downloadToFile("https://example.invalid/pkg.tar.gz", dest, nil, nil,
		releasePlan(proxyRouteForTest(&proxyCalls), directRouteForTest(&directCalls)))
	if err == nil {
		t.Fatal("两条通路都失败时应当返回错误")
	}
	if !errors.Is(err, errUpdateNetworkFailed) {
		t.Fatalf("错误应可识别为网络失败，实际: %v", err)
	}
	if !bytes.Contains([]byte(err.Error()), []byte("请检查网络或代理")) {
		t.Fatalf("错误信息缺少「请检查网络或代理」提示: %v", err)
	}
	if got := atomic.LoadInt32(&proxyCalls); got != 2 {
		t.Fatalf("代理通路尝试 %d 次, want 2", got)
	}
	if got := atomic.LoadInt32(&directCalls); got != 2 {
		t.Fatalf("直连通路尝试 %d 次, want 2", got)
	}
}

func TestDownloadFallsBackFromProxyToDirect(t *testing.T) {
	data := testPayload(64 << 10)
	srv := httptest.NewServer(http.HandlerFunc((&pkgServer{data: data}).handler))
	defer srv.Close()

	var proxyCalls int32
	useRoutes(t, []updateRoute{proxyRouteForTest(&proxyCalls), directRoute(srv)})

	dest := filepath.Join(t.TempDir(), "pkg.tar.gz")
	n, err := (&UpdateManager{}).downloadToFile(srv.URL+"/pkg.tar.gz", dest, nil, nil,
		releasePlan(proxyRouteForTest(&proxyCalls), directRoute(srv)))
	if err != nil {
		t.Fatalf("代理失败后应回退直连成功: %v", err)
	}
	if n != int64(len(data)) {
		t.Fatalf("下载字节 = %d, want %d", n, len(data))
	}
	if got := atomic.LoadInt32(&proxyCalls); got != 2 {
		t.Fatalf("代理应先用满 2 次机会, 实际 %d", got)
	}
}

func TestDownloadRetriesAfterShortTransfer(t *testing.T) {
	data := testPayload(192 << 10)
	stats := &pkgServer{data: data, shortAfter: 96 << 10}
	srv := httptest.NewServer(http.HandlerFunc(stats.handler))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "pkg.tar.gz")
	useRoutes(t, []updateRoute{directRoute(srv)})

	n, err := (&UpdateManager{}).downloadToFile(srv.URL+"/pkg.tar.gz", dest, nil, nil,
		releasePlan(directRoute(srv)))
	if err != nil {
		t.Fatalf("断流后应重试并续传成功: %v", err)
	}
	if n != int64(len(data)) {
		t.Fatalf("下载字节 = %d, want %d", n, len(data))
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatal("重试续传后内容与源不一致")
	}
	// 必须真的重试过，并且第二次请求从断点处续传。
	attempts, ranges := stats.stats()
	if attempts < 2 {
		t.Fatalf("断流后应重试，实际只请求 %d 次", attempts)
	}
	if len(ranges) == 0 || ranges[len(ranges)-1] != fmt.Sprintf("bytes=%d-", 96<<10) {
		t.Fatalf("重试请求的 Range = %v, want bytes=%d-", ranges, 96<<10)
	}
}

// --- 暂停与取消 ---

func newSlowServer(t *testing.T, data []byte) (*httptest.Server, *pkgServer) {
	t.Helper()
	s := &pkgServer{data: data, chunkSize: 8 << 10, chunkDelay: 5 * time.Millisecond}
	srv := httptest.NewServer(http.HandlerFunc(s.handler))
	t.Cleanup(srv.Close)
	return srv, s
}

func TestDownloadPauseKeepsPartialThenResumes(t *testing.T) {
	data := testPayload(512 << 10)
	srv, stats := newSlowServer(t, data)
	useRoutes(t, []updateRoute{directRoute(srv)})
	m := &UpdateManager{}

	dest := filepath.Join(t.TempDir(), "pkg.tar.gz")
	ctrl := newDownloadControl(true, updateKindHarness)
	// 进度过半就暂停：用进度回调驱动，时序确定，不靠 sleep 猜。
	progress := func(downloaded, total int64) {
		if downloaded > int64(len(data))/4 {
			ctrl.stop("pause")
		}
	}
	_, err := m.downloadToFile(srv.URL+"/pkg.tar.gz", dest, progress, ctrl, releasePlan(directRoute(srv)))
	if !errors.Is(err, errUpdatePaused) {
		t.Fatalf("应当返回 errUpdatePaused，实际: %v", err)
	}
	partial := partialSize(dest)
	if partial <= 0 || partial >= int64(len(data)) {
		t.Fatalf("暂停后应保留部分字节，实际 %d / %d", partial, len(data))
	}

	// 继续下载：应当带 Range 从半成品之后接上，并最终与源一致。
	ctrl2 := newDownloadControl(true, updateKindHarness)
	n, err := m.downloadToFile(srv.URL+"/pkg.tar.gz", dest, nil, ctrl2, releasePlan(directRoute(srv)))
	if err != nil {
		t.Fatalf("续传失败: %v", err)
	}
	if n != int64(len(data)) {
		t.Fatalf("续传后总字节 = %d, want %d", n, len(data))
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatal("续传后内容与源不一致")
	}
	_, ranges := stats.stats()
	if len(ranges) == 0 || ranges[len(ranges)-1] != fmt.Sprintf("bytes=%d-", partial) {
		t.Fatalf("续传请求的 Range = %v, want bytes=%d-", ranges, partial)
	}
}

func TestDownloadCancelRemovesPartial(t *testing.T) {
	data := testPayload(512 << 10)
	srv, _ := newSlowServer(t, data)
	useRoutes(t, []updateRoute{directRoute(srv)})

	dest := filepath.Join(t.TempDir(), "pkg.tar.gz")
	ctrl := newDownloadControl(true, updateKindHarness)
	progress := func(downloaded, total int64) {
		if downloaded > int64(len(data))/4 {
			ctrl.stop("cancel")
		}
	}
	_, err := (&UpdateManager{}).downloadToFile(srv.URL+"/pkg.tar.gz", dest, progress, ctrl,
		releasePlan(directRoute(srv)))
	if !errors.Is(err, errUpdateCancelled) {
		t.Fatalf("应当返回 errUpdateCancelled，实际: %v", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("取消后半成品应被删除，stat err = %v", statErr)
	}
}

// 本地半成品比远端还长（远端换了资产 / 半成品损坏）时服务端会回 416：
// 绝不能把这份坏文件当「已下完」，必须清理半成品并重新完整下载。
func TestDownloadRecoversFromStalePartialOn416(t *testing.T) {
	data := testPayload(64 << 10)
	stats := &pkgServer{data: data}
	srv := httptest.NewServer(http.HandlerFunc(stats.handler))
	defer srv.Close()
	useRoutes(t, []updateRoute{directRoute(srv)})

	dest := filepath.Join(t.TempDir(), "pkg.tar.gz")
	// 造一个比远端更大的「半成品」（内容也不对）。
	if err := os.WriteFile(dest, testPayload(128<<10), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := (&UpdateManager{}).downloadToFile(srv.URL+"/pkg.tar.gz", dest, nil, nil,
		releasePlan(directRoute(srv)))
	if err != nil {
		t.Fatalf("416 后应清理半成品并重新下载成功: %v", err)
	}
	if n != int64(len(data)) {
		t.Fatalf("下载字节 = %d, want %d", n, len(data))
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatal("416 之后文件内容应为远端的完整内容")
	}
}

// 败在本地磁盘（写不进去）时不该提示「检查网络或代理」——修网络没用。
func TestDownloadLocalIOFailureIsNotReportedAsNetwork(t *testing.T) {
	data := testPayload(32 << 10)
	stats := &pkgServer{data: data}
	srv := httptest.NewServer(http.HandlerFunc(stats.handler))
	defer srv.Close()
	useRoutes(t, []updateRoute{directRoute(srv)})

	// 目标目录不存在 → OpenFile 报 PathError（HTTP 侧其实是通的）。
	dest := filepath.Join(t.TempDir(), "no-such-dir", "pkg.tar.gz")
	_, err := (&UpdateManager{}).downloadToFile(srv.URL+"/pkg.tar.gz", dest, nil, nil,
		releasePlan(directRoute(srv)))
	if err == nil {
		t.Fatal("写不进去时应当返回错误")
	}
	if errors.Is(err, errUpdateNetworkFailed) {
		t.Fatalf("本地磁盘失败不应被归类为网络失败（会误导用户去查代理）: %v", err)
	}
	if bytes.Contains([]byte(err.Error()), []byte("请检查网络或代理")) {
		t.Fatalf("本地磁盘失败不该出现「请检查网络或代理」: %v", err)
	}
}

// --- 插件市场的独立策略：只直连、不续传、不暂停 ---

// 即使开了代理，市场也必须只走直连（用户要求：市场不走代理）。
func TestMarketPlanIsDirectOnlyEvenWithProxy(t *testing.T) {
	prevCfg := GetConfig()
	prevProbe := proxyReachableFn
	initConfig(&AppConfig{ProxyEnabled: true, ProxyUpdate: true, ProxyAddr: "http://127.0.0.1:7890"})
	proxyReachableFn = func(string) bool { return true }
	t.Cleanup(func() {
		initConfig(&prevCfg)
		proxyReachableFn = prevProbe
	})
	m := &UpdateManager{}

	market := m.downloadPlanFor(updateKindMarket)
	if len(market.routes) != 1 || market.routes[0].label != "直连" {
		t.Fatalf("市场只应有直连通路，实际 %+v", market.routes)
	}
	if market.resume || market.pausable {
		t.Fatalf("市场不应支持续传/暂停，实际 resume=%v pausable=%v", market.resume, market.pausable)
	}

	// 对照：发布资产（harness/dsh）仍然是「代理 + 直连」且支持续传/暂停。
	release := m.downloadPlanFor(updateKindHarness)
	if len(release.routes) != 2 || release.routes[0].label == "直连" || release.routes[1].label != "直连" {
		t.Fatalf("发布资产应为「代理在前、直连在后」，实际 %+v", release.routes)
	}
	if !release.resume || !release.pausable {
		t.Fatalf("发布资产应支持续传与暂停，实际 resume=%v pausable=%v", release.resume, release.pausable)
	}
	if dshPlan := m.downloadPlanFor(updateKindDsh); !dshPlan.resume || !dshPlan.pausable {
		t.Fatal("dsh 应走与 harness 相同的策略")
	}
}

// 市场不续传：本地即使有残留，也必须从头下（不发 Range），且最终内容与源一致。
func TestMarketDownloadDoesNotResume(t *testing.T) {
	data := testPayload(128 << 10)
	stats := &pkgServer{data: data}
	srv := httptest.NewServer(http.HandlerFunc(stats.handler))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "market.tar.gz")
	// 造一份「上一轮的残留」（内容不对，长度也不对）。
	if err := os.WriteFile(dest, testPayload(40<<10), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := (&UpdateManager{}).downloadToFile(srv.URL+"/market.tar.gz", dest, nil, nil,
		marketPlan(directRoute(srv)))
	if err != nil {
		t.Fatalf("下载失败: %v", err)
	}
	if n != int64(len(data)) {
		t.Fatalf("下载字节 = %d, want %d", n, len(data))
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatal("不续传时必须从头覆盖，文件内容应与源一致")
	}
	attempts, ranges := stats.stats()
	if len(ranges) != 0 {
		t.Fatalf("市场下载不应发送 Range，实际 %v", ranges)
	}
	if attempts != 1 {
		t.Fatalf("一次就该成功，实际请求 %d 次", attempts)
	}
}

// 市场下载失败后不留半成品（没有「下次续传」这回事）。
func TestMarketDownloadLeavesNoPartialOnFailure(t *testing.T) {
	var calls int32
	dest := filepath.Join(t.TempDir(), "market.tar.gz")
	_, err := (&UpdateManager{}).downloadToFile("https://example.invalid/market.tgz", dest, nil, nil,
		marketPlan(directRouteForTest(&calls)))
	if err == nil {
		t.Fatal("应当返回错误")
	}
	if !errors.Is(err, errUpdateNetworkFailed) {
		t.Fatalf("错误应可识别为网络失败: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("市场每条通路仍应重试 2 次，实际 %d", got)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("失败后不应留下半成品，stat err = %v", statErr)
	}
}

// 不支持的暂停请求应被忽略（否则市场下载会被前端误暂停）。
func TestDownloadControlIgnoresPauseWhenNotPausable(t *testing.T) {
	np := newDownloadControl(false, updateKindMarket)
	if np.stop("pause") {
		t.Fatal("不可暂停的下载不应接受暂停")
	}
	if stop, _ := np.stopped(); stop {
		t.Fatal("被忽略的暂停不应改变中断状态")
	}
	if !np.stop("cancel") {
		t.Fatal("取消在任何下载上都应生效")
	}

	p := newDownloadControl(true, updateKindHarness)
	if !p.stop("pause") {
		t.Fatal("可暂停的下载应接受暂停")
	}
	if stop, reason := p.stopped(); !stop || reason != "pause" {
		t.Fatalf("状态 = (%v,%q), want (true,pause)", stop, reason)
	}
	if p.stop("cancel") {
		t.Fatal("已有中断原因时，后到的请求不应生效")
	}
}

// --- 通路构造 ---

func TestUpdateClientsFallsBackToDirectWhenProxyUnreachable(t *testing.T) {
	prevCfg := GetConfig()
	prevProbe := proxyReachableFn
	initConfig(&AppConfig{ProxyEnabled: true, ProxyUpdate: true, ProxyAddr: "http://127.0.0.1:7890"})
	proxyReachableFn = func(string) bool { return false }
	t.Cleanup(func() {
		initConfig(&prevCfg)
		proxyReachableFn = prevProbe
	})

	routes := (&UpdateManager{}).updateClients()
	if len(routes) != 1 || routes[0].label != "直连" {
		t.Fatalf("代理不可达时应当只有直连一条通路，实际 %+v", routes)
	}

	proxyReachableFn = func(string) bool { return true }
	routes = (&UpdateManager{}).updateClients()
	if len(routes) != 2 || routes[0].label == "直连" || routes[1].label != "直连" {
		t.Fatalf("代理可达时应为「代理在前、直连在后」两条通路，实际 %+v", routes)
	}
}

// 「代理dsh」（ProxyEnabled）不再影响更新：「代理更新」（ProxyUpdate）才是更新
// 是否先从代理走的开关，两者相互独立。
func TestUpdateProxyControlledByProxyUpdateSwitch(t *testing.T) {
	prevCfg := GetConfig()
	prevProbe := proxyReachableFn
	t.Cleanup(func() {
		initConfig(&prevCfg)
		proxyReachableFn = prevProbe
	})
	// 代理地址探测恒为可达：通路条数只取决于开关，不取决于网络。
	proxyReachableFn = func(string) bool { return true }

	directOnly := func(routes []updateRoute) bool {
		return len(routes) == 1 && routes[0].label == "直连"
	}
	proxyFirst := func(routes []updateRoute) bool {
		return len(routes) == 2 && routes[0].label != "直连" && routes[1].label == "直连"
	}
	hasProxyTransport := func(c *http.Client) bool {
		tr, ok := c.Transport.(*http.Transport)
		return ok && tr.Proxy != nil
	}

	// 只开「代理dsh」：dsh 出网走代理，但更新（下载通路 + 元数据客户端）仍走直连。
	initConfig(&AppConfig{ProxyEnabled: true, ProxyAddr: "http://127.0.0.1:7890"})
	if routes := (&UpdateManager{}).updateClients(); !directOnly(routes) {
		t.Fatalf("只开「代理dsh」时更新应只走直连，实际 %+v", routes)
	}
	if hasProxyTransport((&UpdateManager{}).httpClientForUpdate()) {
		t.Fatal("只开「代理dsh」时元数据拉取不应走代理")
	}

	// 只开「代理更新」：dsh 出网直连，更新优先走代理、直连兜底。
	initConfig(&AppConfig{ProxyUpdate: true, ProxyAddr: "http://127.0.0.1:7890"})
	if routes := (&UpdateManager{}).updateClients(); !proxyFirst(routes) {
		t.Fatalf("开「代理更新」后应为「代理在前、直连在后」，实际 %+v", routes)
	}
	if !hasProxyTransport((&UpdateManager{}).httpClientForUpdate()) {
		t.Fatal("开「代理更新」且代理可达时元数据拉取应走代理")
	}

	// 「代理更新」开着但代理地址为空：静默退回直连，不报错。
	initConfig(&AppConfig{ProxyUpdate: true})
	if routes := (&UpdateManager{}).updateClients(); !directOnly(routes) {
		t.Fatalf("代理地址为空时应只走直连，实际 %+v", routes)
	}
}

// --- 更新日志按目标分开（harness / dsh / market） ---

// 标签映射：三类更新各自一个标签，未知目标退回通用的 [update]。
func TestUpdateLogTag(t *testing.T) {
	cases := []struct {
		in   updateKind
		want string
	}{
		{updateKindHarness, "[harness]"},
		{updateKindDsh, "[dsh]"},
		{updateKindMarket, "[market]"},
		{"", "[update]"},
		{"unknown", "[update]"},
	}
	for _, tc := range cases {
		if got := updateLogTag(tc.in); got != tc.want {
			t.Fatalf("updateLogTag(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// 待安装包名（kind+版本）反推目标：启动清理与“删除更新包”的日志据此归类。
func TestPendingKind(t *testing.T) {
	cases := map[string]updateKind{
		"harness-1.2.6.tar.gz":  updateKindHarness,
		"dsh-0.1.7-rc.3.tar.gz": updateKindDsh,
		"market-1.66.1.tar.gz":  updateKindMarket,
		"something-else.tgz":    "",
	}
	for name, want := range cases {
		if got := pendingKind(name); got != want {
			t.Fatalf("pendingKind(%q) = %q, want %q", name, got, want)
		}
	}
}

// 底层下载函数的日志必须带上所属目标（plan.kind 一路传下来），
// 这样 dsh 服务更新的下载日志不会混进 harness 的那一堆里。
func TestDownloadLogsTaggedByKind(t *testing.T) {
	out, _ := captureLogs(t)
	data := testPayload(8 << 10)
	srv := httptest.NewServer(http.HandlerFunc((&pkgServer{data: data}).handler))
	defer srv.Close()

	plan := releasePlan(directRoute(srv))
	plan.kind = updateKindDsh
	dest := filepath.Join(t.TempDir(), "dsh-pkg.tar.gz")
	if _, err := (&UpdateManager{}).downloadToFile(srv.URL+"/dsh-pkg.tar.gz", dest, nil, nil, plan); err != nil {
		t.Fatalf("下载失败: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "[dsh] download finished via") {
		t.Fatalf("下载日志缺少 [dsh] 标签:\n%s", got)
	}
	if strings.Contains(got, "[harness]") || strings.Contains(got, "[update] download") {
		t.Fatalf("下载日志混入了其它目标的标签:\n%s", got)
	}
}

// 取消/暂停的日志同样按目标打标签（downloadControl 记住 kind）。
func TestPauseLogTaggedByKind(t *testing.T) {
	out, _ := captureLogs(t)
	m := &UpdateManager{}
	ctrl := newDownloadControl(true, updateKindHarness)
	m.ctrlMu.Lock()
	m.ctrl = ctrl
	m.ctrlMu.Unlock()
	if !m.PauseUpdate() {
		t.Fatal("进行中的可暂停下载应接受暂停")
	}
	if got := out.String(); !strings.Contains(got, "[harness] download pause requested") {
		t.Fatalf("暂停日志缺少 [harness] 标签:\n%s", got)
	}
}
