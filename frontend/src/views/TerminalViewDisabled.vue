<script setup lang="ts">
// TerminalViewDisabled.vue —— 无终端构建（TERMINAL=0）时由 vite.config.ts 的 alias
// 顶替 TerminalView.vue。
//
// 它刻意不导入 xterm、不连 WebSocket、不读快捷指令：终端功能的任何代码都不进入产物。
// 正常导航里没有终端入口（见 App.vue），旧 `/terminal` 路径会重定向到概览；
// 本文件只兜住 `?view=terminal` 这类手写直达链接。
defineOptions({ name: 'TerminalViewDisabled' })
</script>

<template>
  <div />
</template>
