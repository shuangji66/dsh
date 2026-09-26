package main

// terminal_limits_test.go —— 终端连接的两条资源上限（审查第 8 条）：
//   - 帧长/消息长必须有上限：帧头长度由客户端声明（64 位可达 TB 级），无校验的
//     make 会直接打爆内存；分片累积同理。
//   - 写必须带超时：客户端不读时写会阻塞，而广播/回放发生在 histMu 临界区内，
//     会把 PTY 读、/api/sessions 与新挂载一起拖死。

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

// deadlineRecConn 记录 SetWriteDeadline 的调用，并让 Write 立刻失败（模拟半开连接）。
type deadlineRecConn struct {
	net.Conn
	mu        sync.Mutex
	closed    bool
	failWrite bool
	deadlines []time.Time
}

// failWrites 让后续的 Write 立刻失败（模拟半开连接）；attach 等前置写入需要先成功。
func (c *deadlineRecConn) failWrites() {
	c.mu.Lock()
	c.failWrite = true
	c.mu.Unlock()
}

func (c *deadlineRecConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	fail := c.failWrite
	c.mu.Unlock()
	if fail {
		return 0, errors.New("模拟半开连接：写失败")
	}
	return len(p), nil
}

func (c *deadlineRecConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *deadlineRecConn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deadlines = append(c.deadlines, t)
	return nil
}

func (c *deadlineRecConn) lastDeadline() (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.deadlines) == 0 {
		return time.Time{}, false
	}
	return c.deadlines[len(c.deadlines)-1], true
}

// 写一帧必须带上写超时（否则不读数据的客户端会永久占住 histMu）。
func TestWriteFrameSetsWriteDeadline(t *testing.T) {
	c := &deadlineRecConn{}
	c.failWrites()
	before := time.Now()
	err := writeFrame(c, wsFrame(opText, []byte("x")))
	if err == nil {
		t.Fatal("写失败应返回错误（调用方据此摘掉死连接）")
	}
	dl, ok := c.lastDeadline()
	if !ok {
		t.Fatal("writeFrame 必须先设置写超时")
	}
	if !dl.After(before) {
		t.Fatalf("写超时应在未来，实际 %v", dl)
	}
	if dl.After(before.Add(connWriteTimeout + time.Second)) {
		t.Fatalf("写超时过大（%v），偏离 connWriteTimeout", dl)
	}
}

// 写不动的连接必须被摘掉并关闭（会话继续活在后台）。
func TestDropConnDetachesAndCloses(t *testing.T) {
	s := &Session{}
	c := &deadlineRecConn{}
	if _, err := s.attach(c); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if !s.isOwner(c) {
		t.Fatal("attach 后应成为操作端")
	}

	c.failWrites()
	s.dropConn(c, errors.New("write timeout"))

	if s.isOwner(c) {
		t.Fatal("写不动的连接应被解挂载")
	}
	if !c.closed {
		t.Fatal("写不动的连接应被关闭")
	}
}

// 64 位长度声明超大帧：必须在 make 之前被拒绝（旧实现在这里会尝试分配 TB 级内存）。
func TestWsReadFrameRejectsHugeFrame(t *testing.T) {
	var buf bytes.Buffer
	hdr := []byte{0x80 | opText, 0x80 | 127} // 无掩码，64 位长度
	buf.Write(hdr)
	var ln [8]byte
	binary.BigEndian.PutUint64(ln[:], 1<<40) // 1 TiB
	buf.Write(ln[:])
	// 刻意不提供任何载荷：若实现先 make 再读，就会尝试分配 1 TiB。

	done := make(chan error, 1)
	go func() {
		_, _, _, err := wsReadFrame(&buf)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, errFrameTooLarge) {
			t.Fatalf("超大帧应返回 errFrameTooLarge，实际 %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("超大帧未被拒绝（旧实现会在 make 处爆内存或阻塞读载荷）")
	}

	// 消息累积上限同样存在（与单帧上限是两道独立闸门）。
	if maxMessageBytes <= maxFrameBytes {
		t.Fatalf("消息上限应大于单帧上限，实际 frame=%d message=%d", maxFrameBytes, maxMessageBytes)
	}
}

// 正常帧（带掩码的小帧）仍要能正确解出载荷：上限不能误伤正常输入。
func TestWsReadFrameAcceptsMaskedFrame(t *testing.T) {
	payload := []byte("ls -la\r")
	mask := [4]byte{0x11, 0x22, 0x33, 0x44}
	var buf bytes.Buffer
	buf.WriteByte(0x80 | opText)
	buf.WriteByte(0x80 | byte(len(payload)))
	buf.Write(mask[:])
	masked := make([]byte, len(payload))
	for i, b := range payload {
		masked[i] = b ^ mask[i%4]
	}
	buf.Write(masked)

	op, fin, got, err := wsReadFrame(&buf)
	if err != nil {
		t.Fatalf("正常帧应解析成功: %v", err)
	}
	if op != opText || !fin {
		t.Fatalf("op=%d fin=%v, want opText/fin", op, fin)
	}
	if string(got) != string(payload) {
		t.Fatalf("载荷 = %q, want %q", got, payload)
	}
}
