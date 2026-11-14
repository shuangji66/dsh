<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from '@/composables/useI18n'
import { useBodyScrollLock } from '@/composables/useBodyScrollLock'
import { useDialogA11y } from '@/composables/useDialogA11y'
import DialogCloseButton from '@/components/DialogCloseButton.vue'

// 安装插件弹窗：只负责「输入规格 + 显示进度/错误」，安装本身由 PluginsView 发起并轮询
// （后端是异步任务，关掉弹窗也不会中断安装 —— 见 plugin_install.go）。
const props = defineProps<{
  visible: boolean
  // 后台任务进行中：输入框锁住，底部按钮变成「关闭」
  installing: boolean
  // 进度/状态文案（后端 message + messageRef 已经由视图本地化）
  progress?: string
  // 失败文案（后端错误已本地化，可能带 pnpm 诊断尾行）
  errorText?: string
}>()

const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void
  (e: 'submit', spec: string): void
}>()

const { t } = useI18n()

const open = ref(props.visible)
const spec = ref('')
const input = ref<HTMLInputElement | null>(null)

watch(
  () => props.visible,
  async (v) => {
    open.value = v
    if (!v) return
    spec.value = ''
    await nextTick()
    if (!props.installing) input.value?.focus()
  }
)

useBodyScrollLock(() => open.value)

const panel = ref<HTMLElement | null>(null)
useDialogA11y(() => open.value, panel, { onClose: close })

const canSubmit = computed(() => !props.installing && spec.value.trim() !== '')

function close() {
  open.value = false
  emit('update:visible', false)
}

function submit() {
  if (!canSubmit.value) return
  emit('submit', spec.value.trim())
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
          :aria-label="t('plugin_install_title')"
          class="relative w-full max-w-md bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6 outline-none"
        >
          <DialogCloseButton :label="t('dialog_close')" @close="close" />
          <h3 class="g-dialog-title mb-3">{{ t('plugin_install_title') }}</h3>

          <label for="plugin-install-spec" class="block text-xs text-ink-faint dark:text-[#8A8A92] mb-1.5">
            {{ t('plugin_install_label') }}
          </label>
          <input
            id="plugin-install-spec"
            ref="input"
            v-model="spec"
            type="text"
            class="g-input font-mono"
            :placeholder="t('plugin_install_placeholder')"
            :disabled="installing"
            autocomplete="off"
            spellcheck="false"
            @keyup.enter="submit"
          />

          <!-- 支持的写法：示例是代码（不可翻译），小标题走 i18n；整块可选中方便复制 -->
          <div class="mt-3 rounded-lg border border-line dark:border-[#2A2A32] bg-black/[0.02] dark:bg-white/[0.03] px-3 py-2.5 select-text">
            <p class="text-xs text-ink-faint dark:text-[#8A8A92] mb-1.5">{{ t('plugin_install_forms') }}</p>
            <ul class="space-y-1 font-mono text-xs text-ink-soft dark:text-[#A6A6AD] break-all">
              <li>dsh-better-sidebar</li>
              <li>dsh-better-sidebar@0.32.1</li>
              <li>github:owner/repo</li>
              <li>https://github.com/owner/repo</li>
              <li>/vol1/1000/plugins/my-plugin</li>
              <li>dsh plugin add dsh-better-sidebar</li>
            </ul>
          </div>

          <!-- 进行中：安装是后台任务，关掉弹窗不会中断 -->
          <div v-if="installing" class="mt-4 rounded-lg border border-line dark:border-[#2A2A32] px-3 py-2.5">
            <div class="flex items-center gap-2.5">
              <span class="w-4 h-4 flex-shrink-0 rounded-full border-2 border-brand border-t-transparent animate-spin"></span>
              <span class="text-sm text-ink-soft dark:text-[#A6A6AD] break-all">
                {{ progress || t('plugin_install_running') }}
              </span>
            </div>
            <p class="mt-2 text-xs text-ink-faint dark:text-[#8A8A92] leading-relaxed">
              {{ t('plugin_install_background') }}
            </p>
          </div>

          <!-- 失败：错误里常带 pnpm 的英文诊断尾行，允许拖选复制 -->
          <div
            v-if="errorText"
            class="mt-4 rounded-lg px-3 py-2 text-xs leading-relaxed break-words whitespace-pre-wrap select-text bg-danger/10 dark:bg-[#EF4444]/10 border border-danger/30 dark:border-[#EF4444]/30 text-danger-ink"
          >
            {{ errorText }}
          </div>

          <div class="g-dialog-actions">
            <button class="g-btn-secondary" @click="close">
              {{ installing ? t('plugin_install_close') : t('confirm_cancel') }}
            </button>
            <button class="g-btn-secondary" :disabled="!canSubmit" @click="submit">
              {{ installing ? t('plugin_install_running') : t('plugin_install_submit') }}
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
