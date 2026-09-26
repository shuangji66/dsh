package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeConn 是 attach/kickDetached 的最小 net.Conn 替身：把写出的帧收进缓冲，
// 记录是否被关闭。net.Pipe 是同步无缓冲的，attach 里的 ready 帧写入会直接阻塞，
// 所以这里必须用带缓冲的假连接而不是 Pipe。
type fakeConn struct {
	net.Conn // 嵌 nil 接口：只实现被调用到的方法（Write / Close / SetWriteDeadline）
	mu      sync.Mutex
	buf     bytes.Buffer
	closed  bool
}

func (f *fakeConn) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.buf.Write(p)
}

func (f *fakeConn) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeConn) SetWriteDeadline(time.Time) error { return nil }

func (f *fakeConn) bytes() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.buf.Bytes()...)
}

// TestAttachSingleMountPoint 覆盖单挂载点语义：新挂载成为唯一操作端并返回被顶掉的
// 旧连接；非操作端的输入/尺寸请求被丢弃（errNotOwner）；旧连接 detach 不影响新连接。
func TestAttachSingleMountPoint(t *testing.T) {
	s := &Session{} // hist 为 nil：attach 不回放历史，只发 ready 帧

	c1 := &fakeConn{}
	prev, err := s.attach(c1)
	if err != nil {
		t.Fatalf("first attach: %v", err)
	}
	if prev != nil {
		t.Fatalf("first attach should not return a previous conn, got %v", prev)
	}
	if !s.isOwner(c1) {
		t.Fatal("first conn should be the owner")
	}
	if !strings.Contains(string(c1.bytes()), "\x1b]ready\x07") {
		t.Fatal("attach should send the ready control frame")
	}

	c2 := &fakeConn{}
	prev, err = s.attach(c2)
	if err != nil {
		t.Fatalf("second attach: %v", err)
	}
	if prev != c1 {
		t.Fatalf("second attach should return the previous conn, got %v", prev)
	}
	if s.isOwner(c1) {
		t.Fatal("detached conn must not stay owner")
	}
	if !s.isOwner(c2) {
		t.Fatal("new conn should be the owner")
	}

	// 被顶掉的旧连接：输入与尺寸请求都在服务端丢弃（不会写到 PTY / 改 PTY 尺寸）
	if werr := s.writeInput(c1, []byte("rm -rf /")); !errors.Is(werr, errNotOwner) {
		t.Fatalf("writeInput from non-owner: got %v, want errNotOwner", werr)
	}
	if rerr := s.resize(c1, 80, 24); !errors.Is(rerr, errNotOwner) {
		t.Fatalf("resize from non-owner: got %v, want errNotOwner", rerr)
	}

	// 旧连接断开（detach）不得摘掉新连接
	s.detach(c1)
	if !s.isOwner(c2) {
		t.Fatal("detach of old conn must not detach the new owner")
	}
}

// TestKickDetached 覆盖顶号通知：先发 \x1b]detached\x07，再发 close(4001)，最后关闭。
func TestKickDetached(t *testing.T) {
	c := &fakeConn{}
	kickDetached(c)

	raw := c.bytes()
	if !strings.Contains(string(raw), oscDetached) {
		t.Fatalf("kickDetached should send %q, got %q", oscDetached, raw)
	}
	// close 帧：0x88 + 长度 + 2 字节大端状态码
	idx := bytes.Index(raw, []byte{0x80 | opClose})
	if idx < 0 || idx+4 > len(raw) {
		t.Fatalf("kickDetached should send a close frame, got %q", raw)
	}
	if code := binary.BigEndian.Uint16(raw[idx+2 : idx+4]); code != wsCloseTaken {
		t.Fatalf("close code = %d, want %d", code, wsCloseTaken)
	}
	if !c.closed {
		t.Fatal("kickDetached should close the conn")
	}
}
