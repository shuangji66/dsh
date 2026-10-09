<script setup lang="ts">
import { computed, ref } from 'vue'
import { api, type DshVersionEntry, type ServerVersions } from '@/serverapi'
import { uiErrText, uiText, useI18n } from '@/composables/useI18n'
import { useBodyScrollLock } from '@/composables/useBodyScrollLock'
import { useReleaseNotes } from '@/composables/useReleaseNotes'
import DialogCloseButton from '@/components/DialogCloseButton.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import GithubIconLink from '@/components/GithubIconLink.vue'
import ReleaseNotesDialog from '@/components/ReleaseNotesDialog.vue'
import { repoSlugs, repoURL } from '@/constants/repos'

// dsh 服务版本列表弹窗：一行一个版本，行内完成「下载 / 删除 / 切换」。
//
// 数据与动作全部来自父组件（UpdateSection 持有 SSE 快照与 API 调用），本组件只负责
// 呈现与「点哪个版本、做什么」的意图 —— props 进来、事件出去，保证状态只有一个来源。
//
// 交互要点（与后端 server.go 一一对应）：
//   - 下载中不支持暂停，只能取消；取消会连安装目录与 npm 下载缓存一起清掉；
//   - 安装成功的版本才能「切换」；「切换」会停 dsh 并用该版本重启，故有二次确认；
//   - 当前正在使用的版本不允许删除（后端也会拒绝）。
const props = defineProps<{
  visible: boolean
  status: ServerVersions | null
  loading: boolean
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'refresh'): void
  (e: 'install', version: string): void
  (e: 'cancel'): void
  (e: 'remove', version: string): void
  (e: 'switch', version: string): void
}>()

const { t } = useI18n()

// 版本行 = 镜像源列表 + 本地独有版本（后端已合并、已过滤、已降序）。
const rows = computed<DshVersionEntry[]>(() => props.status?.versions ?? [])

// 正在安装的那一份（同一时刻只有一个）：只有它这一行显示进度。
const install = computed(() => props.status?.install ?? null)
const installing = computed(() => {
  const ph = install.value?.phase ?? ''
  return ph === 'downloading' || ph === 'installing' || ph === 'verifying'
})

function isInstalling(version: string): boolean {
  return installing.value && install.value?.version === version
}

// 阶段文案：把后端阶段翻成用户看得懂的一句话。
const installPhaseText = computed(() => {
  switch (install.value?.phase) {
    case 'downloading': return t('dsh_ver_phase_downloading', { mirror: install.value?.mirror || '—' })
    case 'installing': return t('dsh_ver_phase_installing')
    case 'verifying': return t('dsh_ver_phase_verifying')
    default: return ''
  }
})

// 已获取的包数（npm 不预先给出依赖总量，所以这里只报数量、不报百分比）。
const fetchedText = computed(() =>
  install.value?.fetched ? t('dsh_ver_fetched', { n: install.value.fetched }) : ''
)

// 二次确认弹窗（取消下载 / 删除版本 / 切换版本）。
//
// 「显示与否」与「操作哪个版本」刻意分成两个状态：ConfirmDialog 在确认时会先发
// update:visible(false) 再发 confirm，若把目标版本挂在可见性上（关闭即清空），
// 确认回调就拿不到版本号了。
const cancelConfirm = ref(false)
const deleteConfirm = ref(false)
const switchConfirm = ref(false)
const pendingVersion = ref('')

function openSwitch(version: string) {
  pendingVersion.value = version
  switchConfirm.value = true
}
function confirmSwitch() {
  const v = pendingVersion.value
  pendingVersion.value = ''
  emit('switch', v)
}
function openDelete(version: string) {
  pendingVersion.value = version
  deleteConfirm.value = true
}
function confirmDelete() {
  const v = pendingVersion.value
  pendingVersion.value = ''
  emit('remove', v)
}
function confirmCancel() {
  emit('cancel')
}

// 弹窗打开期间锁定背景页面滚动（引用计数，叠加的二次确认框由 ConfirmDialog 自己再锁一层，
// 全部关掉才解锁）—— 否则在移动端可以拖拽弹窗背后的控制台页面。
useBodyScrollLock(() => props.visible || cancelConfirm.value || deleteConfirm.value || switchConfirm.value)

// --- 更新日志（点版本号打开） ---
//
// 这一处**刻意由本组件自己拉取**（其它动作都走「props 进来、事件出去」）：更新日志只是
// 只读内容、不进父组件的状态，而且是「点开才要」，让父组件为此维护一套请求状态没有意义。
// 取数/缓存/迟到的响应丢弃都在 useReleaseNotes 里（市场弹窗共用同一份）。
// 直接解构成具名变量：ref 在模板里会自动解包，但**从对象里取出来的 ref 不会**，
// 拆开后模板里就能直接写 notesVersion / notesLoading…（否则每处都得写 .value）。
const {
  version: notesVersion,
  loading: notesLoading,
  available: notesAvailable,
  note: notesNote,
  noteEn: notesNoteEn,
  errorText: notesError,
  open: openNotes,
  close: closeNotes
} = useReleaseNotes(
  (version) => api.dshVersionNotes(version),
  (e) => uiErrText(e, t('release_notes_unavailable'))
)
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
        <div class="g-modal-mask" @click="emit('close')"></div>
        <div class="relative w-full max-w-lg bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6">
          <DialogCloseButton :label="t('dialog_close')" @close="emit('close')" />

          <!-- 标题 + 上游仓库（deepseek-ai/deepseek-harness）的 GitHub 裸图标外链，
               紧靠标题；右侧留出 X 的位置。与 harness 弹窗、市场弹窗同一形态。 -->
          <div class="flex items-center gap-2 pr-11 mb-2">
            <h3 class="g-dialog-title !pr-0">{{ t('dsh_ver_dialog_title') }}</h3>
            <GithubIconLink :href="repoURL(repoSlugs.dsh)" :label="repoSlugs.dsh" />
          </div>
          <p class="text-xs text-ink-faint dark:text-[#8A8A92] mb-4 leading-relaxed">{{ t('dsh_ver_dialog_desc') }}</p>

          <!-- 版本列表来源（镜像源）与刷新入口 -->
          <div class="flex items-center justify-between gap-3 mb-3">
            <span class="text-xs text-ink-faint dark:text-[#8A8A92]">{{ t('dsh_ver_source') }}</span>
            <button
              class="g-btn-secondary h-7 px-2.5 text-xs"
              :disabled="loading || installing"
              @click="emit('refresh')"
            >{{ loading ? t('dsh_ver_refreshing') : t('dsh_ver_refresh') }}</button>
          </div>

          <!-- 列表拉取失败：只提示、不阻塞（本地已装版本仍可切换/删除） -->
          <div
            v-if="status?.error"
            class="mb-3 rounded-lg px-3 py-2 text-xs break-words select-text bg-[#F59E0B]/10 dark:bg-[#F59E0B]/15 border border-[#F59E0B]/40 text-[#B45309] dark:text-[#FBBF24]"
          >{{ status.error }}</div>

          <div v-if="loading && rows.length === 0" class="py-6 text-center text-sm text-ink-faint dark:text-[#8A8A92]">
            {{ t('dsh_ver_loading') }}
          </div>
          <div v-else-if="rows.length === 0" class="py-6 text-center text-sm text-ink-faint dark:text-[#8A8A92]">
            {{ t('dsh_ver_empty') }}
          </div>

          <div v-else class="border border-[#E8E8EC] dark:border-[#2A2A32] rounded-lg divide-y divide-[#E8E8EC] dark:divide-[#2A2A32] max-h-80 overflow-auto">
            <div v-for="row in rows" :key="row.version" class="px-3 py-2.5">
              <div class="flex items-center justify-between gap-3">
                <!-- 版本号 + 标注：dist-tag（latest/alpha/next…）+ 本地状态。
                     窄屏（移动端）放不下时，要求「版本号独占一行、标签与状态整组换到下一行」——
                     因此标签与状态包成同一个 flex 项：它要么贴着版本号同一行，要么整组下移，
                     不会出现版本号和某一个标签各占半行。 -->
                <div class="flex flex-wrap items-center gap-x-2 gap-y-1 min-w-0">
                  <!-- 版本号可点：打开该版本的更新日志（上游 GitHub Release 正文）。
                       下划线 + 悬停变品牌色是「可点」的提示；whitespace-nowrap 不能去掉 ——
                       去掉会在连字符处折断成两行（见 AGENTS.md 弹窗换行那条）。 -->
                  <button
                    type="button"
                    class="font-mono text-sm font-semibold whitespace-nowrap underline underline-offset-4 decoration-ink-soft/50 dark:decoration-[#A6A6AD]/50 text-ink dark:text-white hover:text-brand dark:hover:text-brand hover:decoration-brand transition-colors"
                    :title="t('release_notes_title')"
                    @click="openNotes(row.version)"
                  >{{ row.version }}</button>
                  <div class="flex flex-wrap items-center gap-x-2 gap-y-1 min-w-0">
                    <span
                      v-for="tag in row.tags || []"
                      :key="tag"
                      class="inline-flex items-center px-1.5 py-0.5 rounded text-xs font-medium bg-brand/10 text-brand dark:bg-brand/20 dark:text-brand"
                    >{{ tag }}</span>
                    <span
                      v-if="row.active"
                      class="g-status bg-success/10 text-success"
                    >{{ t('dsh_ver_active') }}</span>
                    <span
                      v-else-if="row.installed"
                      class="g-status bg-black/5 text-ink-soft dark:bg-white/10 dark:text-[#A6A6AD]"
                    >{{ t('dsh_ver_installed') }}</span>
                  </div>
                </div>

                <!-- 行内操作：未安装 → 下载；已安装且非当前 → 切换 + 删除；安装中 → 取消 -->
                <div class="flex items-center gap-2 flex-shrink-0">
                  <template v-if="isInstalling(row.version)">
                    <button class="g-btn-danger h-8 px-3 text-xs" @click="cancelConfirm = true">
                      {{ t('dsh_ver_cancel') }}
                    </button>
                  </template>
                  <template v-else-if="row.installed">
                    <button
                      v-if="!row.active"
                      class="g-btn-secondary h-8 px-3 text-xs"
                      :disabled="installing"
                      @click="openSwitch(row.version)"
                    >{{ t('dsh_ver_switch') }}</button>
                    <button
                      v-if="!row.active"
                      class="g-btn-danger h-8 px-3 text-xs"
                      :disabled="installing"
                      @click="openDelete(row.version)"
                    >{{ t('dsh_ver_delete') }}</button>
                  </template>
                  <button
                    v-else
                    class="g-btn-secondary h-8 px-3 text-xs"
                    :disabled="installing"
                    @click="emit('install', row.version)"
                  >{{ t('dsh_ver_download') }}</button>
                </div>
              </div>

              <!-- 该行的下载/包处理进度：阶段 + 已获取包数 + 实时尾行；总量未知，用不确定进度条 -->
              <div v-if="isInstalling(row.version)" class="mt-2">
                <div class="h-1.5 rounded-full bg-black/10 dark:bg-white/10 overflow-hidden">
                  <div class="h-full rounded-full bg-brand progress-indeterminate"></div>
                </div>
                <div class="mt-1 flex items-start justify-between gap-3 text-xs text-ink-soft dark:text-[#A6A6AD]">
                  <span>{{ installPhaseText }}</span>
                  <span v-if="fetchedText" class="font-mono flex-shrink-0">{{ fetchedText }}</span>
                </div>
                <div
                  v-if="install?.message"
                  class="mt-0.5 font-mono text-xs text-ink-faint dark:text-[#8A8A92] break-all line-clamp-2 select-text"
                >{{ uiText(install.message, install.messageRef) }}</div>
                <div class="mt-0.5 text-xs text-ink-faint dark:text-[#8A8A92]">{{ t('dsh_ver_no_pause') }}</div>
              </div>
            </div>
          </div>

          <!-- 失败 / 已取消的残留提示：**成功不在这里显示** —— 改成一次性 toast
               （见 UpdateSection 的结果提示），「已完成」这种终态常驻弹窗没有意义。 -->
          <div
            v-if="!installing && install && (install.phase === 'error' || install.cancelled)"
            class="mt-3 rounded-lg px-3 py-2 text-xs break-words select-text"
            :class="install.phase === 'error'
              ? 'bg-danger/10 dark:bg-[#EF4444]/10 border border-danger/30 dark:border-[#EF4444]/30 text-[#EF4444]'
              : 'bg-black/5 dark:bg-white/5 border border-line dark:border-[#2A2A32] text-ink-soft dark:text-[#A6A6AD]'"
          >
            <template v-if="install.phase === 'error'">{{ uiText(install.error, install.errorRef) }}</template>
            <template v-else>{{ t('dsh_ver_cancelled') }}</template>
          </div>
        </div>
      </div>
    </Transition>

    <!-- 取消下载：会清除安装目录与下载缓存 -->
    <ConfirmDialog
      v-model:visible="cancelConfirm"
      :title="t('dsh_ver_cancel_confirm_title')"
      :message="t('dsh_ver_cancel_confirm_msg')"
      :confirm-text="t('dsh_ver_cancel_confirm_ok')"
      :cancel-text="t('confirm_cancel')"
      danger
      @confirm="confirmCancel"
    />

    <!-- 删除已安装版本 -->
    <ConfirmDialog
      v-model:visible="deleteConfirm"
      :title="t('dsh_ver_delete_confirm_title')"
      :message="t('dsh_ver_delete_confirm_msg', { v: pendingVersion })"
      :confirm-text="t('dsh_ver_delete_confirm_ok')"
      :cancel-text="t('confirm_cancel')"
      danger
      @confirm="confirmDelete"
    />

    <!-- 切换版本：会停 dsh 并用该版本重新启动 -->
    <ConfirmDialog
      v-model:visible="switchConfirm"
      :title="t('dsh_ver_switch_confirm_title')"
      :message="t('dsh_ver_switch_confirm_msg', { v: pendingVersion })"
      :confirm-text="t('dsh_ver_switch_confirm_ok')"
      :cancel-text="t('confirm_cancel')"
      @confirm="confirmSwitch"
    />

    <!-- 该版本的更新日志（点版本号打开；只有一个关闭按钮） -->
    <ReleaseNotesDialog
      :visible="!!notesVersion"
      :version="notesVersion"
      :loading="notesLoading"
      :available="notesAvailable"
      :note="notesNote"
      :note-en="notesNoteEn"
      :error-text="notesError"
      @close="closeNotes()"
    />
  </Teleport>
</template>
