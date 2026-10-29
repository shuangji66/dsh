<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue'
import type { Component } from 'vue'
import { useRoute } from 'vue-router'
import OverviewView from '@/views/OverviewView.vue'

const route = useRoute()

// 子页面按需分包，但**默认页（概览）静态引入**：它本来每次打开都要加载，再拆成一层
// 异步 chunk 只会把首屏变成 index → ViewSwitcher → Overview 三跳（本地实测多 ~40ms，
// 弱网/网关下多一个 RTT）。静态引入后 ViewSwitcher 的 chunk 直接带概览，首屏两跳。
// 其余页面保持懒加载：设置/目录/插件/终端/日志都不会拖进首屏。
const views: Record<string, Component> = {
  overview: OverviewView,
  settings: defineAsyncComponent(() => import('@/views/SettingsView.vue')),
  directory: defineAsyncComponent(() => import('@/views/DirectoriesView.vue')),
  plugins: defineAsyncComponent(() => import('@/views/PluginsView.vue')),
  terminal: defineAsyncComponent(() => import('@/views/TerminalView.vue')),
  logs: defineAsyncComponent(() => import('@/views/LogView.vue')),
}

// 当前子页面由 URL 的 ?view= 查询参数决定；默认进入概览页。
// 未知值必须回退到 overview，且与 App.vue 的 `current`（导航高亮）保持同一口径：
// 以前这里回退的是 settings，`?view=bogus` 会「导航高亮概览、页面却是设置」。
const view = computed(() => {
  const q = typeof route.query.view === 'string' ? route.query.view : ''
  // 无终端构建（TERMINAL=0）：`?view=terminal` 直达链接退回概览。
  // 这里的 TerminalView 仍会被 import 解析，但构建时已被 vite 的 alias 换成占位页，
  // xterm 与终端逻辑不会进入产物。
  if (q === 'terminal' && !__TERMINAL_ENABLED__) return 'overview'
  // 必须用 own property 判断：`q in views` 会把 toString / constructor / __proto__
  // 这些原型链上的键也当成合法页面，`views[q]` 拿到的是函数，component :is 会报错。
  return Object.prototype.hasOwnProperty.call(views, q) ? q : 'overview'
})
</script>

<template>
  <!-- KeepAlive 缓存已挂载的子页面，切换标签时保留终端会话/输入内容，不被销毁重建 -->
  <KeepAlive include="TerminalView">
    <component :is="views[view] || views.overview" />
  </KeepAlive>
</template>