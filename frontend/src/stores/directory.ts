// directory.ts
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api, type PluginInfo } from '@/serverapi'
import { TrimApp } from '@trimjs/web-app'
import { useToastStore } from '@/stores/toast'

export const useDirectoryStore = defineStore('directory', () => {
  const paths = ref<string[]>([])
  const convertedPaths = ref<Record<string, string>>({})
  const loading = ref(false)
  const uid = ref<number>(0)

  // 插件列表缓存：模块单例（store 常驻），切换子页面不重复命令拉取；
  // 打开控制台/手动刷新（整页重新加载）会重建 store，缓存随之清空，
  // 卸载/重置插件后通过 clearPluginsCache()+loadPlugins(true) 主动重拉。
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

  async function load() {
    loading.value = true
    try {
      const p = await api.fnosUserAccess(uid.value)
      paths.value = p.paths || []
      if (paths.value.length > 0) {
        await convertPaths(paths.value)
      }
    } catch (e) {
      toast.show((e as Error).message, 'error')
    } finally {
      loading.value = false
    }
  }

  async function convertPaths(rawPaths: string[]) {
    try {
      const language = navigator.language || 'zh-CN'
      // 改为调用后端 API
      const result = await api.convertPath(rawPaths, language)
      if (result?.result) {
        const map: Record<string, string> = {}
        result.result.forEach((item: { path: string; semanticPath: string }) => {
          map[item.path] = item.semanticPath
        })
        convertedPaths.value = map
      }
    } catch (e) {
      console.warn('路径转换失败:', e)
    }
  }

  async function remove(path: string) {
    try {
      await api.fnosDeleteUserAccess(uid.value, path)
      paths.value = paths.value.filter((p) => p !== path)
      delete convertedPaths.value[path]
    } catch (e) {
      toast.show((e as Error).message, 'error')
    }
  }

  return { paths, convertedPaths, loading, uid, plugins, pluginsLoading, load, remove, loadPlugins, clearPluginsCache }
})