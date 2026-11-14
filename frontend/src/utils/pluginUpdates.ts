// src/utils/pluginUpdates.ts —— 插件卡片「有新版本」标记的合并规则（纯函数，便于单测）。
//
// 卡片上的标记有两个来源：
//   1. 单卡手动检查（模块级 usePluginUpdateChecks，切子页面不丢）；
//   2. 每小时后台检测的内存缓存（GET /api/plugins/updates，页面每 5 分钟轮询）。
//
// 合并口径是「手动优先」，但手动结论必须仍然对得上**列表里当前装着的版本**：
// `current` 是检查那一刻的已装版本，检查之后插件若在页面之外被装/降级/换版本（例如在
// 终端里 `dsh plugin add`），这条结论就过期了 —— 继续用它会让卡片显示「1.2.0 → 1.1.0」
// 这种降级箭头，黄色「更新」按钮还会真把插件装回旧版。过期时回退到后台结论。
//
// 后台结论没有 `current`，但同样要求 latest 与已装版本不同：后端缓存与列表刷新之间有
// 时间差，装完（缓存尚未刷新）时不要还挂着箭头。
import type { PluginInfo, PluginUpdateEntry } from '@/serverapi'

/** 单卡手动检查的结果（模块级 usePluginUpdateChecks 保存的状态）。 */
export type PluginUpdateCheckState = {
  status: 'checking' | 'done'
  latest?: string
  hasUpdate?: boolean
  /**
   * 检查那一刻列表里的已装版本（后端 `current` 字段）。
   *
   * 与当前的 `PluginInfo.version` 不一致即视为结论过期；缺省（旧状态 / 旧后端）时按
   * 可用处理，不因拿不到这个字段就把结论丢掉。
   */
  current?: string
}

/** 一张卡片当前的更新标记（hasUpdate=false 时 latest 恒为空串）。 */
export interface CardUpdate {
  hasUpdate: boolean
  latest: string
}

/**
 * 合并手动检查与后台检测的结论，返回 `name -> CardUpdate` 视图（模板与筛选共用）。
 *
 * 手动结论优先，但过期（`current` 与已装版本不符）时不算数，回退后台结论；
 * 后台结论要求 latest 非空且与已装版本不同。
 */
export function resolveCardUpdates(
  plugins: PluginInfo[],
  manual: Record<string, PluginUpdateCheckState>,
  background: Record<string, PluginUpdateEntry>
): Record<string, CardUpdate> {
  const out: Record<string, CardUpdate> = {}
  for (const p of plugins) {
    const checked = manual[p.name]
    if (checked?.status === 'done' && (checked.current === undefined || checked.current === p.version)) {
      out[p.name] = { hasUpdate: checked.hasUpdate === true, latest: checked.latest || '' }
      continue
    }
    const bg = background[p.name]
    out[p.name] = bg?.hasUpdate && bg.latestVersion && bg.latestVersion !== p.version
      ? { hasUpdate: true, latest: bg.latestVersion }
      : { hasUpdate: false, latest: '' }
  }
  return out
}
