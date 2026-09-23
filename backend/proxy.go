package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// readyPath 是反代自留的就绪状态端点：等待页用它轮询当前启动阶段，收到
// ready=true 后跳转到真正的 dsh 界面。与 /_login、/_logout 同属反代自留路径
// （绝对路径，与登录跳转同样的根挂载假设），不会转发给 dsh。
const readyPath = "/_ready"

// bootstrapScript is the browser-side patch that mirrors proxy.js BOOTSTRAP_SCRIPT.
// It follows REVERSE_PROXY_ADAPTATION.md:
//   - randomUUID polyfill (non-secure HTTP contexts),
//   - client privileged-state injection: __DSH_TRANSPORT__ ownsHost,
//   - module-loader hook that forces connection.isLoopback = true,
//   - open-in-app block: 服务器部署没有本地 GUI 应用，"在本地编辑器打开工作区"
//     （/open-in-app/apps、/open-in-app/icon/<app>、/open-in-app/open）无效，
//     注入脚本拦截整个 /open-in-app/ 前缀，apps 探测失败即隐藏头部按钮。
//     匹配前先剥掉 document.baseURI 给出的挂载前缀：dsh 客户端用的是相对路径，
//     子路径部署（平台网关）下真实 pathname 是 "<前缀>/open-in-app/…"。
//
// 历史：第 3 项曾同时 patch ui-settings 的 SettingsScopeController.enqueue
// （用于兜住设置写入），但新版 ui-settings 只导出 apply/inject、不再导出该类，
// 该分支恒不生效（且自带 try/catch，属静默死代码），已移除；设置可写性现由
// 上面的 ownsHost 注入保证（isLoopback → persistence="host"）。
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

  // 3. 模块加载器 Hook：劫持 connection 模块句柄，强制 isLoopback = true
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

  // 4. 屏蔽 dsh 的"用本地编辑器打开工作区"功能（open-in-app）
  //    dsh 新版本在会话头部提供 open-in-app 拆分按钮，宿主注册了三个路由：
  //    GET /open-in-app/apps、GET /open-in-app/icon/<app>、POST /open-in-app/open。
  //    服务器部署没有本地 GUI 应用可打开，这些请求在服务器上无效（点击还会
  //    在宿主机派生进程）。客户端设计：/open-in-app/apps 探测失败即发布空
  //    列表、不渲染按钮（open-in-app 的 controller.run() 与 OpenInAppAction
  //    均按“无可用应用”处理），因此拦截整个 /open-in-app/ 前缀即可让该功能
  //    整体消失，也不会再产生 /open-in-app/icon/zed 之类的图标请求。
  //
  //    路径比较必须“先剥挂载前缀”：dsh 客户端发的是相对路径（"open-in-app/apps"，
  //    见 dsh-client-ui-open-in-app 里的 *_ROUTE 常量 = 宿主路由去首斜杠），浏览器
  //    按 <base href="./"> 解析，所以经平台网关访问（子路径挂载 /app/Harness/dsh）时
  //    实际 pathname 是 "<前缀>/open-in-app/apps"；只按根路径比较会漏拦，宿主照常
  //    返回应用列表（含 zed），按钮又冒出来。
  //    前缀取 document.baseURI 去掉结尾斜杠 —— 与浏览器解析这些相对路径用的是同一
  //    份基准，既不硬编码挂载点，也不影响根挂载（HARNESS_PROXY_BASEURL 未启用时
  //    baseURI 就是站点根，前缀为空，行为与原来完全一致）。
  var mountPath = function () {
    try {
      var base = new URL(document.baseURI || location.href, location.href).pathname;
      return base.replace(/\/+$/, "");
    } catch (_e) { return ""; }
  };
  var isOpenInAppUrl = function (input) {
    var raw;
    if (typeof input === "string") raw = input;
    else if (typeof URL === "function" && input instanceof URL) raw = String(input);
    else if (input && typeof input === "object" && typeof input.url === "string") raw = input.url; // Request
    else return false;
    try {
      // 解析基准同样取 baseURI：与浏览器把相对路径变成真实请求 URL 的结果一致。
      var p = new URL(raw, document.baseURI || location.href).pathname;
      var prefix = mountPath();
      if (prefix && p.indexOf(prefix + "/") === 0) p = p.slice(prefix.length);
      return p === "/open-in-app" || p.indexOf("/open-in-app/") === 0;
    } catch (_e) { return false; }
  };

  // 4a. fetch 拦截：对 /open-in-app/* 返回 404（非 ok、不触网），
  //     客户端按“无可用应用”降级，按钮不渲染。
  var rawFetch = window.fetch;
  if (typeof rawFetch === "function") {
    window.fetch = function (input, init) {
      if (isOpenInAppUrl(input)) {
        try {
          return Promise.resolve(new Response(null, { status: 404, statusText: "Not Found" }));
        } catch (_e) {
          // 无 Response 构造器的极端环境：返回最小失败对象（ok=false 即失败）
          return Promise.resolve({ ok: false, status: 404, statusText: "Not Found", url: String(input) });
        }
      }
      return rawFetch.apply(this, arguments);
    };
  }

  // 4b. XMLHttpRequest 拦截（防御未来改用 XHR 的客户端）：不发请求，
  //     直接模拟失败（readyState=DONE、status=0、触发 error 事件）。
  try {
    if (typeof XMLHttpRequest !== "undefined") {
      var xhrOpen = XMLHttpRequest.prototype.open;
      var xhrSend = XMLHttpRequest.prototype.send;
      XMLHttpRequest.prototype.open = function (method, url) {
        this.__dshOpenInAppBlocked = isOpenInAppUrl(url);
        return xhrOpen.apply(this, arguments);
      };
      XMLHttpRequest.prototype.send = function (body) {
        if (this.__dshOpenInAppBlocked) {
          this.__dshOpenInAppBlocked = false;
          var self = this;
          var failXhr = function () {
            try {
              Object.defineProperty(self, "readyState", { value: 4, configurable: true, writable: true });
              Object.defineProperty(self, "status", { value: 0, configurable: true, writable: true });
            } catch (_e) {}
            if (typeof self.onreadystatechange === "function") { try { self.onreadystatechange(); } catch (_e) {} }
            try {
              var ev = new Event("error");
              self.dispatchEvent(ev);
              if (typeof self.onerror === "function") { try { self.onerror(ev); } catch (_e) {} }
            } catch (_e) {}
          };
          if (typeof Promise !== "undefined" && typeof Promise.resolve === "function") Promise.resolve().then(failXhr);
          else setTimeout(failXhr, 0);
          return;
        }
        return xhrSend.apply(this, arguments);
      };
    }
  } catch (_e) {}

  // 4c. <img src="/open-in-app/icon/<app>"> 图标请求拦截：不赋值 src，
  //     即不发起网络请求（兜底；apps 已被 4a 拦截时按钮不会渲染图标）。
  try {
    if (typeof HTMLImageElement !== "undefined") {
      var imgSrcDesc = Object.getOwnPropertyDescriptor(HTMLImageElement.prototype, "src");
      if (imgSrcDesc && typeof imgSrcDesc.set === "function") {
        var imgSrcGet = imgSrcDesc.get;
        var imgSrcSet = imgSrcDesc.set;
        Object.defineProperty(HTMLImageElement.prototype, "src", {
          configurable: true,
          get: function () { return imgSrcGet ? imgSrcGet.call(this) : undefined; },
          set: function (v) {
            if (!isOpenInAppUrl(v)) imgSrcSet.call(this, v);
          },
        });
      }
    }
  } catch (_e) {}

  // 6. 触屏：模型 / 推理等级菜单不再被「焦点搬家」误关（iOS 上菜单内选项点了没反应的根因）
  //
  //    dsh 0.1.7 的模型座位（conversation.input.model，即「模型 · 推理等级」两级菜单）自己
  //    实现了菜单，并在根节点上挂了 onBlur：
  //      if (event.relatedTarget instanceof Node && (rootRef.contains(rel) || menuRef.contains(rel))) return;
  //      close();
  //    （见 dsh-client-ui-model-selection 的 ModelSelect）。它假定「失焦目标一定是个 Node」
  //    并且这个 Node 一定落在 root / menu 里 —— 桌面成立：点选项时焦点落到那个选项上，
  //    relatedTarget 就是它，守卫放行，菜单留着，click 正常派发给选项。
  //
  //    iOS WebKit 不成立：菜单内按钮之间的焦点搬家，focusout 的 relatedTarget 是 null
  //    （真机实测：菜单里已聚焦的选项 focusout 事件 relatedTarget === null）。守卫于是越过
  //    return 直接 close()，菜单在 mousedown 之后、click 之前被卸载 —— 选项的 click 没有
  //    目标、React 的 onClick 不跑，也就永远不会发出 session/selectModel。现象正是：
  //    「菜单能开、根面板（模型 / 推理等级 两行）点得动、一进二级面板点选项就毫无反应」，
  //    而桌面鼠标点击正常（桌面 relatedTarget 是那个选项）。
  //
  //    修法：触屏上把「模型座位自己那个菜单内部」的 focusout 在捕获阶段拦掉传播 —— React 的
  //    委托监听挂在更低的容器上，拦在最外层就让它收不到这次失焦，菜单不会被误关，随后的
  //    click 正常落到选项上。
  //
  //    与设置页的「浏览器兼容模式」（AppConfig.BrowserCompat）共用一个开关：和上面第 1 处
  //    原生函数判断一样，它只为受影响的引擎补兼容（这里只在触屏 + iOS 类 WebKit 上有作用），
  //    开关关闭时整段不生效 —— 未开启该开关的 iPhone 用户仍会看到菜单点不动，设置页提示
  //    里已说明这一点。边界刻意收窄：
  //      * 只认模型座位（[data-slot="conversation.input.model"]）且菜单确实开着
  //        （aria-expanded="true"），菜单用触发器的 aria-controls 精确定位（规范属性，
  //        不依赖 dsh 的样式哈希）；命令 / 权限 / 会话行等其他菜单一律不碰；
  //      * 只拦 target 在该菜单内的 focusout：编辑面与菜单以外的失焦照旧（点外部关菜单走的是
  //        mousedown，不受影响）；
  //      * 只在触屏设备上挂监听，且监听内每次现读「浏览器兼容模式」开关（关闭时直接放行）；
  //        桌面（无触摸）与未开开关的用户行为与官方 dsh 完全一致。
  try {
    var touchPrimaryForMenu = (navigator.maxTouchPoints || 0) > 0 || "ontouchstart" in window;
    if (touchPrimaryForMenu) {
      document.addEventListener("focusout", function (event) {
        // 开关在运行时读：注入脚本与开关标记的先后顺序无关（生产里两者同在一个 <head> 注入块，
        // 开关标记在前；这里再兜一层，测试与手工注入也就不用关心顺序）。
        if (window.__DSH_BROWSER_COMPAT__ !== true) return;
        var node = event.target;
        if (!node || typeof node.closest !== "function") return;
        var seat = document.querySelector('[data-slot="conversation.input.model"] button[aria-haspopup="menu"]');
        if (!seat || seat.getAttribute("aria-expanded") !== "true") return;
        var menu = document.getElementById(seat.getAttribute("aria-controls") || "");
        if (menu !== null && menu.contains(node)) event.stopPropagation();
      }, true);
      window.__DSH_MODEL_MENU_FOCUS_GUARD__ = window.__DSH_BROWSER_COMPAT__ === true;
    }
  } catch (_e) {}

  // 调试标记：页面加载后可用 window.__DSH_OPEN_IN_APP_BLOCKED__ 确认 open-in-app 屏蔽生效、
  // window.__DSH_MODEL_MENU_FOCUS_GUARD__ 确认触屏模型菜单的失焦守卫已武装（它随「浏览器兼容模式」开关，开关关闭时该标记不会出现）。
  try { window.__DSH_OPEN_IN_APP_BLOCKED__ = true; } catch (_e) {}
})();`

// dshDiagScript 是**移动端诊断打点**（默认关闭；排查「iPhone 上模型 / 推理等级菜单点了没反应」
// 这类只在真机出现的问题时用，它本身不修任何东西）。
//
// 开关（任一满足即注入，见 dshDiagEnabled）：
//   - 环境变量 HARNESS_DSH_DIAG=1：进程级开启，适合长时间盯守；
//   - dsh 页面 URL 带 ?dsh-diag=1：只对这次访问开启（例如手机上用 Safari 打开
//     https://<网关>/app/Harness/dsh/?dsh-diag=1）。
//
// 为什么用「URL 路径打点」而不新开后端接口：反代把 <prefix>/dsh-diag/<事件>/<分片>/<数据>
// 当作挂载内路径转发给 dsh（dsh 回 404，无副作用），而平台网关 / nginx 会把完整请求行写进
// access.log（/usr/trim/nginx/logs/access.log），于是不需要任何额外接口就能把一个真机客户端
// 的现场取回来。解读工具见 .tools/dsh-diag/（README + read-diag.py）。
//
// 事件：s=状态快照与 10s 心跳（视口 / 座位 disabled / 座位中心命中谁 / 编辑面是否可编辑 /
// 菜单矩形 / 滚动位置）；e=命中模型座位或菜单的指针事件；d=菜单打开期间的 mousedown / click
// 明细（命中链、命中点元素、被按节点是否仍连接）；m=菜单出现/消失（消失时带最近 10 条事件
// 尾巴——用来判断「谁把菜单关掉的」）；o=菜单 portal 节点被新建/移除；f=焦点变化（含 trigger
// 与 relatedTarget，iOS 的关键）；n=座位节点是否被替换；x/r=JS 报错与未处理拒绝。
const dshDiagScript = `// harness 移动端诊断打点（默认关闭，见 backend/proxy.go 的 dshDiagEnabled）：
// 由注入脚本把「模型座位与菜单」相关的事件以 URL path 形式打回本站
// （<prefix>/dsh-diag/<事件>/<分片>/<数据>），dsh 回 404、无副作用，
// 但会落进平台网关 / nginx 的 access.log；解读工具见 .tools/dsh-diag/。
(function () {
  var seq = 0;
  function send(kind, obj) {
    try {
      var data = encodeURIComponent(JSON.stringify(obj));
      if (!data) return;
      var chunk = 600, n = Math.ceil(data.length / chunk) || 1;
      for (var i = 0; i < n; i++) {
        var url = "dsh-diag/" + kind + "/" + (i + 1) + "-" + n + "/" + data.slice(i * chunk, (i + 1) * chunk);
        try { if (navigator.sendBeacon) navigator.sendBeacon(url); else { var img = new Image(); img.src = url; } } catch (_e) {}
      }
      seq++;
    } catch (_e) {}
  }
  function d(el) {
    if (el === null || el === undefined) return null;
    if (!(el instanceof Element)) return "?" + String(el).slice(0, 10);
    var s = el.tagName + "." + String(el.getAttribute("class") || "").slice(0, 26);
    var role = el.getAttribute("role");
    return role ? s + "[" + role + "]" : s;
  }
  function q(sel) { return document.querySelector(sel); }
  function seatBtn() { return q('[data-slot="conversation.input.model"] button'); }
  function menuEl() { return q('[role="menu"]'); }
  function grpEl() { return q('[class*="_groups"]'); }
  function rect(el) { if (!el) return null; var r = el.getBoundingClientRect(); return [r.x | 0, r.y | 0, r.width | 0, r.height | 0]; }
  function trailPush(type, target, extra) {
    trail.push({ ty: type, tg: d(target), x: extra || null, t: Date.now() % 10000000 });
    if (trail.length > 16) trail.shift();
  }
  var trail = [];
  function snap(tag) {
    var b = seatBtn(), m = menuEl(), g = grpEl(), ed = q("[data-composer-input]"), card = q("[data-composer-card]"), frame = q('[class*="_frame"]'), vv = window.visualViewport;
    var r = b ? b.getBoundingClientRect() : null;
    var hit = r && r.width > 0 ? document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2) : null;
    return {
      tag: tag, t: Date.now() % 10000000, ua: navigator.userAgent.slice(-46),
      iw: window.innerWidth, ih: window.innerHeight,
      vv: vv ? [Math.round(vv.height), Math.round(vv.scale * 100) / 100, Math.round(vv.offsetTop)] : null,
      seat: b ? { dis: !!b.disabled, exp: b.getAttribute("aria-expanded"), r: [r.x | 0, r.y | 0, r.width | 0, r.height | 0], txt: (b.innerText || "").replace(/\s+/g, " ").slice(0, 30) } : null,
      hit: hit ? [d(hit), d(hit.parentElement)] : null,
      ed: ed ? [ed.getAttribute("contenteditable"), ed.getAttribute("aria-disabled")] : null,
      menuR: rect(m), grpScroll: g ? [g.scrollTop | 0, g.scrollHeight | 0, g.clientHeight | 0] : null,
      menus: document.querySelectorAll('[role="menu"]').length,
      act: d(document.activeElement), card: !!card, sb: frame ? frame.hasAttribute("data-sidebar-collapsed") : null
    };
  }
  function boot() { send("s", snap("boot")); setInterval(function () { send("s", snap("tick")); }, 10000); }
  if (document.readyState === "complete") setTimeout(boot, 4000);
  else window.addEventListener("load", function () { setTimeout(boot, 4000); });
  document.addEventListener("DOMContentLoaded", function () { send("s", snap("dom")); });

  // ---- all pointer/mouse/click traffic while a menu is open (or just closed) ----
  var lastClose = 0, menuWasOpen = false;
  document.addEventListener("pointerdown", function (e) { trailPush("pd", e.target, e.pointerType); }, true);
  document.addEventListener("mousedown", function (e) {
    var m = menuEl();
    trailPush("md", e.target, m ? "menuOpen" : "");
    if (m) send("d", { k: "mousedown", tgt: d(e.target), chain: [d(e.target.parentElement), d(e.target.parentElement && e.target.parentElement.parentElement), d(e.target.parentElement && e.target.parentElement && e.target.parentElement.parentElement)], inMenu: !!(e.target.closest && e.target.closest('[role="menu"]')), inSeat: !!(e.target.closest && e.target.closest('[data-slot="conversation.input.model"]')), act: d(document.activeElement), pd: e.defaultPrevented, at: Date.now() % 10000000 });
  }, true);
  document.addEventListener("mouseup", function (e) { trailPush("mu", e.target); }, true);
  document.addEventListener("pointercancel", function (e) { trailPush("pcancel", e.target); }, true);
  document.addEventListener("click", function (e) {
    var m = document.querySelectorAll('[role="menu"]').length > 0 || (Date.now() % 10000000) - lastClose < 600;
    if (!m) return;
    var pt = [e.clientX | 0, e.clientY | 0];
    var at = document.elementFromPoint(e.clientX, e.clientY);
    trailPush("cl", e.target);
    send("d", { k: "click", tgt: d(e.target), chain: [d(e.target.parentElement), d(e.target.parentElement && e.target.parentElement.parentElement)], at: d(at), pt: pt, inMenu: !!(e.target.closest && e.target.closest('[role="menu"]')), mdownTargetAlive: pressedTarget ? (pressedTarget.isConnected === true) : null, pressed: d(pressedTarget), pd: e.defaultPrevented, act: d(document.activeElement), menus: document.querySelectorAll('[role="menu"]').length, t: Date.now() % 10000000 });
    pressedTarget = null;
  }, true);
  var pressedTarget = null;
  document.addEventListener("mousedown", function (e) { pressedTarget = e.target; }, true);

  // ---- any focus change anywhere (trigger focus is the blind spot of v1) ----
  document.addEventListener("focusin", function (e) { send("f", { z: "doc", ev: "in", tgt: d(e.target), t: Date.now() % 10000000 }); }, true);
  document.addEventListener("focusout", function (e) { send("f", { z: "doc", ev: "out", tgt: d(e.target), to: d(e.relatedTarget), t: Date.now() % 10000000 }); }, true);
  // ---- focus of the seat subtree (trigger) and the editor ----
  function hookFocus() {
    var seat = q('[data-slot="conversation.input.model"]');
    if (seat && !seat.__fhook) {
      seat.__fhook = 1;
      seat.addEventListener("focusin", function (e) { send("f", { el: "seat", ev: "in", tgt: d(e.target), t: Date.now() % 10000000 }); }, true);
      seat.addEventListener("focusout", function (e) { send("f", { el: "seat", ev: "out", to: d(e.relatedTarget), t: Date.now() % 10000000 }); }, true);
    }
    var ed = q("[data-composer-input]");
    if (ed && !ed.__fhook) {
      ed.__fhook = 1;
      ed.addEventListener("focusin", function () { send("f", { el: "ed", ev: "in", t: Date.now() % 10000000 }); }, true);
      ed.addEventListener("focusout", function (e) { send("f", { el: "ed", ev: "out", to: d(e.relatedTarget), t: Date.now() % 10000000 }); }, true);
    }
  }
  setInterval(hookFocus, 800);

  // ---- menu presence + seat node identity (remount detector) ----
  var lastMenu = null, lastSeatNode = null;
  setInterval(function () {
    var open = document.querySelectorAll('[role="menu"]').length > 0;
    if (open !== lastMenu) {
      lastMenu = open;
      if (!open) lastClose = Date.now() % 10000000;
      send("m", { open: open, menus: document.querySelectorAll('[role="menu"]').length, menuR: rect(menuEl()), grpScroll: (function () { var g = grpEl(); return g ? [g.scrollTop | 0, g.scrollHeight | 0, g.clientHeight | 0] : null; })(), seatTxt: (function () { var b = seatBtn(); return b ? (b.innerText || "").replace(/\s+/g, " ").slice(0, 30) : null; })(), t: Date.now() % 10000000, exp: (function () { var b = seatBtn(); return b ? b.getAttribute("aria-expanded") : null; })(), act: d(document.activeElement), trail: open ? null : trail.slice(-10) });
    }
    var seat = q('[data-slot="conversation.input.model"]');
    if (seat !== lastSeatNode) { send("n", { ev: lastSeatNode === null ? "seat-first" : "seat-node-changed", prev: !!lastSeatNode, t: Date.now() % 10000000 }); lastSeatNode = seat; }
  }, 200);
  // ---- portal node identity: does the menu get (re)created / removed right at the tap? ----
  function observePortal() {
    if (typeof MutationObserver === "undefined" || !document.body) return;
    try {
      var mo = new MutationObserver(function (muts) {
      muts.forEach(function (mu) {
        Array.prototype.forEach.call(mu.addedNodes, function (n) {
          if (n instanceof Element && n.matches && n.matches('[role="menu"], [class*="_7KE1Ra_menu"]')) {
            send("o", { ev: "menu-add", cls: d(n), t: Date.now() % 10000000, trail: trail.slice(-6) });
          }
        });
        Array.prototype.forEach.call(mu.removedNodes, function (n) {
          if (n instanceof Element && n.matches && n.matches('[role="menu"], [class*="_7KE1Ra_menu"]')) {
            send("o", { ev: "menu-remove", cls: d(n), t: Date.now() % 10000000, trail: trail.slice(-8) });
          }
        });
      });
    });
      mo.observe(document.body, { childList: true });
    } catch (_e) {}
  }
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", observePortal);
  else observePortal();
  window.addEventListener("error", function (e) { send("x", { m: String(e.message).slice(0, 80), at: String(e.filename).slice(-24) + ":" + e.lineno }); });
  window.addEventListener("unhandledrejection", function (e) { send("r", { m: String((e.reason && (e.reason.message || e.reason)) || "").slice(0, 80) }); });
})();`

// browserCompatFlagScript 输出浏览器兼容开关的运行时标记。
//
// 为什么把开关放在 HTML 而不是直接改写 bundle 字节：
// dsh 的插件 bundle 响应带 `Cache-Control: public, max-age=31536000, immutable`，
// 浏览器对普通刷新（F5）会直接命中磁盘缓存、不再回源，若按开关状态改变 bundle
// 内容，切换后在旧缓存过期前不会生效（实测 fromCache=true）。
// 而 HTML 文档每次刷新都会回源（实测 fromCache=false），因此让 bundle 内容保持
// 与开关无关的恒定形态（缓存安全），把开关状态放在 HTML 里注入为全局标记，
// 即可做到“切换开关 → 普通刷新 → 立即生效”。
func browserCompatFlagScript(enabled bool) string {
	v := "false"
	if enabled {
		v = "true"
	}
	return "<script>window.__DSH_BROWSER_COMPAT__=" + v + ";</script>"
}

// dshDiagEnabled 判断这次要不要给 dsh 页面注入移动端诊断打点（默认关闭）：
// 环境变量 HARNESS_DSH_DIAG（进程级）或页面 URL 的 ?dsh-diag=1（访问级），任一满足即可。
// 每次现读环境变量（不缓存），便于测试直接 t.Setenv、运行时改环境也无需重启。
func dshDiagEnabled(r *http.Request) bool {
	if os.Getenv("HARNESS_DSH_DIAG") != "" {
		return true
	}
	if r == nil || r.URL == nil {
		return false
	}
	v := r.URL.Query().Get("dsh-diag")
	return v == "1" || v == "true"
}

func injectIntoHTML(body []byte, diag bool) []byte {
	inject := "<style>[data-slot=\"settings.action\"] { display:none !important; }</style>" +
		browserCompatFlagScript(GetConfig().BrowserCompat) +
		"<script>" + bootstrapScript + "</script>"
	if diag {
		inject += "<script>" + dshDiagScript + "</script>"
	}
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

// nativeFunctionFormatPair 修复 dsh 的 V8-only 原生函数格式判断。
//
// dsh-util-values 的 hasIntrinsicConstructor（被内联进 dsh-api-session-controller
// 客户端 bundle）用精确字符串比较判断“这是不是本 realm 的原生构造器”：
//
//	Function.prototype.toString.call(constructor) === `function ${name}() { [native code] }`
//
// 该字面量只适配 V8 的单行格式。SpiderMonkey（Firefox/Zen）把原生函数源码格式化为
// 多行（实测 Firefox 156："function Object() {\n    [native code]\n}"），于是精确比较
// 恒为 false → isIntrinsicObjectPrototype 对任何普通对象都返回 false →
// snapshotJsonValue 对普通 JSON 返回 undefined → 客户端 assistant-stream 校验抛
// TypeError("Assistant stream raw chunk must be a lossless JSON object")，历史渲染在
// 首条消息前中止，界面停在“载入历史…”。
//
// 该错误是普通 TypeError，不是 RemoteError，因此 doOpen() 的 catch 不会把它归类为
// remote failure（isRemoteFailure 只看 isDSHRemoteError 标记），openState 就永久停在
// "loading"，且没有任何重试路径 —— 这正是刷新/切换会话才能恢复的原因。
//
// 修复：比较前把空白折叠为单个空格，使各引擎的原生函数格式统一到 V8 形态。该比较是
// “重复安装/跨 realm”启发式而非安全边界，折叠空白只会让原本在 V8 下本就通过的值继续
// 通过，不会放宽任何实际约束。
const (
	nativeFnCheckV8Only = "Function.prototype.toString.call(constructor) === `function ${name}() { [native code] }`"
	// nativeFnCheckGated 是同一比较的“运行时开关”形态：
	//   - 开关关闭（未定义或非 true）时求值为原始表达式，与官方 dsh 完全一致；
	//   - 开关开启时先折叠空白再比较，修复 SpiderMonkey / JavaScriptCore。
	// 该替换本身与开关状态无关、对同一份 bundle 恒定，因此 bundle 字节在开关切换
	// 前后保持不变（对 immutable 缓存安全）；真正的开关判定发生在浏览器运行时，
	// 由 HTML 注入的 window.__DSH_BROWSER_COMPAT__ 提供。
	nativeFnCheckGated = "(globalThis.__DSH_BROWSER_COMPAT__ === true ? Function.prototype.toString.call(constructor).replace(/\\s+/gu, \" \") : Function.prototype.toString.call(constructor)) === `function ${name}() { [native code] }`"
)

// rewriteJSBundle 对转发路径上的 JS 产物做一类改写：原生函数格式判断的引擎兼容修复
// （Firefox/Zen 历史不加载的直接原因）。
//
// 该改写以“锚点未命中则原样返回”为前提，保证 dsh 版本变化时只会退化为不生效，
// 而不会破坏页面。
//
// 已移除的两处历史改写（均为恒不生效的死代码，且其目标已由别的机制覆盖）：
//   - settings 作用域字面量替换（`connection.isLoopback ? "host" : "memory"` → `"host"`）：
//     该形态仅存在于 dsh ≤0.1.1-rc.2；自 0.1.2-alpha.5 起改为
//     `ctx.remote.$host.isLoopback ? "host" : "memory"`，四处 pattern 全部零匹配。
//     而 dsh ≥0.1.2-alpha.5 的 `$host.isLoopback` 派生自 `connection.isLoopback`，
//     已由 bootstrapScript 的 ownsHost 注入置为 true，无需再改写字节。
//     注：dsh-client-ui-settings-general 的同名判断是 `? new SettingsDocumentStore(...)
//     : void 0` 形态，本就不在替换范围内。
//   - 会话窗口自愈探针（依赖旧版 sessions.list 快照的 `current` 字段与
//     `followCurrent()`）：新版 session-controller 已删除该字段与方法，改用显式
//     sessions.retain(target, {source:"mainView"}) 打开窗口，探针入口条件恒不成立。
func rewriteJSBundle(buf []byte) []byte {
	s := string(buf)

	// 引擎兼容修复：替换为“运行时受开关控制”的等价表达式。
	// 替换结果与开关状态无关（同一份 bundle 恒定输出），因此对 immutable 缓存安全；
	// 是否真正归一化空白由 HTML 注入的 window.__DSH_BROWSER_COMPAT__ 在运行时决定。
	if strings.Contains(s, nativeFnCheckV8Only) {
		s = strings.ReplaceAll(s, nativeFnCheckV8Only, nativeFnCheckGated)
	}

	return []byte(s)
}

// waitingPageHTML 是反代在「dsh 尚未就绪」时对外提供的等待页。
//
// 反代从控制台启动的第一刻就在监听（早于 dsh 启动），所以本页承担了原来完全
// 缺失的那段体验：页面样式与登录页一致（见 loginPageHTML）、中英文并列显示，
// 并新增两项行为：
//   - 轮询 /_ready 拿到真实状态（__WAIT_STATE__ 由服务端首次注入）。启动过程
//     （starting/auth/deps）只显示一句话「正在等待 DeepSeek Harness 服务就绪」——
//     换取凭据、安装 node-pty 都是内部细节，不对外展示；只有需要用户动手的状态
//     （已停止 / 启动失败 / 未自动启动）才分别给出指引。旧版不看状态、10 秒后
//     无条件报「启动失败」，属误报；
//   - 一旦 /_ready 报告就绪立即 location.replace 跳转，不再靠整页刷新撞时机。
//
// 无 JS（或轮询被中间层拦掉）时仍有兜底：serveWaitingPage 会带上 10 秒的
// <meta refresh>，整页重载同样会重新判定能否放行。
const waitingPageHTML = `<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="theme-color" content="#FAFAFA">
<title>正在等待 DeepSeek Harness 服务就绪</title>
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
    --warn-bg:rgba(217,119,6,.08);
    --warn-border:rgba(217,119,6,.3);
    --warn-color:#b45309;
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
      --warn-bg:rgba(251,191,36,.1);
      --warn-border:rgba(251,191,36,.3);
      --warn-color:#fbbf24;
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
    color:var(--err-color);text-align:left;line-height:1.6;word-break:break-word}
  .hint{display:none;margin-top:16px;padding:10px 12px;background:var(--warn-bg);
    border:1px solid var(--warn-border);border-radius:6px;font-size:13px;
    color:var(--warn-color);text-align:left;line-height:1.6;word-break:break-word}
  .hint .en{display:block;opacity:.85;margin-top:4px}
</style></head><body><div class="wrap">
  <div class="card">
    <div class="spinner" id="spinner"></div>
    <h1 id="title">正在等待 DeepSeek Harness 服务就绪 / Waiting for the DeepSeek Harness service to be ready</h1>
    <p class="sub" id="sub">服务就绪后本页会自动跳转，请稍候…
      <span class="en">This page jumps automatically once the service is ready.</span>
    </p>
    <div class="err" id="detail"></div>
    <div class="hint" id="hint">
      已等待 <b id="elapsed">0</b> 秒仍未就绪。若长时间没有进展，请到控制台查看日志，
      必要时卸载不兼容的插件后重试。
      <span class="en">Still not ready after <b id="elapsed-en">0</b>s. Check the console
        logs if this persists; uninstall incompatible plugins and retry if needed.</span>
    </div>
  </div>
</div>
<script>
window.__DSH_WAIT__=__WAIT_STATE__;
(function(){
  // HINT_AFTER：等待超过这个时长才给出「可能需要排查」的黄色提示（旧版固定在
  // 10 秒后就报「启动失败」，属于误报）。
  // READY 由服务端按挂载前缀注入（根挂载为 "/_ready"，子路径挂载为
  // "/app/Harness/dsh/_ready"）。这里必须是绝对路径：相对路径会随当前 URL 的
  // 目录层级漂移（如 /app/Harness/dsh/a/b 下会解析成 /app/Harness/dsh/a/_ready）。
  var READY=__READY_URL__, POLL=1500, HINT_AFTER=30000;
  // 启动过程（starting/auth/deps）对用户是同一件事——等服务就绪，内部细节
  // （依赖准备、凭据落定之类）不对外展示；下面几个阶段是「需要用户动手」的状态，
  // 才分别给出指引。
  var WAITING=['正在等待 DeepSeek Harness 服务就绪',
    'Waiting for the DeepSeek Harness service to be ready',
    '服务就绪后本页会自动跳转，请稍候。',
    'This page jumps automatically once the service is ready.'];
  var TEXT={
    starting:WAITING, auth:WAITING, deps:WAITING,
    stopped:['dsh 服务已停止','The dsh service is stopped',
      '可在控制台「概览」页重新启动 dsh 服务，启动后本页会自动跳转。',
      'Restart dsh from the console overview page; this page jumps once it is up.'],
    failed:['dsh 服务启动失败','Failed to start the dsh service',
      '请检查控制台日志，必要时卸载不兼容的插件后重试。',
      'Check the console logs; uninstall incompatible plugins and retry if needed.'],
    disabled:['未自动启动 dsh 服务','dsh auto-start is disabled',
      '控制台以 HARNESS_AUTOSTART=0 启动；请在控制台「概览」页手动启动 dsh。',
      'The console started with HARNESS_AUTOSTART=0; start dsh from the console overview page.'],
    ready:['服务已就绪，正在跳转…','Ready, redirecting…',
      '正在进入 dsh 界面。','Entering the dsh interface.']
  };
  var titleEl=document.getElementById('title');
  var subEl=document.getElementById('sub');
  var detailEl=document.getElementById('detail');
  var hintEl=document.getElementById('hint');
  var spinnerEl=document.getElementById('spinner');
  var elapsedEl=document.getElementById('elapsed');
  var elapsedEnEl=document.getElementById('elapsed-en');

  // 等待起点（跨整页重载保留），仅用于显示已等待时长；跳转成功后清除（见 poll）。
  var KEY='dsh_start_wait_ts';
  var start=0, now=Date.now();
  try{ start=parseInt(sessionStorage.getItem(KEY)||'0',10)||0; }catch(e){}
  if(!start||start>now){ start=now; try{ sessionStorage.setItem(KEY,String(start)); }catch(e){} }

  function render(s){
    var t=TEXT[(s&&s.phase)||'starting']||TEXT.starting;
    titleEl.textContent=t[0]+' / '+t[1];
    subEl.innerHTML=t[2]+'<span class="en">'+t[3]+'</span>';
    var broken=s&&(s.phase==='failed'||s.phase==='stopped'||s.phase==='disabled');
    spinnerEl.style.display=broken?'none':'';
    if(s&&s.phase==='failed'&&s.detail){ detailEl.textContent=s.detail; detailEl.style.display='block'; }
    else { detailEl.style.display='none'; }
  }

  function tickElapsed(){
    var secs=Math.max(0,Math.round((Date.now()-start)/1000));
    elapsedEl.textContent=secs;
    elapsedEnEl.textContent=secs;
    if(secs*1000>=HINT_AFTER) hintEl.style.display='block';
  }
  setInterval(tickElapsed,1000);
  tickElapsed();

  var busy=false, jumped=false;
  function poll(){
    if(busy||jumped) return;
    busy=true;
    fetch(READY,{cache:'no-store',headers:{'Accept':'application/json'}})
      .then(function(res){
        // 被重定向（登录失效）或非 200：交给整页重载去走登录流程。
        if(res.redirected||!res.ok) throw new Error('not ready');
        return res.json();
      })
      .then(function(s){
        busy=false;
        if(s&&s.ready){
          jumped=true;
          render({phase:'ready'});
          // 本次等待结束：清掉起点，下一次等待（例如之后某次 dsh 重启）从 0 计时，
          // 而不是沿用上一次的时长把黄色提示立刻顶出来。
          try{ sessionStorage.removeItem(KEY); }catch(e){}
          location.replace(location.href);
          return;
        }
        render(s||{});
        setTimeout(poll,POLL);
      })
      .catch(function(){
        busy=false;
        setTimeout(function(){ location.reload(); },POLL);
      });
  }
  render(window.__DSH_WAIT__);
  poll();
})();
</script>
</body></html>`

// waitingPageHTMLFor 把当前阶段与就绪轮询地址注入页面脚本。detail 是任意错误
// 文本、mount 前缀来自环境变量，二者都用 JSON 编码注入（encoding/json 默认转义
// < > & 为 \u003c 等），既不会截断脚本，也不会让它们变成可执行标记。
func waitingPageHTMLFor(st proxyState, mount proxyMount) string {
	raw, err := json.Marshal(st)
	if err != nil {
		raw = []byte(`{"phase":"starting"}`)
	}
	ready, err := json.Marshal(mount.join(readyPath))
	if err != nil {
		ready = []byte(`"/_ready"`)
	}
	page := strings.Replace(waitingPageHTML, "__WAIT_STATE__", string(raw), 1)
	return strings.Replace(page, "__READY_URL__", string(ready), 1)
}

// serveWaitingPage 在 dsh 尚未就绪（或正在重启）时输出等待页。页面脚本会轮询
// /_ready，并在就绪后立即跳转；Refresh 头是无 JS 客户端的兜底路径。
// 传入的 r 已经剥掉挂载前缀（见 stripMount），因此两处对外地址都要用 mount 补回。
func serveWaitingPage(w http.ResponseWriter, r *http.Request, st proxyState, mount proxyMount) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Refresh", "10; url="+mount.joinURI(r.URL.RequestURI()))
	w.WriteHeader(200)
	io.WriteString(w, waitingPageHTMLFor(st, mount))
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
	// boot 是控制台启动流水线的阶段（见 boot.go）。反代早于 dsh 启动即开始监听，
	// 就绪判定必须同时看它、dsh 端口与会话凭据，详见 state()。
	boot *bootState
	// runMu/runAt/runAlive 缓存“dsh 进程是否还在”（state() 里区分“启动中/已停止”
	// 用）。DshManager.Running() 在 dsh 已不存在时会每次重扫 /proc，而等待页上的
	// 浏览器会持续轮询 /_ready —— 加一层 2 秒共享缓存，避免多个等待页把 /proc
	// 扫成热点（该值只影响等待页文案，滞后 2 秒无副作用）。
	runMu    sync.Mutex
	runAt    time.Time
	runAlive bool
	// mount 是这条监听对外占据的路径（见 proxyMount）：历史 TCP 端口是根挂载
	// （prefix 为空，路径原样转发）；平台网关转发的 unix socket 挂在子路径下
	// （默认 /app/Harness/dsh），进站请求剥掉前缀、自留路径与跳转目标补回前缀。
	mount proxyMount
	// gatewayLine 标记这条监听是否为「平台网关那条线」（Unix Socket，见
	// startProxySocket）。只有它认网关注入的身份头（X-Trim-Username /
	// X-Trim-Isadmin / X-Trim-Userid）并据此跳过登录鉴权：该 socket 不在网络上
	// 暴露，只有本机网关进程能连；TCP 端口线在局域网内可达，认这三个头等于把
	// 端口鉴权交给客户端自己声明。
	gatewayLine bool
}

// proxyMount 描述反代对外的挂载点（baseurl）。
//
//   - prefix 为空：根挂载。反代直接占据站点根，请求路径原样转发给 dsh（历史行为，
//     TCP 端口监听即如此，端口见 AppConfig.ProxyPort）。
//   - prefix 非空（如 "/app/Harness/dsh"）：反代挂在子路径下。这类部署来自平台网关
//     ——它把 http://<fnip>:<port>/app/Harness/dsh 整段转发到本进程的 unix socket，
//     反代必须把前缀剥干净再转发给 dsh（dsh 只认 /、/api、/plugins 等根路径），
//     并把自留路径（/_login、/_logout、/_ready）与 302 目标重新加上前缀，使
//     浏览器始终留在子路径下。
//
// dsh 0.1.7-alpha.1 起其前端产物完全按文档相对路径生成（index 由
// dsh-host-frontend-static 注入 <base href="./">，插件 bundle / API / SSE / 流
// mux 都用去前导斜杠的相对形式），因此浏览器会自动把地址拼成
// "<prefix>/api/..."，dsh 侧不需要任何 baseurl 配置——配对成立的唯一条件就是
// 反代把前缀剥离干净（并保持 URL 以 "/" 结尾，相对路径的基准才是挂载目录）。
type proxyMount struct {
	prefix string // 形如 "/app/Harness/dsh"，无尾斜杠；空串表示根挂载
}

// newProxyMount 归一化挂载前缀：空串或 "/" 表示根挂载；其余去掉尾斜杠并补前导斜杠。
func newProxyMount(baseURL string) proxyMount {
	p := strings.TrimSpace(baseURL)
	if p == "" || p == "/" {
		return proxyMount{}
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return proxyMount{prefix: strings.TrimRight(p, "/")}
}

func (m proxyMount) root() bool { return m.prefix == "" }

// dir 返回浏览器可见的挂载目录（根挂载即 "/"）。
func (m proxyMount) dir() string { return m.prefix + "/" }

// join 把反代自留的绝对路径（/_login、/_logout、/_ready）拼成浏览器可见路径。
func (m proxyMount) join(path string) string { return m.prefix + path }

// joinURI 把挂载内的请求 URI（以 "/" 开头，可带 query）拼成浏览器可见地址。
func (m proxyMount) joinURI(uri string) string {
	if !strings.HasPrefix(uri, "/") {
		return m.dir()
	}
	return m.prefix + uri
}

// strip 把请求路径换算成挂载内路径。返回的 path 以 "/" 开头（挂载根为 "/"），
// 空串表示「挂载点本身但缺尾斜杠」；第二个返回值表示该路径是否属于本挂载。
func (m proxyMount) strip(path string) (string, bool) {
	if m.root() {
		return path, true
	}
	if path == m.prefix {
		return "", true
	}
	if strings.HasPrefix(path, m.prefix+"/") {
		return path[len(m.prefix):], true
	}
	return "", false
}

func newReverseProxy(a *Auth, dsh *DshManager, boot *bootState) *reverseProxy {
	// TCP 端口监听（根挂载）：不认网关注入的身份头，见 gatewayLine。
	return newReverseProxyMount(a, dsh, boot, "", false)
}

// newReverseProxyAt 构造挂在 baseURL 下的反代（空串 = 根挂载，历史行为）。
//
// 该构造只用于 Unix Socket 监听（平台网关那条线，见 startProxySocket），因此信任
// 网关注入的身份头：飞牛 OS 已完成登录认证，这条线上的请求跳过 harness 登录鉴权。
func newReverseProxyAt(a *Auth, dsh *DshManager, boot *bootState, baseURL string) *reverseProxy {
	return newReverseProxyMount(a, dsh, boot, baseURL, true)
}

func newReverseProxyMount(a *Auth, dsh *DshManager, boot *bootState, baseURL string, gatewayLine bool) *reverseProxy {
	if boot == nil {
		boot = newBootState()
	}
	return &reverseProxy{auth: a, dsh: dsh, boot: boot, mount: newProxyMount(baseURL), gatewayLine: gatewayLine}
}

// stripMount 在进入任何业务分支之前把挂载前缀剥掉：
//   - 根挂载原样返回；
//   - 挂载点本身（"<prefix>" 无尾斜杠）重定向到目录 "<prefix>/"。dsh 前端的
//     <base href="./"> 以「目录」为基准，缺尾斜杠会让相对路径解析到站点根，
//     资源与 API 全部跑出子路径；
//   - "<prefix>/..." 返回剥掉前缀的请求副本（原始 r 保持不变，等待页的 Refresh
//     与登录跳转需要浏览器可见的带前缀地址）；
//   - 其余路径不属于本挂载（网关配置错误），直接 404 并说明本监听服务的挂载点。
func (p *reverseProxy) stripMount(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	if p.mount.root() {
		return r, true
	}
	path, ok := p.mount.strip(r.URL.Path)
	if !ok {
		http.Error(w, "not found: this listener serves "+p.mount.dir(), http.StatusNotFound)
		return nil, false
	}
	if path == "" {
		target := p.mount.dir()
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		// 301 会把非 GET 请求降级成 GET；POST 等用 308 保持方法与请求体。
		code := http.StatusMovedPermanently
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			code = http.StatusPermanentRedirect
		}
		http.Redirect(w, r, target, code)
		return nil, false
	}
	stripped := r.Clone(r.Context())
	stripped.URL.Path = path
	if stripped.URL.RawPath != "" {
		// 保留转义形态（dsh 的插件 bundle 路径里带 ?? 与逗号，转义与否必须原样透传）。
		if raw, ok := p.mount.strip(stripped.URL.RawPath); ok && raw != "" {
			stripped.URL.RawPath = raw
		} else {
			stripped.URL.RawPath = ""
		}
	}
	return stripped, true
}

// getChecker returns a BackendChecker using the current DshPort from config.
func (p *reverseProxy) getChecker() *BackendChecker {
	cfg := GetConfig()
	return newBackendChecker(cfg.DshPort)
}

// dshAliveCached 是 DshManager.Running() 的 2 秒共享缓存（见 reverseProxy 字段注释）。
func (p *reverseProxy) dshAliveCached() bool {
	p.runMu.Lock()
	defer p.runMu.Unlock()
	if time.Since(p.runAt) < 2*time.Second {
		return p.runAlive
	}
	p.runAlive = p.dsh.Running()
	p.runAt = time.Now()
	return p.runAlive
}

// state 汇总「现在能否把请求放行给 dsh」，同时给出等待页要显示的阶段。
//
// 放行需要同时满足三件事，缺一不可：
//  1. 启动流水线已收尾（boot 不再是 starting/auth/deps）——流水线收尾可能重启
//     dsh（例如 node-pty 的 pnpm install 之后），提前放行会让界面随即失效；
//  2. dsh 端口已监听；
//  3. 本代 dsh 的会话凭据已落定（SessionSettled）—— 否则带不出 dsh-auth-* cookie，
//     转发过去只会被 dsh 拒绝。
//
// 返回的 checker 复用本次端口探测的结果，避免热路径上重复拨号。
func (p *reverseProxy) state() (proxyState, *BackendChecker) {
	checker := p.getChecker()
	portUp := checker.quick(500 * time.Millisecond)
	settled := p.dsh.SessionSettled()
	phase, detail := p.boot.get()

	switch {
	case p.boot.booting():
		// 启动流水线进行中：即使端口已通也不放行，按流水线的阶段显示等待页。
	case portUp && settled:
		return proxyState{Phase: phaseReady, Ready: true, Detail: detail}, checker
	case !portUp:
		// 端口未监听。流水线之外只有几种情况要区分：显式说明（启动失败 / 未自动
		// 启动）原样保留；进程还在（自重启、更新后拉起）显示“启动中”；进程不在
		// （用户手动停止）显示“已停止”。
		if phase != phaseFailed && phase != phaseDisabled {
			if p.dshAliveCached() {
				phase = phaseStarting
			} else {
				phase = phaseStopped
			}
			detail = ""
		}
	default:
		// 端口通了但凭据还没换到：继续等凭据。
		phase = phaseAuth
		detail = ""
	}
	return proxyState{Phase: phase, Detail: detail}, checker
}

// serveReadyState 输出等待页轮询用的就绪状态（JSON）。不缓存：轮询必须拿到实时值。
func (p *reverseProxy) serveReadyState(w http.ResponseWriter, st proxyState) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, st)
}

func (p *reverseProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 子路径挂载：先剥离挂载前缀，后续所有分支（鉴权、握手、就绪门禁、等待页、
	// 转发）都工作在「挂载内路径」上——dsh 只认根路径，剥干净才能命中它的
	// /、/api、/plugins 路由。
	stripped, ok := p.stripMount(w, r)
	if !ok {
		return
	}
	r = stripped
	// 飞牛网关访问：网关那条线（gatewayLine）上的请求带网关注入的身份头，说明飞牛
	// OS 已完成登录认证，因此跳过 harness 登录鉴权。端口线不认这些头。
	// 注意跳过鉴权不等于跳过就绪门禁：等待页、/_ready 轮询与 dsh 会话凭据判定都照旧。
	gw, viaGateway := gatewayVisitor{}, false
	if p.gatewayLine {
		gw, viaGateway = parseGatewayVisitor(r)
	}
	if p.auth.handleAuthRoutes(w, r, p.mount) {
		return
	}
	// WebSocket upgrade: forward the raw connection to dsh after the auth gate.
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		p.handleUpgrade(w, r, viaGateway)
		return
	}
	st, checker := p.state()
	// 鉴权先于就绪判断。反代从控制台启动的第一刻就在监听，此时 dsh 往往还没起来：
	// 若沿用旧的「先看 dsh 是否就绪、再鉴权」顺序，访客在控制台启动期间只能看到
	// 等待页而无法登录。现在未登录先给登录页，登录后再看等待页/放行。
	// 仅面板后端的内部探测路径（如 /dsh-market/）视为可信、跳过鉴权；
	// 普通浏览器流量（含经 nginx 嵌套反代到达的）必须通过面板登录鉴权。
	if !p.isInternalRequest(r) {
		if !viaGateway && !p.auth.isAuthed(r) {
			// next 记的是挂载内路径（如 "/api/x"），登录成功后由 handleAuthRoutes
			// 用 mount.join 补回前缀，浏览器因此留在子路径下。
			next := safeNext(r.URL.Path + "?" + r.URL.RawQuery)
			http.Redirect(w, r, p.mount.join(authLogin)+"?next="+url.QueryEscape(next), http.StatusFound)
			return
		}
		if r.URL.Path == readyPath {
			p.serveReadyState(w, st)
			return
		}
	}
	if !st.Ready {
		serveWaitingPage(w, r, st, p.mount)
		return
	}
	// 记录访问：端口访问按 harness 会话令牌记（IP、最近访问时间、登录有效期），
	// 网关访问按飞牛用户记（网关不签会话 cookie，列表里只标记「网关访问」）。
	if viaGateway {
		p.auth.recordGatewayVisitor(gw)
	} else {
		p.auth.recordVisitor(r)
	}
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
// viaGateway 表示该请求来自飞牛网关那条线且带网关身份头：飞牛 OS 已认证，跳过
// 面板登录鉴权（与 HTTP 分支同一份判定）。
func (p *reverseProxy) handleUpgrade(w http.ResponseWriter, r *http.Request, viaGateway bool) {
	// Auth guard: browsers can't follow a 302 on an upgrade, so reject with 401.
	// 与 HTTP 请求一致，WebSocket 连接也必须通过面板登录鉴权（harness_session），
	// 不得仅凭 dsh 的会话 cookie 放行，以免绕过控制台登录鉴权。
	if !viaGateway && !p.auth.isAuthed(r) {
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
	// 凭据门禁与 HTTP 一致（见 state）：本代 dsh 的会话 cookie 还没换取完成时，
	// 上游会因缺少 dsh-auth-* cookie 拒绝升级，直接 503 让客户端稍后重试。
	if !p.dsh.SessionSettled() {
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}
	// 保留原有的 10 秒容忍窗口：dsh 自重启（市场一键重启）期间端口会短暂消失，
	// 此时客户端已在界面上，等它回来比立刻 503 更友好。
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

	// dsh 市场（dsh-market）一键自重启检测：前端点击“立即重启”会对
	// POST /dsh-market/restart 发起请求，dsh-market 排定自重启后返回 200
	//（实际约 0.5s 后对自己 SIGTERM，由 detached helper 拉起新进程）。此时
	// 通知 DshManager 作废缓存并后台重新发现新 dsh PID，刷新 PID 文件与
	// 概览 CPU/内存监控（否则旧 PID 的僵尸态会让监控读到 0）。
	if isPrivilegedControlRoute(r.URL.Path) && r.Method == http.MethodPost && resp.StatusCode == http.StatusOK {
		p.dsh.notifySelfRestart()
	}

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
			body = injectIntoHTML(body, dshDiagEnabled(r))
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
