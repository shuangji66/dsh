<script setup lang="ts">
// PageHeader.vue —— 子页面统一标题栏（参考 fluxor 的顶部操作栏）。
//
// 结构：一张横向卡片包住「图标 + 标题」与右侧操作按钮组；按钮由调用方经默认插槽传入，
// 因此各页面的按钮样式/数量互不影响，但整体的外框、内边距、标题字号与图标风格全站一致。
// 标题图标直接用 utils/icons.ts 里的原始 SVG 字符串（与左侧导航同一套）。
//
// 间距由调用方控制：flex 布局的页面（终端 / 日志，父级用 gap）不要再加 margin，
// 普通滚动页传 class="mb-6" 即可（单根组件上的 class 会落到 <header> 上）。
defineProps<{ title: string; icon?: string }>()
</script>

<template>
  <header class="g-card px-5 py-3 min-h-14 flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
    <h2 class="font-display text-base font-semibold text-ink dark:text-white flex items-center gap-2.5 min-w-0">
      <span v-if="icon" class="w-5 h-5 flex-shrink-0 inline-block text-brand" v-html="icon"></span>
      <span class="truncate">{{ title }}</span>
    </h2>
    <div class="flex flex-wrap items-center gap-2">
      <slot />
    </div>
  </header>
</template>
