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
function reloadOnceForStaleChunk() {
  try {
    const last = Number(sessionStorage.getItem(CHUNK_RELOAD_KEY) || 0)
    if (Number.isFinite(last) && Date.now() - last < 30_000) return
    sessionStorage.setItem(CHUNK_RELOAD_KEY, String(Date.now()))
  } catch {
    /* sessionStorage 不可用：不加节流，直接重载 */
  }
  window.location.reload()
}

window.addEventListener('vite:preloadError', (event) => {
  // 阻止 Vite 默认的 console.error；重载后错误自然消失。
  event.preventDefault()
  reloadOnceForStaleChunk()
})

// 路由懒加载（ViewSwitcher 自身）走的是同一条动态 import；某些浏览器/构建组合下
// 只有 router.onError 能拿到错误，这里再兜一层（同一个节流键，不会重复刷新）。
router.onError((err) => {
  if (/dynamically imported module|module script failed/i.test(String(err))) {
    reloadOnceForStaleChunk()
  }
})

createApp(App).use(createPinia()).use(router).mount('#app')