<script setup lang="ts">
// GithubIconLink.vue —— 弹窗标题右侧的 GitHub 裸图标外链。
//
// 「裸图标」= 不带任何按钮外观（无边框、无底色、无内边距、无按钮尺寸约束），
// 只作为外链入口：悬停变前景色，点击在新标签页打开对应仓库。用在标题右侧、
// 紧靠标题的位置（更新弹窗标题 / 关于弹窗标题）——不要给它套 g-btn-* 之类的
// 按钮样式，也不要改成带文字的按钮。
defineProps<{
  // 目标仓库地址
  href: string
  // 无障碍标签与悬停提示（一般传 t('about_github') 或 owner/repo）
  label: string
}>()
</script>

<template>
  <a
    :href="href"
    target="_blank"
    rel="noopener"
    :title="label"
    :aria-label="label"
    class="inline-flex shrink-0 items-center text-ink-soft dark:text-[#A6A6AD] hover:text-ink dark:hover:text-white transition-colors"
  >
    <svg class="w-4 h-4" viewBox="0 0 24 24" fill="currentColor">
      <path d="M12 .5C5.7.5.5 5.7.5 12c0 5.1 3.3 9.4 7.9 10.9.6.1.8-.2.8-.5v-1.7c-3.2.7-3.9-1.4-3.9-1.4-.5-1.3-1.3-1.7-1.3-1.7-1-.7.1-.7.1-.7 1.2.1 1.8 1.2 1.8 1.2 1 1.8 2.7 1.3 3.4 1 .1-.8.4-1.3.7-1.6-2.6-.3-5.3-1.3-5.3-5.7 0-1.3.5-2.3 1.2-3.1-.1-.3-.5-1.5.1-3.1 0 0 1-.3 3.2 1.2a11 11 0 0 1 5.8 0C17.2 4.7 18.2 5 18.2 5c.6 1.6.2 2.8.1 3.1.8.8 1.2 1.8 1.2 3.1 0 4.4-2.7 5.4-5.3 5.7.4.4.8 1.1.8 2.2v3.2c0 .3.2.6.8.5 4.6-1.5 7.9-5.8 7.9-10.9C23.5 5.7 18.3.5 12 .5z" />
    </svg>
  </a>
</template>
