// usePluginUpdateChecks —— 插件卡片「手动检查更新」的结果缓存（模块级单例）。
//
// 为什么放模块级而不是 PluginsView 里：检查是一次一次点的，用户很可能点完就去别的
// 子页面看一眼再回来；视图卸载会把组件内的 ref 一起带走，回来就只剩「没检查过」。
// 放在模块级后，切子页面（SPA 内跳转）状态都在；刷新整个页面会丢（与详情弹窗的
// 临时缓存同一口径，后台定时检测的结果另有后端缓存兜底）。
//
// 状态形状与「手动 > 后台、过期则回退」的合并规则在 utils/pluginUpdates.ts（有单测）；
// 这里只负责持有这份状态。`current` 必须和结果一起存：页面之外换过版本后，旧结论不能
// 再当真（否则会给出降级箭头并真的把插件装回旧版）。
import { ref } from 'vue'
import type { PluginUpdateCheckState } from '@/utils/pluginUpdates'

export type { PluginUpdateCheckState }

const checks = ref<Record<string, PluginUpdateCheckState>>({})

export function usePluginUpdateChecks() {
  return { checks }
}
