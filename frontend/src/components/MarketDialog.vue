<script setup lang="ts">
import { computed, ref } from 'vue'
import type { UpdateStatus } from '@/serverapi'
import { uiText, useI18n } from '@/composables/useI18n'
import { useBodyScrollLock } from '@/composables/useBodyScrollLock'
import DialogCloseButton from '@/components/DialogCloseButton.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'

// 插件市场（dshmarket）弹窗：未安装 → 安装最新版；已安装 → 更新 / 卸载。
//
// 市场现在是一个普通的 profile 插件（不再是 dsh server 包自带的 bundle），因此
// 「未安装」是正常状态之一，弹窗必须能把它装上 —— 这是市场面板自己给不了的能力。
// 动作经 dsh 的插件命令完成（见后端 market.go），安装/卸载成功后后端会重启 dsh。
//
// 同样遵循「props 进来、事件出去」：状态由父组件（UpdateSection 的 SSE 快照）持有。
const props = defineProps<{
  visible: boolean
  status: UpdateStatus
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'install'): void
  (e: 'update'): void
  (e: 'remove'): void
  // ack：请父组件收起后端那份终态（成功已 toast / 失败详情已看过），
  // 否则概览页切走再切回会把同一条提示再弹一次。
  (e: 'ack'): void
}>()

const { t } = useI18n()

const installed = computed(() => !!props.status.localVersion)
const latest = computed(() => props.status.latestVersion || '')
const hasUpdate = computed(() => props.status.hasUpdate)

// 进行中（安装/卸载）：禁用关闭与一切动作按钮；进度只能等它跑完（后端不支持取消）。
const phase = computed(() => props.status.phase || '')
const busy = computed(() => phase.value === 'installing' || phase.value === 'removing')

const phaseText = computed(() => {
  if (phase.value === 'removing') return t('market_removing')
  if (phase.value === 'installing') return t('market_installing')
  return ''
})

// 卸载是不可逆的（要重新下载安装），因此单独二次确认；安装/更新不额外确认。
const removeConfirm = ref(false)
function confirmRemove() {
  emit('remove')
}

function close() {
  // 关掉弹窗**不会**中断安装/卸载（插件命令在 dsh 侧继续跑，结果由 toast 通知），
  // 因此 busy 时也允许关闭 —— 与 dsh 版本弹窗保持一致。
  // 若停在终态（成功已提示过、失败详情已看过）就收起它，避免切回概览页重复提示。
  if (phase.value === 'done' || phase.value === 'error') emit('ack')
  emit('close')
}

// 弹窗打开期间锁定背景页面滚动（叠加的卸载确认框由 ConfirmDialog 自己再锁一层，
// 引用计数归零才解锁），否则移动端能拖拽弹窗背后的控制台页面。
useBodyScrollLock(() => props.visible || removeConfirm.value)
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
      <div v-if="visible" class="fixed inset-0 z-50 flex items-center justify-center p-4 backdrop-blur-sm">
        <div class="g-modal-mask" @click="close"></div>
        <div class="relative w-full max-w-sm bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6">
          <DialogCloseButton :label="t('dialog_close')" @close="close" />
          <h3 class="g-dialog-title mb-3">{{ t('market_dialog_title') }}</h3>

          <!-- 进行中：安装/卸载都在跑插件命令，成功后会重启 dsh 让新 bundle 生效 -->
          <div v-if="busy" class="py-4">
            <div class="flex items-center gap-3">
              <span class="inline-block animate-spin h-5 w-5 border-2 border-brand border-t-transparent rounded-full"></span>
              <span class="text-sm text-ink-soft dark:text-[#A6A6AD]">{{ phaseText }}</span>
            </div>
            <div class="mt-3 h-1.5 rounded-full bg-black/10 dark:bg-white/10 overflow-hidden">
              <div class="h-full rounded-full bg-brand progress-indeterminate"></div>
            </div>
            <div v-if="status.message" class="mt-2 font-mono text-xs text-ink-faint dark:text-[#8A8A92] break-all select-text">
              {{ uiText(status.message, status.messageRef) }}
            </div>
            <div class="mt-2 text-xs text-ink-faint dark:text-[#8A8A92]">{{ t('market_busy_hint') }}</div>
          </div>

          <template v-else>
            <!-- 已安装：版本对照 -->
            <div v-if="installed" class="space-y-2">
              <div class="flex items-center justify-between">
                <span class="text-sm text-ink-soft dark:text-[#A6A6AD]">{{ t('update_local_ver') }}</span>
                <span class="font-mono text-sm font-semibold text-ink dark:text-white">{{ status.localVersion }}</span>
              </div>
              <div class="flex items-center justify-between">
                <span class="text-sm text-ink-soft dark:text-[#A6A6AD]">{{ t('update_latest_ver') }}</span>
                <span class="font-mono text-sm font-semibold text-ink dark:text-white">{{ latest || '—' }}</span>
              </div>
            </div>
            <!-- 未安装：说明用正文档（text-sm + ink-soft，见 style.css 的弹窗字号约定），
                 要装的版本号是元信息，用 text-xs + ink-faint。 -->
            <p v-else class="text-sm leading-relaxed text-ink-soft dark:text-[#A6A6AD]">
              {{ t('market_not_installed_desc') }}
            </p>
            <p v-if="!installed && latest" class="mt-2 text-xs text-ink-faint dark:text-[#8A8A92]">
              {{ t('market_install_target', { v: latest }) }}
            </p>
            <p v-if="!installed && !latest" class="mt-2 text-sm leading-relaxed text-ink-soft dark:text-[#A6A6AD]">
              {{ t('market_latest_unknown') }}
            </p>

            <!-- 失败提示 -->
            <div
              v-if="status.error"
              class="mt-3 rounded-lg px-3 py-2 text-xs break-words select-text bg-danger/10 dark:bg-[#EF4444]/10 border border-danger/30 dark:border-[#EF4444]/30 text-[#EF4444]"
            >{{ uiText(status.error, status.errorRef) }}</div>

            <!-- 已安装但检测不到更新信息（检测失败）：给出诊断原因 -->
            <div
              v-if="installed && !latest && !status.error && status.marketDir"
              class="mt-3 rounded-lg px-3 py-2 text-xs break-words select-text bg-black/5 dark:bg-white/5 border border-line dark:border-[#2A2A32] text-ink-soft dark:text-[#A6A6AD]"
            >{{ status.marketDir }}</div>
          </template>

          <!-- 底部动作：未安装 → 安装；已安装 → 更新（有新版时）/ 卸载 -->
          <div v-if="!busy" class="g-dialog-actions">
            <template v-if="installed">
              <button v-if="hasUpdate" class="g-btn-warning" @click="emit('update')">{{ t('market_update_btn') }}</button>
              <button class="g-btn-danger" @click="removeConfirm = true">{{ t('market_remove_btn') }}</button>
            </template>
            <button v-else class="g-btn-secondary" @click="emit('install')">{{ t('market_install_btn') }}</button>
          </div>
        </div>
      </div>
    </Transition>

    <!-- 卸载二次确认 -->
    <ConfirmDialog
      v-model:visible="removeConfirm"
      :title="t('market_remove_confirm_title')"
      :message="t('market_remove_confirm_msg')"
      :confirm-text="t('market_remove_confirm_ok')"
      :cancel-text="t('confirm_cancel')"
      danger
      @confirm="confirmRemove"
    />
  </Teleport>
</template>
