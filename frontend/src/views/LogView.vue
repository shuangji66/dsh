<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useToastStore } from '@/stores/toast'
import { sseUrl } from '@/serverapi'
import { useI18n } from '@/composables/useI18n'

const toast = useToastStore()
const { t } = useI18n()
const logContent = ref('')
const logPath = ref('')
const loading = ref(true)
const error = ref('')
const stickToBottom = ref(true) // 是否跟随底部自动滚动
let logES: EventSource | null = null

// 导出日志原文件：浏览器下载后端附件
function exportLog() {
  if (!logPath.value) {
    toast.show(t('log_not_configured'), 'error')
    return
  }
  const a = document.createElement('a')
  a.href = sseUrl('/api/logs/download')
  a.download = ''
  document.body.appendChild(a)
  a.click()
  a.remove()
}

const el = ref<HTMLElement | null>(null)

// 日志过大时仅保留尾部，避免渲染超大文本卡顿
const MAX_LEN = 500 * 1024

function applySnapshot(snap: { path?: string; content?: string; exists?: boolean }) {
  if (snap.path !== undefined) logPath.value = snap.path
  const content = snap.content || ''
  const box = el.value
  // 记录新内容到达前是否处于底部
  const wasAtBottom = !box || box.scrollHeight - box.scrollTop - box.clientHeight < 40
  if (content !== logContent.value) {
    let c = content
    if (c.length > MAX_LEN) {
      c = '…（日志过长，仅显示末尾）\n' + c.slice(-MAX_LEN)
    }
    logContent.value = c
    if (wasAtBottom) {
      nextTick().then(scrollToBottom)
    }
  }
  loading.value = false
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

// 通过 SSE 监听后端主动推送，替换 2 秒轮询
function connectLogStream() {
  logES?.close()
  const es = new EventSource(sseUrl('/api/logs/stream'))
  logES = es
  es.addEventListener('log', (ev) => {
    try {
      const data = JSON.parse((ev as MessageEvent).data)
      applySnapshot(data)
    } catch {
      /* ignore malformed frames */
    }
  })
  es.onerror = () => {
    es.close()
    logES = null
  }
}

onMounted(() => {
  connectLogStream()
})

onBeforeUnmount(() => {
  logES?.close()
  logES = null
})
</script>

<template>
  <div class="py-8 sm:py-12 px-4 sm:px-8 max-w-5xl mx-auto">
    <header class="flex items-center justify-between gap-4 mb-6 flex-wrap">
      <div>
        <p class="text-ink-faint dark:text-[#8A8A92] text-sm font-medium uppercase tracking-widest">{{ t('logs_title') }}</p>
      </div>
      <div class="flex items-center gap-2">
        <button class="g-btn-secondary !h-9" @click="exportLog">{{ t('log_export') }}</button>
      </div>
    </header>

    <div v-if="error && !logContent" class="g-card p-8 text-center text-ink-soft dark:text-[#A6A6AD] text-sm">
      {{ error }}
    </div>

    <div v-else class="g-card overflow-hidden">
      <div class="flex items-center justify-between gap-3 px-4 py-2.5 bg-surface dark:bg-[#111115] border-b border-line dark:border-[#2A2A32]">
        <span class="font-mono text-xs text-ink-soft dark:text-[#A6A6AD] truncate">{{ logPath || t('log_no_file') }}</span>
        <button
          @click="stickToBottom = !stickToBottom"
          class="g-btn-ghost text-xs flex-shrink-0"
          :title="stickToBottom ? t('log_pause_scroll') : t('log_resume_scroll')"
        >
          {{ stickToBottom ? t('log_auto_scroll_on') : t('log_auto_scroll_off') }}
        </button>
      </div>
      <pre
        ref="el"
        @scroll="onScroll"
        class="h-[62vh] overflow-auto p-4 bg-[#0f1115] text-[#d6dce4] text-xs leading-5 font-mono whitespace-pre-wrap break-all m-0"
      >{{ logContent || t('log_empty') }}</pre>
    </div>
  </div>
</template>