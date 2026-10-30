import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import './style.css'
// xterm 的样式由 TerminalView.vue 自己 import：它会跟着那个懒加载 chunk 一起加载，
// 无终端构建（TERMINAL=0）里 TerminalView 被占位页顶替，xterm 的 CSS 也就不会进产物。
import { primeTrimApp } from './utils/trimApp'

// 尽早与飞牛桌面（宿主）握手：宿主的握手监听只保留 60 秒（超时即销毁，之后不再应答），
// 而 SDK 那份「无人应答」的初始化会永久 pending，导致资源页的添加/打开静默失效。
// 详见 utils/trimApp.ts 的说明。
primeTrimApp()

// 懒加载 chunk 失效兜底。
//
// 控制台升级（fpk 替换二进制）后，旧标签页仍在内存里，但它引用的 hash 资源已经被新的
// 内嵌 FS 替换掉了；此时打开一个没加载过的子页面，动态 import 会拿到 spaHandler 回退的
// index.html（HTML 响应），浏览器按 MIME 拒绝执行 —— 页面只剩空白，用户只能自己刷新。
// Vite 在 preload/动态 import 失败时会派发 `vite:preloadError`（可 cancel），这里拦下它
// 并整页重载一次，让入口重新拿到新的 index.html 与新的 hash。
//
// 用 sessionStorage 做 30 秒节流：后端真的故障时不至于无限刷新；升级本身只需要一次重载。
const CHUNK_RELOAD_KEY = 'console-chunk-reload-at'
const CHUNK_RELOAD_MIN_INTERVAL_MS = 30_000
const CHUNK_RELOAD_BANNER_ID = 'console-chunk-reload-banner'

/** 只有「取不到脚本/模块」才算陈旧资源；CSS 预加载失败不该整页重载（会丢终端会话与未保存表单）。 */
function isChunkLoadFailure(payload: unknown): boolean {
  const message = payload instanceof Error ? payload.message : String(payload ?? '')
  if (!message || /unable to preload css/i.test(message)) return false
  return /dynamically imported module|module script failed|importing a module script failed|unable to preload|failed to fetch/i
    .test(message)
}

/**
 * 返回 true 表示这次**真的**发起了重载（调用方据此决定是否吞掉错误）。
 * 节流命中时返回 false —— 千万不要在这种情况下还 `preventDefault()`：
 * Vite 的 handlePreloadError 在 defaultPrevented 时是 `return` 而不是 `throw`
 * （`baseModule().catch(handlePreloadError)`），动态 import 会以 undefined 解析，
 * defineAsyncComponent 把它缓存成「已解析」，此后永不重试 —— 页面永久空白，
 * router.onError 也拿不到错误。
 */
function reloadOnceForStaleChunk(): boolean {
  try {
    const last = Number(sessionStorage.getItem(CHUNK_RELOAD_KEY) || 0)
    if (Number.isFinite(last) && Date.now() - last < CHUNK_RELOAD_MIN_INTERVAL_MS) return false
    sessionStorage.setItem(CHUNK_RELOAD_KEY, String(Date.now()))
  } catch {
    /* sessionStorage 不可用：没有节流信息，按「可以重载」处理 */
  }
  window.location.reload()
  return true
}

/**
 * 自动重载被节流挡下时的兜底：给用户一个能点的出口（此时界面可能已经是空白页，
 * 所以只用原生 DOM，不依赖 Vue 是否还活着）。文案双语并排，与等待页同一口径。
 */
function showChunkReloadBanner(): void {
  if (document.getElementById(CHUNK_RELOAD_BANNER_ID)) return
  const banner = document.createElement('div')
  banner.id = CHUNK_RELOAD_BANNER_ID
  banner.setAttribute('role', 'alert')
  banner.style.cssText = [
    'position:fixed',
    'left:50%',
    'bottom:24px',
    'transform:translateX(-50%)',
    'z-index:2147483000',
    'display:flex',
    'align-items:center',
    'gap:12px',
    'max-width:calc(100vw - 32px)',
    'padding:10px 14px',
    'border-radius:10px',
    'background:#111115',
    'color:#fff',
    'font:13px/1.5 system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif',
    'box-shadow:0 8px 30px rgba(0,0,0,.28)',
  ].join(';')
  const text = document.createElement('span')
  text.textContent = '界面资源加载失败，请刷新页面。 / Failed to load interface assets. Please reload.'
  const button = document.createElement('button')
  button.type = 'button'
  button.textContent = '刷新 / Reload'
  button.style.cssText = 'flex:0 0 auto;padding:4px 12px;border:1px solid rgba(255,255,255,.4);border-radius:8px;background:transparent;color:inherit;font:inherit;cursor:pointer'
  button.addEventListener('click', () => window.location.reload())
  banner.append(text, button)
  document.body.appendChild(banner)
}

window.addEventListener('vite:preloadError', (event) => {
  const payload = (event as Event & { payload?: unknown }).payload
  if (!isChunkLoadFailure(payload)) return
  // 真的重载了才吞掉错误；被节流时让错误继续抛（原因见 reloadOnceForStaleChunk 的注释）。
  if (reloadOnceForStaleChunk()) {
    event.preventDefault()
    return
  }
  showChunkReloadBanner()
})

// 路由懒加载（ViewSwitcher 自身）走的是同一条动态 import；某些浏览器/构建组合下
// 只有 router.onError 能拿到错误，这里再兜一层（同一个节流键，不会重复刷新）。
router.onError((err) => {
  if (!isChunkLoadFailure(err)) return
  if (!reloadOnceForStaleChunk()) showChunkReloadBanner()
})

createApp(App).use(createPinia()).use(router).mount('#app')