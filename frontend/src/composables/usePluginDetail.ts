// usePluginDetail —— 插件详情文档的懒加载与临时缓存。
//
// 详情是包里的静态文件，列表页不需要它：**只有真正打开弹窗时才请求**（点一次读一次）。
// 读到结果后在同一次页面会话里临时记住（模块级 Map，刷新页面即失效）；「无内容」也缓存，
// 避免每次点开都白跑一次接口；读取失败不缓存，重开弹窗可以重试。
//
// 缓存 key 带界面语言：官方按展示语言选文档（locale/<语言>.json 的 meta.detail），
// 切换中文/英文后同一插件应该显示对应语言的文档。
import { ref } from 'vue'
import { api, type PluginDetailPayload } from '@/serverapi'
import { uiErrText, useI18n } from '@/composables/useI18n'

export interface PluginDetailEntry {
  status: 'loading' | 'ready' | 'error'
  payload?: PluginDetailPayload
  error?: string
}

const cache = new Map<string, PluginDetailEntry>()

export function usePluginDetail() {
  const { locale, t } = useI18n()
  const entry = ref<PluginDetailEntry | null>(null)
  // 快速连点不同插件的详情时，只认最后一次请求的结果（缓存仍按各自 key 落盘）
  let seq = 0

  async function load(name: string) {
    const key = `${locale.value}:${name}`
    const cached = cache.get(key)
    if (cached) {
      entry.value = cached
      return
    }
    const current = ++seq
    entry.value = { status: 'loading' }
    try {
      const payload = await api.pluginDetail(name, locale.value)
      const ready: PluginDetailEntry = { status: 'ready', payload }
      cache.set(key, ready)
      if (current === seq) entry.value = ready
    } catch (e) {
      // 失败不进缓存：下次打开重新读，网络/控制台重启恢复后不用刷新页面
      if (current === seq) entry.value = { status: 'error', error: uiErrText(e, t('plugin_detail_failed')) }
    }
  }

  // reset 清掉当前展示的条目（弹窗关闭时调用），不影响缓存。
  function reset() {
    seq += 1
    entry.value = null
  }

  return { entry, load, reset }
}
