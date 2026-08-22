<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { useDirectoryStore } from '@/stores/directory'
import { TrimApp } from '@trimjs/web-app'
import { useToastStore } from '@/stores/toast'

const store = useDirectoryStore()
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
  store.load()
  toast.show('授权目录已更新', 'success')
}

async function openPicker() {
  try {
    if (sdk.isStandaloneWeb) {
      // 独立 Web 环境...
    } else {
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
  <div class="p-8 max-w-3xl">
    <div class="flex items-center justify-between mb-4">
      <h1 class="text-2xl font-bold text-slate-900 dark:text-slate-100">飞牛目录授权</h1>
      <button class="px-4 py-2 bg-indigo-600 hover:bg-indigo-500 rounded-lg text-sm font-medium text-white" @click="openPicker()">
        申请授权
      </button>
    </div>

    <!-- 移除 msg 和 error 显示块 -->

    <div v-if="loading" class="text-slate-500 dark:text-slate-400 text-sm py-8 text-center">加载中...</div>

    <div v-else-if="paths.length === 0" class="text-slate-500 dark:text-slate-400 text-sm py-8 text-center bg-slate-100 dark:bg-slate-900 border border-slate-200 dark:border-slate-800 rounded-xl">
      暂无已授权的共享目录
    </div>

    <ul v-else class="bg-slate-100 dark:bg-slate-900 border border-slate-200 dark:border-slate-800 rounded-xl divide-y divide-slate-200 dark:divide-slate-800">
      <li v-for="p in paths" :key="p" class="flex items-center justify-between px-4 py-3">
        <div class="flex flex-col gap-0.5">
          <span class="font-mono text-sm text-slate-800 dark:text-slate-200">{{ p }}</span>
          <span v-if="convertedPaths[p]" class="text-xs text-slate-500 dark:text-slate-400">
            {{ convertedPaths[p] }}
          </span>
        </div>
        <div class="flex items-center gap-2">
          <button class="px-3 py-1 text-xs bg-indigo-100 dark:bg-indigo-900/40 hover:bg-indigo-200 dark:hover:bg-indigo-800 border border-indigo-300 dark:border-indigo-700 rounded-md text-indigo-800 dark:text-indigo-200"
            @click="openFileManager(p)">打开</button>
          <button class="px-3 py-1 text-xs bg-rose-100 dark:bg-rose-900/40 hover:bg-rose-200 dark:hover:bg-rose-800 border border-rose-300 dark:border-rose-700 rounded-md text-rose-800 dark:text-rose-200"
            @click="store.remove(p)">移除</button>
        </div>
      </li>
    </ul>
  </div>
</template>