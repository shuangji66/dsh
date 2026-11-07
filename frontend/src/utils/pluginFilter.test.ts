import { describe, expect, it } from 'vitest'
import { countByFilter, filterPlugins } from '@/utils/pluginFilter'
import type { PluginInfo } from '@/serverapi'

// disabled 只在「声明了 dsh.bundle、但不在 profile.bundles 里」时下发；普通依赖没有该字段。
const list: PluginInfo[] = [
  { name: 'dshmarket', version: '1.0.0', bundle: true, disabled: false },
  { name: 'dsh-fnos-advanced', version: '0.7.3', bundle: true, disabled: true },
  { name: '@scope/plain-dep', version: '2.0.0' },
  { name: 'DSH-Better-Sidebar', version: '0.26.1', bundle: true, disabled: true }
] as PluginInfo[]

describe('pluginFilter', () => {
  it('counts disabled bundles and keeps plain dependencies under "enabled"', () => {
    expect(countByFilter(list)).toEqual({ all: 4, enabled: 2, disabled: 2 })
    expect(countByFilter([])).toEqual({ all: 0, enabled: 0, disabled: 0 })
  })

  it('filters by category', () => {
    expect(filterPlugins(list, 'all', '').map((p) => p.name)).toEqual([
      'dshmarket',
      'dsh-fnos-advanced',
      '@scope/plain-dep',
      'DSH-Better-Sidebar'
    ])
    // 普通依赖没有「停用」这个状态，所以它落在「已启用」里。
    expect(filterPlugins(list, 'enabled', '').map((p) => p.name)).toEqual([
      'dshmarket',
      '@scope/plain-dep'
    ])
    expect(filterPlugins(list, 'disabled', '').map((p) => p.name)).toEqual([
      'dsh-fnos-advanced',
      'DSH-Better-Sidebar'
    ])
  })

  it('searches the plugin name case-insensitively and trims the query', () => {
    expect(filterPlugins(list, 'all', 'MARKET').map((p) => p.name)).toEqual(['dshmarket'])
    expect(filterPlugins(list, 'all', '  sidebar ').map((p) => p.name)).toEqual(['DSH-Better-Sidebar'])
    expect(filterPlugins(list, 'all', '   ').length).toBe(4)
    expect(filterPlugins(list, 'all', 'nothing-matches')).toEqual([])
  })

  it('combines category and query', () => {
    // 注意大小写：查询词统一 toLowerCase，`DSH-Better-Sidebar` 也匹配 "dsh-"。
    expect(filterPlugins(list, 'disabled', 'dsh-').map((p) => p.name)).toEqual([
      'dsh-fnos-advanced',
      'DSH-Better-Sidebar'
    ])
    expect(filterPlugins(list, 'enabled', 'advanced')).toEqual([])
  })
})
