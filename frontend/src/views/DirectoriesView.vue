<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, computed } from 'vue'
import { storeToRefs } from 'pinia'
import { useDirectoriesStore } from '@/stores/directories'
import { useSettingsStore } from '@/stores/settings'
import { TrimApp } from '@trimjs/web-app'
import { useToastStore } from '@/stores/toast'
import { useI18n } from '@/composables/useI18n'
import { api } from '@/serverapi'
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

onMounted(() => {
  store.load()
  loadHomeInfo()
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
})
</script>

<template>
  <div class="py-8 sm:py-12 px-4 sm:px-8 max-w-3xl mx-auto">
    <header class="flex items-center justify-between mb-8">
      <div>
        <p class="text-ink-faint dark:text-[#8A8A92] text-sm font-medium uppercase tracking-widest">{{ t('nav_directory') }}</p>
      </div>
      <div class="flex items-center gap-2">
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
  </div>
</template>