/// <reference types="vite/client" />

// 编译期功能开关，由 vite.config.ts 的 define 注入（见 Makefile / build.sh 的 TERMINAL=0）。
// 终端功能可整体剔除：关闭时终端页、xterm 与快捷指令都不进入产物。
declare const __TERMINAL_ENABLED__: boolean

declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<{}, {}, any>
  export default component
}