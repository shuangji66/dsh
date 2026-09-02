package main

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// bootstrapScript is the browser-side patch that mirrors proxy.js BOOTSTRAP_SCRIPT.
// It follows REVERSE_PROXY_ADAPTATION.md:
//   - randomUUID polyfill (non-secure HTTP contexts),
//   - client privileged-state injection: __DSH_TRANSPORT__ ownsHost,
//   - module-loader hook that forces connection.isLoopback = true,
//   - settings-scope enqueue fix (keeps plugin-config / model-settings writable).
const bootstrapScript = `(function () {
  // 1. randomUUID polyfill for non-secure (HTTP IP) contexts
  var c = window.crypto;
  if (c && typeof c.randomUUID !== "function" && typeof c.getRandomValues === "function") {
    var g = c.getRandomValues.bind(c);
    var uuid = function () {
      var b = new Uint8Array(16); g(b);
      b[6]=(b[6]&15)|64; b[8]=(b[8]&63)|128;
      var h=Array.from(b,function(x){return ("0"+x.toString(16)).slice(-2)}).join("");
      return h.slice(0,8)+"-"+h.slice(8,12)+"-"+h.slice(12,16)+"-"+h.slice(16,20)+"-"+h.slice(20);
    };
    var ins=function(t){try{Object.defineProperty(t,"randomUUID",{configurable:true,writable:true,value:uuid});return typeof t.randomUUID==="function";}catch(_e){return false;}};
    if(!ins(c)&&Object.getPrototypeOf(c))ins(Object.getPrototypeOf(c));
  }

  // 2. 客户端特权状态注入：声明 ownsHost，走通上游原生特权分支
  // （client-connection 检测到 ownsHost 时把 isLoopback 初始化为 true）
  try { window.__DSH_TRANSPORT__ = Object.assign(window.__DSH_TRANSPORT__ || {}, { ownsHost: true }); } catch (_e) {}

  // 3. 模块加载器 Hook：单个 loader.load 内处理 connection 与 settings 两个模块
  //   - connection：劫持句柄，强制 isLoopback = true
  //   - ui-settings：修复 SettingsScopeController.enqueue，避免设置保存被丢弃
  var hookModuleLoader = function (loader) {
    if (!loader || typeof loader.load !== "function" || loader.__hooked) return loader;
    var rawLoad = loader.load.bind(loader);
    loader.load = function (handoff) {
      if (handoff && typeof handoff.id === "string" && typeof handoff.factory === "function") {
        if (handoff.id === "@deepseek-ai/dsh-client-connection") {
          var rawFactory = handoff.factory;
          handoff.factory = function () {
            var modExports = rawFactory.apply(this, arguments);
            if (modExports && typeof modExports.apply === "function") {
              var rawApply = modExports.apply;
              modExports.apply = function (ctx) {
                if (ctx && typeof ctx.provide === "function") {
                  var proxyCtx = new Proxy(ctx, {
                    get: function (target, prop, receiver) {
                      if (prop === "provide") {
                        return function (name, handle) {
                          if (name === "connection" && handle && typeof handle === "object") {
                            try { Object.defineProperty(handle, "isLoopback", { value: true, writable: true, configurable: true }); }
                            catch (_e) { handle.isLoopback = true; }
                          }
                          return Reflect.apply(target.provide, target, arguments);
                        };
                      }
                      return Reflect.get(target, prop, receiver);
                    }
                  });
                  return rawApply.call(this, proxyCtx);
                }
                return rawApply.apply(this, arguments);
              };
            }
            return modExports;
          };
        } else if (handoff.id === "@deepseek-ai/dsh-client-ui-settings") {
          var rawSettings = handoff.factory;
          handoff.factory = function () {
            var modExports = rawSettings.apply(this, arguments);
            try {
              var Ctl = modExports && modExports.SettingsScopeController;
              if (Ctl && Ctl.prototype && typeof Ctl.prototype.enqueue === "function") {
                var oe = Ctl.prototype.enqueue;
                Ctl.prototype.enqueue = function (op) {
                  if (this.disposed) return Promise.resolve();
                  var self = this;
                  var t = this.tail.then(function () { if (self.disposed) return; return op(); });
                  this.tail = t.catch(function () {}); return t;
                };
              }
            } catch (_e) {}
            return modExports;
          };
        }
      }
      return rawLoad(handoff);
    };
    loader.__hooked = true;
    return loader;
  };
  if (window.__ModuleLoader__) {
    hookModuleLoader(window.__ModuleLoader__);
  } else {
    var storedLoader = undefined;
    try {
      Object.defineProperty(window, "__ModuleLoader__", {
        configurable: true,
        enumerable: true,
        get: function () { return storedLoader; },
        set: function (val) { storedLoader = hookModuleLoader(val); }
      });
    } catch (_e) {}
  }
})();`

func injectIntoHTML(body []byte) []byte {
	inject := "<style>[data-slot=\"settings.action\"] { display:none !important; }</style><script>" + bootstrapScript + "</script>"
	s := string(body)
	idx := strings.Index(strings.ToLower(s), "<head")
	var pos int
	if idx != -1 {
		ci := strings.Index(s[idx:], ">")
		if ci != -1 {
			pos = idx + ci + 1
			return []byte(s[:pos] + inject + s[pos:])
		}
	}
	return []byte(inject + s)
}

func rewriteJSBundle(buf []byte) []byte {
	pairs := [][2]string{
		{`connection.isLoopback ? "host" : "memory"`, `"host"`},
		{`connection.isLoopback ? 'host' : 'memory'`, `'host'`},
		{`connection.isLoopback?"host":"memory"`, `"host"`},
		{`connection.isLoopback?'host':'memory'`, `'host'`},
	}
	s := string(buf)
	for _, p := range pairs {
		if strings.Contains(s, p[0]) {
			s = strings.ReplaceAll(s, p[0], p[1])
		}
	}
	return []byte(s)
}

// waitingPageHTML is served while the forwarded dsh backend is not yet ready.
// Its look mirrors the auth login page (see loginPageHTML), and it shows both
// Chinese and English text at once. A spinner replaces the previous hourglass
// glyph. Inline JS records when the wait started (sessionStorage) and reveals a
// timeout message once dsh has been unavailable for a while.
const waitingPageHTML = `<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="theme-color" content="#FAFAFA">
<title>服务启动中</title>
<style>
  :root{
    --brand:#6366F1;
    --brand-soft:rgba(99,102,241,.12);
    --bg:#FAFAFA;
    --surface:#FFFFFF;
    --line:#E8E8EC;
    --ink:#0A0A0A;
    --ink-soft:#6B6B6B;
    --ink-faint:#9C9C9C;
    --shadow:0 2px 10px rgba(0,0,0,.04);
    --err-bg:rgba(239,68,68,.08);
    --err-border:rgba(239,68,68,.35);
    --err-color:#dc2626;
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --bg:#0B0B0F;
      --surface:#111115;
      --line:#2A2A32;
      --ink:#EDEDF0;
      --ink-soft:#A6A6AD;
      --ink-faint:#8A8A92;
      --shadow:0 2px 10px rgba(0,0,0,.5);
      --err-bg:rgba(248,113,113,.12);
      --err-border:rgba(248,113,113,.35);
      --err-color:#f87171;
    }
  }
  *{box-sizing:border-box}
  html,body{height:100%}
  body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;
    font-family:'DM Sans',ui-sans-serif,system-ui,-apple-system,'Segoe UI',Roboto,
      'PingFang SC','Microsoft YaHei',sans-serif;
    background:var(--bg);color:var(--ink);-webkit-font-smoothing:antialiased;
    padding:24px}
  .wrap{width:100%;max-width:400px}
  .card{background:var(--surface);border:1px solid var(--line);border-radius:12px;
    padding:40px 32px;box-shadow:var(--shadow);overflow:hidden;text-align:center}
  .spinner{width:36px;height:36px;margin:0 auto 20px;border-radius:50%;
    border:3px solid var(--brand-soft);border-top-color:var(--brand);
    animation:spin 1s linear infinite}
  @keyframes spin{to{transform:rotate(360deg)}}
  h1{margin:0 0 8px;font-size:22px;font-weight:600;
    font-family:'General Sans','DM Sans',ui-sans-serif,system-ui,sans-serif;
    letter-spacing:-.02em;color:var(--ink)}
  .sub{margin:0;font-size:13px;color:var(--ink-soft);line-height:1.6}
  .sub .en{display:block;color:var(--ink-faint);margin-top:4px}
  .err{display:none;margin-top:20px;padding:10px 12px;background:var(--err-bg);
    border:1px solid var(--err-border);border-radius:6px;font-size:13px;
    color:var(--err-color);text-align:left;line-height:1.6;word-break:break-all}
  .err .en{display:block;color:var(--err-color);opacity:.8;margin-top:4px}
</style></head><body><div class="wrap">
  <div class="card">
    <div class="spinner"></div>
    <h1>服务启动中 / Starting service</h1>
    <p class="sub">dsh web 尚未就绪，页面将每隔 2 秒自动重试…
      <span class="en">dsh web is not ready yet. This page will retry every 2 seconds.</span>
    </p>
    <div class="err" id="timeout">
      dsh 服务启动失败，请检查日志，卸载不兼容的插件后重试
      <span class="en">Failed to start dsh. Please check the logs, uninstall incompatible plugins and retry.</span>
    </div>
  </div>
</div>
<script>
(function(){
  var KEY='dsh_start_wait_ts';
  var now=Date.now();
  var start=0;
  try{ start=parseInt(sessionStorage.getItem(KEY)||'0',10)||0; }catch(e){}
  if(!start){ start=now; try{ sessionStorage.setItem(KEY,String(start)); }catch(e){} }
  var waited=now-start;
  var timeoutEl=document.getElementById('timeout');
  var LIMIT=10000; // 10s
  if(!timeoutEl) return;
  if(waited>=LIMIT){ timeoutEl.style.display='block'; }
  else {
    setTimeout(function(){ timeoutEl.style.display='block'; }, LIMIT-waited+250);
  }
})();
</script>
</body></html>`

// serveWaitingPage writes the styled "starting" page used while the upstream
// dsh backend is not ready yet. It auto-refreshes every 2 seconds.
func serveWaitingPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Refresh", "2; url="+r.URL.RequestURI())
	w.WriteHeader(200)
	io.WriteString(w, waitingPageHTML)
}

// BackendChecker performs reachability checks on the upstream dsh port.
type BackendChecker struct {
	port int
}

func newBackendChecker(port int) *BackendChecker {
	return &BackendChecker{port: port}
}

func (b *BackendChecker) hostPort() string {
	return "127.0.0.1:" + strconv.Itoa(b.port)
}

func (b *BackendChecker) quick(timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(b.port), timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (b *BackendChecker) wait(max time.Duration) bool {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		if b.quick(2 * time.Second) {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return b.quick(1 * time.Second)
}

type reverseProxy struct {
	auth *Auth
	dsh  *DshManager
}

func newReverseProxy(a *Auth, dsh *DshManager) *reverseProxy {
	return &reverseProxy{auth: a, dsh: dsh}
}

// getChecker returns a BackendChecker using the current DshPort from config.
func (p *reverseProxy) getChecker() *BackendChecker {
	cfg := GetConfig()
	return newBackendChecker(cfg.DshPort)
}

func (p *reverseProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if p.auth.handleAuthRoutes(w, r) {
		return
	}
	// WebSocket upgrade: forward the raw connection to dsh after the auth gate.
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		p.handleUpgrade(w, r)
		return
	}
	checker := p.getChecker()
	if !checker.quick(500 * time.Millisecond) {
		serveWaitingPage(w, r)
		return
	}
	if !p.isInternalRequest(r) && !p.auth.isAuthed(r) {
		// 鉴权失效：自动重定向到登录页（dsh 网页打开时下一次请求即被重定向）
		// 仅面板后端的内部探测路径（如 /dsh-market/）视为可信、跳过鉴权；
		// 普通浏览器流量（含经 nginx 嵌套反代到达的）必须通过面板登录鉴权。
		next := safeNext(r.URL.Path + "?" + r.URL.RawQuery)
		http.Redirect(w, r, authLogin+"?next="+url.QueryEscape(next), http.StatusFound)
		return
	}
	// 记录访客（IP、最近访问时间、登录有效期）
	p.auth.recordVisitor(r)
	p.forward(w, r, checker)
}

// isPrivilegedControlRoute reports whether the proxied path is a dsh
// process-control endpoint that enforces a strict same-origin loopback guard
// (trustedRestartRequest in dshmarket/src/restart.ts): it only accepts requests
// that look like they came directly from a browser on loopback and REJECTS any
// forwarding trace (Forwarded / x-forwarded-* / x-real-ip). When the proxy
// forwards such a request it must therefore NOT append proxy headers (which the
// normal forward path does) and must rewrite Host/Origin to the dsh upstream so
// the same-origin check passes.
func isPrivilegedControlRoute(path string) bool {
	switch path {
	case "/dsh-market/restart":
		return true
	}
	return false
}

// isLoopback reports whether the request originates from the local host
// (127.0.0.1 / ::1). Such internal requests are trusted and bypass auth, so the
// panel backend can reach dsh's own API (e.g. /dsh-market/install) via the proxy.
func (p *reverseProxy) isLoopback(r *http.Request) bool {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		h = r.RemoteAddr
	}
	h = strings.TrimPrefix(h, "::ffff:")
	return h == "127.0.0.1" || h == "::1" || h == "localhost"
}

// isInternalRequest reports whether the request is an internal panel-backend
// call that may bypass the login auth. Only the panel's own backend probing
// paths (e.g. /dsh-market/install) qualify — ordinary browser traffic reaching
// the proxy through a nested domain reverse proxy (nginx on the same host) must
// still pass login auth, so loopback alone is not enough to skip it.
func (p *reverseProxy) isInternalRequest(r *http.Request) bool {
	if !p.isLoopback(r) {
		return false
	}
	return strings.HasPrefix(r.URL.Path, "/dsh-market/")
}

// handleUpgrade proxies a WebSocket upgrade by hijacking the client connection
// and piping raw bytes to the dsh upstream, mirroring proxy.js upgradeHandler.
func (p *reverseProxy) handleUpgrade(w http.ResponseWriter, r *http.Request) {
	// Auth guard: browsers can't follow a 302 on an upgrade, so reject with 401.
	// 与 HTTP 请求一致，WebSocket 连接也必须通过面板登录鉴权（harness_session），
	// 不得仅凭 dsh 的会话 cookie 放行，以免绕过控制台登录鉴权。
	if !p.auth.isAuthed(r) {
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			conn.Write([]byte("HTTP/1.1 401 Unauthorized\r\nConnection: close\r\n\r\n"))
			conn.Close()
		} else {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		}
		return
	}
	checker := p.getChecker()
	if !checker.wait(10 * time.Second) {
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return
	}
	client, brw, err := hj.Hijack()
	if err != nil {
		return
	}
	_ = brw.Flush()

	upstream := checker.hostPort()
	proxy, err := net.Dial("tcp", upstream)
	if err != nil {
		client.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		client.Close()
		return
	}
	// Rebuild the request line and headers, dropping browser-only hop headers.
	var b strings.Builder
	target := "ws://" + upstream + r.URL.RequestURI()
	u, _ := url.Parse(target)
	b.WriteString("GET " + u.RequestURI() + " HTTP/1.1\r\n")
	headers := r.Header.Clone()
	// dsh 启动时一次性 token 已用于换取会话 cookie；WebSocket 升级请求
	// 同样携带该 cookie、访问不带 token 的地址以通过 dsh 验证。
	if ck := p.dsh.AuthCookie(); ck != "" {
		headers.Set("Cookie", ck)
	}
	for _, h := range []string{"Origin", "Sec-Fetch-Site", "Sec-Fetch-Mode", "Sec-Fetch-Dest", "Connection", "Upgrade"} {
		headers.Del(h)
	}
	headers.Set("Host", upstream)
	headers.Set("Connection", "Upgrade")
	headers.Set("Upgrade", "websocket")
	//headers.Del("Sec-WebSocket-Key") // keep the client's original key
	for k, vv := range headers {
		for _, v := range vv {
			b.WriteString(k + ": " + v + "\r\n")
		}
	}
	b.WriteString("\r\n")
	proxy.Write([]byte(b.String()))

	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := client.Read(buf)
			if n > 0 {
				if _, werr := proxy.Write(buf[:n]); werr != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		proxy.Close()
	}()
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := proxy.Read(buf)
			if n > 0 {
				if _, werr := client.Write(buf[:n]); werr != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		client.Close()
	}()
}

func (p *reverseProxy) forward(w http.ResponseWriter, r *http.Request, checker *BackendChecker) {
	upstream := "http://" + checker.hostPort()
	outReq, err := http.NewRequest(r.Method, upstream+r.URL.RequestURI(), r.Body)
	if err != nil {
		http.Error(w, "Bad upstream request", http.StatusBadRequest)
		return
	}
	outReq.Header = r.Header.Clone()
	// dsh 启动时一次性 token 已用于换取会话 cookie（见 DshManager.ExchangeToken）；
	// 反代转发到 dsh 时携带该 cookie、访问不带 token 的地址即可通过验证。
	// 注意：需在 Clone 客户端请求头之后再设置，确保 dsh 的 cookie 优先生效。
	if ck := p.dsh.AuthCookie(); ck != "" {
		outReq.Header.Set("Cookie", ck)
	}
	if outReq.Header.Get("sec-fetch-site") != "" {
		outReq.Header.Set("sec-fetch-site", "same-origin")
	}
	outReq.Host = checker.hostPort()
	// 特权控制类请求（如 /dsh-market/restart）在 dsh 侧有严格安全栅栏
	// （trustedRestartRequest）：要求请求看起来像“来自同源回环浏览器的直接请求”，
	// 且不允许携带任何转发标记头（Forwarded / x-forwarded-* / x-real-ip），
	// 否则以 403 拒绝。因此反代转发这类请求时不能附加转发头，并把 Host/Origin
	// 统一改写为 dsh 上游同源，从而放行反代后的一键重启。
	if isPrivilegedControlRoute(r.URL.Path) {
		outReq.Header.Del("Forwarded")
		outReq.Header.Del("X-Forwarded-For")
		outReq.Header.Del("X-Forwarded-Host")
		outReq.Header.Del("X-Forwarded-Proto")
		outReq.Header.Del("X-Real-Ip")
		upstreamOrigin := "http://" + checker.hostPort()
		outReq.Header.Set("Origin", upstreamOrigin)
		outReq.Header.Set("Referer", upstreamOrigin+"/")
		outReq.Header.Set("Sec-Fetch-Site", "same-origin")
		outReq.Header.Set("Sec-Fetch-Mode", "same-origin")
	} else {
		outReq.Header.Set("x-forwarded-for", ip(r))
		outReq.Header.Set("x-forwarded-host", r.Host)
		outReq.Header.Set("x-forwarded-proto", "http")
	}
	if outReq.Header.Get("origin") != "" {
		outReq.Header.Set("origin", "http://"+checker.hostPort())
	}
	outReq.Header.Set("accept-encoding", "identity")

	tr := &http.Transport{DisableCompression: true}
	resp, err := tr.RoundTrip(outReq)
	if err != nil {
		http.Error(w, "Proxy error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("access-control-allow-origin", "*")

	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	enc := strings.ToLower(resp.Header.Get("Content-Encoding"))
	encoded := enc == "gzip" || enc == "br" || enc == "deflate" || enc == "zstd"

	if strings.HasPrefix(ct, "text/event-stream") {
		w.Header().Set("cache-control", "no-cache, no-transform")
		w.Header().Set("x-accel-buffering", "no")
		w.Header().Del("Content-Length")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		return
	}

	isHTML := strings.Contains(ct, "text/html")
	isJS := strings.Contains(ct, "javascript") || strings.Contains(ct, "text/javascript")
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300

	if (isHTML || isJS) && ok && !encoded {
		body, _ := io.ReadAll(resp.Body)
		if isHTML {
			body = injectIntoHTML(body)
		} else if isJS {
			body = rewriteJSBundle(body)
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func ip(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return strings.TrimPrefix(host, "::ffff:")
}