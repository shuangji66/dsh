<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { useDirectoryStore } from '@/stores/directory'
import { useSettingsStore } from '@/stores/settings'
import { TrimApp } from '@trimjs/web-app'
import { useToastStore } from '@/stores/toast'
import { useI18n } from '@/composables/useI18n'
import { api, type PluginInfo } from '@/serverapi'

const store = useDirectoryStore()
const settings = useSettingsStore()
const { paths, loading, convertedPaths } = storeToRefs(store)
const toast = useToastStore()
const { t } = useI18n()

const sdk = new TrimApp()
const authState = ref('')

// 插件管理状态
const plugins = ref<PluginInfo[]>([])
const pluginsLoading = ref(false)
const removingPlugin = ref<string | null>(null)
const resetting = ref(false)

async function loadPlugins() {
  pluginsLoading.value = true
  try {
    const p = await api.listPlugins()
    plugins.value = p.plugins || []
  } catch (e) {
    toast.show((e as Error).message, 'error')
  } finally {
    pluginsLoading.value = false
  }
}

async function removePlugin(name: string) {
  removingPlugin.value = name
  try {
    const p = await api.removePlugin(name)
    if (p.ok) {
      plugins.value = plugins.value.filter((pl) => pl.name !== name)
      toast.show(t('plugin_removed', { name: p.removed }), 'success')
    } else {
      toast.show(p.msg || t('plugin_remove_failed'), 'error')
    }
  } catch (e) {
    toast.show((e as Error).message, 'error')
  } finally {
    removingPlugin.value = null
  }
}

async function resetPlugins() {
  resetting.value = true
  try {
    const p = await api.resetPlugins()
    if (p.ok) {
      const failed = (p.results || []).filter((r) => !r.ok)
      if (failed.length > 0) {
        toast.show(t('plugin_reset_partial', { n: String(failed.length) }), 'error')
      } else {
        toast.show(t('plugin_reset_done'), 'success')
      }
    }
  } catch (e) {
    toast.show((e as Error).message, 'error')
  } finally {
    resetting.value = false
    await loadPlugins()
  }
}

onMounted(() => {
  store.load()
  loadPlugins()
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
        sidebarGroup: ['myFiles', 'otherShare', 'favorites'],
      })
      toast.show(t('directory_open_window'), 'info', 4000)
    } else {
      // iframe/桥接模式：SDK 选择用户文件并授权给当前应用
      const result = await sdk.pickUserFile({
        directory: true,
        title: t('directory_pick_title'),
        okText: t('directory_pick_ok'),
        sidebarGroup: ['myFiles', 'otherShare', 'favorites'],
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
        <p class="text-ink-faint dark:text-[#8A8A92] text-sm font-medium uppercase tracking-widest">{{ t('directory_title') }}</p>
      </div>
      <button class="g-btn-primary flex-shrink-0" @click="openPicker()">{{ t('directory_add') }}</button>
    </header>

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

    <div v-else class="flex flex-col gap-3">
      <div v-for="p in paths" :key="p" class="g-card g-card-hover p-4">
        <div class="flex flex-col gap-1 min-w-0">
          <span class="font-mono text-sm text-ink dark:text-white break-all">{{ p }}</span>
          <span v-if="convertedPaths[p]" class="text-xs text-ink-soft dark:text-[#A6A6AD] truncate">
            {{ convertedPaths[p] }}
          </span>
        </div>
        <div class="flex items-center gap-2 mt-3 pt-3 border-t border-line dark:border-[#2A2A32]">
          <button class="g-btn-secondary h-8 px-3 text-xs" @click="openFileManager(p)">{{ t('directory_open') }}</button>
          <button class="g-btn-danger h-8 px-3 text-xs" @click="store.remove(p)">{{ t('directory_remove') }}</button>
        </div>
      </div>
    </div>

    <!-- 插件管理 -->
    <section class="g-card g-card-hover p-6 mt-6">
      <div class="flex items-center justify-between mb-4">
        <h2 class="font-display text-lg font-semibold text-ink dark:text-white">{{ t('plugin_title') }}</h2>
        <div class="flex items-center gap-2">
          <button class="g-btn-secondary h-8 px-3 text-xs" :disabled="pluginsLoading" @click="loadPlugins">
            {{ t('plugin_refresh') }}
          </button>
          <button class="g-btn-danger h-8 px-3 text-xs" :disabled="resetting || pluginsLoading" @click="resetPlugins">
            {{ t('plugin_reset') }}
          </button>
        </div>
      </div>
      <p class="text-sm text-ink-soft dark:text-[#A6A6AD] mb-5">{{ t('plugin_desc') }}</p>

      <div v-if="pluginsLoading" class="flex items-center justify-center py-8 text-ink-faint text-sm">
        <span class="w-4 h-4 rounded-full border-2 border-line border-t-brand animate-spin mr-2"></span>
        {{ t('plugin_loading') }}
      </div>

      <div v-else-if="plugins.length === 0" class="py-8 text-center text-sm text-ink-faint dark:text-[#8A8A92]">
        {{ t('plugin_empty') }}
      </div>

      <ul v-else class="divide-y divide-line dark:divide-[#2A2A32]">
        <li v-for="p in plugins" :key="p.name" class="py-3 flex items-center justify-between gap-3">
          <div class="min-w-0">
            <div class="font-mono text-sm font-medium text-ink dark:text-white truncate">{{ p.name }}</div>
            <div class="text-xs text-ink-faint dark:text-[#8A8A92]">{{ p.version }}</div>
          </div>
          <button
            class="g-btn-danger h-8 px-3 text-xs flex-shrink-0"
            :disabled="removingPlugin === p.name || resetting"
            @click="removePlugin(p.name)"
          >
            {{ removingPlugin === p.name ? t('plugin_removing') : t('plugin_uninstall') }}
          </button>
        </li>
      </ul>
    </section>
  </div>
</template>