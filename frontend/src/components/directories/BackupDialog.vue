<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { uiText, useI18n } from '@/composables/useI18n'
import { useBodyScrollLock } from '@/composables/useBodyScrollLock'
import { useDialogA11y } from '@/composables/useDialogA11y'
import { useDshBackup } from '@/composables/useDshBackup'
import DialogCloseButton from '@/components/DialogCloseButton.vue'
import { formatBytes } from '@/utils/format'

// 备份当前主目录 ~/.dsh 的弹窗。
//
// 备份在**服务端**异步执行（见 useDshBackup）：本组件只是观察者 ——
//   · 进行中 → 进度条 + 取消（取消会删掉不完整的备份文件）；
//   · 关闭（X / 关闭按钮 / 遮罩）都不终止备份，重新打开时先同步后端进度再继续显示；
//   · 结束时 → 结论（成功 / 已取消 / 失败）。
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ (e: 'update:visible', v: boolean): void }>()

const { t } = useI18n()
const {
  status: backupStatus,
  running: backupRunning,
  cancelling: backupCancelling,
  starting: backupStarting,
  lastError: backupLastError,
  start,
  cancel,
  syncForDialog,
  clearResult,
} = useDshBackup()

useBodyScrollLock(() => props.visible)

const panel = ref<HTMLElement | null>(null)
useDialogA11y(() => props.visible, panel, { onClose: close })

// 打开弹窗时的同步状态：先显示「检查中」，避免确认视图闪一下再跳到进度视图。
const backupSyncing = ref(false)

const backupPercent = computed(() => {
  const st = backupStatus.value
  if (!st || st.totalBytes <= 0) return 0
  // 打包期间源目录仍可能被 dsh 写入，百分比可能略微超过 100，一律夹取。
  return Math.min(100, Math.max(0, Math.round((st.bytes / st.totalBytes) * 100)))
})
// 总量未知（预扫描失败/空目录）时不显示百分比，交给不确定进度条（.progress-indeterminate）
const backupTotalKnown = computed(() => (backupStatus.value?.totalBytes ?? 0) > 0)
// 结束后的结论文案（成功/取消/失败），供弹窗的结果视图使用
const backupResultText = computed(() => {
  const st = backupStatus.value
  if (!st || st.running) return ''
  if (st.cancelled) return t('backup_cancelled')
  if (st.ok) return t('directory_backup_success', { name: st.name || '' })
  return uiText(st.error, st.errorRef, t('directory_backup_failed'))
})

watch(
  () => props.visible,
  async (visible) => {
    if (!visible) return
    backupSyncing.value = true
    try {
      await syncForDialog()
    } finally {
      backupSyncing.value = false
    }
  },
  { immediate: true }
)

function close() {
  // 关闭只是停止渲染，服务端备份继续跑；清掉本轮结果，下次打开回到确认视图。
  emit('update:visible', false)
  clearResult()
}

async function confirmBackup() {
  await start()
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
      <div v-if="props.visible" class="fixed inset-0 z-50 flex items-center justify-center p-4 backdrop-blur-sm">
        <div class="g-modal-mask" @click="close"></div>
        <div
          ref="panel"
          tabindex="-1"
          role="dialog"
          aria-modal="true"
          :aria-label="t('confirm_backup_title')"
          class="relative w-full max-w-sm bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6 outline-none"
        >
          <DialogCloseButton :label="t('dialog_close')" @close="close" />
          <h3 class="g-dialog-title mb-3">{{ t('confirm_backup_title') }}</h3>

          <!-- 进行中：进度条 + 进度文案。总量未知时用不确定进度条（不伪造百分比）。 -->
          <div v-if="backupRunning" class="py-2">
            <div class="flex items-center justify-between text-sm text-ink-soft dark:text-[#A6A6AD] mb-1">
              <span>{{ t('backup_running') }}</span>
              <span v-if="backupTotalKnown">{{ backupPercent }}%</span>
            </div>
            <div class="h-2 rounded-full bg-black/10 dark:bg-white/10 overflow-hidden">
              <div
                class="h-full rounded-full bg-brand transition-all duration-300"
                :class="backupTotalKnown ? '' : 'progress-indeterminate'"
                :style="backupTotalKnown ? { width: backupPercent + '%' } : {}"
              ></div>
            </div>
            <div class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1">
              {{ t('backup_progress_size', { done: formatBytes(backupStatus?.bytes || 0), total: formatBytes(backupStatus?.totalBytes || 0) }) }}
              · {{ t('backup_progress_files', { done: backupStatus?.files || 0, total: backupStatus?.totalFiles || 0 }) }}
            </div>
            <p class="text-sm text-ink-soft dark:text-[#A6A6AD] mt-3 leading-relaxed">{{ t('backup_keep_hint') }}</p>
            <p v-if="backupCancelling" class="text-xs text-danger-ink mt-1">{{ t('backup_cancelling') }}</p>
          </div>

          <!-- 打开弹窗时的状态同步：避免确认视图闪一下再跳到进度视图 -->
          <div v-else-if="backupSyncing" class="py-8 text-center">
            <div class="inline-block animate-spin h-5 w-5 border-2 border-brand border-t-transparent rounded-full"></div>
          </div>

          <!-- 已结束：结论（成功 / 已取消 / 失败） -->
          <div v-else-if="backupStatus" class="py-2">
            <p class="text-sm leading-relaxed whitespace-pre-line break-words" :class="backupStatus.ok ? 'text-ink dark:text-white' : 'text-ink-soft dark:text-[#A6A6AD]'">
              {{ backupResultText }}
            </p>
            <p v-if="backupStatus.ok" class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1">
              {{ t('restore_size') }}: {{ formatBytes(backupStatus.size || 0) }}
            </p>
          </div>

          <!-- 未开始：确认 -->
          <div v-else>
            <p class="text-sm text-ink-soft dark:text-[#A6A6AD] leading-relaxed whitespace-pre-line">{{ t('confirm_backup_msg') }}</p>
            <p v-if="backupLastError" class="mt-3 rounded-lg bg-danger/10 dark:bg-[#EF4444]/10 border border-danger/30 dark:border-[#EF4444]/30 px-3 py-2 text-xs text-danger-ink break-words">{{ backupLastError }}</p>
          </div>

          <div class="g-dialog-actions">
            <button class="g-btn-secondary" @click="close">{{ t('dialog_close') }}</button>
            <!-- 进行中：取消备份（后端中止打包并删除不完整的备份文件） -->
            <button
              v-if="backupRunning"
              class="g-btn-danger"
              :disabled="backupCancelling"
              @click="cancel()"
            >{{ backupCancelling ? t('backup_cancelling') : t('backup_cancel_btn') }}</button>
            <!-- 取消/失败后可直接重试（成功只给关闭） -->
            <button
              v-else-if="backupStatus && !backupStatus.ok"
              class="g-btn-secondary"
              :disabled="backupStarting"
              @click="confirmBackup()"
            >{{ t('backup_retry') }}</button>
            <button
              v-else-if="!backupStatus && !backupSyncing"
              class="g-btn-secondary"
              :disabled="backupStarting"
              @click="confirmBackup()"
            >{{ t('backup_start') }}</button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
