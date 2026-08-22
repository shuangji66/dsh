<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { useDirectoryStore } from '@/stores/directory'
import { useSettingsStore } from '@/stores/settings'
import { TrimApp } from '@trimjs/web-app'
import { useToastStore } from '@/stores/toast'

const store = useDirectoryStore()
const settings = useSettingsStore()
const { paths, loading, convertedPaths } = storeToRefs(store)
const toast = useToastStore()

const sdk = new TrimApp()
const authState = ref('')

onMounted(() => {
  store.load()
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
      toast.show('授权成功', 'success')
      return
    }
    if (result.status === 'cancel' || result.error === 'access_denied') {
      toast.show('已取消授权', 'info')
      return
    }
  }
  store.load()
  toast.show('授权目录已更新', 'success')
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
        toast.show('无法获取应用标识，请稍后重试', 'error')
        return
      }
      // 回调地址指向本应用基路径下的 callback.html（由后端托管提供）
      const basePath = document.baseURI ? new URL(document.baseURI).pathname.replace(/\/$/, '') : ''
      const redirectUri = window.location.origin + basePath + '/callback.html'
      await sdk.openAppAuth('pickUserFile', {
        appName,
        redirectUri,
        directory: true,
        title: '选择授权目录',
        okText: '确认授权',
        sidebarGroup: ['myFiles', 'otherShare', 'favorites'],
      })
      toast.show('已打开授权窗口，完成选择后将自动刷新', 'info', 4000)
    } else {
      // iframe/桥接模式：SDK 选择用户文件并授权给当前应用
      const result = await sdk.pickUserFile({
        directory: true,
        title: '选择授权目录',
        okText: '确认授权',
        sidebarGroup: ['myFiles', 'otherShare', 'favorites'],
      })
      if (result?.data?.length) {
        await store.load()
        toast.show('授权成功', 'success')
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
    toast.show(`打开文件管理器失败: ${(err as Error).message}`, 'error')
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
        <p class="text-ink-faint dark:text-[#8A8A92] text-sm font-medium uppercase tracking-widest">工作区</p>
      </div>
      <button class="g-btn-primary flex-shrink-0" @click="openPicker()">添加</button>
    </header>

    <div v-if="loading" class="flex items-center justify-center py-16 text-ink-faint text-sm">
      <span class="w-4 h-4 rounded-full border-2 border-line border-t-brand animate-spin mr-2"></span>
      加载中...
    </div>

    <div v-else-if="paths.length === 0"
      class="g-card p-10 flex flex-col items-center justify-center text-center py-16">
      <div class="w-12 h-12 rounded-full bg-brand/10 flex items-center justify-center mb-4">
        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" class="w-6 h-6 text-brand" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
      </div>
      <p class="text-sm font-medium text-ink dark:text-white">暂无已授权的目录</p>
      <p class="text-sm text-ink-soft dark:text-[#A6A6AD] mt-1">点击添加按钮授权你的飞牛目录。</p>
    </div>

    <div v-else class="flex flex-col gap-3">
      <div v-for="p in paths" :key="p" class="g-card g-card-hover p-4">
        <div class="flex flex-col gap-1 min-w-0">
          <span class="font-mono text-sm text-ink dark:text-white break-all">{{ p }}</span>
          <span v-if="convertedPaths[p]" class="text-xs text-ink-soft dark:text-[#A6A6AD] truncate">
            {{ convertedPaths[p] }}
          </span>
        </div>
        <div class="flex items-center gap-2 mt-3 pt-3 border-t border-line dark:border-[#2A2A32]">
          <button class="g-btn-secondary h-8 px-3 text-xs" @click="openFileManager(p)">打开</button>
          <button class="g-btn-danger h-8 px-3 text-xs" @click="store.remove(p)">移除</button>
        </div>
      </div>
    </div>
  </div>
</template>