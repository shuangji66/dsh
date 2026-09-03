<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api, sseUrl, type UpdateKind, type UpdateStatus, type ServerBackup } from '@/serverapi'
import { useToastStore } from '@/stores/toast'
import { useI18n } from '@/composables/useI18n'

// 概览页传入：dsh 访问地址列表（显示在版本号下方）
const props = defineProps<{ accessUrls?: string[] }>()

const toast = useToastStore()
const { t } = useI18n()

// 后端推送的更新状态（harness / dsh 各一份）。本地版本号由 `/api/update/status`
// 统一提供（dsh 版本原 `/api/dsh/version` 端点已移除）。
const harnessStatus = ref<UpdateStatus>({ kind: 'harness', localVersion: '', latestVersion: '', hasUpdate: false, checkedAt: '' })
const dshStatus = ref<UpdateStatus>({ kind: 'dsh', localVersion: '', latestVersion: '', hasUpdate: false, checkedAt: '' })

// 各目标是否正在“检查更新”
const checking = ref<Record<UpdateKind, boolean>>({ harness: false, dsh: false })

// 弹窗状态
const dialogVisible = ref(false)
const dialogKind = ref<UpdateKind>('harness')
const updating = ref(false) // 是否正在执行更新
const updatingDone = ref(false) // 更新成功后短暂显示“更新成功”
const targetVersion = ref('') // 用户点击更新时正在安装的目标版本号

// dsh server 回滚状态
const serverBackups = ref<ServerBackup[]>([]) // 可用备份列表
const backupsLoaded = ref(false)
const rollbackVisible = ref(false) // 回滚选择弹窗
const rollbackRunning = ref(false) // 回滚是否进行中
const rollbackError = ref('') // 回滚错误
const selectedRollback = ref<string | null>(null) // 选中的备份名
const confirmRollbackVisible = ref(false) // 回滚二次确认
const deleteConfirmName = ref<string | null>(null) // 待删除的备份名

let es: EventSource | null = null
let reloadTimer: ReturnType<typeof setTimeout> | null = null
let rollbackPollTimer: ReturnType<typeof setInterval> | null = null

// 从后端快照合并到本地响应式状态
function merge(snap: { harness: UpdateStatus; dsh: UpdateStatus }) {
  if (snap.harness) {
    harnessStatus.value = { ...snap.harness, localVersion: snap.harness.localVersion || '' }
  }
  if (snap.dsh) {
    dshStatus.value = { ...snap.dsh, localVersion: snap.dsh.localVersion || '' }
  }
}

// 弹窗所指向的目标状态
const dialogStatus = computed<UpdateStatus>(() =>
  dialogKind.value === 'harness' ? harnessStatus.value : dshStatus.value
)

// 弹窗标题
const dialogTitle = computed(() =>
  dialogKind.value === 'harness' ? t('update_dialog_title_harness') : t('update_dialog_title_dsh')
)

// 版本号右上角红点：有更新时显示
function hasUpdateDot(kind: UpdateKind): boolean {
  return kind === 'harness' ? harnessStatus.value.hasUpdate : dshStatus.value.hasUpdate
}
function versionText(kind: UpdateKind): string {
  return kind === 'harness' ? harnessStatus.value.localVersion : dshStatus.value.localVersion
}
function latestText(kind: UpdateKind): string {
  return kind === 'harness' ? harnessStatus.value.latestVersion : dshStatus.value.latestVersion
}

// 通过 SSE 监听后端推送的更新检测结果
function connectUpdateStream() {
  es?.close()
  const s = new EventSource(sseUrl('/api/update/stream'))
  es = s
  s.addEventListener('update', (ev) => {
    try {
      const data = JSON.parse((ev as MessageEvent).data)
      if (data && (data.harness || data.dsh)) merge(data)
    } catch {
      /* ignore malformed frames */
    }
  })
  s.onerror = () => {
    // 连接断开时释放旧连接；EventSource 会内置重连
    s.close()
    es = null
  }
}

// “检查更新”按钮：通知后端执行一次检测，随后 SSE 推送最新结果
async function doCheck(kind: UpdateKind) {
  checking.value[kind] = true
  try {
    // 后端同步执行检测并返回最新结果
    const snap = await api.updateCheck()
    merge(snap)
    const st = kind === 'harness' ? snap.harness : snap.dsh
    if (st?.error) {
      toast.show(st.error, 'error')
    } else if (st && !st.hasUpdate) {
      toast.show(t('update_no_new'), 'success')
    }
  } catch (e) {
    toast.show((e as Error).message || t('update_error_unknown'), 'error')
  } finally {
    checking.value[kind] = false
  }
}

// 打开更新弹窗
function openDialog(kind: UpdateKind) {
  dialogKind.value = kind
  dialogVisible.value = true
}

// 执行自我更新
async function doApply() {
  const kind = dialogKind.value
  if (updating.value) return
  // 记录本次要安装的目标版本，用于判定更新是否完成。
  targetVersion.value = dialogStatus.value.latestVersion
  updating.value = true
  updatingDone.value = false
  try {
    await api.updateApply(kind)
    // 后端异步执行下载/备份/替换/重启；由 SSE 推送完成状态。
    // 兜底超时：若长时间未收到完成状态则主动刷新页面（尽力而为）。
    reloadTimer = setTimeout(() => {
      window.location.reload()
    }, 45000)
  } catch (e) {
    // 请求阶段即失败（参数错误等）
    toast.show((e as Error).message || t('update_failed'), 'error')
    updating.value = false
  }
}

// 侦测后端推送的完成/失败状态
function watchForCompletion(kind: UpdateKind, st: UpdateStatus) {
  if (kind !== dialogKind.value || !updating.value) return
  // 失败：后端推送了 error
  if (st.error) {
    clearTimeout(reloadTimer ?? undefined)
    updating.value = false
    updatingDone.value = false
    toast.show(st.error || t('update_failed'), 'error')
    return
  }
  // 成功：不再有更新，且本地版本已到达本次要安装的目标版本
  // （更新后本地版本号会等于目标版本，如 harness-1.0.1 或 dsh-0.1.3）
  if (!st.hasUpdate && targetVersion.value && st.localVersion === targetVersion.value) {
    clearTimeout(reloadTimer ?? undefined)
    updating.value = false
    updatingDone.value = true
    // 短暂显示“更新成功”，随后关闭弹窗并刷新页面
    setTimeout(() => {
      dialogVisible.value = false
      window.location.reload()
    }, 1200)
  }
}

function closeDialog() {
  if (updating.value) return
  dialogVisible.value = false
}

// --- dsh server 回滚 ---

// 拉取 server 备份列表
async function fetchBackups() {
  try {
    const res = await api.listBackups()
    serverBackups.value = res.backups || []
  } catch { /* ignore */ }
  backupsLoaded.value = true
}

// 回滚是否有可用备份（用于显示回滚图标）
const hasServerBackups = computed(() => serverBackups.value.length > 0)

// 格式化文件大小
function fmtSize(bytes: number): string {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / 1024 / 1024).toFixed(1) + ' MB'
}
// 格式化备份时间
function fmtBackupDate(iso: string): string {
  const d = new Date(iso)
  if (isNaN(d.getTime())) return '—'
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

// 打开回滚弹窗
function openRollback() {
  rollbackError.value = ''
  rollbackRunning.value = false
  selectedRollback.value = null
  fetchBackups() // 每次打开都刷新列表
  rollbackVisible.value = true
}

// 选中一个备份
function selectBackup(name: string) {
  selectedRollback.value = name
}

// 选中备份后点击"回滚到"按钮 → 弹出二次确认
function openConfirmRollback() {
  if (!selectedRollback.value) return
  confirmRollbackVisible.value = true
}

// 执行回滚（二次确认后）
async function doRollback() {
  const name = selectedRollback.value
  if (!name) return
  confirmRollbackVisible.value = false
  rollbackRunning.value = true
  rollbackError.value = ''
  try {
    await api.rollback(name)
    // 轮询回滚状态直到完成
    pollRollbackStatus()
  } catch (e) {
    rollbackError.value = (e as Error).message || t('rollback_failed')
    rollbackRunning.value = false
  }
}

// 轮询回滚状态
function pollRollbackStatus() {
  if (rollbackPollTimer) clearInterval(rollbackPollTimer)
  rollbackPollTimer = setInterval(async () => {
    try {
      const res = await api.rollbackStatus()
      const st = res.status
      if (st.done) {
        clearInterval(rollbackPollTimer!)
        rollbackPollTimer = null
        rollbackRunning.value = false
        if (st.ok) {
          toast.show(t('rollback_success'), 'success')
          rollbackVisible.value = false
          // 回滚成功后刷新页面（dsh 已重启）
          setTimeout(() => window.location.reload(), 1000)
        } else {
          rollbackError.value = st.error || t('rollback_failed')
        }
      }
    } catch {
      // 网络错误时继续轮询
    }
  }, 2000)
}

// 删除备份
function openDeleteConfirm(name: string) {
  deleteConfirmName.value = name
}

async function doDeleteBackup() {
  const name = deleteConfirmName.value
  if (!name) return
  try {
    await api.deleteBackup(name)
    toast.show(t('rollback_deleted'), 'success')
    deleteConfirmName.value = null
    fetchBackups()
  } catch (e) {
    toast.show((e as Error).message, 'error')
  }
}

// 打开 dsh 访问地址（新标签页）
function openAccessUrl(url: string) {
  const u = url.trim()
  if (!u) return
  const final = /^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(u) ? u : 'http://' + u
  window.open(final, '_blank', 'noopener')
}

onMounted(() => {
  // 拉取一次后端状态快照作为初始值
  api.updateStatus().then(merge).catch(() => {})
  connectUpdateStream()
  fetchBackups()
})

onBeforeUnmount(() => {
  es?.close()
  es = null
  if (reloadTimer) clearTimeout(reloadTimer)
  if (rollbackPollTimer) clearInterval(rollbackPollTimer)
})

// 侦测后端推送的完成/失败状态：当对应状态变化且正在更新时判定结果。
watch(
  () => dialogKind.value === 'harness' ? harnessStatus.value : dshStatus.value,
  (st) => {
    if (dialogVisible.value && updating.value) watchForCompletion(dialogKind.value, st)
  },
  { deep: true }
)
</script>
<template>
  <div>
    <!-- 版本信息行 -->
    <div class="mt-4 border-t border-line dark:border-[#2A2A32] pt-4">
      <!-- harness 控制台版本 -->
      <div class="flex items-center justify-between gap-3 py-2">
        <span class="text-xs text-ink-soft dark:text-[#A6A6AD] whitespace-nowrap">{{ t('update_harness_ver') }}</span>
        <div class="flex items-center gap-3 min-w-0">
          <!-- 版本号右对齐 + 常驻下划线：点击打开更新弹窗 -->
          <button class="relative font-mono text-sm font-semibold text-ink dark:text-white underline underline-offset-4 decoration-ink-soft/50 dark:decoration-[#A6A6AD]/50" @click="openDialog('harness')">
            {{ versionText('harness') }}
            <!-- 有更新时右上角红点 -->
            <span v-if="hasUpdateDot('harness')" class="absolute -top-1.5 -right-2.5 h-2.5 w-2.5 rounded-full bg-[#EF4444] shadow"></span>
          </button>
          <!-- 检查更新：SVG 刷新图标 -->
          <button class="flex-shrink-0 text-ink-soft dark:text-[#A6A6AD] hover:text-ink dark:hover:text-white transition-colors disabled:opacity-50" title="检查更新" :disabled="checking.harness" @click="doCheck('harness')">
            <svg :class="checking.harness ? 'animate-spin' : ''" class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M23 4v6h-6"></path><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"></path>
            </svg>
          </button>
        </div>
      </div>

      <!-- dsh 服务版本 -->
      <div class="flex items-center justify-between gap-3 py-2">
        <span class="text-xs text-ink-soft dark:text-[#A6A6AD] whitespace-nowrap">{{ t('update_dsh_ver') }}</span>
        <div class="flex items-center gap-3 min-w-0">
          <!-- 有备份时显示回滚图标（版本号左侧） -->
          <button
            v-if="hasServerBackups"
            class="flex-shrink-0 text-ink-soft dark:text-[#A6A6AD] hover:text-brand dark:hover:text-brand transition-colors"
            :title="t('rollback_title')"
            @click="openRollback"
          >
            <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/></svg>
          </button>
          <button class="relative font-mono text-sm font-semibold text-ink dark:text-white underline underline-offset-4 decoration-ink-soft/50 dark:decoration-[#A6A6AD]/50" @click="openDialog('dsh')">
            {{ versionText('dsh') }}
            <span v-if="hasUpdateDot('dsh')" class="absolute -top-1.5 -right-2.5 h-2.5 w-2.5 rounded-full bg-[#EF4444] shadow"></span>
          </button>
          <button class="flex-shrink-0 text-ink-soft dark:text-[#A6A6AD] hover:text-ink dark:hover:text-white transition-colors disabled:opacity-50" title="检查更新" :disabled="checking.dsh" @click="doCheck('dsh')">
            <svg :class="checking.dsh ? 'animate-spin' : ''" class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M23 4v6h-6"></path><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"></path>
            </svg>
          </button>
        </div>
      </div>
    </div>

    <!-- dsh 快捷访问地址列表（显示在版本号下方，带分隔线） -->
    <div v-if="accessUrls && accessUrls.length" class="mt-3 border-t border-line dark:border-[#2A2A32] pt-3">
      <div class="text-xs text-ink-soft dark:text-[#A6A6AD] mb-2">{{ t('access_urls_overview_title') }}</div>
      <div class="space-y-1">
        <template v-for="(url, i) in accessUrls" :key="i">
          <div class="flex items-center gap-2 py-0.5">
            <!-- 访问图标（地址左侧） -->
            <button
              class="flex-shrink-0 text-ink-soft dark:text-[#A6A6AD] hover:text-brand dark:hover:text-brand transition-colors"
              :title="t('access_urls_open')"
              @click="openAccessUrl(url)"
            >
              <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>
            </button>
            <!-- 访问地址（左对齐 + 下划线） -->
            <button
              class="flex-1 min-w-0 text-left text-xs font-mono text-ink dark:text-[#EDEDF0] underline underline-offset-4 decoration-ink-soft/40 dark:decoration-[#A6A6AD]/40 truncate hover:text-brand dark:hover:text-brand transition-colors"
              :title="url"
              @click="openAccessUrl(url)"
            >{{ url }}</button>
          </div>
        </template>
      </div>
    </div>

    <!-- 更新弹窗 -->
    <Teleport to="body">
      <Transition
        enter-active-class="transition duration-200 ease-out"
        enter-from-class="opacity-0"
        enter-to-class="opacity-100"
        leave-active-class="transition duration-150 ease-in"
        leave-from-class="opacity-100"
        leave-to-class="opacity-0"
      >
        <div v-if="dialogVisible" class="fixed inset-0 z-50 flex items-center justify-center p-4">
          <div class="absolute inset-0 bg-black/50" @click="closeDialog"></div>
          <div class="relative w-full max-w-sm bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6">
            <h3 class="font-display text-lg font-semibold text-ink dark:text-white mb-1">{{ dialogTitle }}</h3>

            <div v-if="updatingDone" class="py-6 text-center">
              <div class="text-sm font-medium text-success dark:text-[#10B981] mb-1">{{ t('update_done') }}</div>
            </div>

            <template v-else>
              <div class="mt-3 space-y-2">
                <div class="flex items-center justify-between">
                  <span class="text-sm text-ink-soft dark:text-[#A6A6AD]">{{ t('update_local_ver') }}</span>
                  <span class="font-mono text-sm font-semibold text-ink dark:text-white">{{ versionText(dialogKind) }}</span>
                </div>
                <div class="flex items-center justify-between">
                  <span class="text-sm text-ink-soft dark:text-[#A6A6AD]">{{ t('update_latest_ver') }}</span>
                  <span class="font-mono text-sm font-semibold text-ink dark:text-white">{{ latestText(dialogKind) || '—' }}</span>
                </div>
              </div>

              <!-- 失败提示 -->
              <div v-if="dialogStatus.error" class="mt-3 rounded-lg bg-danger/10 dark:bg-[#EF4444]/10 border border-danger/30 dark:border-[#EF4444]/30 px-3 py-2 text-xs text-[#EF4444] break-words">
                {{ dialogStatus.error }}
              </div>

              <!-- 无更新提示 -->
              <div v-else-if="!dialogStatus.hasUpdate" class="mt-3 text-sm text-ink-soft dark:text-[#A6A6AD]">
                {{ t('update_no_update') }}
              </div>
            </template>

            <div class="flex justify-end gap-3 mt-6">
              <button class="g-btn-secondary" :disabled="updating" @click="closeDialog">{{ t('update_close') }}</button>
              <button
                v-if="dialogStatus.hasUpdate && !updatingDone"
                class="g-btn-primary"
                :disabled="updating"
                @click="doApply"
              >{{ updating ? t('update_updating') : t('update_button') }}</button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

    <!-- 回滚选择弹窗 -->
    <Teleport to="body">
      <Transition
        enter-active-class="transition duration-200 ease-out"
        enter-from-class="opacity-0"
        enter-to-class="opacity-100"
        leave-active-class="transition duration-150 ease-in"
        leave-from-class="opacity-100"
        leave-to-class="opacity-0"
      >
        <div v-if="rollbackVisible" class="fixed inset-0 z-50 flex items-center justify-center p-4">
          <div class="absolute inset-0 bg-black/50" @click="rollbackRunning ? null : (rollbackVisible = false)"></div>
          <div class="relative w-full max-w-lg bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6">
            <h3 class="font-display text-lg font-semibold text-ink dark:text-white mb-2">{{ t('rollback_title') }}</h3>
            <p class="text-sm text-ink-soft dark:text-[#A6A6AD] mb-4">{{ t('rollback_desc') }}</p>

            <div v-if="rollbackRunning" class="py-8 text-center">
              <div class="inline-block animate-spin h-6 w-6 border-2 border-brand border-t-transparent rounded-full mb-2"></div>
              <div class="text-sm text-ink-soft dark:text-[#A6A6AD]">{{ t('rollback_running') }}</div>
            </div>

            <template v-else>
              <div v-if="rollbackError" class="mb-3 rounded-lg bg-danger/10 dark:bg-[#EF4444]/10 border border-danger/30 dark:border-[#EF4444]/30 px-3 py-2 text-xs text-[#EF4444] break-words">{{ rollbackError }}</div>

              <div v-if="serverBackups.length === 0 && backupsLoaded" class="py-6 text-center text-sm text-ink-faint dark:text-[#8A8A92]">
                {{ t('rollback_empty') }}
              </div>

              <div v-else class="border border-[#E8E8EC] dark:border-[#2A2A32] rounded-lg divide-y divide-[#E8E8EC] dark:divide-[#2A2A32] max-h-64 overflow-auto">
                <label
                  v-for="b in serverBackups"
                  :key="b.name"
                  class="flex items-center gap-3 px-3 py-2 cursor-pointer hover:bg-black/5 dark:hover:bg-white/5 transition-colors"
                >
                  <input
                    type="radio"
                    name="rollback"
                    :value="b.name"
                    :checked="selectedRollback === b.name"
                    class="accent-brand flex-shrink-0"
                    @change="selectBackup(b.name)"
                  />
                  <div class="flex-1 min-w-0">
                    <div class="text-sm font-mono text-ink dark:text-[#EDEDF0] truncate">{{ b.name }}</div>
                    <div class="text-xs text-ink-faint dark:text-[#8A8A92]">
                      {{ t('rollback_date') }}: {{ fmtBackupDate(b.modified) }} · {{ t('rollback_size') }}: {{ fmtSize(b.size) }}
                    </div>
                  </div>
                  <!-- 删除备份 -->
                  <button
                    type="button"
                    class="flex-shrink-0 text-ink-soft dark:text-[#A6A6AD] hover:text-danger dark:hover:text-[#EF4444] transition-colors"
                    :title="t('rollback_delete')"
                    @click.prevent="openDeleteConfirm(b.name)"
                  >
                    <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
                  </button>
                </label>
              </div>
            </template>

            <div class="flex justify-end gap-3 mt-6">
              <button class="g-btn-secondary" :disabled="rollbackRunning" @click="rollbackVisible = false">{{ t('update_close') }}</button>
              <button
                v-if="selectedRollback && !rollbackRunning"
                class="g-btn-primary"
                :disabled="!selectedRollback"
                @click="openConfirmRollback"
              >{{ t('rollback_to') }}</button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

    <!-- 回滚二次确认弹窗 -->
    <Teleport to="body">
      <Transition
        enter-active-class="transition duration-200 ease-out"
        enter-from-class="opacity-0"
        enter-to-class="opacity-100"
        leave-active-class="transition duration-150 ease-in"
        leave-from-class="opacity-100"
        leave-to-class="opacity-0"
      >
        <div v-if="confirmRollbackVisible" class="fixed inset-0 z-[60] flex items-center justify-center p-4">
          <div class="absolute inset-0 bg-black/50" @click="confirmRollbackVisible = false"></div>
          <div class="relative w-full max-w-sm bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6">
            <h3 class="font-display text-lg font-semibold text-ink dark:text-white mb-3">{{ t('rollback_confirm_title') }}</h3>
            <p class="text-sm text-ink-soft dark:text-[#A6A6AD] leading-relaxed mb-6 whitespace-pre-line">{{ t('rollback_confirm_msg', { name: selectedRollback || '' }) }}</p>
            <div class="flex justify-end gap-3 mt-6">
              <button class="g-btn-secondary" @click="confirmRollbackVisible = false">{{ t('confirm_cancel') }}</button>
              <button class="g-btn-primary" @click="doRollback">{{ t('rollback_confirm_ok') }}</button>
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
        <div v-if="deleteConfirmName" class="fixed inset-0 z-[60] flex items-center justify-center p-4">
          <div class="absolute inset-0 bg-black/50" @click="deleteConfirmName = null"></div>
          <div class="relative w-full max-w-sm bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6">
            <h3 class="font-display text-lg font-semibold text-ink dark:text-white mb-3">{{ t('rollback_delete_confirm_title') }}</h3>
            <p class="text-sm text-ink-soft dark:text-[#A6A6AD] leading-relaxed mb-6 whitespace-pre-line">{{ t('rollback_delete_confirm_msg', { name: deleteConfirmName || '' }) }}</p>
            <div class="flex justify-end gap-3 mt-6">
              <button class="g-btn-secondary" @click="deleteConfirmName = null">{{ t('confirm_cancel') }}</button>
              <button class="g-btn-primary !bg-danger hover:!bg-danger/90" @click="doDeleteBackup">{{ t('rollback_delete') }}</button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>
