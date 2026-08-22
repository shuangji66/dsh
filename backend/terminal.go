package main

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

// TerminalHandler serves an interactive bash terminal over a WebSocket on the
// admin unix socket. It uses a PTY to provide a fully interactive bash session.
type TerminalHandler struct {
	renv *RuntimeEnv
	mu   sync.Mutex
	sess map[net.Conn]*os.Process // store process for cleanup
}

func NewTerminalHandler(renv *RuntimeEnv) *TerminalHandler {
	return &TerminalHandler{renv: renv, sess: map[net.Conn]*os.Process{}}
}

// wsAcceptKey computes the Sec-WebSocket-Accept value for a handshake key.
func wsAccept(key string) string {
	h := sha1.New()
	io.WriteString(h, key+"258EAFA5-E914-47DA-95CA-C5AB0DC85B11")
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func parseWSHeaders(header http.Header) (string, bool) {
	key := header.Get("Sec-WebSocket-Key")
	proto := header.Get("Sec-WebSocket-Protocol")
	if key == "" {
		return "", false
	}
	return proto, true
}

// ServeHTTP hijacks the connection and upgrades to a WebSocket, then pipes it
// to an interactive bash session with PTY.
func (t *TerminalHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return
	}
	proto, ok := parseWSHeaders(r.Header)
	if !ok {
		http.Error(w, "missing Sec-WebSocket-Key", http.StatusBadRequest)
		return
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		return
	}
	// Complete the WebSocket handshake.
	protoHdr := ""
	if proto != "" {
		protoHdr = "\r\nSec-WebSocket-Protocol: " + proto
	}
	conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + wsAccept(r.Header.Get("Sec-WebSocket-Key")) + protoHdr + "\r\n\r\n"))
	_ = brw.Flush()

	// Start bash with a PTY.
	cmd := exec.Command("/bin/bash")
	cmd.Dir = t.renv.Home
	cmd.Env = t.terminalEnv()
	// Use pty.Start to allocate a PTY and set the process group.
	f, err := pty.Start(cmd)
	if err != nil {
		conn.Write(wsFrame(opText, []byte("failed to start bash: "+err.Error())))
		conn.Close()
		return
	}
	defer f.Close()

	// Store the process for cleanup.
	t.mu.Lock()
	t.sess[conn] = cmd.Process
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		delete(t.sess, conn)
		t.mu.Unlock()
	}()

	// Set up a goroutine to read from PTY master and send to WebSocket.
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				if _, werr := conn.Write(wsFrame(opText, buf[:n])); werr != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		conn.Write(wsFrame(opClose, nil))
	}()

	// Read WebSocket frames from client and write to PTY master.
	for {
		op, payload, rerr := wsReadFrame(brw)
		if rerr != nil {
			break
		}
		switch op {
		case opText, opBin:
			// Write payload to PTY master.
			f.Write(payload)
		case opPing:
			conn.Write(wsFrame(opPong, payload))
		case opClose:
			goto done
		}
	}
done:
	cmd.Process.Kill()
	cmd.Wait()
	conn.Close()
}

// terminalEnv builds the environment for the bash session: current app user's
// PATH and HOME are captured from the runtime; proxy settings follow config.
func (t *TerminalHandler) terminalEnv() []string {
	cfg := GetConfig()
	env := []string{
		"HOME=" + t.renv.Home,
		"PATH=" + t.renv.Path,
		"TERM=xterm-256color",
		"LANG=" + t.renv.Lang,
		"COLORTERM=truecolor",
		"PWD=" + t.renv.Home,
		"PS1=\\u@\\h:\\w\\$ ",
		"SHELL=/bin/bash",
	}
	if t.renv.PnpmHome != "" {
		env = append(env, "PNPM_HOME="+t.renv.PnpmHome)
	}
	if cfg.ProxyEnabled && cfg.ProxyAddr != "" {
		env = append(env, "http_proxy="+cfg.ProxyAddr, "https_proxy="+cfg.ProxyAddr, "HTTP_PROXY="+cfg.ProxyAddr, "HTTPS_PROXY="+cfg.ProxyAddr)
	}
	env = append(env, os.Environ()...)
	return env
}

const (
	opText  = 0x1
	opBin   = 0x2
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

// wsReadFrame reads a single client frame, unmasking the payload. A partial
// reader is passed in so the buffered bytes from the handshake are not lost.
func wsReadFrame(r io.Reader) (byte, []byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return 0, nil, err
	}
	op := h[0] & 0x0f
	masked := h[1]&0x80 != 0
	ln := uint64(h[1] & 0x7f)
	if ln == 126 {
		var e [2]byte
		if _, err := io.ReadFull(r, e[:]); err != nil {
			return 0, nil, err
		}
		ln = uint64(binary.BigEndian.Uint16(e[:]))
	} else if ln == 127 {
		var e [8]byte
		if _, err := io.ReadFull(r, e[:]); err != nil {
			return 0, nil, err
		}
		ln = binary.BigEndian.Uint64(e[:])
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(r, mask[:]); err != nil {
			return 0, nil, err
		}
	}
	payload := make([]byte, ln)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return op, payload, nil
}

// CloseAll terminates every live terminal session (used on shutdown).
func (t *TerminalHandler) CloseAll() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for c, proc := range t.sess {
		if proc != nil {
			proc.Kill()
		}
		c.Close()
	}
	t.sess = map[net.Conn]*os.Process{}
}