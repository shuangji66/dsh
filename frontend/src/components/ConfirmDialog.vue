<script setup lang="ts">
import { ref, watch } from 'vue'
import { useBodyScrollLock } from '@/composables/useBodyScrollLock'
import { useI18n } from '@/composables/useI18n'
import DialogCloseButton from '@/components/DialogCloseButton.vue'

const props = defineProps<{
  visible: boolean
  title?: string
  message?: string
  confirmText?: string
  cancelText?: string
  danger?: boolean
  // 确认按钮文字（加载中显示用），非空时进入等待状态
  confirmLoading?: boolean
}>()

const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void
  (e: 'confirm'): void
  (e: 'cancel'): void
}>()

const open = ref(props.visible)
watch(
  () => props.visible,
  (v) => (open.value = v)
)

const { t } = useI18n()

// 弹窗打开期间锁定页面滚动，避免在弹窗背后继续滚动/拖动页面
useBodyScrollLock(() => open.value)

function close() {
  open.value = false
  emit('update:visible', false)
  emit('cancel')
}

function onConfirm() {
  close()
  emit('confirm')
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
        <!-- 遮罩 -->
        <div class="g-modal-mask" @click="close"></div>
        <!-- 弹窗 -->
        <div
          class="relative w-full max-w-sm bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6"
        >
          <!-- 右上角 X：所有弹窗统一（见 DialogCloseButton.vue）；放在标题前是为了让
               键盘 Tab 顺序也是「先关闭、再取消、最后确认」 -->
          <DialogCloseButton :label="t('dialog_close')" :disabled="props.confirmLoading" @close="close" />
          <h3 class="g-dialog-title mb-3">{{ title }}</h3>
          <!-- 自定义内容（默认插槽）优先；否则回退到 message 文本 -->
          <slot>
            <p class="text-sm text-ink-soft dark:text-[#A6A6AD] leading-relaxed mb-6 whitespace-pre-line">{{ message }}</p>
          </slot>
          <div class="g-dialog-actions">
            <button class="g-btn-secondary" :disabled="props.confirmLoading" @click="close">{{ cancelText }}</button>
            <!-- 危险操作（删除/取消下载等）用红字红框，其余一律边框不填色 —— 弹窗按钮统一见 style.css -->
            <button
              :class="danger ? 'g-btn-danger' : 'g-btn-secondary'"
              :disabled="props.confirmLoading"
              @click="onConfirm"
            >{{ confirmText }}</button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>