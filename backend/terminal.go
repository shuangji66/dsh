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
//
// 它同时是**历史文件的写入侧上限**（见 Session.trimHistoryLocked）：文件本身若不
// 截断，读取侧的限制只能保证「一次不读太多」，磁盘占用仍随会话时长无限增长
// —— 一个长跑会话 + 高频输出足以把 TRIM_PKGVAR 所在分区写满。
const maxHistoryBytes = 4 * 1024 * 1024

const (
	// maxFrameBytes 是单个 WebSocket 帧的载荷上限。帧头里的长度是客户端说了算的
	// （16/64 位），畸形或恶意的一帧可以声明 TB 级长度 —— 直接 make 会把控制台打爆
	// （OOM 杀掉本进程；而它同时是 dsh 的守护进程，等于整站掉线）。
	maxFrameBytes = 1 << 20 // 1 MiB
	// maxMessageBytes 是分片累积后单条消息的上限：否则用连续的 continuation 帧就能
	// 把 msgBuf 撑爆。
	maxMessageBytes = 8 << 20 // 8 MiB
	// connWriteTimeout 是向终端连接写数据的超时。客户端不再读取（笔记本休眠、网络
	// 半开、代理不再收）时，写会在发送缓冲写满后阻塞 —— 而广播与历史回放都发生在
	// histMu 临界区内：没有超时就会连带冻结 PTY 读、/api/sessions（list 持 m.mu 再取
	// histMu）与新挂载，连「重连夺回」这条自救路径都会失效。
	connWriteTimeout = 5 * time.Second
)

// errFrameTooLarge 表示客户端发来的帧/消息超过上限，连接会被关闭。
var errFrameTooLarge = errors.New("terminal websocket frame too large")

// wsChunkSize 是历史回放时单帧的最大字节数。
const wsChunkSize = 32 * 1024 // 32KB

// errNotOwner：请求来自已不是操作端的连接（已被其他设备顶掉的旧连接）。
// 上层据此静默忽略：它只是残留帧，不是错误。
var errNotOwner = errors.New("not session owner")

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

// writeFrame 向终端连接写一帧，带写超时。返回错误表示该端已经写不动了
// （不读数据/连接已断），调用方应把它当死连接处理。timeout 取值见 connWriteTimeout。
func writeFrame(c net.Conn, frame []byte) error {
	if c == nil {
		return nil
	}
	// 每次写前重设：上一次的过期时间不能留下来影响本次。
	_ = c.SetWriteDeadline(time.Now().Add(connWriteTimeout))
	_, err := c.Write(frame)
	return err
}

// dropConn 把写不动的连接从会话上摘掉并关闭。会话本身不受影响（继续运行并写历史
// 文件），下次挂载仍可回放。调用方可能持有 histMu —— detach 只取 connMu，顺序与
// pump/attach 一致，不会反向嵌套。
func (s *Session) dropConn(c net.Conn, err error) {
	if c == nil {
		return
	}
	s.detach(c)
	_ = c.Close()
	logWarn("[terminal] session %s: dropped unresponsive client connection: %v", s.id, err)
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
			s.appendHistoryLocked(chunk)
			s.connMu.Lock()
			c := s.conn
			s.connMu.Unlock()
			if c != nil {
				if werr := writeFrame(c, wsFrame(opText, chunk)); werr != nil {
					// 写不动就不写了：摘掉并关闭它，别让它继续占着 histMu。
					s.dropConn(c, werr)
				}
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
	s.appendHistoryLocked(note)
	s.connMu.Lock()
	c := s.conn
	s.connMu.Unlock()
	if c != nil {
		if werr := writeFrame(c, wsFrame(opText, note)); werr == nil {
			if werr = writeFrame(c, wsFrame(opText, []byte("\x1b]exit\x07"))); werr != nil {
				s.dropConn(c, werr)
			}
		} else {
			s.dropConn(c, werr)
		}
	}
	s.histMu.Unlock()
}

// appendHistoryLocked 把一段 PTY 输出追加到历史文件，并维护写入侧上限。
// 调用方必须持有 s.histMu（与「回放历史 + 实时广播」共用同一临界区）。
//
// 抽成独立方法是为了让「写入侧上限」这条语义可以被单测覆盖：沙箱里建不起 PTY
// （/dev/ptmx 权限），pump 本体没法起，但这段纯文件逻辑可以。
func (s *Session) appendHistoryLocked(chunk []byte) {
	if s.hist == nil {
		return
	}
	if _, err := s.hist.Write(chunk); err != nil {
		// 历史写失败不影响实时流，但回放能力会降级 —— 留一条线索（重复行由
		// logging.go 的重复抑制兜住，不会刷屏）。
		logWarn("[terminal] session %s: append history failed: %v", s.id, err)
		return
	}
	s.trimHistoryLocked()
}

// trimHistoryLocked 给历史文件加**写入侧上限**：超过 maxHistoryBytes 时滚动截断，
// 只保留末尾 maxHistoryBytes 字节。调用方必须持有 s.histMu，且在 Write 之后调用。
//
// 为什么选「滚动截断」而不是「给已退出会话加 TTL 回收」：
//   - 读取侧本来只回放末尾 maxHistoryBytes，截掉更早的字节不影响任何可见语义
//     （attach 回放、/api/session/history、/api/sessions 的 size 都只看末尾那段）；
//   - 会话继续运行时磁盘占用有界，而 TTL 回收只解决「已退出会话」那一半问题；
//   - TTL 回收会让「浏览器断开 → 稍后回来重连」这条被明确要求保留的路径出现
//     新语义（过期后 history 为空、会话从列表消失），风险更大。
//
// 保留的边界是「末尾 N 字节」而不是整数个转义序列：attach/history 的读取路径本来
// 就从任意偏移开始回放，这里与它们保持一致（终端渲染对残缺的头部序列本就容错）。
func (s *Session) trimHistoryLocked() {
	if s.hist == nil {
		return
	}
	st, err := s.hist.Stat()
	if err != nil || st.Size() <= maxHistoryBytes {
		return
	}
	keep := int64(maxHistoryBytes)
	start := st.Size() - keep
	buf := make([]byte, keep)
	// ReadAt 不改文件游标；写入侧仍靠 O_APPEND 追加，两者互不干扰。
	if _, err := s.hist.ReadAt(buf, start); err != nil {
		logWarn("[terminal] session %s: read history tail failed: %v", s.id, err)
		return
	}
	if err := s.hist.Truncate(0); err != nil {
		logWarn("[terminal] session %s: truncate history failed: %v", s.id, err)
		return
	}
	// 文件是 O_APPEND 打开的，Truncate(0) 之后 Write 必然落在位置 0。
	if _, err := s.hist.Write(buf); err != nil {
		logWarn("[terminal] session %s: rewrite history tail failed: %v", s.id, err)
	}
}

// attach 把连接挂载到会话：先回放历史文件内容（上限 maxHistoryBytes，分帧发送），
// 再发送 ready 标记，最后接管实时流。整段在 histMu 内完成，防止新增输出既被回放
// 又被实时广播造成重复。
//
// **单挂载点语义**：一个会话在任何时刻只允许一个操作端（能写输入、能改 PTY 尺寸）。
// 挂载成功即成为操作端，返回值是**被顶掉的旧连接**（无则 nil），由调用方负责通知并
// 关闭它（terminal.go 的 kickDetached）——只影响这一个会话，其他会话的连接不动。
func (s *Session) attach(c net.Conn) (net.Conn, error) {
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
						if werr := writeFrame(c, wsFrame(opText, chunk[:n])); werr != nil {
							return nil, werr
						}
					}
					if rerr != nil {
						break
					}
				}
			}
		}
	}
	if err := writeFrame(c, wsFrame(opText, []byte("\x1b]ready\x07"))); err != nil {
		return nil, err
	}
	// 换主与历史回放同处一个 histMu 临界区：新连接既不漏字节，也不与广播交叉。
	s.connMu.Lock()
	prev := s.conn
	s.conn = c
	s.connMu.Unlock()
	if prev == c {
		prev = nil // 重复挂载同一连接：无需顶号
	}
	return prev, nil
}

// isOwner 判断该连接是否仍是会话的操作端（被顶掉的旧连接为 false）。
func (s *Session) isOwner(c net.Conn) bool {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	return s.conn != nil && s.conn == c
}

// detach 解除当前连接挂载（不杀会话，会话继续运行并写入历史文件）。
func (s *Session) detach(c net.Conn) {
	s.connMu.Lock()
	if s.conn == c {
		s.conn = nil
	}
	s.connMu.Unlock()
}

// writeInput 把客户端输入写入 PTY。**只有当前操作端能写**：被顶掉的旧连接即使还有
// 残留帧抵达，也在服务端被丢弃——否则两台设备会同时操作同一个 PTY（两边都能打字，
// 却只有一边看得到输出）。
func (s *Session) writeInput(c net.Conn, p []byte) error {
	if !s.isOwner(c) {
		return errNotOwner
	}
	_, err := s.pty.Write(p)
	return err
}

// resize 调整 PTY 尺寸。同样只接受操作端的请求：被顶掉的旧设备随后会触发一次
// 尺寸重排，若不加限制就会把 PTY 改成它自己的窗口大小。
func (s *Session) resize(c net.Conn, cols, rows uint16) error {
	if !s.isOwner(c) {
		return errNotOwner
	}
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

// terminalEnv builds the environment for the bash session: the harness process
// environment is used as the base, then the app user's HOME/PATH and the
// terminal-specific values are written over it in place.
//
// 这里刻意不注入代理设置：终端是用户自己的 shell，控制台的代理配置只服务于 dsh 与
// harness 的对外请求（dsh 进程见 DshManager.buildEnv）。注入只会让控制台配置与平台
// 自己导出的代理变量在同一份环境里并存，而 os.Environ() 本来就带着后者。
//
// 构造顺序很关键：os.Environ() 必须先铺底，再用 set() **原位覆盖**。os/exec 的
// dedupEnv 对同名键是「最后一个生效」，反过来写（先显式值、后 append 环境）会让
// LANG 的 UTF-8 兜底、TERM、SHELL 等被父进程的同名变量整体覆盖 —— dsh.go 的
// buildEnv 用的就是这套「原位替换」写法，这里保持一致。
func terminalEnv(renv *RuntimeEnv, home string) []string {
	env := os.Environ()
	set := func(kv string) {
		key := kv[:strings.IndexByte(kv, '=')]
		for i, e := range env {
			if strings.HasPrefix(e, key+"=") {
				env[i] = kv
				return
			}
		}
		env = append(env, kv)
	}
	// HOME 解析失败（空）时不要写 HOME=""，否则 bash 会拿到一个空主目录，
	// 保留父进程环境里的值更合理。
	if home != "" {
		set("HOME=" + home)
		set("PWD=" + home)
	}
	set("PATH=" + renv.Path)
	set("TERM=xterm-256color")
	set("LANG=" + localeLang(renv.Lang))
	set("COLORTERM=truecolor")
	set("PS1=\\u@\\h:\\w\\$ ")
	set("SHELL=/bin/bash")
	if renv.PnpmHome != "" {
		set("PNPM_HOME=" + renv.PnpmHome)
	}
	return env
}

// effectiveHome 返回终端会话应使用的主目录：用户在资源页切换过主目录后以
// AppConfig.HomeDir 为准（与 DshManager.effectiveHome 同一语义），否则回退到启动时
// 解析出的 HOME。终端必须与 dsh 服务用同一份 HOME —— 否则在终端里执行
// `dsh plugin --profile web …` 操作的是另一份 ~/.dsh，与 dsh 服务实际加载的 profile
// 不是同一个（现象：控制台/市场里改了插件，终端里看不到）。
func (m *SessionManager) effectiveHome() string {
	if h := GetConfig().HomeDir; h != "" {
		return h
	}
	if m.renv != nil {
		return m.renv.Home
	}
	return os.Getenv("HOME")
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
	// 用「当前生效的主目录」（跟随资源页的切换），与 dsh 服务保持一致。
	home := m.effectiveHome()
	if home != "" {
		cmd.Dir = home
	}
	cmd.Env = terminalEnv(m.renv, home)
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
	return s.close()
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
// **单挂载点**：一个会话同时只有一个操作端，新挂载会顶掉旧连接（kickDetached）。
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
		if _, aerr := s.attach(conn); aerr != nil {
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
		// 单挂载点：挂载成功即成为操作端，旧连接被顶掉并收到「已被接管」通知。
		prev, aerr := s.attach(conn)
		if aerr != nil {
			conn.Close()
			return
		}
		if prev != nil {
			kickDetached(prev)
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
			// 分片累积同样要有上限：否则客户端用连续的 continuation 帧就能把
			// msgBuf 撑到内存耗尽（单帧上限在 wsReadFrame 里，这里管整条消息）。
			if len(msgBuf)+len(payload) > maxMessageBytes {
				logWarn("[terminal] message exceeds %d bytes, closing connection", maxMessageBytes)
				goto done
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
							// 只有操作端的尺寸生效（被顶掉的旧设备会随后触发一次重排）
							if err := sess.resize(conn, uint16(cols), uint16(rows)); err != nil && !errors.Is(err, errNotOwner) {
								logWarn("[terminal] resize failed: %v", err)
							}
						}
					}
				}
			} else if strings.HasPrefix(s, "\x1b]ping") && strings.HasSuffix(s, "\x07") {
				// 心跳：仅保持连接不被代理/NAT 空闲超时断开，不写入 PTY
			} else {
				if sess, ok := t.mgr.get(id); ok {
					// 非操作端的残留输入在服务端丢弃（见 Session.writeInput 的说明）
					if werr := sess.writeInput(conn, msgBuf); werr != nil && !errors.Is(werr, errNotOwner) {
						logWarn("[terminal] write input failed: %v", werr)
					}
				}
			}
		case opPing:
			if werr := writeFrame(conn, wsFrame(opPong, payload)); werr != nil {
				logWarn("[terminal] pong write failed: %v", werr)
				goto done
			}
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
	// wsCloseTaken 是「会话被其他设备接管」的 WebSocket 关闭码（4000–4999 为应用自定
	// 区间）。前端据此提示「已在其他设备打开」，不自动重连（否则两台设备会互抢）。
	wsCloseTaken = 4001
	// oscDetached 是顶号通知（OSC 控制帧，不写入 PTY）。与 close 帧双保险：
	// 帧先到 → 立即提示；帧被代理吞掉时还能靠 close 码判定。
	oscDetached = "\x1b]detached\x07"

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

// wsCloseFrame 构造服务端 close 帧：payload = 2 字节状态码 + UTF-8 reason。
func wsCloseFrame(code uint16, reason string) []byte {
	payload := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(payload, code)
	copy(payload[2:], reason)
	return wsFrame(opClose, payload)
}

// kickDetached 通知被顶掉的旧连接：先发 \x1b]detached\x07，再发 close(4001)，最后关闭。
// 写带 1s 超时：旧连接 TCP 缓冲可能已满，绝不能让这次写阻塞拖住新设备的挂载。
func kickDetached(c net.Conn) {
	_ = c.SetWriteDeadline(time.Now().Add(time.Second))
	_, _ = c.Write(wsFrame(opText, []byte(oscDetached)))
	_, _ = c.Write(wsCloseFrame(wsCloseTaken, "detached"))
	_ = c.Close()
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
	// 上限校验放在最前面（读完长度、还没读掩码/载荷、更没 make）：长度由客户端声明，
	// 64 位长度可到 TB 级，畸形帧根本不该继续处理。
	if ln > maxFrameBytes {
		return 0, false, nil, errFrameTooLarge
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
