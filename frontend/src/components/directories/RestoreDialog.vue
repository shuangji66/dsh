<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { api, type DshDataBackup } from '@/serverapi'
import { uiErrText, uiText, useI18n } from '@/composables/useI18n'
import { useToastStore } from '@/stores/toast'
import { useBodyScrollLock } from '@/composables/useBodyScrollLock'
import { useDialogA11y } from '@/composables/useDialogA11y'
import DialogCloseButton from '@/components/DialogCloseButton.vue'
import { formatBytes, formatDateTime } from '@/utils/format'

// 恢复 dsh 数据备份：选择 → 二次确认 → 轮询恢复状态 → 成功后整页刷新。
// 三个弹窗（选择 / 确认 / 删除确认）放在同一个组件里，叠加时的焦点与滚动锁
// 由 useDialogA11y / useBodyScrollLock 的引用计数统一处理。
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ (e: 'update:visible', v: boolean): void }>()

const { t } = useI18n()
const toast = useToastStore()

const restoreBusy = ref(false)
const restoreError = ref('')
const dshBackups = ref<DshDataBackup[]>([])
const selectedRestore = ref<string | null>(null)
const confirmRestoreVisible = ref(false)
const restoreDeleteName = ref<string | null>(null)
let restorePollTimer: ReturnType<typeof setInterval> | null = null

useBodyScrollLock(
  () => props.visible || confirmRestoreVisible.value || restoreDeleteName.value !== null
)

const restorePanel = ref<HTMLElement | null>(null)
const confirmRestorePanel = ref<HTMLElement | null>(null)
const deleteBackupPanel = ref<HTMLElement | null>(null)

function close() {
  if (restoreBusy.value) return
  emit('update:visible', false)
}

useDialogA11y(() => props.visible, restorePanel, { onClose: close })
useDialogA11y(() => confirmRestoreVisible.value, confirmRestorePanel, {
  onClose: () => (confirmRestoreVisible.value = false)
})
useDialogA11y(() => restoreDeleteName.value !== null, deleteBackupPanel, {
  onClose: () => (restoreDeleteName.value = null)
})

// 拉取 dsh 数据备份列表
async function fetchDshBackups() {
  try {
    const res = await api.listDshDataBackups()
    dshBackups.value = res.backups || []
  } catch {
    /* 列表拉取失败保持空列表；用户可关闭重开重试 */
  }
}

watch(
  () => props.visible,
  (visible) => {
    if (!visible) return
    restoreError.value = ''
    restoreBusy.value = false
    selectedRestore.value = null
    confirmRestoreVisible.value = false
    restoreDeleteName.value = null
    void fetchDshBackups()
  },
  { immediate: true }
)

function selectRestore(name: string) {
  selectedRestore.value = name
}

// 二次确认恢复
function openConfirmRestore() {
  if (!selectedRestore.value) return
  confirmRestoreVisible.value = true
}

// 执行恢复
async function doRestore() {
  const name = selectedRestore.value
  if (!name) return
  confirmRestoreVisible.value = false
  restoreBusy.value = true
  restoreError.value = ''
  try {
    await api.dshDataRestore(name)
    // 轮询恢复状态直到完成
    if (restorePollTimer) clearInterval(restorePollTimer)
    restorePollTimer = setInterval(async () => {
      try {
        const res = await api.dshDataRestoreStatus()
        const st = res.status
        if (st.done) {
          clearInterval(restorePollTimer!)
          restorePollTimer = null
          restoreBusy.value = false
          if (st.ok) {
            toast.show(t('restore_success'), 'success')
            emit('update:visible', false)
            // 恢复数据后刷新页面（dsh 已重启，加载新配置）
            setTimeout(() => window.location.reload(), 1000)
          } else {
            restoreError.value = uiText(st.error, st.errorRef, t('restore_failed'))
          }
        }
      } catch {
        /* 单次失败继续轮询 */
      }
    }, 2000)
  } catch (e) {
    restoreError.value = uiErrText(e, t('restore_failed'))
    restoreBusy.value = false
  }
}

function openRestoreDelete(name: string) {
  restoreDeleteName.value = name
}

async function doRestoreDelete() {
  const name = restoreDeleteName.value
  if (!name) return
  try {
    await api.deleteDshDataBackup(name)
    toast.show(t('restore_deleted'), 'success')
    restoreDeleteName.value = null
    void fetchDshBackups()
  } catch (e) {
    toast.show(uiErrText(e, t('common_op_failed')), 'error')
  }
}

onBeforeUnmount(() => {
  if (restorePollTimer) clearInterval(restorePollTimer)
  restorePollTimer = null
})
</script>

<template>
  <!-- 恢复备份选择弹窗 -->
  <Teleport to="body">
    <Transition
      enter-active-class="transition duration-200 ease-out"
      enter-from-class="opacity-0"
      enter-to-class="opacity-100"
      leave-active-class="transition duration-150 ease-in"
      leave-from-class="opacity-100"
      leave-to-class="opacity-0"
    >
      <div v-if="props.visible" class="fixed inset-0 z-50 flex items-center justify-center p-4 backdrop-blur-sm">
        <div class="g-modal-mask" @click="close"></div>
        <div
          ref="restorePanel"
          tabindex="-1"
          role="dialog"
          aria-modal="true"
          :aria-label="t('confirm_restore_title')"
          class="relative w-full max-w-lg bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6 outline-none"
        >
          <DialogCloseButton :label="t('dialog_close')" :disabled="restoreBusy" @close="close" />
          <h3 class="g-dialog-title mb-2">{{ t('confirm_restore_title') }}</h3>

          <div v-if="restoreBusy" class="py-8 text-center">
            <div class="inline-block animate-spin h-6 w-6 border-2 border-danger border-t-transparent rounded-full mb-2"></div>
            <div class="text-sm text-ink-soft dark:text-[#A6A6AD]">{{ t('restore_running') }}</div>
          </div>

          <template v-else>
            <div v-if="restoreError" class="mb-3 rounded-lg bg-danger/10 dark:bg-[#EF4444]/10 border border-danger/30 dark:border-[#EF4444]/30 px-3 py-2 text-xs text-danger-ink break-words">{{ restoreError }}</div>

            <div v-if="dshBackups.length === 0" class="py-6 text-center text-sm text-ink-faint dark:text-[#8A8A92]">
              {{ t('restore_empty') }}
            </div>

            <div v-else class="border border-[#E8E8EC] dark:border-[#2A2A32] rounded-lg divide-y divide-[#E8E8EC] dark:divide-[#2A2A32] max-h-64 overflow-auto">
              <label
                v-for="b in dshBackups"
                :key="b.name"
                class="flex items-center gap-3 px-3 py-2 cursor-pointer hover:bg-black/5 dark:hover:bg-white/5 transition-colors"
              >
                <input
                  type="radio"
                  name="dshrestore"
                  :value="b.name"
                  :checked="selectedRestore === b.name"
                  class="accent-danger flex-shrink-0"
                  @change="selectRestore(b.name)"
                />
                <div class="flex-1 min-w-0">
                  <div class="text-sm font-mono text-ink dark:text-[#EDEDF0] truncate">{{ b.name }}</div>
                  <div class="text-xs text-ink-faint dark:text-[#8A8A92]">
                    {{ t('restore_date') }}: {{ formatDateTime(b.modified) }} · {{ t('restore_size') }}: {{ formatBytes(b.size) }}
                  </div>
                </div>
                <button
                  type="button"
                  class="flex-shrink-0 text-ink-soft dark:text-[#A6A6AD] hover:text-danger-ink transition-colors"
                  :title="t('restore_delete')"
                  @click.prevent="openRestoreDelete(b.name)"
                >
                  <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
                </button>
              </label>
            </div>
          </template>

          <div class="g-dialog-actions">
            <button class="g-btn-secondary" :disabled="restoreBusy" @click="close">{{ t('confirm_cancel') }}</button>
            <button
              v-if="selectedRestore && !restoreBusy"
              class="g-btn-danger"
              @click="openConfirmRestore"
            >{{ t('restore_to') }}</button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>

  <!-- 恢复备份二次确认弹窗 -->
  <Teleport to="body">
    <Transition
      enter-active-class="transition duration-200 ease-out"
      enter-from-class="opacity-0"
      enter-to-class="opacity-100"
      leave-active-class="transition duration-150 ease-in"
      leave-from-class="opacity-100"
      leave-to-class="opacity-0"
    >
      <div v-if="confirmRestoreVisible" class="fixed inset-0 z-[60] flex items-center justify-center p-4 backdrop-blur-sm">
        <div class="g-modal-mask" @click="confirmRestoreVisible = false"></div>
        <div
          ref="confirmRestorePanel"
          tabindex="-1"
          role="dialog"
          aria-modal="true"
          :aria-label="t('confirm_restore_title')"
          class="relative w-full max-w-sm bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6 outline-none"
        >
          <DialogCloseButton :label="t('dialog_close')" @close="confirmRestoreVisible = false" />
          <h3 class="g-dialog-title mb-3">{{ t('confirm_restore_title') }}</h3>
          <p class="text-sm text-ink-soft dark:text-[#A6A6AD] leading-relaxed mb-6 whitespace-pre-line">{{ t('confirm_restore_msg', { name: selectedRestore || '' }) }}</p>
          <div class="g-dialog-actions">
            <button class="g-btn-secondary" @click="confirmRestoreVisible = false">{{ t('confirm_cancel') }}</button>
            <button class="g-btn-danger" @click="doRestore">{{ t('confirm_ok') }}</button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>

  <!-- 删除备份确认弹窗 -->
  <Teleport to="body">
    <Transition
      enter-active-class="transition duration-200 ease-out"
      enter-from-class="opacity-0"
      enter-to-class="opacity-100"
      leave-active-class="transition duration-150 ease-in"
      leave-from-class="opacity-100"
      leave-to-class="opacity-0"
    >
      <div v-if="restoreDeleteName" class="fixed inset-0 z-[60] flex items-center justify-center p-4 backdrop-blur-sm">
        <div class="g-modal-mask" @click="restoreDeleteName = null"></div>
        <div
          ref="deleteBackupPanel"
          tabindex="-1"
          role="dialog"
          aria-modal="true"
          :aria-label="t('restore_delete_confirm_title')"
          class="relative w-full max-w-sm bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6 outline-none"
        >
          <DialogCloseButton :label="t('dialog_close')" @close="restoreDeleteName = null" />
          <h3 class="g-dialog-title mb-3">{{ t('restore_delete_confirm_title') }}</h3>
          <p class="text-sm text-ink-soft dark:text-[#A6A6AD] leading-relaxed mb-6 whitespace-pre-line">{{ t('restore_delete_confirm_msg', { name: restoreDeleteName || '' }) }}</p>
          <div class="g-dialog-actions">
            <button class="g-btn-secondary" @click="restoreDeleteName = null">{{ t('confirm_cancel') }}</button>
            <button class="g-btn-danger" @click="doRestoreDelete">{{ t('restore_delete') }}</button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
