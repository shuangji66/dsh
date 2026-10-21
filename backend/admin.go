package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
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
	renv     *RuntimeEnv
	dsh      *DshManager
	fnos     *FnosClient
	auth     *Auth
	update   *UpdateManager
	sessions *SessionManager
	spa      http.Handler
	// boot 是启动阶段状态机：反代用它决定等待页/放行，设置页切换反代端口时
	// 重建监听也要复用它（见 rebindProxyPort）。
	boot *bootState
	// crypt 是快捷指令命令内容的落盘加密器（AES-256-GCM，见 secret.go）。
	// **可能为 nil**：主密钥读不到/解不出时进程照常启动（控制台是这个 NAS 的带外访问
	// 入口，不该为一条快捷指令把整个控制台停掉），但写路径一律拒绝，绝不退化成写明文。
	crypt *cryptor
	// dshUI 是挂在控制台自己的 admin socket 下的 dsh 反代（baseurl 之下的一层
	// `/gui`）。它存在的唯一目的：让「飞牛入口」落在**控制台同一个网关入口**
	// （/app/Harness）里，复用平台给控制台入口的鉴权；App 的网关鉴权是按入口注入的，
	// 跨到独立的 /app/Harness/dsh 入口时带不过去（详见 proxy.go 的 gatewayLine/trusted）。
	// nil 表示未启用（测试或没有 auth/dsh 的实例）。
	dshUI *reverseProxy
}

// newAdminMux wires the admin SPA mux onto the unix socket. crypt 可以为 nil，
// 语义见 AdminMux.crypt。
func newAdminMux(renv *RuntimeEnv, dsh *DshManager, auth *Auth, upd *UpdateManager, boot *bootState, crypt *cryptor) *AdminMux {
	if boot == nil {
		boot = newBootState()
	}
	m := &AdminMux{
		renv:     renv,
		dsh:      dsh,
		auth:     auth,
		fnos:     NewFnosClient(renv),
		update:   upd,
		sessions: NewSessionManager(renv),
		boot:     boot,
		crypt:    crypt,
	}
	// 控制台入口下的 dsh 反代：路径 = 控制台 baseurl + "/gui"（例如
	// /app/Harness/gui）。它由 trim_http_cgi 按控制台 entry 鉴权后转发到 app.sock，
	// 因此 App 里打开它不会跨到独立的 dsh 入口、也就没有入口 token 的注入问题。
	if auth != nil && dsh != nil {
		guiBase := strings.TrimRight(renv.AdminBaseURL, "/") + "/gui"
		m.dshUI = newReverseProxyMount(auth, dsh, boot, guiBase, true)
		m.dshUI.trusted = true
	}
	return m
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
//
// 只用于**没有 code** 的场合（内部错误、诊断文本）。面向用户的文案一律走 writeErrU /
// writeErrFrom：它们会把 code + params 一并回报，前端据此走 i18n（见 uimsg.go）。
func writeErr(w http.ResponseWriter, msg string, status int) {
	writeErrBody(w, status, msg, uiMsg{})
}

// writeErrU 写回一条带 code 的用户错误（toast / 弹窗文案）。
//
// 中文原文（zh）同时是「前端不认这个 code」时的兜底文案；params 以 k/v 成对给出：
//
//	writeErrU(w, http.StatusBadRequest, "err_need_field", "缺少 %s", "field", "version")
func writeErrU(w http.ResponseWriter, status int, code, zh string, kv ...string) {
	writeErrBody(w, status, formatKV(zh, kv), uiMsg{Code: code, Params: kvMap(kv)})
}

// writeErrFrom 把服务层返回的错误写回：带 code 的走 code（前端 i18n），busy 类并发拒绝
// 自动用 409，其余按 fallbackStatus。
func writeErrFrom(w http.ResponseWriter, err error, fallbackStatus int) {
	status := fallbackStatus
	if isBusyErr(err) {
		status = http.StatusConflict
	}
	m, _ := uiMsgOf(err)
	writeErrBody(w, status, err.Error(), m)
}

// writeErrOperation 保留旧名（busy 类 → 409，其余 → 400），内部走 writeErrFrom。
func writeErrOperation(w http.ResponseWriter, err error) {
	writeErrFrom(w, err, http.StatusBadRequest)
}

// writeErrBody 是三个错误出口的公共实现：body 里除 error 原文外还带 code/params（有才带）。
func writeErrBody(w http.ResponseWriter, status int, msg string, ref uiMsg) {
	body := map[string]interface{}{"ok": false, "error": msg}
	if ref.Code != "" {
		body["code"] = ref.Code
		if len(ref.Params) > 0 {
			body["params"] = ref.Params
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

// sessionInfo is the JSON summary of a live terminal session.
type sessionInfo struct {
	ID      string `json:"id"`
	Created string `json:"created"`
	Size    int64  `json:"size"`
	Exited  bool   `json:"exited"`
}

// handleSessions 返回活动终端会话列表（供前端启动时恢复会话）。
func (m *AdminMux) handleSessions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{"ok": true, "sessions": m.sessions.list()})
}

// handleSessionHistory 读取指定会话的历史镜像内容。
func (m *AdminMux) handleSessionHistory(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeErrU(w, http.StatusBadRequest, "err_need_id", "缺少 id")
		return
	}
	data, err := m.sessions.history(id)
	if err != nil {
		writeErr(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "id": id, "size": len(data), "content": string(data)})
}

// handleCloseSession 关闭指定会话（终止进程 + 删除历史文件）。
func (m *AdminMux) handleCloseSession(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeErrU(w, http.StatusBadRequest, "err_need_id", "缺少 id")
		return
	}
	if err := m.sessions.closeByID(id); err != nil {
		writeErr(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "id": id})
}

// handleClearSession 清空指定会话的历史镜像文件（前端「清屏」同步）。
func (m *AdminMux) handleClearSession(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeErrU(w, http.StatusBadRequest, "err_need_id", "缺少 id")
		return
	}
	if err := m.sessions.clearHistory(id); err != nil {
		writeErr(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "id": id})
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
			serveBytes(w, r, reqPath, b)
			return
		}
		// 不存在的路径回退到 index.html 以支持 SPA 前端路由
		if b, err := fs.ReadFile(fsys, "index.html"); err == nil {
			if base != "" {
				b = rewriteIndexBase(b, base)
			}
			serveBytes(w, r, "index.html", b)
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

// serveBytes 输出一个内嵌静态文件，并按文件类型带上缓存策略：
//
//   - `assets/**` 是 Vite 构建的**内容哈希产物**（index-DAFlHLaA.css / LogView-xxx.js），
//     内容一变文件名就变，因此可以放心长期强缓存：升级后 index.html 会指向新文件名，
//     浏览器直接拿新的，不会用到旧字节。这一层是控制台首屏的最大收益（几个百 KB 的
//     bundle 不再每次回源）。
//   - 其余文件（index.html / callback.html 等入口文件，文件名不带哈希）必须**每次回源
//     校验**（no-cache + ETag）：缓存住就会在升级后继续指向已被替换掉的旧哈希资源，
//     表现为「重启了控制台还是旧界面」——这正是 dsh 插件 bundle 那边踩过的坑。
//     ETag 由实际输出的字节（含注入的 base href）算出，校验命中回 304，不重传正文。
func serveBytes(w http.ResponseWriter, r *http.Request, name string, data []byte) {
	if ct := mimeTypeByExt(path.Ext(name)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		etag := etagOf(data)
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "no-cache")
		if etagMatches(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Write(data)
}

// etagOf 用内容摘要生成强 ETag（内嵌 FS 没有可用的修改时间，内容摘要才是可靠校验器）。
func etagOf(data []byte) string {
	sum := sha256.Sum256(data)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

// etagMatches 判断请求头里的 If-None-Match 是否命中当前 ETag：支持逗号分隔的多值与
// `*`，并忽略弱校验前缀（W/）—— 浏览器侧我们只发强 ETag，这里宽松匹配即可。
func etagMatches(header, etag string) bool {
	if header == "" {
		return false
	}
	if strings.TrimSpace(header) == "*" {
		return true
	}
	for _, part := range strings.Split(header, ",") {
		p := strings.TrimSpace(part)
		p = strings.TrimPrefix(p, "W/")
		if p == etag {
			return true
		}
	}
	return false
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

// --- 飞牛入口（当前访问环境下经网关访问 dsh 的地址） ---

// browserOrigin 推断浏览器实际使用的访问源（scheme://host[:port]）。
//
// 控制台经 fnOS 网关（nginx → admin unix socket）提供，请求本身不带浏览器地址，
// 但浏览器发出的同源请求会带上 Origin / Referer —— 其值就是控制台当前访问地址
// （形如 http://192.168.1.111:5666/app/Harness/）。两者都拿不到时退回请求的
// Host（网关通常原样透传），scheme 由 X-Forwarded-Proto 或连接本身推断。
func browserOrigin(r *http.Request) string {
	for _, h := range []string{r.Header.Get("Origin"), r.Header.Get("Referer")} {
		if u, err := url.Parse(strings.TrimSpace(h)); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
			return u.Scheme + "://" + u.Host
		}
	}
	host := firstHeaderValue(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = firstHeaderValue(r.Host)
	}
	if host == "" {
		return ""
	}
	scheme := firstHeaderValue(r.Header.Get("X-Forwarded-Proto"))
	if scheme != "http" && scheme != "https" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	return scheme + "://" + host
}

// firstHeaderValue 取头部里最靠左（最原始）的那个值并小写化，兼容转发头
// 形如 "http, https" 的逗号串。
func firstHeaderValue(v string) string {
	if i := strings.Index(v, ","); i >= 0 {
		v = v[:i]
	}
	return strings.ToLower(strings.TrimSpace(v))
}

// fnosEntryURL 计算当前访问环境下的「飞牛入口」：控制台的当前访问地址剥离控制台
// baseurl（HARNESS_ADMIN_BASEURL）后得到飞牛 OS 的访问源，再拼上控制台入口下的
// dsh 反代路径 `/gui`。
//
// 为什么不拼独立 dsh 入口（HARNESS_PROXY_BASEURL，/app/Harness/dsh）：
// 飞牛 App 的网关鉴权是**按入口（gatewayPrefix）注入**的，控制台入口的凭据不会
// 自动带到独立的 dsh 入口；App 内从控制台跳过去会先收到 trim_http_cgi 的
// `invalid token`（需手动刷新）。把 dsh 挂在控制台入口之下（/app/Harness/gui），
// 由控制台自己的 app.sock 反代到 dsh，请求就始终在控制台入口的鉴权范围内。
//
// 例：控制台 http://192.168.1.111:5666/app/Harness
// → 飞牛 OS 访问源 http://192.168.1.111:5666
// → http://192.168.1.111:5666/app/Harness/gui
//
// 拿不到访问地址（无 Origin/Referer/Host）时返回空串，由前端退回浏览器自身地址。
func (m *AdminMux) fnosEntryURL(r *http.Request) string {
	base := strings.TrimRight(m.renv.AdminBaseURL, "/")
	if base == "" {
		// 没有控制台 baseurl（例如端口直连、非网关部署）：没有可拼的网关入口。
		return ""
	}
	if m.dshUI == nil {
		return ""
	}
	origin := browserOrigin(r)
	if origin == "" {
		return ""
	}
	// 控制台在当前访问环境下的完整地址，剥离控制台 baseurl 后剩下的就是飞牛 OS 访问源。
	consoleURL := strings.TrimRight(origin, "/") + base
	fnosOrigin := strings.TrimSuffix(consoleURL, base)
	if fnosOrigin == "" {
		return ""
	}
	return fnosOrigin + m.dshUI.mount.dir()
}

// --- Settings API ---
func (m *AdminMux) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	cfg := GetConfig()
	locked := m.dsh.Running()
	writeJSON(w, map[string]interface{}{
		"config": cfg,
		"locked": locked,
		"runtime": map[string]interface{}{
			"configFile":   m.renv.ConfigFile,
			"adminSock":    m.renv.AdminSock,
			"adminBaseURL": m.renv.AdminBaseURL,
			"appName":      m.renv.TRIMAppName,
			"proxyPort":    cfg.ProxyPort,
			// dsh 服务在平台网关（fnOS）下占据的挂载 baseurl，前端据此自行换算
			// 「飞牛入口」（后端换算不出来时兜底用）。
			"proxyBaseURL": m.renv.ProxyBaseURL,
			// 控制台入口下的 dsh 反代路径（/app/Harness/gui/）。「飞牛入口」用它，
			// 不再用独立的 /app/Harness/dsh（App 的网关鉴权按入口注入，跨入口带不过去）。
			"consoleDshURL": strings.TrimRight(m.renv.AdminBaseURL, "/") + "/gui/",
			// 当前访问环境下的「飞牛入口」地址；推不出（拿不到访问地址）时为空。
			"fnosEntryURL": m.fnosEntryURL(r),
			// 当前 node（平台经 PATH 提供的 node24）自身的堆上限（MB，由
			// v8.getHeapStatistics().heap_size_limit 折算）。它就是「自动设置」时
			// dsh 实际拿到的上限，前端在开关打开时用它展示，不再展示持久化的手动值。
			// 探测不到时为 0，前端显示空。
			"nodeHeapLimitMB": detectNodeHeapLimitMB(),
			// 主目录（即 dsh 的 HOME）的语义路径与实际系统路径。主目录固定为本应用的
			// shares 目录，**不可切换**（见 DshManager.effectiveHome 的说明）。
			"defaultHomeSemantic": m.defaultHomeSemantic(),
			"defaultHomeDir":      m.dsh.effectiveHome(),
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
		writeErrU(w, http.StatusBadRequest, "err_config_format", "配置格式错误")
		return
	}
	// 保存前校验密码强度：仅当开启登录鉴权时校验（≥8位，含字母、数字、标点）；
	// 关闭鉴权时不校验，允许保留任意历史密码，避免切换开关被旧密码拦截。
	if req.Config.AuthEnabled && req.Config.Password != "" {
		if v := validatePassword(req.Config.Password); v != "" {
			writeErrU(w, http.StatusBadRequest, "err_password_weak", "密码不符合要求：%s", "detail", v)
			return
		}
	}
	// 校验登录有效期（小时）：必须为正整数，且不超过 720 小时（30 天）
	if req.Config.AuthTTLHours <= 0 {
		writeErrU(w, http.StatusBadRequest, "err_ttl_min", "登录有效期必须大于 0 小时")
		return
	}
	if req.Config.AuthTTLHours > 720 {
		writeErrU(w, http.StatusBadRequest, "err_ttl_max", "登录有效期不能超过 720 小时（30 天）")
		return
	}
	// 校验 dsh 内存限制（MB）：不超过 65536 MB（64GB）。
	// 未设置（<=0，例如旧前端缓存提交的空值）不拒绝，按当前 node 版本自身的堆上限
	// 补齐（与 LoadConfig 的默认值同源），这样「默认值」始终是这台机器上 node 的
	// 实际上限，而不是某个写死的数字。
	if req.Config.DshMemLimit > 65536 {
		writeErrU(w, http.StatusBadRequest, "err_mem_too_big", "dsh 内存限制不能超过 65536 MB（64GB）")
		return
	}
	// 内存限制的兜底：默认值取决于 PATH 中 node 自身的堆上限。
	if req.Config.DshMemLimit <= 0 {
		req.Config.DshMemLimit = defaultDshMemLimit(0)
	}
	if req.Config.DshMemLimit <= 0 {
		writeErrU(w, http.StatusBadRequest, "err_mem_zero", "dsh 内存限制必须大于 0 MB")
		return
	}
	// npm 镜像源（设置页「npm 镜像源」）：只接受设置页列出的那几个值。
	// 空串表示**旧版前端缓存**没有提交该字段，按「未修改」处理，沿用已保存的选择
	// （不能因为一次旧版前端保存就把用户选好的源重置掉）。
	if req.Config.NpmRegistry == "" {
		req.Config.NpmRegistry = GetConfig().NpmRegistry
	}
	if req.Config.NpmRegistry == "" {
		req.Config.NpmRegistry = npmRegistryDefault
	}
	if !validNpmRegistry(req.Config.NpmRegistry) {
		writeErrU(w, http.StatusBadRequest, "err_npm_registry_invalid",
			"未知的 npm 镜像源: %s", "registry", req.Config.NpmRegistry)
		return
	}
	prevNpmRegistry := normalizeNpmRegistry(GetConfig().NpmRegistry)
	// 手动设置的 node 堆内存上限过低时拒绝保存（前端同阈值红字提示并阻止提交）：
	// 这个量级下 dsh 起来就会频繁 GC / OOM，比「保存成功但跑不稳」更糟。
	// 「自动设置」不受此限：自动模式不使用这个持久化值，它可能是历史遗留的小数值。
	if !req.Config.DshMemAuto && req.Config.DshMemLimit < minDshMemLimitMB {
		writeErrU(w, http.StatusBadRequest, "err_mem_too_low", "node 堆内存上限过低，请增大分配（至少 %s MB）", "min", strconv.Itoa(minDshMemLimitMB))
		return
	}
	locked := m.dsh.Running()
	// 校验两个端口：都必须在 1025-65535（应用以自身 uid 运行，绑定 <1025 的特权
	// 端口需要 CAP_NET_BIND_SERVICE，注定失败），且不能相同（两者会争抢同一个 TCP 端口）。
	// 反代端口的字段缺失/为 0（旧版前端缓存提交的配置）按“未修改”处理，沿用已存端口。
	if req.Config.ProxyPort == 0 {
		req.Config.ProxyPort = GetConfig().ProxyPort
	}
	if !validListenPort(req.Config.ProxyPort) {
		writeErrU(w, http.StatusBadRequest, "err_port_proxy_range", "反代端口必须在 %s-%s 之间", "min", strconv.Itoa(minListenPort), "max", strconv.Itoa(maxListenPort))
		return
	}
	// dsh 运行中时端口被锁定（SaveConfig 会保留旧值），比较基准也要用锁定的旧值。
	effDshPort := req.Config.DshPort
	if locked {
		effDshPort = GetConfig().DshPort
	}
	// dsh 端口同样必须校验：历史上这里只拿它和反代端口比相等，落盘一个 0/超出范围的
	// 值会让 `dsh web --port 0` 随机端口、反代永远停在等待页（见 loadJSONFile 的兜底）。
	if !validListenPort(effDshPort) {
		writeErrU(w, http.StatusBadRequest, "err_port_dsh_range", "dsh 端口必须在 %s-%s 之间", "min", strconv.Itoa(minListenPort), "max", strconv.Itoa(maxListenPort))
		return
	}
	if req.Config.ProxyPort == effDshPort {
		writeErrU(w, http.StatusBadRequest, "err_port_same", "反代端口不能与 dsh 端口相同")
		return
	}
	// 反代端口变动需要即时重绑监听（反代是本进程自己的监听，不像 dsh 端口那样必须
	// 停 dsh 才能改）。绑定失败直接拒绝保存，此时配置与旧监听都保持原样。
	if err := m.rebindProxyPort(req.Config.ProxyPort); err != nil {
		writeErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := SaveConfig(m.renv, req.Config, locked); err != nil {
		writeErrU(w, http.StatusInternalServerError, "err_save_failed", "保存失败: %s", "detail", err.Error())
		return
	}
	// 切换镜像源后，手上那些版本快照都来自**旧源**：异步跑一次完整检测（checkOnce），
	// 用新源重拉 dsh 版本列表与两个受管插件的最新版，让概览页尽快反映新源。
	//   - 拉取失败时 refreshVersions / refreshPluginStatus 都保留上一份缓存并写错误，
	//     不会把「新源不可达」误报成「没有版本」；
	//   - 它确实会顺带起一次只读的 `dsh plugin list`（本函数最后一次检测），用户可以
	//     接受这一次开销：换源之后界面继续显示旧源的「最新版」更容易误导人。
	if req.Config.NpmRegistry != prevNpmRegistry && m.update != nil && m.update.server != nil {
		go m.update.checkOnce()
	}
	writeJSON(w, map[string]interface{}{"ok": true, "locked": locked, "config": GetConfig()})
}

// rebindProxyPort 把反代的 TCP 监听（根挂载）切换到 port。
//
// 端口未变时不做任何事。改动时**先绑定新端口**再关闭旧监听：占用/无权限等问题会在
// startProxy 里同步暴露，调用方据此拒绝保存，旧监听仍在服务，端口不会出现“配置改了
// 但没人监听”的空窗。成功切换不需要重启控制台，也不需要动 dsh。
//
// 注意：本函数只切监听，不写配置 —— 配置由 handleSaveSettings 在重绑成功后统一保存，
// 保证失败路径（保存出错）不会留下“监听变了但磁盘没变”的顺序歧义：此时内存与实际
// 监听一致（SaveConfig 先更新内存再落盘），最坏只是下次启动回退到磁盘上的旧端口。
func (m *AdminMux) rebindProxyPort(port int) error {
	old := GetConfig().ProxyPort
	if port == old {
		return nil
	}
	if err := startProxy(port, m.auth, m.dsh, m.boot, m); err != nil {
		return fmt.Errorf("反代端口 %d 监听失败: %v", port, err)
	}
	logInfo("[proxy] reverse proxy port switched %d -> %d", old, port)
	return nil
}

// handleGetLogs 读取日志文件内容并返回给前端。
func (m *AdminMux) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	path := m.renv.LogFile
	if path == "" {
		writeErrU(w, http.StatusNotFound, "err_log_not_configured", "日志文件未配置")
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, map[string]interface{}{"ok": true, "path": path, "content": "", "exists": false})
			return
		}
		writeErrU(w, http.StatusInternalServerError, "err_log_read_failed", "读取日志失败: %s", "detail", err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "path": path, "content": string(data), "exists": true})
}

// handleDownloadLog 以附件形式下发日志原文件，供前端“导出”按钮下载。
func (m *AdminMux) handleDownloadLog(w http.ResponseWriter, r *http.Request) {
	path := m.renv.LogFile
	if path == "" {
		writeErrU(w, http.StatusNotFound, "err_log_not_configured", "日志文件未配置")
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeErrU(w, http.StatusNotFound, "err_log_missing", "日志文件不存在")
			return
		}
		writeErrU(w, http.StatusInternalServerError, "err_log_read_failed", "读取日志失败: %s", "detail", err.Error())
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
		writeErrU(w, http.StatusConflict, "err_dsh_running", "dsh 已在运行")
		return
	}
	// 「没有选中的版本」在这里就拦住并给中文指引：DshManager.Start 那条错误是英文的
	// （它会被日志打印，见 AGENTS 规则 7），直接透出会让用户看到一句英文。
	if m.dsh.selectedDshVersion() == "" {
		writeErrU(w, http.StatusConflict, "err_no_dsh_version", "未安装 dsh 服务：请先在控制台「概览」页的版本列表里下载一个版本，再点「切换」")
		return
	}
	if err := m.dsh.Start(); err != nil {
		writeErrU(w, http.StatusInternalServerError, "err_dsh_start_failed", "启动失败: %s", "detail", err.Error())
		return
	}
	// 启动后等待并捕获一次性访问 token（新版 dsh 打印在启动日志里），用 token
	// 换取 dsh 会话 cookie（反代转发时携带该 cookie），并标记本代凭据已落定，
	// 否则反代会一直停在等待页（见 DshManager.SessionSettled）。
	go captureDshSession(m.dsh)
	// dsh 起来了才查得到插件：让市场版本从「—」变成实际值（见 RefreshMarketAfterDshStart）。
	m.update.RefreshMarketAfterDshStart()
	writeJSON(w, m.dsh.Status())
}

func (m *AdminMux) handleDshStop(w http.ResponseWriter, r *http.Request) {
	if err := m.dsh.Stop(); err != nil {
		writeErrU(w, http.StatusInternalServerError, "err_dsh_stop_failed", "停止失败: %s", "detail", err.Error())
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
	// 重启后等待并捕获新的访问 token（每次启动 dsh 都会生成新的 token），用 token
	// 换取 dsh 会话 cookie，并标记本代凭据已落定（反代据此放行等待页）。
	go captureDshSession(m.dsh)
	// dsh 起来了才查得到插件：让市场版本从「—」变成实际值（见 RefreshMarketAfterDshStart）。
	m.update.RefreshMarketAfterDshStart()
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

// --- Plugin management (work 区插件卡片) ---

// handleListPlugins 返回 dsh 的 web 插件列表（解析 `dsh plugin --profile web list`），
// 并按官方 dsh 的 bundle 选择给每个插件标注启停状态：
//   - 包声明了 `dsh.bundle`：在 `dsh.profile.bundles` 里 = 已启用，不在 = 已停用；
//   - 没有声明 `dsh.bundle` 的普通依赖不是官方插件页里的 bundle，控制台不提供启停。
func (m *AdminMux) handleListPlugins(w http.ResponseWriter, r *http.Request) {
	out, err := m.dsh.runPluginCmd("list")
	if err != nil {
		writeErrU(w, http.StatusInternalServerError, "err_plugin_list_failed", "执行插件列表失败: %s", "detail", err.Error())
		return
	}
	plugins := parsePluginList(out)
	profileWebDir := filepath.Join(m.dsh.effectiveHome(), ".dsh", "profiles", "web")
	bundles, err := readProfileBundles(profileWebDir)
	if err != nil {
		logWarn("[plugin] read profile bundles failed: %v", err)
		writeErrU(w, http.StatusInternalServerError, "err_plugin_profile_failed", "读取 profile 失败: %s", "detail", err.Error())
		return
	}
	selected := make(map[string]bool, len(bundles))
	for _, name := range bundles {
		selected[name] = true
	}
	for i := range plugins {
		name := plugins[i].Name
		meta, err := readPluginManifest(profileWebDir, name)
		if err != nil {
			// 读不到 manifest 就当作「不是 bundle」：前端因此不渲染启停开关，
			// 而不是给一个点下去必定 400 的开关。
			logWarn("[plugin] read manifest of %s failed: %v", name, err)
			continue
		}
		plugins[i].Bundle = meta.declaresBundle()
		if plugins[i].Bundle {
			plugins[i].Disabled = !selected[name]
		}
		// 该插件若被启用，是否需要重启 dsh 服务才能生效（客户端插件/带原生依赖）
		plugins[i].NeedsRestart = meta.hasClientPart() || holdsNativeAddon(profileWebDir, name)
	}
	writeJSON(w, map[string]interface{}{"ok": true, "plugins": plugins, "raw": out})
}

type togglePluginReq struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// validPluginName 校验请求体里的插件名。名字会参与两处危险动作：
//   - `packageDeclaresBundle(profileWebDir, name)` 把它拼进
//     `filepath.Join(profileWebDir, "node_modules", name, "package.json")` ——
//     `../../..` 之类的名字可以穿越到任意目录读 package.json；
//   - `dsh plugin --profile web remove <name>` 把它当 argv —— 以 `-` 开头的名字
//     会被 dsh 当成选项（argv 注入）。
//
// 规则与 profile.go 的 packageNameRe 一致（npm 包名），不合法的一律 400：宁可拒掉
// 一个奇怪但合法的名字，也不能让控制台变成任意文件读取/选项注入的入口。
func validPluginName(name string) bool {
	return packageNameRe.MatchString(name)
}

// pluginProfileWriteBusy 报告此刻是否有别的操作正在写 profile 清单。启停是「读-改-写」
// 整个 package.json，而 `dsh plugin add/remove`（控制台插件页与插件市场都走它）也在写
// 同一个文件：两个写入者各写一次原子 rename，必有一端静默丢失（依赖或被启停状态）。
// 因此写之前先问一句，忙就回 409 让用户稍后重试。
//
// 判据三个，都是本机事实：
//   - 控制台自己发起的 `dsh plugin …` 正在跑（PluginCmdRunning，覆盖整条命令的时长）；
//   - dsh 的 profile 写锁仍被活着的进程持有（profileLockHeld；陈旧锁不算）——
//     pnpm 写清单时确实持有它，但只在写的那一瞬，所以它只是补充；
//   - 插件市场自己在装/更新（marketBusyFn，市场面板发起时控制台没有命令计数）。
// 市场不回答（没装/未就绪）按不忙处理，与忙守卫的既有语义一致。
func (m *AdminMux) pluginProfileWriteBusy() bool {
	if m.dsh == nil {
		return false
	}
	if m.dsh.PluginCmdRunning() || m.dsh.profileLockHeld() {
		return true
	}
	if m.update != nil {
		if busy, _ := marketBusyFn(m.update); busy {
			return true
		}
	}
	return false
}

// pluginDeclaresBundle 是 packageDeclaresBundle 的「读不到就当 false」版本，供请求路径判分支用：
// 真正的读取错误会由随后的 setPluginBundleEnabled 原样报出来。
func pluginDeclaresBundle(profileWebDir, name string) bool {
	declares, err := packageDeclaresBundle(profileWebDir, name)
	return err == nil && declares
}

// handleTogglePlugin 按官方 dsh 的包级开关启停单个插件：
// 停用 = 从 `dsh.profile.bundles` 移除（依赖保留），启用 = 追加到列表末尾。
// 与官方 setBundleEnabled 一致，不写 cordis.patch.yml，也不同步插件市场的 state.json。
//
// 启用前还要过官方那条**兼容性栅栏**（selectBundle 的 incompatible-version）：插件对
// 当前 dsh 版本的 `engines.dsh` / dsh peer 依赖不满足时拒绝启用，除非 profile 的
// compatibility.json 里对该 `name@version` + 当前版本有显式放行记录。
//
// 另外判断该插件启用后是否需要重启 dsh 服务才能生效，返回给前端决定提示文案。
func (m *AdminMux) handleTogglePlugin(w http.ResponseWriter, r *http.Request) {
	var body togglePluginReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeErrU(w, http.StatusBadRequest, "err_need_plugin_name", "缺少插件名")
		return
	}
	// 名字会参与拼路径读取 package.json，必须先校验（见 validPluginName）。
	if !validPluginName(body.Name) {
		writeErrU(w, http.StatusBadRequest, "err_plugin_name_invalid", "插件名不合法（仅允许 npm 包名）")
		return
	}
	// 写盘竞态：别和正在安装/卸载插件的进程抢同一份 profile 清单。
	if m.pluginProfileWriteBusy() {
		writeErrOperation(w, busyf("err_plugin_profile_busy",
			"插件安装/卸载正在进行（profile 清单正在被写入），请等它结束后再启停"))
		return
	}
	profileWebDir := filepath.Join(m.dsh.effectiveHome(), ".dsh", "profiles", "web")
	// 兼容性只在「启用」时判：停用永远安全，而且启用会让 dsh 在下次编排时真的加载它。
	// 先确认它确实是 bundle（普通依赖由 setPluginBundleEnabled 回 err_plugin_not_bundle，
	// 与官方 selectBundle 的顺序一致），再判版本栅栏。
	if body.Enabled && pluginDeclaresBundle(profileWebDir, body.Name) {
		if runtime := m.dsh.selectedDshVersion(); runtime != "" {
			issues, manifestName, version, parsed := checkPluginCompatibility(profileWebDir, body.Name, runtime)
			if len(issues) > 0 {
				// 放行记录的键用 manifest 里的 name（官方就是这么写的），目录名只作兜底。
				if pluginVersionExempt(profileWebDir, firstNonEmpty(manifestName, body.Name), version, runtime) {
					logInfo("[plugin] %s@%s is incompatible with dsh %s but explicitly exempted; enabling",
						body.Name, version, runtime)
				} else {
					requirement := requirementText(issues)
					if !parsed {
						// 解析不了的写法按官方语义算「无效范围 → 不满足」，同时把缺口记出来。
						logWarn("[plugin] compatibility range of %s is not understood by the console (%s)",
							body.Name, requirement)
					} else {
						logWarn("[plugin] %s@%s refused for dsh %s: %s",
							body.Name, version, runtime, requirement)
					}
					writeErrU(w, http.StatusBadRequest, "err_plugin_incompatible",
						"插件 %s 与当前 dsh %s 不兼容（%s），启用后 dsh 可能崩溃或直接跳过它；请升级插件，或在 dsh 的插件管理里显式放行该版本",
						"pkg", body.Name, "runtime", runtime, "requirement", requirement)
					return
				}
			}
		}
	}
	changed, err := setPluginBundleEnabled(profileWebDir, body.Name, body.Enabled)
	if errors.Is(err, errNotBundle) {
		logWarn("[plugin] toggle %s -> enabled=%v refused: not a bundle (%v)", body.Name, body.Enabled, err)
		writeErrU(w, http.StatusBadRequest, "err_plugin_not_bundle", "该插件未声明 dsh.bundle，无法按官方插件层启停")
		return
	}
	if err != nil {
		logError("[plugin] toggle %s -> enabled=%v failed: %v", body.Name, body.Enabled, err)
		writeErrU(w, http.StatusInternalServerError, "err_plugin_profile_failed", "更新 profile 失败: %s", "detail", err.Error())
		return
	}
	restart := body.Enabled && needsRestart(profileWebDir, body.Name)
	logInfo("[plugin] bundle %s -> enabled=%v changed=%v restart=%v", body.Name, body.Enabled, changed, restart)
	writeJSON(w, map[string]interface{}{
		"ok":      true,
		"name":    body.Name,
		"enabled": body.Enabled,
		"changed": changed,
		"restart": restart,
		"refresh": true,
	})
}

type removePluginReq struct {
	Name string `json:"name"`
}

// handleRemovePlugin 卸载指定插件（`dsh plugin --profile web remove <name>`）。
func (m *AdminMux) handleRemovePlugin(w http.ResponseWriter, r *http.Request) {
	var body removePluginReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeErrU(w, http.StatusBadRequest, "err_need_plugin_name", "缺少插件名")
		return
	}
	// 名字会原样成为 `dsh plugin … remove <name>` 的 argv：以 "-" 开头会被当成选项，
	// 含路径分隔符的名字也不是包名。校验不通过直接 400，连命令都不执行。
	if !validPluginName(body.Name) {
		writeErrU(w, http.StatusBadRequest, "err_plugin_name_invalid", "插件名不合法（仅允许 npm 包名）")
		return
	}
	out, err := m.dsh.runPluginCmd("remove", body.Name)
	if err != nil {
		writeErrU(w, http.StatusInternalServerError, "err_plugin_remove_failed", "卸载失败: %s", "detail", err.Error())
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
	// 忙守卫（AGENTS 第 5 节硬约束）：插件的安装/卸载在 dsh 进程内持有 profile 写锁
	// （profiles/web/package.json.lock），而这里会删掉整个 profiles 目录并重启 dsh
	// （连带终止它的进程组）—— 正好把锁持有者一起带走：那次安装白做，还会留下陈旧锁
	// 让之后所有插件操作白等 120 秒。被拒时不产生任何删除与停机。
	if err := m.busyGuard("重置插件"); err != nil {
		writeErrOperation(w, err)
		return
	}
	// 再与「备份 / 恢复 / 更新」互斥：dsh 数据备份是分钟级的异步任务（见 update.go 的
	// BackupDshData），它读的正是这里要删的 ~/.dsh。撞上就会打出一份「成功但缺 profiles」
	// 的包，或者让那次备份在 walk 中途失败。同一把 applying 锁，双向互斥；拿不到就整次
	// 拒绝，不产生任何删除与停机。
	release, err := m.update.beginExclusiveDshDataOp()
	if err != nil {
		writeErrOperation(w, err)
		return
	}
	// 锁只覆盖「删 profiles」这段破坏性动作：后面那次重启 + node-pty patch 要跑几分钟，
	// 长期占着 applying 会让别的更新/备份一直拿不到锁。
	defer release()

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
			logError("[reset] failed to remove profiles dir %s: %v", profilesDir, derr)
		} else {
			profileDeleted = true
		}
	} else {
		logError("[reset] HOME unresolved, profiles dir not found - reset aborted")
	}

	// 仅当 ~/.dsh/profiles 删除成功后才继续；失败则立即返回，不重启也不 patch。
	if !profileDeleted {
		writeJSON(w, map[string]interface{}{
			"ok":             false,
			"error":          "profiles 目录删除失败",
			"code":           "err_reset_profiles_failed",
			"profileDeleted": false,
			"profilesDir":    profilesDir,
		})
		return
	}

	// 删除成功：把「重启 dsh + node-pty 自动 patch」放到后台执行，立即回包。
	// 这样三者仍严格按“删除成功 → 重启 dsh → patch”的顺序发生，且不阻塞请求。
	go func() {
		if rerr := m.restartDsh(); rerr != nil {
			logError("[reset] dsh restart failed: %v", rerr)
			return
		}
		if perr := m.patchNodePty(); perr != nil {
			logWarn("[node-pty] auto-patch after reset failed: %v", perr)
		}
	}()

	writeJSON(w, map[string]interface{}{
		"ok":             true,
		"started":        true,
		"profileDeleted": true,
		"profilesDir":    profilesDir,
	})
}

// patchNodePty 触发一次 node-pty 的版本固定与清理（重新执行 ensureNodePty）。
// 供插件重置后独立调用；后端的冷启动路径仍由 run 中的 ensureNodePty 承担，顺序不变。
// 重置场景下调用方已在前面重启过 dsh，此处忽略返回的重启标记，不再重复重启。
func (m *AdminMux) patchNodePty() error {
	_, err := ensureNodePty(m.renv, m.dsh.effectiveHome())
	return err
}

// defaultHomeSemantic 返回主目录的“相对/语义”路径。它是本应用的 shares 目录，
// 即 /var/apps/<AppName>/shares/<AppName>，其实际系统路径为启动时的 HOME
// （如 /vol1/@appshare/Harness）。资源页用它作为固定不可移除的第一张卡片。
func (m *AdminMux) defaultHomeSemantic() string {
	name := m.renv.TRIMAppName
	if name == "" {
		name = "Harness"
	}
	return "/var/apps/" + name + "/shares/" + name
}

// handleDshBackup 启动一次 dsh 数据备份（异步）：把当前 HOME 的 ~/.dsh 整体压缩打包为
// dsh-data-<版本>-<YYYYMMDDHHMMSS>.tar.gz 放进统一备份目录。
//
// 打包在服务端的 goroutine 里进行，本接口立刻返回 —— 弹窗关闭、页面刷新都不会终止它；
// 进度由前端轮询 /api/dsh/backup/status 得到，取消走 /api/dsh/backup/cancel。
func (m *AdminMux) handleDshBackup(w http.ResponseWriter, r *http.Request) {
	if err := m.update.BackupDshData(); err != nil {
		writeErrOperation(w, err)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":      true,
		"started": true,
		"status":  m.update.GetDshBackupStatus(),
	})
}

// handleDshBackupStatus 返回当前 dsh 数据备份的进度快照（供弹窗渲染进度条；
// 重新打开弹窗时也用它把进度追平）。
func (m *AdminMux) handleDshBackupStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{"ok": true, "status": m.update.GetDshBackupStatus()})
}

// handleDshBackupCancel 取消正在进行的 dsh 数据备份：打包循环在下一块边界中止，
// 不完整的备份文件被删除。没有备份在跑时是空操作（幂等）。
func (m *AdminMux) handleDshBackupCancel(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{"ok": true, "status": m.update.CancelDshBackup()})
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
		writeErrU(w, http.StatusBadRequest, "err_need_id", "缺少 id")
		return
	}
	// 网关访问没有 harness 会话可吊销（每次请求都由网关注入身份头，删记录也拦不住
	// 下一次访问），但允许手动把这条展示记录删掉，不用等闲置超时自然过期。
	// 语义必须说清楚：只清列表，不踢人；该用户再次访问会重新出现。
	if isGatewayVisitorID(body.ID) {
		if !m.auth.RemoveGatewayVisitor(body.ID) {
			writeJSON(w, map[string]interface{}{"ok": true, "deleted": false, "msg": "该访客不存在", "msgCode": "msg_visitor_not_found"})
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "deleted": true, "msg": "已删除该网关访问记录（该用户再次访问时会重新出现）", "msgCode": "msg_visitor_gateway_removed"})
		return
	}
	if !m.auth.RevokeVisitor(body.ID) {
		writeJSON(w, map[string]interface{}{"ok": true, "deleted": false, "msg": "该访客不存在", "msgCode": "msg_visitor_not_found"})
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "deleted": true, "msg": "已注销该访客", "msgCode": "msg_visitor_kicked"})
}

// --- fnOS proxy ---
func (m *AdminMux) handleFnos(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/api/fnos/")
	switch {
	case p == "user-access" && r.Method == http.MethodGet:
		uid := getUIDFromRequest(r)
		if uid <= 0 {
			writeJSON(w, map[string]interface{}{"ok": false, "error": "无法获取当前用户 UID", "code": "err_uid_missing"})
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
			writeErrU(w, http.StatusBadRequest, "err_uid_missing", "无法获取当前用户 UID")
			return
		}
		var body struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Path == "" {
			writeErrU(w, http.StatusBadRequest, "err_need_path", "缺少 path")
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
	logWarn("[admin] getUIDFromRequest: no valid uid in request")
	return 0
}

// --- AdminMux builder and socket serving (from adminmux.go) ---
func (m *AdminMux) buildHandler() http.Handler {
	base := strings.TrimRight(m.renv.AdminBaseURL, "/")
	term := &terminalHandler{mgr: m.sessions}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// 控制台入口下的 dsh 反代先于 baseurl 剥离：它的 mount 是完整浏览器路径
		// （/app/Harness/gui），由反代自己剥离；交给它之前不能先把 base 去掉。
		if m.dshUI != nil {
			gui := base + "/gui"
			if r.URL.Path == gui || strings.HasPrefix(r.URL.Path, gui+"/") {
				m.dshUI.ServeHTTP(w, r)
				return
			}
			// 兼容网关先把控制台 baseurl 剥掉再转发的部署：补回完整路径后交给反代。
			if base != "" && (r.URL.Path == "/gui" || strings.HasPrefix(r.URL.Path, "/gui/")) {
				r2 := r.Clone(r.Context())
				r2.URL.Path = base + r.URL.Path
				r2.URL.RawPath = ""
				m.dshUI.ServeHTTP(w, r2)
				return
			}
		}
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
		case p == "/api/dsh/busy" && r.Method == http.MethodGet:
			m.handleDshBusy(w, r)
		case p == "/api/dsh/status" && r.Method == http.MethodGet:
			writeJSON(w, m.dsh.Status())
			// 在 buildHandler 的 switch 中添加
		case p == "/api/dsh/restart" && r.Method == http.MethodPost:
			m.handleDshRestart(w, r)
		case p == "/api/dsh/backup" && r.Method == http.MethodPost:
			m.handleDshBackup(w, r)
		case p == "/api/dsh/backup/status" && r.Method == http.MethodGet:
			m.handleDshBackupStatus(w, r)
		case p == "/api/dsh/backup/cancel" && r.Method == http.MethodPost:
			m.handleDshBackupCancel(w, r)
		case p == "/api/dsh/data-backups" && r.Method == http.MethodGet:
			m.handleListDshDataBackups(w, r)
		case p == "/api/dsh/data-backups" && r.Method == http.MethodDelete:
			m.handleDeleteDshDataBackup(w, r)
		case p == "/api/dsh/data-restore" && r.Method == http.MethodPost:
			m.handleDshDataRestore(w, r)
		case p == "/api/dsh/data-restore/status" && r.Method == http.MethodGet:
			m.handleDshDataRestoreStatus(w, r)
		case p == "/api/dsh/backup-dir" && r.Method == http.MethodGet:
			m.handleBackupDir(w, r)
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
		case p == "/api/plugins/toggle" && r.Method == http.MethodPost:
			m.handleTogglePlugin(w, r)
		case p == "/api/plugins/reset" && r.Method == http.MethodPost:
			m.handleResetPlugins(w, r)
		case p == "/api/update/status" && r.Method == http.MethodGet:
			m.handleUpdateStatus(w, r)
		case p == "/api/update/check" && r.Method == http.MethodPost:
			m.handleUpdateCheck(w, r)
		case p == "/api/market/info" && r.Method == http.MethodGet:
			m.handleMarketInfo(w, r)
		case p == "/api/update/stream" && r.Method == http.MethodGet:
			m.handleUpdateStream(w, r)
		// dsh 服务多版本：列表 / 下载安装 / 取消 / 删除 / 切换（见 server.go）
		case p == "/api/dsh/versions" && r.Method == http.MethodGet:
			m.handleDshVersions(w, r)
		case p == "/api/dsh/versions/notes" && r.Method == http.MethodGet:
			m.handleDshVersionNotes(w, r)
		case p == "/api/dsh/versions/install" && r.Method == http.MethodPost:
			m.handleDshVersionInstall(w, r)
		case p == "/api/dsh/versions/cancel" && r.Method == http.MethodPost:
			m.handleDshVersionCancel(w, r)
		case p == "/api/dsh/versions/ack" && r.Method == http.MethodPost:
			m.handleDshVersionAck(w, r)
		case p == "/api/dsh/versions/delete" && r.Method == http.MethodPost:
			m.handleDshVersionDelete(w, r)
		case p == "/api/dsh/versions/switch" && r.Method == http.MethodPost:
			m.handleDshVersionSwitch(w, r)
		// 插件市场（dshmarket）：安装 / 更新 / 卸载 / 收起结果（见 market.go）
		case p == "/api/market/install" && r.Method == http.MethodPost:
			m.handleMarketInstall(w, r)
		case p == "/api/market/update" && r.Method == http.MethodPost:
			m.handleMarketUpdate(w, r)
		case p == "/api/market/remove" && r.Method == http.MethodPost:
			m.handleMarketRemove(w, r)
		case p == "/api/market/done" && r.Method == http.MethodPost:
			m.handleMarketDone(w, r)
		// 增强插件（dsh-fnos-advanced，飞牛目录选择器）：与市场同一套动作
		case p == "/api/advanced/install" && r.Method == http.MethodPost:
			m.handleAdvancedInstall(w, r)
		case p == "/api/advanced/update" && r.Method == http.MethodPost:
			m.handleAdvancedUpdate(w, r)
		case p == "/api/advanced/remove" && r.Method == http.MethodPost:
			m.handleAdvancedRemove(w, r)
		case p == "/api/advanced/done" && r.Method == http.MethodPost:
			m.handleAdvancedDone(w, r)
		// DeepSeek 官方账号（控制台侧完成授权，见 account.go）
		case p == "/api/account" && r.Method == http.MethodGet:
			m.handleAccountStatus(w, r)
		case p == "/api/account/login/start" && r.Method == http.MethodPost:
			m.handleAccountLoginStart(w, r)
		case p == "/api/account/login/status" && r.Method == http.MethodGet:
			m.handleAccountLoginStatus(w, r)
		case p == "/api/account/login/complete" && r.Method == http.MethodPost:
			m.handleAccountLoginComplete(w, r)
		case p == "/api/account/login/cancel" && r.Method == http.MethodPost:
			m.handleAccountLoginCancel(w, r)
		case p == "/api/account/logout" && r.Method == http.MethodPost:
			m.handleAccountLogout(w, r)
		case p == "/api/account/switch" && r.Method == http.MethodPost:
			m.handleAccountSwitch(w, r)
		case p == "/api/account/remove" && r.Method == http.MethodPost:
			m.handleAccountRemove(w, r)
		// OAuth 回调：浏览器/本地转发把 ?code=&state= 送到这里（自留路径，不转发给 dsh）
		case p == accountCallbackPath && r.Method == http.MethodGet:
			m.handleAccountCallback(w, r)
		case p == "/api/quickcmds" && r.Method == http.MethodGet:
			m.handleGetQuickCmds(w, r)
		case p == "/api/quickcmds" && r.Method == http.MethodPost:
			m.handleSaveQuickCmds(w, r)
		case p == "/api/sessions" && r.Method == http.MethodGet:
			m.handleSessions(w, r)
		case p == "/api/session/history" && r.Method == http.MethodGet:
			m.handleSessionHistory(w, r)
		case p == "/api/session" && r.Method == http.MethodDelete:
			m.handleCloseSession(w, r)
		case p == "/api/session/clear" && r.Method == http.MethodPost:
			m.handleClearSession(w, r)
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
		logError("[admin] chmod socket %s: %v", renv.AdminSock, e)
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
		writeErrU(w, http.StatusBadRequest, "err_bad_request", "请求格式错误: %s", "detail", err.Error())
		return
	}
	if len(req.Paths) == 0 {
		writeJSON(w, map[string]interface{}{"ok": true, "result": []map[string]string{}})
		return
	}

	result, err := m.fnos.ConvertPath(req.Paths, req.Language)
	if err != nil {
		writeErrU(w, http.StatusInternalServerError, "err_path_convert_failed", "路径转换失败: %s", "detail", err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "result": result})
}

// --- 版本检测 API ---

// dshBusySnapshot 汇总「是否有插件操作正在进行」，供停止/重启 dsh 的确认弹窗提示。
//
// 这两个按钮**刻意不硬挡**（点它是用户的即时意图），但要把代价说清楚：停 dsh 会
// 中断正在进行的插件安装/卸载，并且可能留下陈旧的 profile 写锁（之后插件列表与安装
// 都会白等 120 秒）。所以由前端在弹窗打开时按需查一次 —— 不放进高频轮询的
// /api/dsh/status，避免每次轮询都去探测市场。
//
// 拆成独立函数是为了可测：处理器只做 JSON 包装。
func (m *AdminMux) dshBusySnapshot() map[string]interface{} {
	// 控制台自己发起的插件命令：本地计数，不需要网络，优先报它（更准确）。
	if m.dsh != nil && m.dsh.PluginCmdRunning() {
		return map[string]interface{}{
			"ok": true, "busy": true, "source": "console", "detail": "dsh plugin …",
		}
	}
	// 市场面板内的安装/更新：它 spawn 的子进程，只能问市场自己。
	if busy, detail := marketBusyFn(m.update); busy {
		return map[string]interface{}{
			"ok": true, "busy": true, "source": "market", "detail": detail,
		}
	}
	return map[string]interface{}{"ok": true, "busy": false}
}

// handleDshBusy 返回 dshBusySnapshot（GET /api/dsh/busy）。
func (m *AdminMux) handleDshBusy(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, m.dshBusySnapshot())
}

// busyGuard 复用更新管理器的「停 dsh / 删 ~/.dsh 之前必须过忙守卫」检查
// （UpdateManager.replaceBusyGuard，来源是市场 /dsh-market/status 的 busy 与控制台
// 的插件命令计数）。凡是会停 dsh 或删 ~/.dsh 的控制台入口都要先过它，否则会把正在
// 持锁的 `dsh plugin …` 连根拔掉并留下陈旧锁（见 AGENTS 第 5 节 gotcha 2）。
// update 未装配时（部分单测）视为不忙。
func (m *AdminMux) busyGuard(action string) error {
	if m.update == nil {
		return nil
	}
	return m.update.replaceBusyGuard(action)
}

// handleUpdateStatus 返回当前 dsh、插件市场与增强插件的版本检测结果。
func (m *AdminMux) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	snap := m.update.snapshot()
	writeJSON(w, map[string]interface{}{
		"ok":       true,
		"dsh":      snap[updateKindDsh],
		"market":   snap[updateKindMarket],
		"advanced": snap[updateKindAdvanced],
		// server 是 dsh 服务多版本的快照（列表 + 选中 + 安装进度），见 server.go。
		"server": m.update.server.snapshot(),
	})
}

// handleUpdateCheck 执行一次手动检查更新（“检查更新”按钮）并返回最新结果。
// 同步执行：前端在拿到响应后即可依据结果提示“暂无更新”或显示红点。
// 三条链路一起检测：dsh、插件市场与增强插件都走 npm 镜像源（见 server.go 的
// refreshVersions 与 market.go 的 refreshManagedPluginStatuses）。
//
// 手动检查一律强制刷新 dsh 版本列表：用户点「检查更新」时看到的是缓存里那份
// 可能已过期的列表（TTL 5 分钟），而 dsh 的发布节奏很快。
func (m *AdminMux) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if err := m.update.server.refreshVersions(true); err != nil {
		logWarn("[dsh] version list refresh failed during manual check: %v", err)
	}
	m.update.checkOnce()
	snap := m.update.snapshot()
	writeJSON(w, map[string]interface{}{
		"ok":       true,
		"dsh":      snap[updateKindDsh],
		"market":   snap[updateKindMarket],
		"advanced": snap[updateKindAdvanced],
		"server":   m.update.server.snapshot(),
	})
}

// handleMarketInfo 返回市场安装位置的只读诊断信息（scope/dir/version/latest/
// updatable/reason）。不触发网络与磁盘写，供 UI 解释“为什么不能更新”。
func (m *AdminMux) handleMarketInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, m.update.marketInfo())
}

// handleUpdateStream 通过 SSE 推送更新检测结果变更（dsh / 市场 / 增强插件三份）。
func (m *AdminMux) handleUpdateStream(w http.ResponseWriter, r *http.Request) {
	setSSEHeaders(w)
	ctx := r.Context()
	send := func() {
		sseSend(w, "update", sseJSON(map[string]interface{}{
			"dsh":      m.update.getStatus(updateKindDsh),
			"market":   m.update.getStatus(updateKindMarket),
			"advanced": m.update.getStatus(updateKindAdvanced),
			// dsh 版本列表与安装进度（server.go）：下载/安装 dsh 版本时前端靠这一帧
			// 实时刷新对应行的进度，不额外开一条 SSE 通道。
			"server": m.update.server.snapshot(),
		}))
	}
	send() // 初始快照
	ch, unsub := m.update.subscribe()
	defer unsub()
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			send()
		case <-keepalive.C:
			// 心跳：写一行 SSE 注释，保证「始终有字节下行」。此前这里是个空分支，
			// 空闲时零字节：fnOS 网关 nginx（location /app/ 下 proxy_read_timeout
			// 300s）会在 5 分钟后判定上游超时并掐断长连接，前端在重连前收不到任何
			// 更新进度——下载明明在进行，页面却一直停在 0%（注释行不触发前端事件，
			// 仅用于保活）。
			ssePing(w)
		}
	}
}

// --- dsh 服务版本管理 API（见 server.go） ---

// handleDshVersions 返回 dsh 版本快照：可选版本列表（镜像源，已过滤 0.1.7-alpha.1
// 之前的）、本地已安装版本、当前选中版本、以及正在进行的安装进度。
//
// refresh=1 时强制联网刷新版本列表（弹窗里的「刷新」按钮）；默认只在缓存过期时拉。
func (m *AdminMux) handleDshVersions(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("refresh") == "1"
	if force {
		if err := m.update.server.refreshVersions(true); err != nil {
			// 列表拉取失败不算致命：下面照常返回本地已安装版本 + Error 字段。
			logWarn("[dsh] refresh versions failed: %v", err)
		}
	}
	writeJSON(w, map[string]interface{}{
		"ok":        true,
		"server":    m.update.server.snapshot(),
		"refreshed": force,
	})
}

// handleDshVersionInstall 启动下载并安装某个 dsh 版本（异步，进度经 SSE 推送）。
func (m *AdminMux) handleDshVersionInstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Version == "" {
		writeErrU(w, http.StatusBadRequest, "err_need_version", "缺少 version")
		return
	}
	if err := m.update.server.Install(body.Version); err != nil {
		writeErrOperation(w, err)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "started": true, "version": body.Version})
}

// handleDshVersionCancel 取消正在进行的 dsh 版本下载/安装，并清除安装目录与下载缓存。
func (m *AdminMux) handleDshVersionCancel(w http.ResponseWriter, r *http.Request) {
	cancelled := m.update.server.CancelInstall()
	writeJSON(w, map[string]interface{}{"ok": true, "cancelled": cancelled})
}

// handleDshVersionAck 收起「上一次安装的结果」（前端已把成功提示成 toast，或已关闭弹窗）。
//
// 为什么需要它：安装结果（done / error / cancelled）会作为终态留在内存里给前端显示，
// 而概览页切走会卸载、切回会重新挂载 —— 状态还在，前端就会把同一条成功提示再弹一次。
// 前端确认收到后调一次这个接口，终态即被清掉。
func (m *AdminMux) handleDshVersionAck(w http.ResponseWriter, r *http.Request) {
	m.update.server.clearInstallState()
	writeJSON(w, map[string]interface{}{"ok": true})
}

// handleDshVersionDelete 删除一个已安装的 dsh 版本目录（当前选中的版本不允许删除）。
func (m *AdminMux) handleDshVersionDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Version == "" {
		writeErrU(w, http.StatusBadRequest, "err_need_version", "缺少 version")
		return
	}
	if err := m.update.server.Delete(body.Version); err != nil {
		writeErrOperation(w, err)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "deleted": body.Version})
}

// handleDshVersionSwitch 切换到某个已安装的 dsh 版本：写配置 → 停 dsh → 用新版本启动。
// 后端不等 dsh 就绪（会话凭据由异步 captureDshSession 换取），前端刷新后即可看到选中标记。
func (m *AdminMux) handleDshVersionSwitch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Version == "" {
		writeErrU(w, http.StatusBadRequest, "err_need_version", "缺少 version")
		return
	}
	if err := m.update.server.Switch(body.Version); err != nil {
		writeErrOperation(w, err)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "switched": body.Version})
}

// handleDshVersionNotes 返回某个 dsh 版本的更新日志（上游 deepseek-ai/deepseek-harness
// 的 GitHub Release 正文，见 server.go 的 LookupVersionNotes）。
//
// 为什么是「点开才查」的独立接口、而不是塞进版本列表快照：日志只在用户点版本号时才要看，
// 塞进列表会让每次刷新版本列表都多打一次 GitHub API（未认证限流 60 次/小时），而版本列表
// 是高频刷新的。整仓 release 拉一次后会按版本号缓存在 notesIndex 里，后续点击直接命中。
//
// available=false 表示「这次没取到」（断网 / 限流），与「取到了但该版本没有日志」
// （available=true + 两个 body 都是空串）区分开，前端据此说不同的话。
func (m *AdminMux) handleDshVersionNotes(w http.ResponseWriter, r *http.Request) {
	version := r.URL.Query().Get("version")
	// 版本号会被拼进 GitHub URL 的 tag，一律先过 validVersionArg（与安装/切换共用）。
	if !validVersionArg(version) {
		writeErrU(w, http.StatusBadRequest, "err_need_version", "缺少 version")
		return
	}
	notes, available := m.update.server.LookupVersionNotes(version)
	writeJSON(w, map[string]interface{}{
		"ok":        true,
		"version":   version,
		"available": available,
		"note":      notes.Note,
		"noteEn":    notes.NoteEn,
	})
}

// --- 插件市场（dshmarket）API（见 market.go） ---

// handleMarketInstall 安装插件市场的最新版（异步，进度经 SSE 推送）。
func (m *AdminMux) handleMarketInstall(w http.ResponseWriter, r *http.Request) {
	if err := m.update.InstallMarket(); err != nil {
		writeErrOperation(w, err)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "started": true, "action": "install"})
}

// handleMarketUpdate 把插件市场更新到最新版（异步）。
func (m *AdminMux) handleMarketUpdate(w http.ResponseWriter, r *http.Request) {
	if err := m.update.UpdateMarket(); err != nil {
		writeErrOperation(w, err)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "started": true, "action": "update"})
}

// handleMarketRemove 卸载插件市场（异步）。
func (m *AdminMux) handleMarketRemove(w http.ResponseWriter, r *http.Request) {
	if err := m.update.RemoveMarket(); err != nil {
		writeErrOperation(w, err)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "started": true, "action": "remove"})
}

// handleMarketDone 清掉一次市场操作的终态（前端收起「已完成」提示时调用）。
func (m *AdminMux) handleMarketDone(w http.ResponseWriter, r *http.Request) {
	m.update.DoneMarket()
	writeJSON(w, map[string]interface{}{"ok": true})
}

// --- 增强插件（dsh-fnos-advanced）API（见 market.go） ---

// handleAdvancedInstall 安装增强插件的最新版（异步，进度经 SSE 推送）。
func (m *AdminMux) handleAdvancedInstall(w http.ResponseWriter, r *http.Request) {
	if err := m.update.InstallAdvanced(); err != nil {
		writeErrOperation(w, err)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "started": true, "action": "install"})
}

// handleAdvancedUpdate 把增强插件更新到最新版（异步）。
func (m *AdminMux) handleAdvancedUpdate(w http.ResponseWriter, r *http.Request) {
	if err := m.update.UpdateAdvanced(); err != nil {
		writeErrOperation(w, err)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "started": true, "action": "update"})
}

// handleAdvancedRemove 卸载增强插件（异步）。
func (m *AdminMux) handleAdvancedRemove(w http.ResponseWriter, r *http.Request) {
	if err := m.update.RemoveAdvanced(); err != nil {
		writeErrOperation(w, err)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "started": true, "action": "remove"})
}

// handleAdvancedDone 清掉一次增强插件操作的终态。
func (m *AdminMux) handleAdvancedDone(w http.ResponseWriter, r *http.Request) {
	m.update.DoneAdvanced()
	writeJSON(w, map[string]interface{}{"ok": true})
}

// --- dsh 数据备份与恢复 API ---

// handleListDshDataBackups 返回 dsh 数据备份列表（dsh-data-<版本>-<时间戳>.tar.gz）。
func (m *AdminMux) handleListDshDataBackups(w http.ResponseWriter, r *http.Request) {
	backups, err := m.update.ListDshDataBackups()
	if err != nil {
		writeErrU(w, http.StatusInternalServerError, "err_backup_list_failed", "读取备份列表失败: %s", "detail", err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "backups": backups})
}

type delDshDataReq struct {
	Name string `json:"name"`
}

// handleDeleteDshDataBackup 删除一个 dsh 数据备份文件。
func (m *AdminMux) handleDeleteDshDataBackup(w http.ResponseWriter, r *http.Request) {
	var body delDshDataReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeErrU(w, http.StatusBadRequest, "err_need_name", "缺少 name")
		return
	}
	if err := m.update.DeleteDshDataBackup(body.Name); err != nil {
		writeErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "deleted": body.Name})
}

type dshDataRestoreReq struct {
	Name string `json:"name"`
}

// handleDshDataRestore 触发 dsh 数据恢复（异步执行，前端通过 /api/dsh/data-restore/status 轮询）。
func (m *AdminMux) handleDshDataRestore(w http.ResponseWriter, r *http.Request) {
	var body dshDataRestoreReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeErrU(w, http.StatusBadRequest, "err_need_name", "缺少 name")
		return
	}
	dir := m.update.backupDir()
	fullPath := filepath.Join(dir, body.Name)
	if err := m.update.RestoreDshData(fullPath); err != nil {
		writeErrOperation(w, err)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "started": true})
}

// handleDshDataRestoreStatus 返回当前 dsh 数据恢复状态。
func (m *AdminMux) handleDshDataRestoreStatus(w http.ResponseWriter, r *http.Request) {
	status := m.update.GetDshRestoreStatus()
	writeJSON(w, map[string]interface{}{"ok": true, "status": status})
}

// handleBackupDir 返回统一备份目录的实际路径（供前端转换语义路径）。
func (m *AdminMux) handleBackupDir(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{"ok": true, "path": m.update.BackupDir()})
}
