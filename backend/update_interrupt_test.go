package main

// update_interrupt_test.go —— 「下载必须能被取消，且半开连接不能挂死」的回归测试
// （审查第 3 条）。
//
// 两条独立缺陷：
//   1. 「取消/暂停」只置位并 close 了一个没人接收的 channel，而读阻塞在 resp.Body.Read
//      里时根本不会去查原因 —— 用户点了没反应，一直挂到看门狗超时或永久挂住。
//   2. 空闲看门狗只在「读到第一个字节」后才被 reset（武装），于是「响应头到了但正文
//      一直不来」这种半开连接没有任何超时能兜住，整条更新链路（m.applying 被占）卡死
//      到进程重启。

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stalledServer 返回一个「先发响应头、再一直不给正文」的服务器：模拟半开/黑洞连接。
// 返回的 client 与 unblock 分别用于发起请求与在测试收尾时释放挂起的 handler。
func stalledServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1024")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush() // 让响应头真的到达客户端：此后对端不再给任何字节
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})
	return srv, func() { close(release) }
}

// 取消必须立刻打断阻塞中的读，而不是等看门狗或永远挂着。
func TestCancelInterruptsStalledBody(t *testing.T) {
	srv, _ := stalledServer(t)
	m := &UpdateManager{}
	dest := filepath.Join(t.TempDir(), "pkg.tar.gz")
	ctrl := newDownloadControl(true, updateKindHarness)
	route := updateRoute{label: "test", client: srv.Client()}

	done := make(chan error, 1)
	go func() {
		_, err := m.downloadOnce(route, srv.URL+"/pkg", dest, nil, ctrl, false)
		done <- err
	}()

	// 给请求一点时间进入阻塞读，再点「取消」。
	time.Sleep(150 * time.Millisecond)
	if !ctrl.stop("cancel") {
		t.Fatal("没有进行中的下载时应能生效一次取消")
	}

	select {
	case err := <-done:
		if !errors.Is(err, errUpdateCancelled) {
			t.Fatalf("取消后 downloadOnce 应返回 errUpdateCancelled，实际 %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("取消没有打断阻塞中的读（旧实现只置位、无人接收 signal）")
	}
}

// 暂停同样必须能打断阻塞中的读（半成品保留供续传）。
func TestPauseInterruptsStalledBody(t *testing.T) {
	srv, _ := stalledServer(t)
	m := &UpdateManager{}
	dest := filepath.Join(t.TempDir(), "pkg.tar.gz")
	ctrl := newDownloadControl(true, updateKindHarness)
	route := updateRoute{label: "test", client: srv.Client()}

	done := make(chan error, 1)
	go func() {
		_, err := m.downloadOnce(route, srv.URL+"/pkg", dest, nil, ctrl, true)
		done <- err
	}()

	time.Sleep(150 * time.Millisecond)
	if !ctrl.stop("pause") {
		t.Fatal("可暂停的下载应能生效一次暂停")
	}
	select {
	case err := <-done:
		if !errors.Is(err, errUpdatePaused) {
			t.Fatalf("暂停后 downloadOnce 应返回 errUpdatePaused，实际 %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("暂停没有打断阻塞中的读")
	}
}

// 看门狗必须在收到第一个字节**之前**就已武装：否则「响应头到了、正文不来」会永久
// 挂住（旧实现里 reset 只在读到数据时被调用）。
func TestIdleWatchdogArmedBeforeFirstByte(t *testing.T) {
	prev := updateIdleTimeout
	updateIdleTimeout = 300 * time.Millisecond
	t.Cleanup(func() { updateIdleTimeout = prev })

	srv, _ := stalledServer(t)
	m := &UpdateManager{}
	dest := filepath.Join(t.TempDir(), "pkg.tar.gz")
	ctrl := newDownloadControl(true, updateKindHarness)
	route := updateRoute{label: "test", client: srv.Client()}

	done := make(chan error, 1)
	go func() {
		_, err := m.downloadOnce(route, srv.URL+"/pkg", dest, nil, ctrl, false)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "传输空闲") {
			t.Fatalf("首个字节之前就应该触发空闲看门狗，实际 err=%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("首个字节之前看门狗未武装：半开连接会永久挂住（旧 bug）")
	}
}
