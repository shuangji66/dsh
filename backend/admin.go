package main

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// AdminMux serves the admin SPA, settings API, fnOS proxy, and terminal on the
// unix admin socket, under the configured baseurl.
type AdminMux struct {
	renv *RuntimeEnv
	dsh  *DshManager
	fnos *FnosClient
	auth *Auth
	spa  http.Handler
}

// newAdminMux wires the admin SPA mux onto the unix socket.
func newAdminMux(renv *RuntimeEnv, dsh *DshManager, auth *Auth) *AdminMux {
	return &AdminMux{
		renv: renv,
		dsh:  dsh,
		auth: auth,
		fnos: NewFnosClient(renv),
	}
}

// writeJSON writes an indented JSON response.
func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

// writeErr writes a JSON error body with the given status.
func writeErr(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": msg})
}

// spaHandler serves the embedded SPA assets.
func spaHandler(fsys fs.FS, baseurl string) http.Handler {
	sub, err := fs.Sub(fsys, "embed")
	if err == nil {
		fsys = sub
	}
	base := strings.TrimRight(baseurl, "/")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if base != "" && strings.HasPrefix(p, base) {
			p = strings.TrimPrefix(p, base)
			if p == "" {
				p = "/"
			}
		}
		reqPath := strings.TrimPrefix(p, "/")
		if reqPath == "" {
			reqPath = "index.html"
		}
		// 优先直接提供存在的文件（含 .html 静态页，如 callback.html）。
		// 首页（index.html）始终注入 base href，保证资源URL带上正确的 baseurl 前缀。
		if b, err := fs.ReadFile(fsys, reqPath); err == nil {
			if reqPath == "index.html" && base != "" {
				b = rewriteIndexBase(b, base)
			}
			serveBytes(w, reqPath, b)
			return
		}
		// 不存在的路径回退到 index.html 以支持 SPA 前端路由
		if b, err := fs.ReadFile(fsys, "index.html"); err == nil {
			if base != "" {
				b = rewriteIndexBase(b, base)
			}
			serveBytes(w, "index.html", b)
			return
		}
		http.NotFound(w, r)
	})
}

func rewriteIndexBase(body []byte, base string) []byte {
	s := string(body)
	s = strings.ReplaceAll(s, `src="./assets/`, `src="`+base+`/assets/`)
	s = strings.ReplaceAll(s, `href="./assets/`, `href="`+base+`/assets/`)
	headTag := `<base href="` + base + `/">`
	if idx := strings.Index(strings.ToLower(s), "<head"); idx != -1 {
		if ci := strings.Index(s[idx:], ">"); ci != -1 {
			pos := idx + ci + 1
			return []byte(s[:pos] + headTag + s[pos:])
		}
	}
	return []byte(headTag + s)
}

func serveBytes(w http.ResponseWriter, name string, data []byte) {
	if ct := mimeTypeByExt(path.Ext(name)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Write(data)
}

func mimeTypeByExt(ext string) string {
	switch ext {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".woff", ".woff2":
		return "font/woff2"
	case ".ico":
		return "image/x-icon"
	}
	return ""
}

func (m *AdminMux) serveSPA(w http.ResponseWriter, r *http.Request, p string) {
	if m.spa == nil {
		http.NotFound(w, r)
		return
	}
	r2 := r.Clone(r.Context())
	r2.URL.Path = p
	m.spa.ServeHTTP(w, r2)
}

// SetSPA attaches the embedded frontend assets to the admin mux.
func (m *AdminMux) SetSPA(fsys fs.FS) {
	m.spa = spaHandler(fsys, m.renv.AdminBaseURL)
}

// --- Settings API ---
func (m *AdminMux) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	cfg := GetConfig()
	locked := m.dsh.Running()
	writeJSON(w, map[string]interface{}{
		"config": cfg,
		"locked": locked,
		"runtime": map[string]interface{}{
			"dshBin":        m.renv.DshBin,
			"configFile":    m.renv.ConfigFile,
			"adminSock":     m.renv.AdminSock,
			"adminBaseURL":  m.renv.AdminBaseURL,
			"appName":       m.renv.TRIMAppName,
			"proxyPort":     m.renv.ProxyPort,
		},
		"status": m.dsh.Status(),
	})
}

type saveSettingsReq struct {
	Config *AppConfig `json:"config"`
}

func (m *AdminMux) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	var req saveSettingsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Config == nil {
		writeErr(w, "配置格式错误", http.StatusBadRequest)
		return
	}
	// 保存前校验密码强度：只要填了密码就必须满足要求（≥8位，含字母、数字、标点）
	if req.Config.Password != "" {
		if v := validatePassword(req.Config.Password); v != "" {
			writeErr(w, "密码不符合要求："+v, http.StatusBadRequest)
			return
		}
	}
	// 校验登录有效期（小时）：必须为正整数，且不超过 720 小时（30 天）
	if req.Config.AuthTTLHours <= 0 {
		writeErr(w, "登录有效期必须大于 0 小时", http.StatusBadRequest)
		return
	}
	if req.Config.AuthTTLHours > 720 {
		writeErr(w, "登录有效期不能超过 720 小时（30 天）", http.StatusBadRequest)
		return
	}
	locked := m.dsh.Running()
	if err := SaveConfig(m.renv, req.Config, locked); err != nil {
		writeErr(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "locked": locked, "config": GetConfig()})
}

// handleGetLogs 读取日志文件内容并返回给前端。
func (m *AdminMux) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	path := os.Getenv("HARNESS_LOG_FILE")
	if path == "" {
		writeErr(w, "日志文件未配置（未设置 HARNESS_LOG_FILE）", http.StatusNotFound)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, map[string]interface{}{"ok": true, "path": path, "content": "", "exists": false})
			return
		}
		writeErr(w, "读取日志失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "path": path, "content": string(data), "exists": true})
}
func (m *AdminMux) handleDshStart(w http.ResponseWriter, r *http.Request) {
	if m.dsh.Running() {
		writeErr(w, "dsh 已在运行", http.StatusConflict)
		return
	}
	if err := m.dsh.Start(); err != nil {
		writeErr(w, "启动失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, m.dsh.Status())
}

func (m *AdminMux) handleDshStop(w http.ResponseWriter, r *http.Request) {
	if err := m.dsh.Stop(); err != nil {
		writeErr(w, "停止失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, m.dsh.Status())
}

// handleDshRestart 重启 dsh 服务（先停止再启动）
func (m *AdminMux) handleDshRestart(w http.ResponseWriter, r *http.Request) {
    // 先停止
    if err := m.dsh.Stop(); err != nil {
        writeErr(w, "停止失败: "+err.Error(), http.StatusInternalServerError)
        return
    }
    // 再启动
    if err := m.dsh.Start(); err != nil {
        writeErr(w, "启动失败: "+err.Error(), http.StatusInternalServerError)
        return
    }
    writeJSON(w, m.dsh.Status())
}

// --- fnOS proxy ---
func (m *AdminMux) handleFnos(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/api/fnos/")
	switch {
	case p == "user-access" && r.Method == http.MethodGet:
		uid := getUIDFromRequest(r)
		if uid <= 0 {
			writeJSON(w, map[string]interface{}{"ok": false, "error": "无法获取当前用户 UID"})
			return
		}
		paths, msg, err := m.fnos.GetUserAccessibleFolders(uid)
		if err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "paths": paths, "msg": msg})

	case p == "user-access" && r.Method == http.MethodDelete:
		uid := getUIDFromRequest(r)
		if uid <= 0 {
			writeErr(w, "无法获取当前用户 UID", http.StatusBadRequest)
			return
		}
		var body struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Path == "" {
			writeErr(w, "缺少 path", http.StatusBadRequest)
			return
		}
		ok, msg, err := m.fnos.DelUserAccessibleFolder(uid, body.Path)
		if err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]interface{}{"ok": ok, "msg": msg})

	case p == "platform-config" && r.Method == http.MethodGet:
		data, err := m.fnos.GetPlatformConfig()
		if err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "data": data})

	default:
		http.NotFound(w, r)
	}
}

func getUIDFromRequest(r *http.Request) int {
	if uidStr := r.Header.Get("X-Trim-Userid"); uidStr != "" {
		if uid, err := strconv.Atoi(uidStr); err == nil && uid > 0 {
			return uid
		}
	}
	logger().Printf("Warning: getUIDFromRequest: unable to get valid UID from request")
	return 0
}

// --- AdminMux builder and socket serving (from adminmux.go) ---
func (m *AdminMux) buildHandler() http.Handler {
	base := strings.TrimRight(m.renv.AdminBaseURL, "/")
	term := NewTerminalHandler(m.renv)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if base != "" && strings.HasPrefix(p, base) {
			p = strings.TrimPrefix(p, base)
			if p == "" {
				p = "/"
			}
		}
		r.URL.Path = p

		if p == "/terminal" && isWebSocket(r) {
			term.ServeHTTP(w, r)
			return
		}

		switch {
		case p == "/api/settings" && r.Method == http.MethodGet:
			m.handleGetSettings(w, r)
		case p == "/api/settings" && r.Method == http.MethodPost:
			m.handleSaveSettings(w, r)
		case p == "/api/dsh/start" && r.Method == http.MethodPost:
			m.handleDshStart(w, r)
		case p == "/api/dsh/stop" && r.Method == http.MethodPost:
			m.handleDshStop(w, r)
		case p == "/api/dsh/status" && r.Method == http.MethodGet:
			writeJSON(w, m.dsh.Status())
		// 在 buildHandler 的 switch 中添加
        case p == "/api/dsh/restart" && r.Method == http.MethodPost:
            m.handleDshRestart(w, r)
		case p == "/api/fnos/convert-path" && r.Method == http.MethodPost:
            m.handleConvertPath(w, r)
		case p == "/api/logs" && r.Method == http.MethodGet:
			m.handleGetLogs(w, r)
		case strings.HasPrefix(p, "/api/fnos/"):
			m.handleFnos(w, r)
		default:
			m.serveSPA(w, r, spaPath(p))
		}
	})
	return mux
}

func spaPath(p string) string {
	if p == "/" || p == "" {
		return "/index.html"
	}
	if path.Ext(p) == "" {
		return "/index.html"
	}
	return p
}

func isWebSocket(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	up := strings.ToLower(r.Header.Get("Upgrade"))
	return up == "websocket"
}

func serveAdminSocket(m *AdminMux) error {
	renv := m.renv
	os.Remove(renv.AdminSock)
	if dir := filepath.Dir(renv.AdminSock); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	ln, err := netListen("unix", renv.AdminSock)
	if err != nil {
		return err
	}
	if e := os.Chmod(renv.AdminSock, 0660); e != nil {
		logger().Printf("admin socket chmod: %v", e)
	}
	h := m.buildHandler()
	srv := &http.Server{Handler: h}
	go func() {
		<-stopCh
		srv.Close()
	}()
	return srv.Serve(ln)
}

type convertPathReq struct {
    Paths    []string `json:"paths"`
    Language string   `json:"language"`
}

func (m *AdminMux) handleConvertPath(w http.ResponseWriter, r *http.Request) {
    var req convertPathReq
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeErr(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
        return
    }
    if len(req.Paths) == 0 {
        writeJSON(w, map[string]interface{}{"ok": true, "result": []map[string]string{}})
        return
    }
    
    result, err := m.fnos.ConvertPath(req.Paths, req.Language)
    if err != nil {
        writeErr(w, "路径转换失败: "+err.Error(), http.StatusInternalServerError)
        return
    }
    writeJSON(w, map[string]interface{}{"ok": true, "result": result})
}