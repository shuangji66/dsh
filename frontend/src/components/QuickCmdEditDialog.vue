<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from '@/composables/useI18n'
import type { QuickCmd } from '@/serverapi'

const props = defineProps<{
  visible: boolean
  cmd: QuickCmd | null
}>()

const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void
  (e: 'save', payload: { name: string; content: string; auto: boolean }): void
}>()

const { t } = useI18n()

const open = ref(props.visible)
watch(
  () => props.visible,
  (v) => {
    open.value = v
    if (v) reset()
  }
)

// 表单状态：编辑时以传入的 cmd 初始化，新增时清空
const name = ref('')
const content = ref('')
const auto = ref(false)
const nameError = ref('')
const contentError = ref('')

function reset() {
  name.value = props.cmd?.name ?? ''
  content.value = props.cmd?.content ?? ''
  auto.value = props.cmd?.auto ?? false
  nameError.value = ''
  contentError.value = ''
}

function close() {
  open.value = false
  emit('update:visible', false)
}

function onSave() {
  nameError.value = name.value.trim() ? '' : t('qc_name_required')
  contentError.value = content.value.trim() ? '' : t('qc_content_required')
  if (nameError.value || contentError.value) return
  emit('save', {
    name: name.value.trim(),
    content: content.value.trim(),
    auto: auto.value
  })
  close()
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
        <div class="absolute inset-0 bg-black/50" @click="close"></div>
        <div
          class="relative w-full max-w-md bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6"
        >
          <h3 class="font-display text-lg font-semibold text-ink dark:text-white mb-4">
            {{ props.cmd ? t('qc_edit_title') : t('qc_add_title') }}
          </h3>

          <label class="block mb-1 text-sm font-medium text-ink dark:text-white">{{ t('qc_name') }}</label>
          <input
            v-model="name"
            type="text"
            class="g-input mb-1"
            :placeholder="t('qc_name_placeholder')"
            maxlength="120"
          />
          <p v-if="nameError" class="text-xs text-[#EF4444] mb-2">{{ nameError }}</p>

          <label class="block mt-3 mb-1 text-sm font-medium text-ink dark:text-white">{{ t('qc_content') }}</label>
          <textarea
            v-model="content"
            rows="3"
            class="g-input mb-1 resize-y font-mono"
            :placeholder="t('qc_content_placeholder')"
          ></textarea>
          <p v-if="contentError" class="text-xs text-[#EF4444] mb-2">{{ contentError }}</p>

          <label class="flex items-start gap-3 mt-3 cursor-pointer select-none group">
            <input v-model="auto" type="checkbox" class="peer sr-only" />
            <span
              class="relative w-5 h-5 mt-0.5 rounded-full shrink-0 border-2 border-line dark:border-[#3A3A42] bg-white dark:bg-[#1F1F26] transition-colors peer-checked:bg-brand peer-checked:border-brand"
            >
              <svg
                v-if="auto"
                xmlns="http://www.w3.org/2000/svg"
                viewBox="0 0 24 24"
                class="absolute inset-0 w-full h-full text-white p-1"
                fill="none"
                stroke="currentColor"
                stroke-width="3"
                stroke-linecap="round"
                stroke-linejoin="round"
              ><path d="M20 6L9 17l-5-5"/></svg>
            </span>
            <span class="min-w-0">
              <span class="block text-sm font-medium text-ink dark:text-white">{{ t('qc_auto') }}</span>
              <span class="block text-xs text-ink-soft dark:text-[#A6A6AD] mt-0.5">{{ t('qc_auto_hint') }}</span>
            </span>
          </label>

          <div class="flex justify-end gap-3 mt-6">
            <button class="g-btn-secondary" @click="close">{{ t('qc_cancel') }}</button>
            <button class="g-btn-primary" @click="onSave">{{ t('qc_save') }}</button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>