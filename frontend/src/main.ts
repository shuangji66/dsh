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

createApp(App).use(createPinia()).use(router).mount('#app')