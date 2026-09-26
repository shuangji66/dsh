<script setup lang="ts">
import { computed, ref, onMounted, onBeforeUnmount, nextTick, watch } from 'vue'
import { useToastStore } from '@/stores/toast'
import { sseUrl } from '@/serverapi'
import { useI18n } from '@/composables/useI18n'
import { useEventStream } from '@/composables/useEventStream'
import PageHeader from '@/components/PageHeader.vue'
import { icons } from '@/utils/icons'

const toast = useToastStore()
const { t } = useI18n()
const logContent = ref('')
const logPath = ref('')
const loading = ref(true)
// 是否跟随新内容自动滚到底。**唯一判据**：由「自动滚动」按钮切换，也由用户主动上翻/回到底部维护
// （见 onScroll）。不能再由「内容到达时的滚动位置」推断 —— 那样按钮对行为毫无影响。
const stickToBottom = ref(true)
// 程序性滚动到尾部时置位：这次 scroll 事件是我们自己造成的，不能拿它当成「用户回到底部」
// 而恢复跟随（否则用户在内容到达与滚动之间主动上翻的暂停会被立刻冲掉）。
let programmaticScroll = false
let programmaticScrollTimer: ReturnType<typeof setTimeout> | null = null
// 拉取失败提示（区分「没有日志」与「拉不到日志」）。SSE 从未连上、也没收到过任何快照时才有值，
// 已经在显示旧内容的情况下断线不报错（useEventStream 会自动重连，回来后会补发快照）。
const streamDown = ref(false)
const gotSnapshot = ref(false)
const error = computed(() => (streamDown.value && !gotSnapshot.value ? t('log_stream_failed') : ''))

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
//   - `[Harness] <时间> [LEVEL] message` → INFO 用日志区域的默认前景色 / WARN 黄 / ERROR 红；
//   - 其余行（没有 [Harness] 前缀）是 dsh 子进程的原样输出 → 固定黄色，
//     内容不做任何加工（后端也是原样透传的）。
// 具体颜色写在 style.css 的 .log-* 里（浅色主题用深一档的语义色），这样底色与行色
// 都跟随主题；这里只给类名，不再写死十六进制颜色。
const HARNESS_LINE = /^\[Harness\] \d{4}\/\d{2}\/\d{2} \d{2}:\d{2}:\d{2} \[(INFO|WARN|ERROR)\]/
const LEVEL_CLASS: Record<string, string> = {
  INFO: '', // 继承 .log-viewer 的主题前景色
  WARN: 'log-line-warn',
  ERROR: 'log-line-error'
}
const DSH_CLASS = 'log-line-dsh'

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
  gotSnapshot.value = true
  const content = snap.content || ''
  if (content !== logContent.value) {
    logContent.value = content
    // 只有「自动滚动」处于开启状态才跟随；用户暂停后即使新内容到达也保持当前视口
    if (stickToBottom.value) {
      nextTick().then(scrollToBottom)
    }
  }
  loading.value = false
}

// 程序性滚到底：先置标记，让随之而来的 scroll 事件不被误判成用户操作。
// 兜底定时器处理「本来就在底部、scrollTop 没变、浏览器不发 scroll 事件」的情况，
// 避免标记悬挂把之后一次真实的用户滚动吞掉。
function scrollToBottom() {
  const box = el.value
  if (!box) return
  programmaticScroll = true
  box.scrollTop = box.scrollHeight
  if (programmaticScrollTimer) clearTimeout(programmaticScrollTimer)
  programmaticScrollTimer = setTimeout(() => {
    programmaticScroll = false
    programmaticScrollTimer = null
  }, 200)
}

function onScroll() {
  const box = el.value
  if (!box) return
  const atBottom = box.scrollHeight - box.scrollTop - box.clientHeight < 40
  if (programmaticScroll) {
    // 程序滚动命中的就是底部：消费掉这次事件，不改跟随状态
    if (atBottom) {
      programmaticScroll = false
      return
    }
    // 目标不是底部 → 这是一次真实（被排在我们之后）的用户滚动，按用户操作处理
    programmaticScroll = false
  }
  // 用户主动上翻查看历史时暂停自动滚动；回到底部附近时恢复
  stickToBottom.value = atBottom
}

// 「自动滚动」按钮：立即生效 —— 切到开启时先滚到底，切到关闭时停止跟随
function toggleStickToBottom() {
  stickToBottom.value = !stickToBottom.value
  if (stickToBottom.value) scrollToBottom()
}

// 通过 SSE 监听后端主动推送，替换 2 秒轮询。
// 用 useEventStream：日志内容长时间不变时后端只发心跳，若连接被反代掐断，
// 重连后会立刻补发一份快照，日志页不会卡在旧内容上不再刷新。
const logStream = useEventStream(() => sseUrl('/api/logs/stream'), {
  log: (data) => applySnapshot(data as { path?: string; content?: string; exists?: boolean })
})

// 首帧宽限：EventSource 首次连接需要一点时间，不能一挂载就报「拉不到日志」。
// 宽限内既没连上、也一份快照都没收到 → 判定为「拉不到日志」，页面给可读提示
// （此前 error 永远是空串，该分支不可达，连不上时页面只显示「暂无日志内容」）。
const STREAM_DOWN_GRACE_MS = 8000
let streamDownTimer: ReturnType<typeof setTimeout> | null = null

function armStreamDownCheck() {
  if (streamDownTimer) return
  streamDownTimer = setTimeout(() => {
    streamDownTimer = null
    if (!logStream.connected.value && !gotSnapshot.value) streamDown.value = true
  }, STREAM_DOWN_GRACE_MS)
}

// 连上即清除失败提示（重连成功会立刻收到一份快照）；断开则重新开始宽限计时
watch(
  () => logStream.connected.value,
  (up) => {
    if (up) streamDown.value = false
    else armStreamDownCheck()
  }
)

onMounted(() => {
  logStream.start()
  armStreamDownCheck()
})

onBeforeUnmount(() => {
  if (streamDownTimer) clearTimeout(streamDownTimer)
  if (programmaticScrollTimer) clearTimeout(programmaticScrollTimer)
})
</script>

<template>
  <!-- 整页铺满：标题栏与日志卡片各占一段，日志卡片 flex-1 撑满剩余高度（内部 pre 自己滚动）。
       高度式子与终端页一致：移动端扣掉底部导航占用高度（含 iPhone 安全区），桌面端占满视口；
       与 main 的 padding-bottom 相加正好等于一个视口，因此本页自身不产生滚动条。
       标题栏的上下留白与终端页保持一致（各子页面的标题栏因此贴在同一高度上）：
       顶部 pt-3/sm:pt-4（12/16px，同终端页外壳的 pt-3/sm:pt-4）、标题栏下方 gap-3（12px，
       同终端页的 gap-3；标题栏本身各页都是同一张 min-h-14 卡片）；底部维持紧凑的
       pb-4/sm:pb-6，把高度尽量留给日志。横向内缩与限宽 `px-4 sm:px-8 max-w-6xl mx-auto`
       同样与终端页及其他子页面一致（本页的滚动条因此不贴窗口边）。 -->
  <div class="log-page flex flex-col gap-3 px-4 sm:px-8 pt-3 sm:pt-4 pb-4 sm:pb-6 max-w-6xl mx-auto w-full h-[calc(100dvh_-_var(--bottom-nav-h))] md:h-[100dvh]">
    <!-- 页头（图标 + 标题 + 导出日志；标题栏按钮统一样式：小一号字号 + 细边框 + 不填充底色） -->
    <PageHeader :title="t('logs_title')" :icon="icons.logs">
      <button class="g-btn-secondary h-8 px-3 text-xs" @click="exportLog">{{ t('log_export') }}</button>
    </PageHeader>

    <div v-if="error && !logContent" class="g-card p-8 text-center text-ink-soft dark:text-[#A6A6AD] text-sm">
      {{ error }}
    </div>

    <div v-else class="g-card flex-1 min-h-0 flex flex-col overflow-hidden">
      <div class="flex items-center justify-between gap-3 px-4 py-2.5 bg-surface dark:bg-[#111115] border-b border-line dark:border-[#2A2A32] shrink-0">
        <span class="font-mono text-xs text-ink-soft dark:text-[#A6A6AD] truncate">{{ logPath || t('log_no_file') }}</span>
        <button
          @click="toggleStickToBottom()"
          class="g-btn-ghost text-xs flex-shrink-0"
          :title="stickToBottom ? t('log_pause_scroll') : t('log_resume_scroll')"
        >
          {{ stickToBottom ? t('log_auto_scroll_on') : t('log_auto_scroll_off') }}
        </button>
      </div>
      <!-- 日志内容（只有这一块允许拖选复制；上面的日志路径与「自动滚动」状态刻意不放）。
           逐行着色：等级见 logLines；dsh 子进程输出固定黄色。
           底色与行色都由 style.css 的 .log-viewer / .log-line-* 提供，跟随主题
           （浅色主题浅底深字，暗色主题深底浅字）；截断提示沿用 WARN 黄。 -->
      <pre
        ref="el"
        @scroll="onScroll"
        class="log-viewer select-text flex-1 min-h-0 overflow-auto p-4 text-xs leading-5 font-mono whitespace-pre-wrap break-all m-0"
      ><span v-if="!logLines.lines.length">{{ t('log_empty') }}</span><template v-else><span v-if="logLines.truncated" class="block log-line-warn">{{ t('log_truncated') }}</span><span v-for="(l, i) in logLines.lines" :key="i" :class="[l.cls, 'block']">{{ l.text }}</span></template></pre>
    </div>
  </div>
</template>