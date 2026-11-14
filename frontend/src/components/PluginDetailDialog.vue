<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from '@/composables/useI18n'
import { useBodyScrollLock } from '@/composables/useBodyScrollLock'
import { useDialogA11y } from '@/composables/useDialogA11y'
import { usePluginDetail } from '@/composables/usePluginDetail'
import DialogCloseButton from '@/components/DialogCloseButton.vue'
import MarkdownText from '@/components/MarkdownText.vue'

// 插件详情弹窗：只读。文档来源与优先级在后端（locale 的 meta.detail 优先、README 兜底，
// 见 backend/plugin_detail.go）；这里只负责懒加载、展示与「无内容」。
const props = defineProps<{ visible: boolean; name: string }>()

const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void
}>()

const { t } = useI18n()
const open = ref(false)
const { entry, load, reset } = usePluginDetail()

// 打开才请求（列表不预取）；换插件也重新取。关闭时清掉展示条目，缓存仍留在 composable 里。
watch(
  () => [props.visible, props.name] as const,
  ([visible, name]) => {
    open.value = visible
    if (visible && name) void load(name)
    else if (!visible) reset()
  },
  { immediate: true }
)

useBodyScrollLock(() => open.value)

const panel = ref<HTMLElement | null>(null)
useDialogA11y(() => open.value, panel, { onClose: close })

const loading = computed(() => entry.value === null || entry.value.status === 'loading')
const errorText = computed(() => (entry.value?.status === 'error' ? entry.value.error || '' : ''))
const markdown = computed(() =>
  entry.value?.status === 'ready' && entry.value.payload?.status === 'found'
    ? entry.value.payload.markdown || ''
    : ''
)
const empty = computed(() => entry.value?.status === 'ready' && entry.value.payload?.status === 'none')

function close() {
  open.value = false
  emit('update:visible', false)
}
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
      <div v-if="open" class="fixed inset-0 z-50 flex items-center justify-center p-4 backdrop-blur-sm">
        <div class="g-modal-mask" @click="close"></div>
        <div
          ref="panel"
          tabindex="-1"
          role="dialog"
          aria-modal="true"
          :aria-label="name"
          class="relative w-full max-w-lg bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6 outline-none"
        >
          <DialogCloseButton :label="t('dialog_close')" @close="close" />
          <h3 class="g-dialog-title mb-3 font-mono break-all">{{ name }}</h3>

          <div v-if="loading" class="flex items-center justify-center gap-2 py-10 text-sm text-ink-faint dark:text-[#8A8A92]">
            <span class="w-4 h-4 rounded-full border-2 border-line border-t-brand animate-spin"></span>
            {{ t('plugin_loading') }}
          </div>

          <!-- 失败：诊断文本可能带后端原文，允许拖选复制 -->
          <div
            v-else-if="errorText"
            class="rounded-lg px-3 py-2 text-xs leading-relaxed break-words whitespace-pre-wrap select-text bg-danger/10 dark:bg-[#EF4444]/10 border border-danger/30 dark:border-[#EF4444]/30 text-danger-ink"
          >
            {{ errorText }}
          </div>

          <div v-else-if="empty" class="py-10 text-center text-sm text-ink-faint dark:text-[#8A8A92]">
            {{ t('plugin_detail_empty') }}
          </div>

          <!-- 正文：长文档在弹窗内滚动；正文常要复制，允许拖选。
               `pr-3` 是给滚动条留的槽：Chromium 的覆盖式滚动条画在内容区右缘上，
               没有这段内边距时正文会直接压到滚动条下面（字被遮住）。 -->
          <div
            v-else
            class="max-h-[55vh] overflow-y-auto pr-3 select-text text-sm text-ink-soft dark:text-[#A6A6AD] leading-relaxed"
          >
            <MarkdownText :source="markdown" />
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
