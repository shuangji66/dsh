// usePluginUpdateChecks —— 插件卡片「手动检查更新」的结果缓存（模块级单例）。
//
// 为什么放模块级而不是 PluginsView 里：检查是一次一次点的，用户很可能点完就去别的
// 子页面看一眼再回来；视图卸载会把组件内的 ref 一起带走，回来就只剩「没检查过」。
// 放在模块级后，切子页面（SPA 内跳转）状态都在；刷新整个页面会丢（与详情弹窗的
// 临时缓存同一口径，后台定时检测的结果另有后端缓存兜底）。
import { ref } from 'vue'

export type PluginUpdateCheckState = {
  status: 'checking' | 'done'
  latest?: string
  hasUpdate?: boolean
}

const checks = ref<Record<string, PluginUpdateCheckState>>({})

export function usePluginUpdateChecks() {
  return { checks }
}
