<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { useDirectoriesStore } from '@/stores/directories'
import { useSettingsStore } from '@/stores/settings'
import { useToastStore } from '@/stores/toast'
import { useDshBackup } from '@/composables/useDshBackup'
import { uiErrText, useI18n } from '@/composables/useI18n'
import { api } from '@/serverapi'
import { trimAppReady } from '@/utils/trimApp'
import { isFnosApp } from '@/utils/fnosApp'
import {
  buildFnosAuthURL,
  fnosAuthReturnURL,
  readFnosAuthReturn,
  rememberFnosAuthState,
  stripFnosAuthReturn,
  takeFnosAuthState,
} from '@/utils/fnosAuth'
import { randomId } from '@/utils/randomId'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import BackupDialog from '@/components/directories/BackupDialog.vue'
import RestoreDialog from '@/components/directories/RestoreDialog.vue'
import PageHeader from '@/components/PageHeader.vue'
import { icons } from '@/utils/icons'

const store = useDirectoriesStore()
const settings = useSettingsStore()
const { paths, loading, convertedPaths } = storeToRefs(store)
const toast = useToastStore()
const { t } = useI18n()
// 只观察备份是否在跑（页头小转圈）；进度/结果在 BackupDialog 内部。
const { running: backupRunning, sync: syncBackup } = useDshBackup()

// 飞牛 App 内置 WebView 的目录授权走「独立 Web」那条路（见 openPicker）：
// App 的宿主桥接 pickUserFile 在 iOS 1.37.x 上会按共享授权处理并报鉴权异常，
// 而平台的网页授权页（/app-auth/pick-user-file）在 App 内可用 —— 同窗口跳过去，
// 选完目录由平台整页跳回本页（结果参数由 handleFnosAuthReturn 解析）。

// 宿主 SDK（@trimjs/web-app）是 utils/trimApp.ts 里的单例，且在页面加载时（main.ts）
// 就已开始握手 —— 不能在这里 new：宿主桌面的握手监听只保留 60 秒，等本页挂载再握手，
// 打开应用一段时间后进来的用户就永远握不上了（详见该文件头注释）。

// 主目录信息（来自后端 settings API 的 runtime）。主目录固定为本应用的 shares 目录，
// 不可切换，因此没有「当前主目录 vs 默认主目录」之分。
const homeDir = ref('') // 主目录（dsh 的 HOME）实际系统路径
const homeDirConverted = ref('') // 主目录 sdk 转换后的语义路径

// 备份 / 恢复弹窗的显示开关：进度、列表、恢复轮询都在各自组件内部。
const backupDialogVisible = ref(false)
const restoreVisible = ref(false) // 恢复备份选择弹窗

// 备份目录（统一备份路径 + sdk 转换后的语义路径）
const backupDirPath = ref('')
const backupDirConverted = ref('')

// 移除授权目录的确认弹窗状态（确认框由 ConfirmDialog 自己锁滚动/焦点）
const removeDirDialogVisible = ref(false)
const removeDirTarget = ref<string | null>(null)

async function loadHomeInfo() {
  if (!settings.runtime) await settings.load()
  const r = settings.runtime
  homeDir.value = r?.defaultHomeDir || ''
  // 转换主目录的语义路径（它不一定在用户授权列表内，需单独转换）
  if (homeDir.value) {
    try {
      const language = navigator.language || 'zh-CN'
      const res = await api.convertPath([homeDir.value], language)
      homeDirConverted.value = res?.result?.[0]?.semanticPath || ''
    } catch {
      homeDirConverted.value = ''
    }
  }
}

function onRemoveDirClick(path: string) {
  removeDirTarget.value = path
  removeDirDialogVisible.value = true
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
  // 后端可能正在跑一次备份（例如本页在上一次备份途中被刷新）：同步一次状态，
  // 页头转圈能接上；没有备份在跑时这次同步没有副作用。
  syncBackup()
  // 飞牛网页授权（App 内「独立 Web」通路）会整页跳回本页并带上结果参数：
  // 先消费结果（toast + 清地址栏），再照常拉列表。
  handleFnosAuthReturn()
  store.load()
  loadHomeInfo()
  loadBackupDir()
  window.addEventListener('message', handleAuthCallback)
})

// 解析平台授权页带回的 status / state / path。
// 失败（state 对不上）时只提示、不当作授权成功 —— 同窗口回跳时 sessionStorage 里的
// state 一定还在，对不上说明不是本次点击发起的授权。
function handleFnosAuthReturn() {
  const result = readFnosAuthReturn()
  if (!result) return

  const expected = takeFnosAuthState()
  stripFnosAuthReturn()

  if (expected && result.state !== expected) {
    toast.show(t('directory_auth_failed'), 'error')
    return
  }

  if (result.status === 'success') {
    toast.show(t('directory_auth_success'), 'success')
    return
  }
  if (result.status === 'cancel' || result.error === 'access_denied') {
    toast.show(t('directory_auth_cancel'), 'info')
    return
  }
  toast.show(t('directory_auth_failed'), 'error')
}

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

// 飞牛 App：把内置 WebView 当作「独立 Web」，打开平台的目录授权页。
// 授权完成后平台整页跳回本页，结果由 onMounted 里的 handleFnosAuthReturn 消费。
async function startFnosAppAuth() {
  let appName = settings.runtime?.appName || ''
  if (!appName) {
    await settings.load()
    appName = settings.runtime?.appName || ''
  }
  if (!appName) {
    toast.show(t('directory_appid_missing'), 'error')
    return
  }

  const state = randomId()
  rememberFnosAuthState(state)
  const url = buildFnosAuthURL({
    appName,
    redirectUri: fnosAuthReturnURL(),
    state,
    directory: true,
  })
  // 同窗口跳转：App 的 WebView 没有可靠的 window.open/opener，弹窗方案会静默失败。
  // 用 replace 而不是 assign，授权完成后 history 里不留一页已经用过的授权页。
  window.location.replace(url)
}

async function openPicker() {
  // 飞牛 App 内置 WebView：官方桥接的 pickUserFile 在 iOS App 上会按「应用共享授权」
  // 处理并报应用鉴权异常（个人目录授权不可用），改走平台的网页授权通路。
  if (isFnosApp()) {
    await startFnosAppAuth()
    return
  }

  // 等宿主桥接就绪；返回 null = 宿主的握手监听已销毁（重开应用窗口才能恢复）
  const sdk = await trimAppReady()
  if (!sdk) {
    toast.show(t('directory_host_bridge_lost'), 'error')
    return
  }
  try {
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
      // 注意：openAppAuth 的 pickUserFile 只支持 appName / redirectUri / state /
      // directory / accept / sidebarGroup（见 SDK 的 buildAppAuthUrl），title 与
      // okText 不在 AppAuthPickFileParams 内、也不会被拼进 URL；弹窗标题由宿主
      // 授权页自行决定，故此处不传（桥接模式 pickUserFile 仍支持，见下方分支）。
      await sdk.openAppAuth('pickUserFile', {
        appName,
        redirectUri,
        directory: true,
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
    toast.show(uiErrText(err, t('common_op_failed')), 'error')
  }
}

async function openFileManager(path: string) {
  // 同 openPicker：桥接未就绪时给明确提示，而不是按钮点了没反应
  const sdk = await trimAppReady()
  if (!sdk) {
    toast.show(t('directory_host_bridge_lost'), 'error')
    return
  }
  try {
    await sdk.openFileManager(path)
  } catch (err) {
    toast.show(t('directory_open_failed', { msg: uiErrText(err) }), 'error')
  }
}

onBeforeUnmount(() => {
  window.removeEventListener('message', handleAuthCallback)
})
</script>

<template>
  <div class="pt-3 sm:pt-4 pb-8 sm:pb-12 px-4 sm:px-8 max-w-6xl mx-auto">
    <!-- 页头（图标 + 标题 + 恢复备份 / 备份 / 添加；标题栏按钮统一样式：
         小一号字号 + 细边框 + 不填充底色） -->
    <PageHeader sticky class="mb-3" :title="t('nav_directory')" :icon="icons.folder">
      <!-- 恢复备份（红色边框红色文字，位于备份按钮左侧） -->
      <button class="g-btn-danger h-8 px-3 text-xs flex-shrink-0" @click="restoreVisible = true">{{ t('directory_restore') }}</button>
      <!-- 备份：备份进行中时按钮不置灰（点开就是进度视图），只加一个小转圈 -->
      <button class="g-btn-secondary h-8 px-3 text-xs flex-shrink-0" @click="backupDialogVisible = true">
        <span
          v-if="backupRunning"
          class="w-3 h-3 rounded-full border-2 border-line dark:border-[#2A2A32] border-t-brand animate-spin"
        ></span>
        {{ t('directory_backup') }}
      </button>
      <button class="g-btn-secondary h-8 px-3 text-xs flex-shrink-0" @click="openPicker()">{{ t('directory_add') }}</button>
    </PageHeader>

    <!-- 每个目录一张卡片，按容器宽度自适应分栏（.g-card-grid）：
         主目录固定为第一张，备份目录紧随其后，其余为已授权目录；加载/空态卡片占满整行。 -->
    <div class="g-card-grid g-fade-in">
      <!-- 主目录（固定第一张，不可移除、不可切换） -->
      <div v-if="homeDir" class="g-card g-card-hover p-4 flex flex-col border-brand/40 dark:border-brand/40 bg-brand/[0.03] dark:bg-brand/[0.05]">
        <div class="flex flex-col gap-1 min-w-0">
          <span class="font-mono text-sm text-ink dark:text-white break-all">{{ homeDir }}</span>
          <span v-if="homeDirConverted" class="text-xs text-ink-soft dark:text-[#A6A6AD] truncate">
            {{ homeDirConverted }}
          </span>
        </div>
        <div class="flex flex-wrap items-center gap-2 mt-auto pt-3 border-t border-line dark:border-[#2A2A32]">
          <span
            class="inline-flex items-center gap-1 px-2 py-1 rounded-full text-xs font-medium bg-brand/10 text-brand dark:bg-brand/20 dark:text-brand"
            :title="t('directory_home_label')"
          >{{ t('directory_home_label') }}</span>
          <button class="g-btn-secondary h-8 px-3 text-xs" @click="openFileManager(homeDir)">{{ t('directory_open') }}</button>
        </div>
      </div>

      <!-- 备份目录（独立卡片，结构与主目录卡片一致：上方路径 + 下方操作行） -->
      <div v-if="backupDirPath" class="g-card g-card-hover p-4 flex flex-col">
        <div class="flex flex-col gap-1 min-w-0">
          <span class="font-mono text-sm text-ink dark:text-white break-all">{{ backupDirPath }}</span>
          <span v-if="backupDirConverted" class="text-xs text-ink-soft dark:text-[#A6A6AD] truncate">{{ backupDirConverted }}</span>
        </div>
        <div class="flex flex-wrap items-center gap-2 mt-auto pt-3 border-t border-line dark:border-[#2A2A32]">
          <span
            class="inline-flex items-center gap-1 px-2 py-1 rounded-full text-xs font-medium bg-black/5 text-ink-soft dark:bg-white/10 dark:text-[#A6A6AD]"
            :title="t('directory_backup_dir')"
          >{{ t('directory_backup_dir') }}</span>
          <button
            class="g-btn-secondary h-8 px-3 text-xs"
            :title="t('directory_backup_dir_open')"
            @click="openFileManager(backupDirPath)"
          >{{ t('directory_open') }}</button>
        </div>
      </div>

      <div v-if="loading" class="col-span-full flex items-center justify-center py-16 text-ink-faint text-sm">
        <span class="w-4 h-4 rounded-full border-2 border-line border-t-brand animate-spin mr-2"></span>
        {{ t('directory_loading') }}
      </div>

      <div v-else-if="paths.length === 0"
        class="g-card p-10 flex flex-col items-center justify-center text-center py-16 col-span-full">
        <div class="w-12 h-12 rounded-full bg-brand/10 flex items-center justify-center mb-4">
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" class="w-6 h-6 text-brand" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
        </div>
        <p class="text-sm font-medium text-ink dark:text-white">{{ t('directory_empty') }}</p>
        <p class="text-sm text-ink-soft dark:text-[#A6A6AD] mt-1">{{ t('directory_empty_desc') }}</p>
      </div>

      <template v-else>
        <div v-for="p in paths" :key="p" class="g-card g-card-hover p-4 flex flex-col">
          <div class="flex flex-col gap-1 min-w-0">
            <span class="font-mono text-sm text-ink dark:text-white break-all">{{ p }}</span>
            <span v-if="convertedPaths[p]" class="text-xs text-ink-soft dark:text-[#A6A6AD] truncate">
              {{ convertedPaths[p] }}
            </span>
          </div>
          <div class="flex flex-wrap items-center gap-2 mt-auto pt-3 border-t border-line dark:border-[#2A2A32]">
            <button class="g-btn-secondary h-8 px-3 text-xs" @click="openFileManager(p)">{{ t('directory_open') }}</button>
            <button class="g-btn-danger h-8 px-3 text-xs" @click="onRemoveDirClick(p)">{{ t('directory_remove') }}</button>
          </div>
        </div>
      </template>
    </div>
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

    <!-- 备份 / 恢复弹窗（内部各自处理滚动锁、焦点与嵌套确认） -->
    <BackupDialog v-model:visible="backupDialogVisible" />
    <RestoreDialog v-model:visible="restoreVisible" />
  </div>
</template>