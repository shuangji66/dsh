package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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

// inSet reports whether s is one of the allowed values.
func inSet(s string, allowed ...string) bool {
	for _, a := range allowed {
		if s == a {
			return true
		}
	}
	return false
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
			"configFile":    m.renv.ConfigFile,
			"adminSock":     m.renv.AdminSock,
			"adminBaseURL":  m.renv.AdminBaseURL,
			"appName":       m.renv.TRIMAppName,
			"proxyPort":     m.renv.ProxyPort,
			// 默认主目录语义路径及其实际系统路径，与当前主目录（dsh 的 HOME）。
			"defaultHomeSemantic": m.defaultHomeSemantic(),
			"defaultHomeDir":      m.renv.Home,
			"homeDir":             m.dsh.effectiveHome(),
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
	// 校验 dsh 内存限制（MB）：必须为正整数，且不超过 65536 MB（64GB）
	if req.Config.DshMemLimit <= 0 {
		writeErr(w, "dsh 内存限制必须大于 0 MB", http.StatusBadRequest)
		return
	}
	if req.Config.DshMemLimit > 65536 {
		writeErr(w, "dsh 内存限制不能超过 65536 MB（64GB）", http.StatusBadRequest)
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

// handleDownloadLog 以附件形式下发日志原文件，供前端“导出”按钮下载。
func (m *AdminMux) handleDownloadLog(w http.ResponseWriter, r *http.Request) {
	path := os.Getenv("HARNESS_LOG_FILE")
	if path == "" {
		writeErr(w, "日志文件未配置", http.StatusNotFound)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeErr(w, "日志文件不存在", http.StatusNotFound)
			return
		}
		writeErr(w, "读取日志失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	fname := filepath.Base(path)
	w.Header().Set("Content-Disposition", `attachment; filename="`+fname+`"`)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Write(data)
}

// checkAgentsBusy 及 /api/dsh/agents-busy 接口已废弃并移除：
// 概览页停止/重启现在总是二次确认，不再探测 agent 任务是否繁忙。

func (m *AdminMux) handleDshStart(w http.ResponseWriter, r *http.Request) {
	if m.dsh.Running() {
		writeErr(w, "dsh 已在运行", http.StatusConflict)
		return
	}
	if err := m.dsh.Start(); err != nil {
		writeErr(w, "启动失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// 启动后等待并捕获一次性访问 token（新版 dsh 打印在启动日志里），
	// 并用 token 换取 dsh 会话 cookie（反代转发时携带该 cookie）。
	go func() {
		if tok := m.dsh.WaitToken(15 * time.Second); tok != "" {
			logger().Printf("dsh access token ready: %s", tok)
			if err := m.dsh.ExchangeToken(); err != nil {
				logger().Printf("dsh token exchange failed: %v", err)
			} else if m.dsh.AuthCookie() == "" {
				logger().Printf("no dsh auth cookie observed (旧版 dsh 或响应无 Set-Cookie)")
			}
		} else {
			logger().Printf("no dsh access token observed (旧版 dsh 或日志未就绪)")
		}
	}()
	writeJSON(w, m.dsh.Status())
}

func (m *AdminMux) handleDshStop(w http.ResponseWriter, r *http.Request) {
	if err := m.dsh.Stop(); err != nil {
		writeErr(w, "停止失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, m.dsh.Status())
}

// restartDsh 停止并重新启动 dsh 服务，重启后异步捕获新的访问 token 并换取
// dsh 会话 cookie（供反代转发时携带）。
func (m *AdminMux) restartDsh() error {
	if err := m.dsh.Stop(); err != nil {
		return fmt.Errorf("停止失败: %w", err)
	}
	if err := m.dsh.Start(); err != nil {
		return fmt.Errorf("启动失败: %w", err)
	}
	// 重启后等待并捕获新的访问 token（每次启动 dsh 都会生成新的 token），
	// 并用 token 换取 dsh 会话 cookie。
	go func() {
		if tok := m.dsh.WaitToken(15 * time.Second); tok != "" {
			logger().Printf("dsh access token ready: %s", tok)
			if err := m.dsh.ExchangeToken(); err != nil {
				logger().Printf("dsh token exchange failed: %v", err)
			} else if m.dsh.AuthCookie() == "" {
				logger().Printf("no dsh auth cookie observed (旧版 dsh 或响应无 Set-Cookie)")
			}
		} else {
			logger().Printf("no dsh access token observed (旧版 dsh 或日志未就绪)")
		}
	}()
	return nil
}

// handleDshRestart 重启 dsh 服务（先停止再启动）。
func (m *AdminMux) handleDshRestart(w http.ResponseWriter, r *http.Request) {
	if err := m.restartDsh(); err != nil {
		writeErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, m.dsh.Status())
}

// handleDshVersion 执行 `dsh -V` 获取 dsh 版本号，供概览页在标题右侧展示。
func (m *AdminMux) handleDshVersion(w http.ResponseWriter, r *http.Request) {
	out, err := m.dsh.runDshCmd("-V")
	if err != nil {
		// 命令失败（如 dsh 未安装/未就绪）时不阻塞页面，返回空版本
		writeJSON(w, map[string]interface{}{"ok": true, "version": strings.TrimSpace(out)})
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "version": strings.TrimSpace(out)})
}

// --- Plugin management (work 区插件卡片) ---

// handleListPlugins 返回 dsh 的 web 插件列表（解析 `dsh plugin --profile web list`）。
func (m *AdminMux) handleListPlugins(w http.ResponseWriter, r *http.Request) {
	out, err := m.dsh.runPluginCmd("list")
	if err != nil {
		writeErr(w, "执行插件列表失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "plugins": parsePluginList(out), "raw": out})
}

type removePluginReq struct {
	Name string `json:"name"`
}

// handleRemovePlugin 卸载指定插件（`dsh plugin --profile web remove <name>`）。
func (m *AdminMux) handleRemovePlugin(w http.ResponseWriter, r *http.Request) {
	var body removePluginReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeErr(w, "缺少插件名", http.StatusBadRequest)
		return
	}
	out, err := m.dsh.runPluginCmd("remove", body.Name)
	if err != nil {
		writeErr(w, "卸载失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "removed": body.Name, "msg": out})
}

// handleResetPlugins 删除 $HOME/.dsh/profiles 目录（dsh 的 profiles 数据，
// 连同插件与配置一并清空），并在后台重启 harness（dsh）服务并触发一次
// node-pty 自动 patch。注意：不再逐个卸载插件，直接清除 profiles 目录即可。
//
// 关键：重启 + node-pty patch 会长期阻塞（ensureNodePty 要轮询等待 pnpm 与
// web 目录，最长可达数分钟），因此改为立即返回、把重活交给后台 goroutine，
// 避免请求线程被拖垮而触发前端网关的 502 Bad Gateway。
func (m *AdminMux) handleResetPlugins(w http.ResponseWriter, r *http.Request) {
	// 删除 $HOME/.dsh/profiles 目录
	// 使用当前生效的主目录（若已在资源页切换过，则以切换后的为准）。
	home := m.dsh.effectiveHome()
	if home == "" {
		home = m.renv.Home
	}
	if home == "" {
		home = os.Getenv("HOME")
	}
	if home == "" {
		if h, herr := os.UserHomeDir(); herr == nil && h != "" {
			home = h
		}
	}
	profilesDir := ""
	if home != "" {
		profilesDir = filepath.Join(home, ".dsh", "profiles")
	}
	profileDeleted := false
	if profilesDir != "" {
		if derr := os.RemoveAll(profilesDir); derr != nil {
			logger().Printf("删除 profiles 目录失败 %s: %v", profilesDir, derr)
		} else {
			profileDeleted = true
		}
	} else {
		logger().Printf("未确定 HOME，无法定位 profiles 目录，重置中止")
	}

	// 仅当 ~/.dsh/profiles 删除成功后才继续；失败则立即返回，不重启也不 patch。
	if !profileDeleted {
		writeJSON(w, map[string]interface{}{
			"ok":             false,
			"error":          "profiles 目录删除失败",
			"profileDeleted": false,
			"profilesDir":    profilesDir,
		})
		return
	}

	// 删除成功：把「重启 dsh + node-pty 自动 patch」放到后台执行，立即回包。
	// 这样三者仍严格按“删除成功 → 重启 dsh → patch”的顺序发生，且不阻塞请求。
	go func() {
		if rerr := m.restartDsh(); rerr != nil {
			logger().Printf("reset 后 dsh 重启失败: %v", rerr)
			return
		}
		if perr := m.patchNodePty(); perr != nil {
			logger().Printf("node-pty auto-patch after reset failed: %v", perr)
		} else {
			logger().Printf("node-pty auto-patch after reset completed")
		}
	}()

	writeJSON(w, map[string]interface{}{
		"ok":             true,
		"started":        true,
		"profileDeleted": true,
		"profilesDir":    profilesDir,
	})
}

// patchNodePty 触发一次 node-pty 的自动 patch（重新执行 dsh 的 node-pty 安装/修补）。
// 供插件重置后独立调用；后端的冷启动路径仍由 run 中的 ensureNodePty 承担，顺序不变。
func (m *AdminMux) patchNodePty() error {
	return ensureNodePty(m.renv)
}

// defaultHomeSemantic 返回默认主目录的“相对/语义”路径。它是本应用的 shares 目录，
// 即 /var/apps/<AppName>/shares/<AppName>，其实际系统路径为启动时的 HOME
// （如 /vol1/@appshare/Harness）。资源页用它作为固定不可移除的第一张卡片。
func (m *AdminMux) defaultHomeSemantic() string {
	name := m.renv.TRIMAppName
	if name == "" {
		name = "Harness"
	}
	return "/var/apps/" + name + "/shares/" + name
}

// setHomeReq 是“设置为主目录”请求体。
type setHomeReq struct {
	Path    string `json:"path"`    // 目标目录的实际系统路径
	Migrate bool   `json:"migrate"` // 是否把当前 HOME 的 ~/.dsh 复制到目标目录
}

// handleSetHome 把某个已授权目录设为 dsh 的 HOME，并在确认后把当前 ~/.dsh 配置
// 复制到目标目录（可选），随后重启 dsh 使新的 HOME 生效。
func (m *AdminMux) handleSetHome(w http.ResponseWriter, r *http.Request) {
	var req setHomeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}
	dest := filepath.Clean(req.Path)
	if dest == "" || dest == "." || !filepath.IsAbs(dest) {
		writeErr(w, "无效的目标目录路径", http.StatusBadRequest)
		return
	}
	// 目标目录必须已存在且为目录。
	if fi, err := os.Stat(dest); err != nil || !fi.IsDir() {
		writeErr(w, "目标目录不存在或不是目录", http.StatusBadRequest)
		return
	}

	current := m.dsh.effectiveHome()
	if current != "" {
		// 规范化比较，避免符号链接/末尾斜杠造成的误判。
		ci, e1 := os.Stat(current)
		di, e2 := os.Stat(dest)
		if e1 == nil && e2 == nil && os.SameFile(ci, di) {
			writeJSON(w, map[string]interface{}{"ok": true, "unchanged": true, "homeDir": current})
			return
		}
	}

	// 可选：迁移当前主目录的 ~/.dsh 配置至目标目录。
	if req.Migrate && current != "" {
		srcDsh := filepath.Join(current, ".dsh")
		if fi, err := os.Stat(srcDsh); err == nil && fi.IsDir() {
			// 迁移即覆盖：若目标目录已存在 .dsh，先整体删除再拷贝，
			// 避免残留旧配置或新旧文件混叠。
			dstDsh := filepath.Join(dest, ".dsh")
			if _, err := os.Lstat(dstDsh); err == nil {
				if err := os.RemoveAll(dstDsh); err != nil {
					writeErr(w, "迁移配置失败: 无法移除目标 ~/.dsh: "+err.Error(), http.StatusInternalServerError)
					return
				}
				logger().Printf("set-home: removed existing ~/.dsh at %s (overwritten by migration)", dstDsh)
			}
			if err := copyDir(srcDsh, dstDsh); err != nil {
				writeErr(w, "迁移配置失败: "+err.Error(), http.StatusInternalServerError)
				return
			}
			logger().Printf("set-home: migrated ~/.dsh from %s to %s", srcDsh, dstDsh)
		} else {
			logger().Printf("set-home: source ~/.dsh not found at %s, skip migration", srcDsh)
		}
	}

	// 保存新的 HOME 配置。
	next := GetConfig()
	next.HomeDir = dest
	if err := SaveConfig(m.renv, &next, false); err != nil {
		writeErr(w, "保存配置失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	logger().Printf("set-home: homeDir switched to %s", dest)

	// 后台重启 dsh，使新的 HOME 环境变量生效（避免阻塞请求线程）。
	go func() {
		if err := m.restartDsh(); err != nil {
			logger().Printf("set-home: restart dsh failed: %v", err)
			return
		}
		logger().Printf("set-home: dsh restarted with new HOME=%s", dest)
	}()

	writeJSON(w, map[string]interface{}{
		"ok":      true,
		"changed": true,
		"homeDir": dest,
		"status":  m.dsh.Status(),
	})
}

// handleDshBackup 把当前 HOME 的 ~/.dsh 目录整体压缩打包为
// dsh-backup-<YYYYMMDDHHmm>.zip，保存在 HOME 目录下。时间戳精确到分钟，
// 例如 dsh-backup-202605050933.zip。
func (m *AdminMux) handleDshBackup(w http.ResponseWriter, r *http.Request) {
	home := m.dsh.effectiveHome()
	if home == "" {
		writeErr(w, "无法获取当前主目录", http.StatusBadRequest)
		return
	}
	src := filepath.Join(home, ".dsh")
	if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
		writeErr(w, ".dsh 目录不存在", http.StatusNotFound)
		return
	}
	name := fmt.Sprintf("dsh-backup-%s.zip", time.Now().Format("200601021504"))
	dest := filepath.Join(home, name)
	if err := zipDir(src, dest); err != nil {
		writeErr(w, "备份失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	var size int64
	if fi, err := os.Stat(dest); err == nil {
		size = fi.Size()
	}
	logger().Printf("backup: %s (%d bytes)", dest, size)
	writeJSON(w, map[string]interface{}{
		"ok":   true,
		"name": name,
		"path": dest,
		"size": size,
	})
}

// zipDir 将 srcDir 目录树递归压缩写入 destZip（zip 文件），保留相对路径。
func zipDir(srcDir, destZip string) error {
	out, err := os.Create(destZip)
	if err != nil {
		return err
	}
	defer out.Close()
	zw := zip.NewWriter(out)

	base := filepath.Clean(srcDir)
	err = filepath.Walk(base, func(p string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if p == base {
			return nil // 跳过根目录条目本身
		}
		// 不包含备份产物自身，避免递归膨胀。
		if strings.HasSuffix(p, ".zip") {
			return nil
		}
		rel, err := filepath.Rel(base, p)
		if err != nil {
			return err
		}
		if info.IsDir() {
			if _, err := zw.Create(rel + "/"); err != nil {
				return err
			}
			return nil
		}
		// 符号链接：以链接形式记录（内容存目标路径文本）。
		if info.Mode()&os.ModeSymlink != 0 {
			hdr, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			hdr.Name = rel
			hdr.Method = zip.Store
			w, err := zw.CreateHeader(hdr)
			if err != nil {
				return err
			}
			link, lerr := os.Readlink(p)
			if lerr != nil {
				return lerr
			}
			_, werr := io.WriteString(w, link)
			return werr
		}
		fh, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		fh.Name = rel
		fh.Method = zip.Deflate
		w, err := zw.CreateHeader(fh)
		if err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		_, cErr := io.Copy(w, f)
		f.Close()
		return cErr
	})
	if err != nil {
		zw.Close()
		os.Remove(destZip)
		return err
	}
	return zw.Close()
}

// --- Visitor API (overview page) ---
func (m *AdminMux) handleGetVisitors(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"ok":       true,
		"visitors": m.auth.Visitors(),
	})
}

type deleteVisitorReq struct {
	ID string `json:"id"`
}

func (m *AdminMux) handleDeleteVisitor(w http.ResponseWriter, r *http.Request) {
	var body deleteVisitorReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		writeErr(w, "缺少 id", http.StatusBadRequest)
		return
	}
	if !m.auth.RevokeVisitor(body.ID) {
		writeJSON(w, map[string]interface{}{"ok": true, "deleted": false, "msg": "该访客不存在"})
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "deleted": true, "msg": "已注销该访客"})
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
		case p == "/api/dsh/version" && r.Method == http.MethodGet:
			m.handleDshVersion(w, r)
		// 在 buildHandler 的 switch 中添加
        case p == "/api/dsh/restart" && r.Method == http.MethodPost:
            m.handleDshRestart(w, r)
		case p == "/api/dsh/set-home" && r.Method == http.MethodPost:
			m.handleSetHome(w, r)
		case p == "/api/dsh/backup" && r.Method == http.MethodPost:
			m.handleDshBackup(w, r)
		case p == "/api/fnos/convert-path" && r.Method == http.MethodPost:
            m.handleConvertPath(w, r)
		case p == "/api/logs" && r.Method == http.MethodGet:
			m.handleGetLogs(w, r)
		case p == "/api/visitors" && r.Method == http.MethodGet:
			m.handleGetVisitors(w, r)
		case p == "/api/visitors" && r.Method == http.MethodDelete:
			m.handleDeleteVisitor(w, r)
		case p == "/api/visitors/stream" && r.Method == http.MethodGet:
			m.handleVisitorsStream(w, r)
		case p == "/api/dsh/stream" && r.Method == http.MethodGet:
			m.handleDshStream(w, r)
		case p == "/api/logs/stream" && r.Method == http.MethodGet:
			m.handleLogsStream(w, r)
		case p == "/api/logs/download" && r.Method == http.MethodGet:
			m.handleDownloadLog(w, r)
		case p == "/api/plugins" && r.Method == http.MethodGet:
			m.handleListPlugins(w, r)
		case p == "/api/plugins/remove" && r.Method == http.MethodPost:
			m.handleRemovePlugin(w, r)
		case p == "/api/plugins/reset" && r.Method == http.MethodPost:
			m.handleResetPlugins(w, r)
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