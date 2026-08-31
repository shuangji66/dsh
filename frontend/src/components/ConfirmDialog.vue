<script setup lang="ts">
import { ref, watch } from 'vue'

const props = defineProps<{
  visible: boolean
  title?: string
  message?: string
  confirmText?: string
  cancelText?: string
  danger?: boolean
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
      <div v-if="open" class="fixed inset-0 z-50 flex items-center justify-center p-4">
        <!-- 遮罩 -->
        <div class="absolute inset-0 bg-black/50" @click="close"></div>
        <!-- 弹窗 -->
        <div
          class="relative w-full max-w-sm bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6"
        >
          <h3 class="font-display text-lg font-semibold text-ink dark:text-white mb-3">{{ title }}</h3>
          <p class="text-sm text-ink-soft dark:text-[#A6A6AD] leading-relaxed mb-6 whitespace-pre-line">{{ message }}</p>
          <div class="flex justify-end gap-3">
            <button class="g-btn-secondary" @click="close">{{ cancelText }}</button>
            <button
              class="g-btn-primary"
              :class="danger ? '!bg-danger hover:!bg-danger/90' : ''"
              @click="onConfirm"
            >{{ confirmText }}</button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>