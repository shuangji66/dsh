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

// bootstrapScript is the browser-side patch that mirrors proxy.js BOOTSTRAP_SCRIPT
// (randomUUID polyfill, connection.isLoopback force, settings-scope enqueue fix).
const bootstrapScript = `(function () {
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
  var wrapped=false;
  function wrap(loader){
    if(wrapped||!loader||typeof loader.load!=="function")return; wrapped=true;
    var ol=loader.load;
    loader.load=function(entry){
      try{
        if(entry&&typeof entry.id==="string"&&typeof entry.factory==="function"){
          if(entry.id==="@deepseek-ai/dsh-client-connection"){
            var cf=entry.factory;
            entry.factory=function(require){
              var exp=cf.apply(this,arguments);
              try{
                var oa=exp&&exp.apply;
                if(typeof oa==="function")exp.apply=function(ctx){
                  var patched=false;
                  try{var op=ctx&&ctx.provide;if(typeof op==="function")ctx.provide=function(name,value){if(name==="connection"&&value){try{Object.defineProperty(value,"isLoopback",{value:true,configurable:true,writable:true});}catch(_e){}}patched=true;return op.apply(ctx,arguments);};}catch(_e){}
                  var rr=oa.apply(this,arguments);
                  try{if(!patched){var cn=ctx&&ctx.get&&ctx.get("connection");if(cn)Object.defineProperty(cn,"isLoopback",{value:true,configurable:true,writable:true});}}catch(_e){}
                  return rr;
                };
              }catch(_e){}
              return exp;
            };
          }else if(entry.id==="@deepseek-ai/dsh-client-ui-settings"){
            var sf=entry.factory;
            entry.factory=function(require){
              var exp=sf.apply(this,arguments);
              try{var Ctl=exp&&exp.SettingsScopeController;if(Ctl&&Ctl.prototype&&typeof Ctl.prototype.enqueue==="function"){var oe=Ctl.prototype.enqueue;Ctl.prototype.enqueue=function(op){if(this.disposed)return Promise.resolve();var self=this;var t=this.tail.then(function(){if(self.disposed)return;return op();});this.tail=t.catch(function(){});return t;};}}catch(_e){}
              return exp;
            };
          }
        }
      }catch(_e){}
      return ol.apply(loader,arguments);
    };
  }
  if(window.__ModuleLoader__)wrap(window.__ModuleLoader__);
  try{Object.defineProperty(window,"__ModuleLoader__",{configurable:true,get:function(){return window.__proxy_boot_loader_store__;},set:function(v){window.__proxy_boot_loader_store__=v;try{wrap(v);}catch(_e){}}});}catch(_e){}
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
}

func newReverseProxy(a *Auth) *reverseProxy {
	return &reverseProxy{auth: a}
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
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Refresh", "2; url="+r.URL.RequestURI())
		w.WriteHeader(200)
		io.WriteString(w, `<!DOCTYPE html><html><head><meta charset="UTF-8"><title>服务启动中</title><style>body{font-family:sans-serif;text-align:center;padding:50px;}</style></head><body><h1>⏳ 服务正在启动中，请稍候...</h1><p>dsh web 尚未就绪，页面将每隔2秒自动重试。</p></body></html>`)
		return
	}
	if !p.auth.isAuthed(r) {
		next := safeNext(r.URL.Path + "?" + r.URL.RawQuery)
		http.Redirect(w, r, authLogin+"?next="+url.QueryEscape(next), http.StatusFound)
		return
	}
	p.forward(w, r, checker)
}

// handleUpgrade proxies a WebSocket upgrade by hijacking the client connection
// and piping raw bytes to the dsh upstream, mirroring proxy.js upgradeHandler.
func (p *reverseProxy) handleUpgrade(w http.ResponseWriter, r *http.Request) {
	// Auth guard: browsers can't follow a 302 on an upgrade, so reject with 401.
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
	for _, h := range []string{"Origin", "Sec-Fetch-Site", "Sec-Fetch-Mode", "Sec-Fetch-Dest", "Connection", "Upgrade"} {
		headers.Del(h)
	}
	headers.Set("Host", upstream)
	headers.Set("Connection", "Upgrade")
	headers.Set("Upgrade", "websocket")
	// headers.Del("Sec-WebSocket-Key") // keep the client's original key
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
	if outReq.Header.Get("sec-fetch-site") != "" {
		outReq.Header.Set("sec-fetch-site", "same-origin")
	}
	outReq.Host = checker.hostPort()
	if outReq.Header.Get("origin") != "" {
		outReq.Header.Set("origin", "http://"+checker.hostPort())
	}
	outReq.Header.Set("x-forwarded-for", ip(r))
	outReq.Header.Set("x-forwarded-host", r.Host)
	outReq.Header.Set("x-forwarded-proto", "http")
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
