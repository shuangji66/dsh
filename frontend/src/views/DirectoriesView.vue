<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, computed } from 'vue'
import { storeToRefs } from 'pinia'
import { useDirectoriesStore } from '@/stores/directories'
import { useSettingsStore } from '@/stores/settings'
import { TrimApp } from '@trimjs/web-app'
import { useToastStore } from '@/stores/toast'
import { useI18n } from '@/composables/useI18n'
import { api, type DshDataBackup } from '@/serverapi'
import ConfirmDialog from '@/components/ConfirmDialog.vue'

const store = useDirectoriesStore()
const settings = useSettingsStore()
const { paths, loading, convertedPaths } = storeToRefs(store)
const toast = useToastStore()
const { t } = useI18n()

const sdk = new TrimApp()

// 主目录信息（来自后端 settings API 的 runtime）：默认主目录与当前主目录。
const defaultHomeDir = ref('') // 默认主目录的实际系统路径
const defaultHomeConverted = ref('') // 默认主目录 sdk 转换后的语义路径
const homeDir = ref('') // 当前生效的主目录（dsh 的 HOME 实际路径）

// 默认主目录是否就是当前主目录（未切换到其它授权目录时成立）
const isDefaultCurrent = computed(
  () => !!defaultHomeDir.value && !!homeDir.value && defaultHomeDir.value === homeDir.value
)

// 标记某个授权目录是否为当前主目录
function isCurrentHome(path: string) {
  return !!path && !!homeDir.value && path === homeDir.value
}

// 设置为主目录的确认弹窗状态
const setHomeDialogVisible = ref(false)
const setHomeTarget = ref<string | null>(null)
const setHomeConverted = ref('') // 目标目录 sdk 转换后的语义路径
const setHomeBusy = ref(false)
const migrateConfig = ref(false)

// 备份当前主目录 ~/.dsh 的确认弹窗状态
const backupDialogVisible = ref(false)
const backupBusy = ref(false)

// dsh 数据备份恢复状态
const restoreVisible = ref(false) // 恢复备份选择弹窗
const restoreBusy = ref(false) // 恢复进行中
const restoreError = ref('')
const dshBackups = ref<DshDataBackup[]>([]) // 可用备份列表
const selectedRestore = ref<string | null>(null) // 选中的备份名
const confirmRestoreVisible = ref(false) // 恢复二次确认
const restoreDeleteName = ref<string | null>(null) // 待删除备份
let restorePollTimer: ReturnType<typeof setInterval> | null = null

// 备份目录（统一备份路径 + sdk 转换后的语义路径）
const backupDirPath = ref('')
const backupDirConverted = ref('')

// 移除授权目录的确认弹窗状态
const removeDirDialogVisible = ref(false)
const removeDirTarget = ref<string | null>(null)

async function loadHomeInfo() {
  if (!settings.runtime) await settings.load()
  const r = settings.runtime
  defaultHomeDir.value = r?.defaultHomeDir || ''
  homeDir.value = r?.homeDir || ''
  // 转换默认主目录的语义路径（默认主目录不一定在用户授权列表内，需单独转换）
  if (defaultHomeDir.value) {
    try {
      const language = navigator.language || 'zh-CN'
      const res = await api.convertPath([defaultHomeDir.value], language)
      defaultHomeConverted.value = res?.result?.[0]?.semanticPath || ''
    } catch {
      defaultHomeConverted.value = ''
    }
  }
}

function onSetHomeClick(path: string) {
  setHomeTarget.value = path
  setHomeConverted.value = convertedPaths.value[path] || ''
  // 若无缓存转换结果（如默认主目录不在授权列表），则单独调用 SDK 转换。
  if (!setHomeConverted.value) {
    api
      .convertPath([path], navigator.language || 'zh-CN')
      .then((res) => {
        setHomeConverted.value = res?.result?.[0]?.semanticPath || ''
      })
      .catch(() => {
        setHomeConverted.value = ''
      })
  }
  migrateConfig.value = false
  setHomeDialogVisible.value = true
}

function onBackupClick() {
  backupDialogVisible.value = true
}

async function confirmBackup() {
  if (backupBusy.value) return
  backupBusy.value = true
  try {
    const p = await api.dshBackup()
    if (p.ok) {
      toast.show(t('directory_backup_success', { name: p.name || '' }), 'success')
    } else {
      toast.show(p.error || t('directory_backup_failed'), 'error')
    }
  } catch (e) {
    toast.show((e as Error).message, 'error')
  } finally {
    backupBusy.value = false
    backupDialogVisible.value = false
  }
}

async function confirmSetHome() {
  if (setHomeBusy.value || !setHomeTarget.value) return
  setHomeBusy.value = true
  const path = setHomeTarget.value
  try {
    const p = await api.dshSetHome(path, migrateConfig.value)
    if (p.ok) {
      toast.show(t('directory_home_switched', { path }), 'success')
      // 主动刷新：重新拉取后端 settings 以获取最新 homeDir，再刷新目录列表
      await settings.load()
      await loadHomeInfo()
      await store.load()
    } else {
      toast.show(p.error || t('directory_home_switch_failed'), 'error')
    }
  } catch (e) {
    toast.show((e as Error).message, 'error')
  } finally {
    setHomeBusy.value = false
    setHomeDialogVisible.value = false
    setHomeTarget.value = null
  }
}

function onRemoveDirClick(path: string) {
  removeDirTarget.value = path
  removeDirDialogVisible.value = true
}

// --- dsh 数据备份恢复 ---

// 拉取 dsh 数据备份列表
async function fetchDshBackups() {
  try {
    const res = await api.listDshDataBackups()
    dshBackups.value = res.backups || []
  } catch { /* ignore */ }
}

// 打开恢复备份弹窗
function onRestoreClick() {
  restoreError.value = ''
  restoreBusy.value = false
  selectedRestore.value = null
  fetchDshBackups()
  restoreVisible.value = true
}

// 选择一个备份
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
            restoreVisible.value = false
            // 恢复数据后刷新页面（dsh 已重启，加载新配置）
            setTimeout(() => window.location.reload(), 1000)
          } else {
            restoreError.value = st.error || t('restore_failed')
          }
        }
      } catch { /* 继续轮询 */ }
    }, 2000)
  } catch (e) {
    restoreError.value = (e as Error).message || t('restore_failed')
    restoreBusy.value = false
  }
}

// 删除备份
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
    fetchDshBackups()
  } catch (e) {
    toast.show((e as Error).message, 'error')
  }
}

// 格式化备份时间
function fmtRestoreDate(iso: string): string {
  const d = new Date(iso)
  if (isNaN(d.getTime())) return '—'
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}
// 格式化文件大小
function fmtRestoreSize(bytes: number): string {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / 1024 / 1024).toFixed(1) + ' MB'
}

// 拉取备份目录实际路径并转换语义路径
async function loadBackupDir() {
  try {
    const res = await api.backupDir()
    backupDirPath.value = res.path || ''
    const language = navigator.language || 'zh-CN'
    const converted = await api.convertPath([backupDirPath.value], language)
    backupDirConverted.value = converted?.result?.[0]?.semanticPath || ''
  } catch {
    backupDirPath.value = ''
    backupDirConverted.value = ''
  }
}

onMounted(() => {
  store.load()
  loadHomeInfo()
  loadBackupDir()
  window.addEventListener('message', handleAuthCallback)
})

function handleAuthCallback(event: MessageEvent) {
  if (event.origin !== window.location.origin) return
  if (event.data?.type !== 'harness:auth-result') return
  const result = event.data.result
  // 独立 Web 环境下通过 OAuth 授权页回传的结果
  if (result) {
    if (result.status === 'success' && result.method === 'pickUserFile') {
      store.load()
      toast.show(t('directory_auth_success'), 'success')
      return
    }
    if (result.status === 'cancel' || result.error === 'access_denied') {
      toast.show(t('directory_auth_cancel'), 'info')
      return
    }
  }
  store.load()
  toast.show(t('directory_auth_updated'), 'success')
}

async function openPicker() {
  try {
    await sdk.ready()
    if (sdk.isStandaloneWeb) {
      // 独立 Web 环境（移动端直达/桌面独立窗口）：无宿主桥接，
      // 需打开 OAuth 授权页，结果经 callback.html 回传
      let appName = settings.runtime?.appName
      if (!appName) {
        await settings.load()
        appName = settings.runtime?.appName
      }
      if (!appName) {
        toast.show(t('directory_appid_missing'), 'error')
        return
      }
      // 回调地址指向本应用基路径下的 callback.html（由后端托管提供）
      const basePath = document.baseURI ? new URL(document.baseURI).pathname.replace(/\/$/, '') : ''
      const redirectUri = window.location.origin + basePath + '/callback.html'
      await sdk.openAppAuth('pickUserFile', {
        appName,
        redirectUri,
        directory: true,
        title: t('directory_pick_title'),
        okText: t('directory_pick_ok'),
        sidebarGroup: ['myFiles', 'external', 'favorites', 'team'],
      })
      toast.show(t('directory_open_window'), 'info', 4000)
    } else {
      // iframe/桥接模式：SDK 选择用户文件并授权给当前应用
      const result = await sdk.pickUserFile({
        directory: true,
        title: t('directory_pick_title'),
        okText: t('directory_pick_ok'),
        sidebarGroup: ['myFiles', 'external', 'favorites', 'team'],
      })
      if (result?.data?.length) {
        await store.load()
        toast.show(t('directory_auth_success'), 'success')
      }
    }
  } catch (err) {
    toast.show((err as Error).message, 'error')
  }
}

async function openFileManager(path: string) {
  try {
    await sdk.openFileManager(path)
  } catch (err) {
    toast.show(t('directory_open_failed', { msg: (err as Error).message }), 'error')
  }
}

onBeforeUnmount(() => {
  window.removeEventListener('message', handleAuthCallback)
  if (restorePollTimer) clearInterval(restorePollTimer)
})
</script>

<template>
  <div class="py-8 sm:py-12 px-4 sm:px-8 max-w-3xl mx-auto">
    <header class="flex items-center justify-between mb-8">
      <div>
        <p class="text-ink-faint dark:text-[#8A8A92] text-sm font-medium uppercase tracking-widest">{{ t('nav_directory') }}</p>
      </div>
      <div class="flex items-center gap-2">
        <!-- 恢复备份（红色边框红色文字，位于备份按钮左侧） -->
        <button class="g-btn-danger h-9 px-3 text-xs flex-shrink-0" @click="onRestoreClick()">{{ t('directory_restore') }}</button>
        <button class="g-btn-secondary flex-shrink-0" :disabled="backupBusy" @click="onBackupClick()">{{ t('directory_backup') }}</button>
        <button class="g-btn-primary flex-shrink-0" @click="openPicker()">{{ t('directory_add') }}</button>
      </div>
    </header>

    <div class="flex flex-col gap-3">
      <!-- 默认主目录（固定第一张，不可移除） -->
      <div v-if="defaultHomeDir" class="g-card p-4 border-brand/40 dark:border-brand/40 bg-brand/[0.03] dark:bg-brand/[0.05]">
        <div class="flex flex-col gap-1 min-w-0">
          <div class="flex items-center gap-2 mb-1">
            <span class="text-[11px] font-medium uppercase tracking-widest text-brand dark:text-brand">{{ t('directory_default_home_title') }}</span>
          </div>
          <span class="font-mono text-sm text-ink dark:text-white break-all">{{ defaultHomeDir }}</span>
          <span v-if="defaultHomeConverted" class="text-xs text-ink-soft dark:text-[#A6A6AD] truncate">
            {{ defaultHomeConverted }}
          </span>
        </div>
        <div class="flex items-center gap-2 mt-3 pt-3 border-t border-line dark:border-[#2A2A32]">
          <!-- 默认主目录：仅当它仍是当前主目录时显示“当前主目录”标记；否则可设置回默认主目录 -->
          <span
            v-if="isDefaultCurrent"
            class="inline-flex items-center gap-1 px-2 py-1 rounded-full text-xs font-medium bg-brand/10 text-brand dark:bg-brand/20 dark:text-brand"
            :title="t('directory_current_home')"
          >{{ t('directory_current_home') }}</span>
          <button
            v-else
            class="g-btn-secondary h-8 px-3 text-xs"
            @click="onSetHomeClick(defaultHomeDir)"
          >{{ t('directory_set_home') }}</button>
          <button class="g-btn-secondary h-8 px-3 text-xs" @click="openFileManager(defaultHomeDir)">{{ t('directory_open') }}</button>
        </div>
        <!-- 备份目录（打开按钮下方，仅支持打开） -->
        <div v-if="backupDirPath" class="mt-3 pt-3 border-t border-line dark:border-[#2A2A32]">
          <div class="flex items-center justify-between gap-2">
            <div class="flex flex-col gap-0.5 min-w-0">
              <span class="text-xs text-ink-soft dark:text-[#A6A6AD]">{{ t('directory_backup_dir') }}</span>
              <span class="font-mono text-xs text-ink dark:text-white break-all">{{ backupDirPath }}</span>
              <span v-if="backupDirConverted" class="text-xs text-ink-soft dark:text-[#A6A6AD] truncate">{{ backupDirConverted }}</span>
            </div>
            <button class="g-btn-secondary h-8 px-3 text-xs flex-shrink-0" @click="openFileManager(backupDirPath)">{{ t('directory_backup_dir_open') }}</button>
          </div>
        </div>
      </div>

      <div v-if="loading" class="flex items-center justify-center py-16 text-ink-faint text-sm">
        <span class="w-4 h-4 rounded-full border-2 border-line border-t-brand animate-spin mr-2"></span>
        {{ t('directory_loading') }}
      </div>

      <div v-else-if="paths.length === 0"
        class="g-card p-10 flex flex-col items-center justify-center text-center py-16">
        <div class="w-12 h-12 rounded-full bg-brand/10 flex items-center justify-center mb-4">
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" class="w-6 h-6 text-brand" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
        </div>
        <p class="text-sm font-medium text-ink dark:text-white">{{ t('directory_empty') }}</p>
        <p class="text-sm text-ink-soft dark:text-[#A6A6AD] mt-1">{{ t('directory_empty_desc') }}</p>
      </div>

      <template v-else>
        <div v-for="p in paths" :key="p" class="g-card g-card-hover p-4">
          <div class="flex flex-col gap-1 min-w-0">
            <span class="font-mono text-sm text-ink dark:text-white break-all">{{ p }}</span>
            <span v-if="convertedPaths[p]" class="text-xs text-ink-soft dark:text-[#A6A6AD] truncate">
              {{ convertedPaths[p] }}
            </span>
          </div>
          <div class="flex items-center gap-2 mt-3 pt-3 border-t border-line dark:border-[#2A2A32]">
            <!-- 若该目录已是当前主目录则标记，否则提供“设置为主目录”按钮 -->
            <span
              v-if="isCurrentHome(p)"
              class="inline-flex items-center gap-1 px-2 py-1 rounded-full text-xs font-medium bg-brand/10 text-brand dark:bg-brand/20 dark:text-brand"
            >{{ t('directory_current_home') }}</span>
            <button
              v-else
              class="g-btn-secondary h-8 px-3 text-xs"
              @click="onSetHomeClick(p)"
            >{{ t('directory_set_home') }}</button>
            <button class="g-btn-secondary h-8 px-3 text-xs" @click="openFileManager(p)">{{ t('directory_open') }}</button>
            <button class="g-btn-danger h-8 px-3 text-xs" @click="onRemoveDirClick(p)">{{ t('directory_remove') }}</button>
          </div>
        </div>
      </template>
    </div>

    <!-- 设置为主目录确认弹窗（含迁移配置圆形复选框） -->
    <ConfirmDialog
      v-model:visible="setHomeDialogVisible"
      :title="t('confirm_set_home_title')"
      :confirm-text="t('confirm_ok')"
      :cancel-text="t('confirm_cancel')"
      :confirm-loading="setHomeBusy"
      @confirm="confirmSetHome()"
    >
      <div class="mb-2">
        <p class="text-sm text-ink-soft dark:text-[#A6A6AD] leading-relaxed whitespace-pre-line mb-3">
          {{ t('confirm_set_home_msg') }}
        </p>
        <p class="font-mono text-xs text-ink dark:text-white break-all bg-ink/[0.03] dark:bg-white/[0.05] rounded-md px-3 py-2 mb-4">{{ setHomeConverted || setHomeTarget || '' }}</p>
        <label class="flex items-start gap-3 cursor-pointer select-none group">
          <input
            v-model="migrateConfig"
            type="checkbox"
            class="peer sr-only"
          />
          <span
            class="relative w-5 h-5 mt-0.5 rounded-full shrink-0 border-2 border-line dark:border-[#3A3A42] bg-white dark:bg-[#1F1F26] transition-colors peer-checked:bg-brand peer-checked:border-brand"
          >
            <svg
              v-if="migrateConfig"
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
            <span class="block text-sm font-medium text-ink dark:text-white">{{ t('set_home_migrate_label') }}</span>
            <span class="block text-xs text-ink-soft dark:text-[#A6A6AD] mt-0.5">{{ t('set_home_migrate_hint') }}</span>
          </span>
        </label>
      </div>
    </ConfirmDialog>

    <!-- 移除授权目录确认弹窗 -->
    <ConfirmDialog
      v-model:visible="removeDirDialogVisible"
      :title="t('confirm_directory_remove_title')"
      :message="t('confirm_directory_remove_msg', { path: removeDirTarget || '' })"
      :confirm-text="t('confirm_ok')"
      :cancel-text="t('confirm_cancel')"
      danger
      @confirm="removeDirTarget && store.remove(removeDirTarget)"
    />

    <!-- 备份当前主目录 ~/.dsh 确认弹窗 -->
    <ConfirmDialog
      v-model:visible="backupDialogVisible"
      :title="t('confirm_backup_title')"
      :message="t('confirm_backup_msg')"
      :confirm-text="t('confirm_ok')"
      :cancel-text="t('confirm_cancel')"
      :confirm-loading="backupBusy"
      @confirm="confirmBackup()"
    />

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
        <div v-if="restoreVisible" class="fixed inset-0 z-50 flex items-center justify-center p-4">
          <div class="absolute inset-0 bg-black/50" @click="restoreBusy ? null : (restoreVisible = false)"></div>
          <div class="relative w-full max-w-lg bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6">
            <h3 class="font-display text-lg font-semibold text-ink dark:text-white mb-2">{{ t('confirm_restore_title') }}</h3>

            <div v-if="restoreBusy" class="py-8 text-center">
              <div class="inline-block animate-spin h-6 w-6 border-2 border-danger border-t-transparent rounded-full mb-2"></div>
              <div class="text-sm text-ink-soft dark:text-[#A6A6AD]">{{ t('restore_running') }}</div>
            </div>

            <template v-else>
              <div v-if="restoreError" class="mb-3 rounded-lg bg-danger/10 dark:bg-[#EF4444]/10 border border-danger/30 dark:border-[#EF4444]/30 px-3 py-2 text-xs text-[#EF4444] break-words">{{ restoreError }}</div>

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
                      {{ t('restore_date') }}: {{ fmtRestoreDate(b.modified) }} · {{ t('restore_size') }}: {{ fmtRestoreSize(b.size) }}
                    </div>
                  </div>
                  <button
                    type="button"
                    class="flex-shrink-0 text-ink-soft dark:text-[#A6A6AD] hover:text-danger dark:hover:text-[#EF4444] transition-colors"
                    :title="t('restore_delete')"
                    @click.prevent="openRestoreDelete(b.name)"
                  >
                    <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
                  </button>
                </label>
              </div>
            </template>

            <div class="flex justify-end gap-3 mt-6">
              <button class="g-btn-secondary" :disabled="restoreBusy" @click="restoreVisible = false">{{ t('confirm_cancel') }}</button>
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
        <div v-if="confirmRestoreVisible" class="fixed inset-0 z-[60] flex items-center justify-center p-4">
          <div class="absolute inset-0 bg-black/50" @click="confirmRestoreVisible = false"></div>
          <div class="relative w-full max-w-sm bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6">
            <h3 class="font-display text-lg font-semibold text-ink dark:text-white mb-3">{{ t('confirm_restore_title') }}</h3>
            <p class="text-sm text-ink-soft dark:text-[#A6A6AD] leading-relaxed mb-6 whitespace-pre-line">{{ t('confirm_restore_msg', { name: selectedRestore || '' }) }}</p>
            <div class="flex justify-end gap-3 mt-6">
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
        <div v-if="restoreDeleteName" class="fixed inset-0 z-[60] flex items-center justify-center p-4">
          <div class="absolute inset-0 bg-black/50" @click="restoreDeleteName = null"></div>
          <div class="relative w-full max-w-sm bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6">
            <h3 class="font-display text-lg font-semibold text-ink dark:text-white mb-3">{{ t('restore_delete_confirm_title') }}</h3>
            <p class="text-sm text-ink-soft dark:text-[#A6A6AD] leading-relaxed mb-6 whitespace-pre-line">{{ t('restore_delete_confirm_msg', { name: restoreDeleteName || '' }) }}</p>
            <div class="flex justify-end gap-3 mt-6">
              <button class="g-btn-secondary" @click="restoreDeleteName = null">{{ t('confirm_cancel') }}</button>
              <button class="g-btn-danger" @click="doRestoreDelete">{{ t('restore_delete') }}</button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>