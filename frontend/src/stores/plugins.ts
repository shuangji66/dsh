// plugins.ts —— 插件卸载 / 重置状态
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api, type PluginInfo } from '@/serverapi'
import { useToastStore } from '@/stores/toast'

export const usePluginsStore = defineStore('plugins', () => {
  // 插件列表缓存：store 常驻，切换子页面不重复命令拉取；卸载/重置插件后
  // 通过 clearPluginsCache()+loadPlugins(true) 主动重拉。
  const plugins = ref<PluginInfo[]>([])
  const pluginsLoading = ref(false)
  let pluginsCached = false

  const toast = useToastStore()

  async function loadPlugins(force = false): Promise<PluginInfo[]> {
    if (!force && pluginsCached) return plugins.value
    pluginsLoading.value = true
    try {
      const p = await api.listPlugins()
      plugins.value = p.plugins || []
      pluginsCached = true
    } catch (e) {
      toast.show((e as Error).message, 'error')
    } finally {
      pluginsLoading.value = false
    }
    return plugins.value
  }

  function clearPluginsCache() {
    pluginsCached = false
    plugins.value = []
  }

  return { plugins, pluginsLoading, loadPlugins, clearPluginsCache }
})