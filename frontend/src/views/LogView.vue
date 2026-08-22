<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useToastStore } from '@/stores/toast'
import { api } from '@/serverapi'

const toast = useToastStore()
const logContent = ref('')
const logPath = ref('')
const loading = ref(true)
const error = ref('')
const stickToBottom = ref(true) // 是否跟随底部自动滚动
let timer: ReturnType<typeof setInterval> | null = null

const el = ref<HTMLElement | null>(null)

// 日志过大时仅保留尾部，避免渲染超大文本卡顿
const MAX_LEN = 500 * 1024

async function load() {
  try {
    const r = await api.logs()
    logPath.value = r.path
    const box = el.value
    // 记录新内容到达前是否处于底部
    const wasAtBottom = !box || box.scrollHeight - box.scrollTop - box.clientHeight < 40
    if (r.content !== logContent.value) {
      let content = r.content
      if (content.length > MAX_LEN) {
        content = '…（日志过长，仅显示末尾）\n' + content.slice(-MAX_LEN)
      }
      logContent.value = content
      if (wasAtBottom) {
        await nextTick()
        scrollToBottom()
      }
    }
    loading.value = false
  } catch (e) {
    error.value = (e as Error).message
    loading.value = false
  }
}

function scrollToBottom() {
  const box = el.value
  if (box) box.scrollTop = box.scrollHeight
}

function onScroll() {
  const box = el.value
  if (!box) return
  // 用户上翻查看历史时暂停自动滚动；回到底部时恢复
  stickToBottom.value = box.scrollHeight - box.scrollTop - box.clientHeight < 40
}

onMounted(() => {
  load()
  timer = setInterval(load, 2000) // 实时刷新
})

onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <div class="py-8 sm:py-12 px-4 sm:px-8 max-w-5xl mx-auto">
    <header class="flex items-center justify-between gap-4 mb-6 flex-wrap">
      <div>
        <p class="text-ink-faint dark:text-[#8A8A92] text-sm font-medium uppercase tracking-widest">日志</p>
        <h1 class="font-display text-2xl font-semibold text-ink dark:text-white mt-1">运行日志</h1>
      </div>
      <div class="flex items-center gap-3">
        <button class="g-btn-ghost" @click="load" :disabled="loading">刷新</button>
        <span class="flex items-center gap-1.5 text-xs text-ink-soft dark:text-[#A6A6AD]">
          <span class="inline-block w-2 h-2 rounded-full bg-success animate-pulse"></span>
          每 2 秒自动刷新
        </span>
      </div>
    </header>

    <div v-if="error && !logContent" class="g-card p-8 text-center text-ink-soft dark:text-[#A6A6AD] text-sm">
      {{ error }}
    </div>

    <div v-else class="g-card overflow-hidden">
      <div class="flex items-center justify-between gap-3 px-4 py-2.5 bg-surface dark:bg-[#111115] border-b border-line dark:border-[#2A2A32]">
        <span class="font-mono text-xs text-ink-soft dark:text-[#A6A6AD] truncate">{{ logPath || '（未配置日志文件，未设置 HARNESS_LOG_FILE）' }}</span>
        <button
          @click="stickToBottom = !stickToBottom"
          class="g-btn-ghost text-xs flex-shrink-0"
          :title="stickToBottom ? '暂停自动滚动' : '恢复自动滚动'"
        >
          {{ stickToBottom ? '自动滚动：开' : '自动滚动：关' }}
        </button>
      </div>
      <pre
        ref="el"
        @scroll="onScroll"
        class="h-[62vh] overflow-auto p-4 bg-[#0f1115] text-[#d6dce4] text-xs leading-5 font-mono whitespace-pre-wrap break-all m-0"
      >{{ logContent || '暂无日志内容' }}</pre>
    </div>
  </div>
</template>