<script setup lang="ts">
// PageHeader.vue —— 子页面统一标题栏（参考 fluxor 的顶部操作栏）。
//
// 结构：一张横向卡片包住「图标 + 标题」与右侧操作按钮组；按钮由调用方经默认插槽传入，
// 因此各页面的按钮行为/数量互不影响，但整体的外框、内边距、标题字号与图标风格全站一致。
// 标题图标直接用 utils/icons.ts 里的原始 SVG 字符串（与左侧导航同一套）。
//
// 标题栏按钮**样式统一**（各页面照抄同一串类名，别自创尺寸）：
//   `g-btn-secondary h-8 px-3 text-xs`（危险操作用 `g-btn-danger h-8 px-3 text-xs`）
// —— 统一字号（text-xs）、统一边框、统一不填充底色；h-8/px-3/text-xs 是 utilities，
// 会覆盖 .g-btn-* 组件类里的 h-10/px-4/text-sm，因此不必写 `!` 前缀。
//
// 标题栏**位置也统一**（各子页面的标题栏贴在同一高度上，基准是终端页）：
//   - 顶部留白 12px（sm 以上 16px）：滚动页外壳用 `pt-3 sm:pt-4`，日志页同理；
//   - 标题栏下方间距 12px：滚动页给本组件传 `mb-3`，日志页用父级 `gap-3`；
//   - 终端页是基准（它自己那层用 `pt-3 sm:pt-4` + `gap-3`），别为它单独挪这两处。
// 标题栏高度本身由 min-h-14 + 内容决定，各页一致；只有按钮多到换行（窄屏终端页）才会变高。
//
// 标题栏**宽度也统一**：所有子页面外壳都是 `px-4 sm:px-8 max-w-6xl mx-auto`（含终端 / 日志），
// 因此各页标题栏左右边缘对齐、宽屏下居中限宽；新页面照抄这串类名（终端页的移动端辅助键条
// 是通栏例外，它在那层内缩之外）。
//
// 间距由调用方控制：flex 布局的页面（终端 / 日志，父级用 gap）不要再加 margin，
// 普通滚动页传 class="mb-3" 即可（单根组件上的 class 会落到最外层 <div> 上）。
//
// ===== sticky：顶栏不参与滚动（概览 / 设置 / 插件 / 目录传它，终端 / 日志不要传） =====
// 后两者是整屏定高布局、自身不滚动，标题栏本来就不会跑掉。四个可滚动子页面传 `sticky`
// 后，标题栏停在视口顶部不动，内容从它后面滚过去。要点：
//   1. 根节点是**一层 div**（终端 / 日志页里这层不带任何类，对布局完全透明：它是父级
//      flex/块级容器里的普通块，卡片照旧撑满宽度、高度不变）；
//   2. 移动端与桌面端的滚动容器不同（前者是 App.vue 的 <main>，后者是文档），而
//      `position: sticky` 粘的是**最近的滚动容器**，所以两端一套规则、不需要写断点；
//   3. `-mt-3 sm:-mt-4` + `pt-3 sm:pt-4`：负外边距先把这层提到外壳顶部留白之上，再用
//      同值内边距把卡片推回原位 —— 于是**静止位与固定位重合**（卡片滚动前后相对视口
//      一动不动，不会先上移 12px 再粘住），且这层的背景连卡片上方那条留白一起盖住。
//      这组数值与各页外壳的 `pt-3 sm:pt-4` 绑死，改外壳顶部留白要一起改；
//   4. 这层必须是不透明的**页面底色**（`bg-bg dark:bg-[#0B0B0F]`，同 App.vue 根节点）：
//      内容从标题栏后面经过，底色把卡片圆角与四周留白都盖住，否则会从缝里漏出内容。
//      它的宽度就是外壳内容宽（= 卡片宽 = 内容宽），所以左右不需要再额外铺满；
//   5. z-20：卡片悬浮（.g-card-hover 的 transform）与 .g-fade-in 会自建层叠上下文，
//      不抬起来就会被内容压住；弹窗（z-50 / z-[60]）与 toast（z-[100]）仍在它之上。
defineProps<{ title: string; icon?: string; sticky?: boolean }>()

// 背景层只在 sticky 时存在，避免给终端 / 日志页多套一层视觉无关的底色。
const wrapperClass = 'sticky top-0 z-20 -mt-3 sm:-mt-4 pt-3 sm:pt-4 bg-bg dark:bg-[#0B0B0F]'
</script>

<template>
  <!-- sticky 时这一层是标题栏的「遮罩层」；非 sticky 时它是纯结构层（不带类） -->
  <div :class="sticky ? wrapperClass : ''">
    <header class="g-card px-5 py-3 min-h-14 flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
      <h2 class="font-display text-base font-semibold text-ink dark:text-white flex items-center gap-2.5 min-w-0">
        <span v-if="icon" class="w-5 h-5 flex-shrink-0 inline-block text-brand" v-html="icon"></span>
        <span class="truncate">{{ title }}</span>
      </h2>
      <div class="flex flex-wrap items-center gap-2">
        <slot />
      </div>
    </header>
  </div>
</template>
