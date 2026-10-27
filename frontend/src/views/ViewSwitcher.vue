<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue'
import { useRoute } from 'vue-router'

const route = useRoute()

// 当前子页面由 URL 的 ?view= 查询参数决定；默认进入概览页
const view = computed(() => {
  const q = (route.query.view as string) || 'overview'
  // 无终端构建（TERMINAL=0）：`?view=terminal` 直达链接退回概览。
  // 这里的 TerminalView 仍会被 import 解析，但构建时已被 vite 的 alias 换成占位页，
  // xterm 与终端逻辑不会进入产物。
  if (q === 'terminal' && !__TERMINAL_ENABLED__) return 'overview'
  return q
})

// 懒加载子页面，保持按需分包
const views: Record<string, ReturnType<typeof defineAsyncComponent>> = {
  overview: defineAsyncComponent(() => import('@/views/OverviewView.vue')),
  settings: defineAsyncComponent(() => import('@/views/SettingsView.vue')),
  directory: defineAsyncComponent(() => import('@/views/DirectoriesView.vue')),
  plugins: defineAsyncComponent(() => import('@/views/PluginsView.vue')),
  terminal: defineAsyncComponent(() => import('@/views/TerminalView.vue')),
  logs: defineAsyncComponent(() => import('@/views/LogView.vue')),
}
</script>

<template>
  <!-- KeepAlive 缓存已挂载的子页面，切换标签时保留终端会话/输入内容，不被销毁重建 -->
  <KeepAlive include="TerminalView">
    <component :is="views[view] || views.settings" />
  </KeepAlive>
</template>