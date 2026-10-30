// plugins.ts —— 插件卸载 / 重置状态
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api, type PluginInfo } from '@/serverapi'
import { useToastStore } from '@/stores/toast'
import { uiErrText, useI18n } from '@/composables/useI18n'

export const usePluginsStore = defineStore('plugins', () => {
  // 插件列表缓存：store 常驻，切换子页面不重复命令拉取；卸载/重置插件后
  // 通过 clearPluginsCache()+loadPlugins(true) 主动重拉。
  const plugins = ref<PluginInfo[]>([])
  const pluginsLoading = ref(false)
  let pluginsCached = false

  const toast = useToastStore()
  const { t } = useI18n()

  async function loadPlugins(force = false): Promise<PluginInfo[]> {
    if (!force && pluginsCached) return plugins.value
    pluginsLoading.value = true
    try {
      const p = await api.listPlugins()
      plugins.value = p.plugins || []
      pluginsCached = true
    } catch (e) {
      toast.show(uiErrText(e, t('common_op_failed')), 'error')
    } finally {
      pluginsLoading.value = false
    }
    return plugins.value
  }

  function clearPluginsCache() {
    pluginsCached = false
    plugins.value = []
  }

  // 按官方 dsh 的包级开关启停：增删 dsh.profile.bundles（依赖保留）。
  // 返回 { ok, name, enabled, restart } 供视图决定是弹“重启 dsh”还是“刷新页面”的 toast。
  //
  // **刻意不在这里清缓存/重拉**：那条 `dsh plugin …` 命令要跑 1~3 秒，调用方需要在
  // 命令**跑完之后**、列表还完整渲染着的时刻拍滚动快照（见 PluginsView 的
  // reloadKeepingScroll）。在这里 clearPluginsCache() 会把列表先清空，快照就只剩 0。
  async function togglePlugin(name: string, enabled: boolean): Promise<{ ok: boolean; name: string; enabled: boolean; restart: boolean; msg?: string }> {
    const p = await api.togglePlugin(name, enabled)
    return { ok: p.ok, name, enabled, restart: !!p.restart, msg: p.error }
  }

  return { plugins, pluginsLoading, loadPlugins, clearPluginsCache, togglePlugin }
})