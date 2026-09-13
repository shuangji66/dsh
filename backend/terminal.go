package main

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
)

// 会话历史回放/读取的大小上限：避免把无限增长的临时文件全部塞进单次 WebSocket
// 或 JSON 响应。仅回放最近 maxHistoryBytes 的内容即可满足“恢复会话历史”。
const maxHistoryBytes = 4 * 1024 * 1024 // 4MB

// wsChunkSize 是历史回放时单帧的最大字节数。
const wsChunkSize = 32 * 1024 // 32KB

// Session 是一个独立的 PTY 终端会话。输出实时镜像到临时历史文件（应用停止时
// 整目录清除），浏览器断开只“解挂载”不杀会话，刷新后凭 id 重新挂载并回放历史。
type Session struct {
	id    string
	renv  *RuntimeEnv
	cmd   *exec.Cmd
	pty   *os.File
	hist  *os.File
	start time.Time

	// histMu 串行化“追加历史 + 广播到已挂载连接”：回放历史与实时追加在同一把
	// 锁内完成，保证不存在重复或丢失的字节。
	histMu sync.Mutex
	conn   net.Conn
	connMu sync.Mutex

	exited bool
	mu     sync.Mutex
	closed bool
}

func (s *Session) markExited() {
	s.mu.Lock()
	s.exited = true
	s.mu.Unlock()
}

func (s *Session) isExited() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exited
}

func (s *Session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// pump 持续读取 PTY 输出：追加到历史文件，并向已挂载的连接实时广播。
// 同一把 histMu 保证“文件”与“连接”两路输出顺序一致。
func (s *Session) pump() {
	buf := make([]byte, 8192)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			s.histMu.Lock()
			if s.hist != nil {
				s.hist.Write(chunk)
			}
			s.connMu.Lock()
			c := s.conn
			s.connMu.Unlock()
			if c != nil {
				c.Write(wsFrame(opText, chunk))
			}
			s.histMu.Unlock()
		}
		if err != nil {
			break
		}
	}
	s.markExited()
	// 进程退出后补一条提示帧与退出控制帧（若还有连接在挂载）
	s.histMu.Lock()
	note := []byte("\r\n\x1b[31m[process exited]\x1b[0m\r\n")
	if s.hist != nil {
		s.hist.Write(note)
	}
	s.connMu.Lock()
	c := s.conn
	s.connMu.Unlock()
	if c != nil {
		c.Write(wsFrame(opText, note))
		c.Write(wsFrame(opText, []byte("\x1b]exit\x07")))
	}
	s.histMu.Unlock()
}

// attach 把连接挂载到会话：先回放历史文件内容（上限 maxHistoryBytes，分帧发送），
// 再发送 ready 标记，最后接管实时流。整段在 histMu 内完成，防止新增输出既被回放
// 又被实时广播造成重复。
func (s *Session) attach(c net.Conn) error {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	if s.hist != nil {
		size, err := s.hist.Seek(0, io.SeekEnd)
		if err == nil {
			start := int64(0)
			if size > maxHistoryBytes {
				start = size - maxHistoryBytes
			}
			if _, err := s.hist.Seek(start, io.SeekStart); err == nil {
				chunk := make([]byte, wsChunkSize)
				for {
					n, rerr := s.hist.Read(chunk)
					if n > 0 {
						if _, werr := c.Write(wsFrame(opText, chunk[:n])); werr != nil {
							return werr
						}
					}
					if rerr != nil {
						break
					}
				}
			}
		}
	}
	if _, err := c.Write(wsFrame(opText, []byte("\x1b]ready\x07"))); err != nil {
		return err
	}
	s.connMu.Lock()
	s.conn = c
	s.connMu.Unlock()
	return nil
}

// detach 解除当前连接挂载（不杀会话，会话继续运行并写入历史文件）。
func (s *Session) detach(c net.Conn) {
	s.connMu.Lock()
	if s.conn == c {
		s.conn = nil
	}
	s.connMu.Unlock()
}

// writeInput 把客户端输入写入 PTY。
func (s *Session) writeInput(p []byte) error {
	_, err := s.pty.Write(p)
	return err
}

// resize 调整 PTY 尺寸。
func (s *Session) resize(cols, rows uint16) error {
	return pty.Setsize(s.pty, &pty.Winsize{Cols: cols, Rows: rows})
}

// close 终止进程并清理资源（删除历史文件）。调用方负责从管理器移除。
func (s *Session) close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	var firstErr error
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
		if err := s.cmd.Wait(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.pty != nil {
		s.pty.Close()
	}
	if s.hist != nil {
		name := s.hist.Name()
		s.hist.Close()
		if err := os.Remove(name); err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// SessionManager 管理全部活动会话。
type SessionManager struct {
	renv     *RuntimeEnv
	mu       sync.Mutex
	sessions map[string]*Session
}

func NewSessionManager(renv *RuntimeEnv) *SessionManager {
	return &SessionManager{renv: renv, sessions: map[string]*Session{}}
}

// terminalEnv builds the environment for the bash session: current app user's
// PATH and HOME are captured from the runtime; proxy settings follow config.
func terminalEnv(renv *RuntimeEnv) []string {
	cfg := GetConfig()
	env := []string{
		"HOME=" + renv.Home,
		"PATH=" + renv.Path,
		"TERM=xterm-256color",
		"LANG=" + localeLang(renv.Lang),
		"COLORTERM=truecolor",
		"PWD=" + renv.Home,
		"PS1=\\u@\\h:\\w\\$ ",
		"SHELL=/bin/bash",
	}
	if renv.PnpmHome != "" {
		env = append(env, "PNPM_HOME="+renv.PnpmHome)
	}
	if cfg.ProxyEnabled && cfg.ProxyAddr != "" {
		env = append(env, "http_proxy="+cfg.ProxyAddr, "https_proxy="+cfg.ProxyAddr, "HTTP_PROXY="+cfg.ProxyAddr, "HTTPS_PROXY="+cfg.ProxyAddr)
	}
	env = append(env, os.Environ()...)
	return env
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return strings.ReplaceAll(time.Now().Format("20060102150405.000000000"), ".", "")
}

// sessionDir returns the temporary mirror directory (from env, default in TRIM_PKGVAR).
func (m *SessionManager) sessionDir() string {
	if m.renv != nil && m.renv.SessionDir != "" {
		return m.renv.SessionDir
	}
	return envOr("HARNESS_SESSION_DIR", filepath.Join(os.Getenv("TRIM_PKGVAR"), "terminal-sessions"))
}

// create 新建会话：生成 id、创建临时历史文件、启动 PTY 与 pump 协程。创建失败时
// 返回错误并保证不残留文件。调用方随后应把 id 通过控制帧告知前端。
func (m *SessionManager) create() (*Session, error) {
	dir := m.sessionDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	id := newID()
	fname := filepath.Join(dir, id+".log")
	// 必须以可读可写打开：pump 追加写（O_APPEND），attach 回放读。
	hist, err := os.OpenFile(fname, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("/bin/bash")
	cmd.Dir = m.renv.Home
	cmd.Env = terminalEnv(m.renv)
	f, err := pty.Start(cmd)
	if err != nil {
		hist.Close()
		os.Remove(fname)
		return nil, err
	}
	s := &Session{
		id:    id,
		renv:  m.renv,
		cmd:   cmd,
		pty:   f,
		hist:  hist,
		start: time.Now(),
	}
	go s.pump()
	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	logger().Printf("[terminal] session %s started", id)
	return s, nil
}

func (m *SessionManager) get(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

// list 返回会话摘要列表（供前端启动时恢复会话）。
func (m *SessionManager) list() []sessionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]sessionInfo, 0, len(m.sessions))
	for id, s := range m.sessions {
		info := sessionInfo{
			ID:      id,
			Created: s.start.Format(time.RFC3339),
			Exited:  s.isExited(),
		}
		s.histMu.Lock()
		if s.hist != nil {
			if st, err := s.hist.Stat(); err == nil {
				info.Size = st.Size()
			}
		}
		s.histMu.Unlock()
		out = append(out, info)
	}
	return out
}

// history 读取会话历史文件内容（上限 maxHistoryBytes，取末尾部分）。
func (m *SessionManager) history(id string) ([]byte, error) {
	s, ok := m.get(id)
	if !ok {
		return nil, errors.New("session not found")
	}
	s.histMu.Lock()
	defer s.histMu.Unlock()
	if s.hist == nil {
		return []byte{}, nil
	}
	size, err := s.hist.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	start := int64(0)
	if size > maxHistoryBytes {
		start = size - maxHistoryBytes
	}
	if _, err := s.hist.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(s.hist)
}

// clearHistory 清空指定会话的历史临时文件（用于前端「清屏」同步清除历史，
// 重连/刷新后不再回放旧内容）。
func (m *SessionManager) clearHistory(id string) error {
	s, ok := m.get(id)
	if !ok {
		return errors.New("session not found")
	}
	s.histMu.Lock()
	defer s.histMu.Unlock()
	if s.hist == nil {
		return nil
	}
	return s.hist.Truncate(0)
}

// closeByID 关闭指定会话并移除。
func (m *SessionManager) closeByID(id string) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if !ok {
		return errors.New("session not found")
	}
	err := s.close()
	logger().Printf("[terminal] session %s closed", id)
	return err
}

// CloseAll 终止所有会话（进程 + 文件），随后由 main 删除整个临时目录。
func (m *SessionManager) CloseAll() {
	m.mu.Lock()
	items := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		items = append(items, s)
	}
	m.sessions = map[string]*Session{}
	m.mu.Unlock()
	for _, s := range items {
		s.close()
	}
}

// terminalHandler serves the terminal WebSocket on the admin unix socket.
type terminalHandler struct {
	mgr *SessionManager
}

// ServeHTTP hijacks the connection, upgrades to WebSocket, then either creates
// a new session (id empty → 先发 \x1b]id;<id>\x07 控制帧) or attaches to an
// existing one (回放历史 + \x1b]ready\x07 后进入实时流)。
// 浏览器断开只解挂载；会话继续运行并写入历史临时文件，刷新后可重新挂载。
func (t *terminalHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		http.Error(w, "missing Sec-WebSocket-Key", http.StatusBadRequest)
		return
	}
	proto := r.Header.Get("Sec-WebSocket-Protocol")
	conn, brw, err := hj.Hijack()
	if err != nil {
		return
	}
	protoHdr := ""
	if proto != "" {
		protoHdr = "\r\nSec-WebSocket-Protocol: " + proto
	}
	conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + wsAccept(key) + protoHdr + "\r\n\r\n"))
	_ = brw.Flush()

	id := r.URL.Query().Get("id")
	if id == "" {
		// 新建会话
		s, cerr := t.mgr.create()
		if cerr != nil {
			conn.Write(wsFrame(opText, []byte("\r\n\x1b[31mcreate session failed: "+cerr.Error()+"\x1b[0m\r\n")))
			conn.Write(wsFrame(opClose, nil))
			conn.Close()
			return
		}
		conn.Write(wsFrame(opText, []byte("\x1b]id;"+s.id+"\x07")))
		if aerr := s.attach(conn); aerr != nil {
			conn.Close()
			return
		}
		id = s.id
	} else {
		s, ok := t.mgr.get(id)
		if !ok {
			conn.Write(wsFrame(opText, []byte("\r\n\x1b[31msession not found: "+id+"\x1b[0m\r\n")))
			conn.Write(wsFrame(opClose, nil))
			conn.Close()
			return
		}
		if aerr := s.attach(conn); aerr != nil {
			conn.Close()
			return
		}
	}

	// 读取客户端帧：普通数据写入 PTY；resize/ping 为 OSC 控制消息不进 PTY。
	var msgBuf []byte
	for {
		op, fin, payload, rerr := wsReadFrame(brw)
		if rerr != nil {
			break
		}
		switch op {
		case opText, opBin, opCont:
			if op != opCont {
				msgBuf = msgBuf[:0]
			}
			msgBuf = append(msgBuf, payload...)
			if !fin {
				continue
			}
			s := string(msgBuf)
			if strings.HasPrefix(s, "\x1b]resize;") && strings.HasSuffix(s, "\x07") {
				trimmed := strings.TrimSuffix(strings.TrimPrefix(s, "\x1b]resize;"), "\x07")
				parts := strings.Split(trimmed, ";")
				if len(parts) == 2 {
					cols, err1 := strconv.Atoi(parts[0])
					rows, err2 := strconv.Atoi(parts[1])
					if err1 == nil && err2 == nil && cols > 0 && rows > 0 {
						if sess, ok := t.mgr.get(id); ok {
							if err := sess.resize(uint16(cols), uint16(rows)); err != nil {
								logger().Printf("[terminal] resize error: %v", err)
							}
						}
					}
				}
			} else if strings.HasPrefix(s, "\x1b]ping") && strings.HasSuffix(s, "\x07") {
				// 心跳：仅保持连接不被代理/NAT 空闲超时断开，不写入 PTY
			} else {
				if sess, ok := t.mgr.get(id); ok {
					sess.writeInput(msgBuf)
				}
			}
		case opPing:
			conn.Write(wsFrame(opPong, payload))
		case opClose:
			goto done
		}
	}
done:
	// 只解除挂载，不杀会话（会话继续运行并写入历史文件）
	if sess, ok := t.mgr.get(id); ok {
		sess.detach(conn)
	}
	conn.Close()
}

// localeLang 决定 bash 会话的 LANG。中文输入依赖 UTF-8 多字节支持：
// 若 LANG 为空或非 UTF-8，回退到内建于 glibc 的 C.UTF-8。
func localeLang(s string) string {
	u := strings.ToUpper(s)
	if s != "" && (strings.Contains(u, "UTF-8") || strings.Contains(u, "UTF8")) {
		return s
	}
	return "C.UTF-8"
}

// wsAccept computes the Sec-WebSocket-Accept value for a handshake key.
func wsAccept(key string) string {
	h := sha1.New()
	io.WriteString(h, key+"258EAFA5-E914-47DA-95CA-C5AB0DC85B11")
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

const (
	opText  = 0x1
	opBin   = 0x2
	opCont  = 0x0
	opClose = 0x8
	opPing  = 0x9
	opPong  = 0xA
)

// wsFrame builds a single unmasked server->client frame.
func wsFrame(op byte, payload []byte) []byte {
	n := len(payload)
	var hdr []byte
	if n < 126 {
		hdr = []byte{0x80 | op, byte(n)}
	} else if n < 65536 {
		hdr = []byte{0x80 | op, 126, byte(n >> 8), byte(n)}
	} else {
		hdr = []byte{0x80 | op, 127}
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], uint64(n))
		hdr = append(hdr, b[:]...)
	}
	return append(hdr, payload...)
}

// wsReadFrame reads a single client frame, unmasking the payload.
func wsReadFrame(r io.Reader) (byte, bool, []byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return 0, false, nil, err
	}
	fin := h[0]&0x80 != 0
	op := h[0] & 0x0f
	masked := h[1]&0x80 != 0
	ln := uint64(h[1] & 0x7f)
	if ln == 126 {
		var e [2]byte
		if _, err := io.ReadFull(r, e[:]); err != nil {
			return 0, false, nil, err
		}
		ln = uint64(binary.BigEndian.Uint16(e[:]))
	} else if ln == 127 {
		var e [8]byte
		if _, err := io.ReadFull(r, e[:]); err != nil {
			return 0, false, nil, err
		}
		ln = binary.BigEndian.Uint64(e[:])
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(r, mask[:]); err != nil {
			return 0, false, nil, err
		}
	}
	payload := make([]byte, ln)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, false, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return op, fin, payload, nil
}