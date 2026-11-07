// src/utils/pluginFilter.ts —— 插件页顶栏下「分类筛选 + 实时搜索」的纯函数部分。
//
// 从 PluginsView 里拆出来只为一件事：这两条口径（什么算「已停用」、搜索匹配哪个字段）
// 是**语义**而不是排版，值得有回归测试——组件里内联的 computed 测不到。
//
// 分类口径与卡片上的状态标记一致：
//   - 已停用 = 声明了 dsh.bundle 但不在 dsh.profile.bundles 里（PluginInfo.disabled）；
//   - 已启用 = 其余全部。普通依赖（没有 dsh.bundle）后端根本不给 disabled 字段，它没有
//     「停用」这个状态；把它排除在「已启用」之外，会让筛选看起来像弄丢了插件。
// 搜索匹配插件名（npm 包名，即卡片上显示的那个名字）：列表接口只下发 name / version，
// 没有独立的「显示名称」字段，所以两者今天就是同一个值。
import type { PluginInfo } from '@/serverapi'

export type PluginFilter = 'all' | 'enabled' | 'disabled'

export type FilterCounts = Record<PluginFilter, number>

/** 每个分类各有多少个插件。`all` 就是列表长度，`enabled` 含普通依赖（见上面）。 */
export function countByFilter(list: PluginInfo[]): FilterCounts {
  let disabled = 0
  for (const p of list) if (p.disabled) disabled += 1
  return { all: list.length, enabled: list.length - disabled, disabled }
}

/**
 * 按分类与搜索词过滤。搜索词大小写不敏感、只匹配插件名；空白搜索词等于不过滤。
 *
 * 不 trim 插件名（它来自后端清单，前导/行尾空白不是我们的数据），只 trim 查询词：
 * 用户在输入框里多打一个空格不该把列表清空。
 */
export function filterPlugins(
  list: PluginInfo[],
  filter: PluginFilter,
  query: string
): PluginInfo[] {
  const q = query.trim().toLowerCase()
  return list.filter((p) => {
    if (filter === 'enabled' && p.disabled) return false
    if (filter === 'disabled' && !p.disabled) return false
    return !q || p.name.toLowerCase().includes(q)
  })
}
