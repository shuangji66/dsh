<script setup lang="ts">
import { computed } from 'vue'
import { renderMarkdown } from '@/composables/useMarkdown'

// 轻量 Markdown 展示组件：把 Markdown 源文本安全渲染为 HTML（v-html 注入）。
// 渲染安全性（原始 HTML 转义、链接协议白名单）见 useMarkdown.ts；
// 本组件只负责展示与样式（列表/标题/代码块/链接等在两种主题下的排版）。
const props = defineProps<{ source: string }>()

const html = computed(() => renderMarkdown(props.source))
</script>

<template>
  <div class="markdown-text" v-html="html"></div>
</template>

<style scoped>
/* 主题变量：浅色默认值 + .dark 覆盖（容器自身的底色/文字色由父级 Tailwind 类控制） */
.markdown-text {
  --md-bg-subtle: rgba(0, 0, 0, 0.06); /* 代码 / pre 底色 */
  --md-border-subtle: rgba(0, 0, 0, 0.12); /* 引用 / 表格边框 */
  font-size: inherit;
  line-height: 1.6;
  word-break: break-word;
}
:global(.dark) .markdown-text {
  --md-bg-subtle: rgba(255, 255, 255, 0.09);
  --md-border-subtle: rgba(255, 255, 255, 0.2);
}

/* 段落 */
.markdown-text :deep(p) {
  margin: 0.4em 0;
}

/* 标题：在 text-xs 容器内按比例缩放的紧凑字号 */
.markdown-text :deep(h1),
.markdown-text :deep(h2),
.markdown-text :deep(h3),
.markdown-text :deep(h4),
.markdown-text :deep(h5),
.markdown-text :deep(h6) {
  margin: 0.6em 0 0.3em;
  font-weight: 600;
  line-height: 1.35;
}
.markdown-text :deep(h1) {
  font-size: 1.35em;
}
.markdown-text :deep(h2) {
  font-size: 1.2em;
}
.markdown-text :deep(h3) {
  font-size: 1.1em;
}
.markdown-text :deep(h4),
.markdown-text :deep(h5),
.markdown-text :deep(h6) {
  font-size: 1em;
}

/* 列表 */
.markdown-text :deep(ul),
.markdown-text :deep(ol) {
  margin: 0.4em 0;
  padding-left: 1.4em;
}
.markdown-text :deep(li) {
  margin: 0.2em 0;
}
.markdown-text :deep(li > input[type='checkbox']) {
  accent-color: #6366f1;
  margin-right: 0.3em;
}

/* 行内代码 / 代码块 */
.markdown-text :deep(code) {
  background: var(--md-bg-subtle);
  border-radius: 4px;
  padding: 0.1em 0.35em;
  font-size: 0.92em;
}
.markdown-text :deep(pre) {
  background: var(--md-bg-subtle);
  border-radius: 6px;
  padding: 0.6em 0.75em;
  margin: 0.5em 0;
  overflow-x: auto;
}
.markdown-text :deep(pre code) {
  background: transparent;
  padding: 0;
  font-size: 0.92em;
}

/* 链接：品牌色 + 下划线，与全局链接交互一致 */
.markdown-text :deep(a) {
  color: #6366f1;
  text-decoration: underline;
  text-underline-offset: 2px;
  word-break: break-all;
}

/* 引用块 */
.markdown-text :deep(blockquote) {
  border-left: 3px solid var(--md-border-subtle);
  margin: 0.5em 0;
  padding-left: 0.8em;
  opacity: 0.85;
}

/* 分隔线 */
.markdown-text :deep(hr) {
  border: none;
  border-top: 1px solid var(--md-border-subtle);
  margin: 0.7em 0;
}

/* 表格（GFM）：最小化边框，容器内横向可滚动 */
.markdown-text :deep(table) {
  border-collapse: collapse;
  margin: 0.5em 0;
  width: 100%;
  font-size: 0.95em;
}
.markdown-text :deep(th),
.markdown-text :deep(td) {
  border: 1px solid var(--md-border-subtle);
  padding: 0.25em 0.5em;
  text-align: left;
}
.markdown-text :deep(th) {
  font-weight: 600;
}
</style>