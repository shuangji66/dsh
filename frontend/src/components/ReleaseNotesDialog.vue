<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from '@/composables/useI18n'
import { useBodyScrollLock } from '@/composables/useBodyScrollLock'
import DialogCloseButton from '@/components/DialogCloseButton.vue'

// ReleaseNotesDialog.vue —— 「某个版本的更新日志」弹窗（dsh 版本列表 / 插件市场共用）。
//
// 结构刻意极简（用户要求）：只有右上角的关闭按钮 + 标题（版本号）+ 正文，**没有任何底部
// 操作行** —— 它只是一个阅读面板，没有可执行的动作。因此标题行用普通的 .g-dialog-title
// 即可（不该加 `!pr-0` 那种给图标让位的写法：这里标题右侧没有任何图标）。
//
// 内容取舍（三条都由后端给的字段决定，见 serverapi 的 ReleaseNotesPayload）：
//   - loading：正在拉取 → 转圈 + 「正在获取」；
//   - available=false：**这次没取到**（断网 / 限流）→ 「暂时取不到」+ 前端自己的错误文本。
//     这跟「该版本没有更新日志」不是一回事，别合并成一句话；
//   - available=true 且正文为空：拉到了、但该版本确实没有 release 正文；
//   - 否则按**界面语言**选正文：中文取 note，英文取 noteEn；noteEn 为空时回退 note
//     （上游有些版本的 release 只有中文段落，没有英文分栏）。
//
// 为什么语言切换不额外拉数据：后端一次就把中英两份都给了，切换语言只是换用哪个字段，
// 不会发请求、也不会让正文先空一下再出现。
const props = defineProps<{
  visible: boolean
  // 目标版本号（标题里展示）
  version: string
  loading?: boolean
  // 后端 available 字段：这次有没有取到
  available?: boolean
  // 中文正文 / 英文正文
  note?: string
  noteEn?: string
  // 拉取失败时的错误文案（已由调用方 uiErrText 处理过）
  errorText?: string
}>()

const emit = defineEmits<{ (e: 'close'): void }>()

const { t, locale } = useI18n()

// 弹窗打开期间锁背景滚动（与其它弹窗一致）。
useBodyScrollLock(() => props.visible)

// 按界面语言选正文：英文缺段落时回退中文。
const body = computed(() => {
  if (locale.value === 'en') return (props.noteEn || '').trim() || (props.note || '').trim()
  return (props.note || '').trim() || (props.noteEn || '').trim()
})
const hasBody = computed(() => body.value !== '')
</script>

<template>
  <Teleport to="body">
    <Transition
      enter-active-class="transition duration-200 ease-out"
      enter-from-class="opacity-0"
      enter-to-class="opacity-100"
      leave-active-class="transition duration-150 ease-in"
      leave-from-class="opacity-100"
      leave-to-class="opacity-0"
    >
      <div v-if="visible" class="fixed inset-0 z-[60] flex items-center justify-center p-4 backdrop-blur-sm">
        <div class="g-modal-mask" @click="emit('close')"></div>
        <div class="relative w-full max-w-lg bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6">
          <DialogCloseButton :label="t('dialog_close')" @close="emit('close')" />
          <!-- 标题行只放「更新日志 + 版本号」：右侧留出 X 的位置由 .g-dialog-title 自带的 pr-11 负责 -->
          <h3 class="g-dialog-title mb-3">
            {{ t('release_notes_title') }}
            <span class="ml-1 font-mono">{{ version }}</span>
          </h3>

          <!-- 拉取中 -->
          <div v-if="loading" class="py-6 flex items-center justify-center gap-3">
            <span class="inline-block animate-spin h-5 w-5 border-2 border-brand border-t-transparent rounded-full"></span>
            <span class="text-sm text-ink-soft dark:text-[#A6A6AD]">{{ t('release_notes_loading') }}</span>
          </div>

          <!-- 这次没取到（断网 / 限流）：可关掉弹窗重试 -->
          <div
            v-else-if="available === false"
            class="rounded-lg bg-black/5 dark:bg-white/5 border border-line dark:border-[#2A2A32] px-3 py-2 text-sm text-ink-soft dark:text-[#A6A6AD] leading-relaxed select-text"
          >{{ errorText || t('release_notes_unavailable') }}</div>

          <!-- 取到了但该版本没有日志正文 -->
          <div
            v-else-if="!hasBody"
            class="rounded-lg bg-black/5 dark:bg-white/5 border border-line dark:border-[#2A2A32] px-3 py-2 text-sm text-ink-soft dark:text-[#A6A6AD] leading-relaxed select-text"
          >{{ t('release_notes_empty') }}</div>

          <!-- 正文：纯文本（后端已把 release 正文里的 HTML 压平），保留换行；超长滚动、不撑破弹窗。
               可选中复制（更新日志常要拿去搜/贴）——全局 user-select:none，这里显式放开。 -->
          <div
            v-else
            class="rounded-lg bg-black/5 dark:bg-white/5 border border-line dark:border-[#2A2A32] px-3 py-2 text-sm text-ink-soft dark:text-[#A6A6AD] leading-relaxed whitespace-pre-wrap break-words max-h-[60vh] overflow-y-auto select-text"
          >{{ body }}</div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
