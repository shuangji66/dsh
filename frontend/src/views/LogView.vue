<script setup lang="ts">
import { computed, ref, onMounted, nextTick } from 'vue'
import { useToastStore } from '@/stores/toast'
import { sseUrl } from '@/serverapi'
import { useI18n } from '@/composables/useI18n'
import { useEventStream } from '@/composables/useEventStream'

const toast = useToastStore()
const { t } = useI18n()
const logContent = ref('')
const logPath = ref('')
const loading = ref(true)
const error = ref('')
const stickToBottom = ref(true) // 是否跟随底部自动滚动

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
const MAX_LINES = 2000

// 日志行着色。行格式与后端 logging.go 的约定一致：
//   - `[Harness] <时间> [LEVEL] message` → INFO 白 / WARN 黄 / ERROR 红；
//   - 其余行（没有 [Harness] 前缀）是 dsh 子进程的原样输出 → 固定黄色，
//     内容不做任何加工（后端也是原样透传的）。
const HARNESS_LINE = /^\[Harness\] \d{4}\/\d{2}\/\d{2} \d{2}:\d{2}:\d{2} \[(INFO|WARN|ERROR)\]/
const LEVEL_CLASS: Record<string, string> = {
  INFO: 'text-[#d6dce4]',
  WARN: 'text-[#fbbf24]',
  ERROR: 'text-[#f87171]'
}
const DSH_CLASS = 'text-[#facc15]'

const logLines = computed(() => {
  let c = logContent.value
  let truncated = false
  if (c.length > MAX_LEN) {
    c = c.slice(-MAX_LEN)
    // 从行首开始，避免首行被截成半截（会被误判成 dsh 输出）
    const nl = c.indexOf('\n')
    if (nl >= 0) c = c.slice(nl + 1)
    truncated = true
  }
  let ls = c.split('\n')
  if (ls.length > MAX_LINES) {
    ls = ls.slice(-MAX_LINES)
    truncated = true
  }
  return {
    truncated,
    lines: ls.map((text) => {
      if (!text) return { text, cls: '' }
      const m = HARNESS_LINE.exec(text)
      return { text, cls: m ? LEVEL_CLASS[m[1]] ?? LEVEL_CLASS.INFO : DSH_CLASS }
    })
  }
})

function applySnapshot(snap: { path?: string; content?: string; exists?: boolean }) {
  if (snap.path !== undefined) logPath.value = snap.path
  const content = snap.content || ''
  const box = el.value
  // 记录新内容到达前是否处于底部
  const wasAtBottom = !box || box.scrollHeight - box.scrollTop - box.clientHeight < 40
  if (content !== logContent.value) {
    logContent.value = content
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

// 通过 SSE 监听后端主动推送，替换 2 秒轮询。
// 用 useEventStream：日志内容长时间不变时后端只发心跳，若连接被反代掐断，
// 重连后会立刻补发一份快照，日志页不会卡在旧内容上不再刷新。
const logStream = useEventStream(() => sseUrl('/api/logs/stream'), {
  log: (data) => applySnapshot(data as { path?: string; content?: string; exists?: boolean })
})

onMounted(() => {
  logStream.start()
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
      <!-- 逐行着色：等级见 logLines；dsh 子进程输出固定黄色 -->
      <pre
        ref="el"
        @scroll="onScroll"
        class="h-[62vh] overflow-auto p-4 bg-[#0f1115] text-[#d6dce4] text-xs leading-5 font-mono whitespace-pre-wrap break-all m-0"
      ><span v-if="!logLines.lines.length">{{ t('log_empty') }}</span><template v-else><span v-if="logLines.truncated" class="block text-[#fbbf24]">{{ t('log_truncated') }}</span><span v-for="(l, i) in logLines.lines" :key="i" :class="[l.cls, 'block']">{{ l.text }}</span></template></pre>
    </div>
  </div>
</template>