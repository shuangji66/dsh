import { describe, expect, it } from 'vitest'
import { resolveCardUpdates } from '@/utils/pluginUpdates'
import type { PluginInfo } from '@/serverapi'

const plugin = (name: string, version: string): PluginInfo => ({ name, version })

describe('resolveCardUpdates', () => {
  it('prefers a fresh manual check over the background result', () => {
    const updates = resolveCardUpdates(
      [plugin('demo', '1.0.0')],
      { demo: { status: 'done', latest: '1.2.0', hasUpdate: true, current: '1.0.0' } },
      { demo: { latestVersion: '1.1.0', hasUpdate: true } }
    )
    expect(updates.demo).toEqual({ hasUpdate: true, latest: '1.2.0' })
  })

  // 检查之后在页面之外换过版本（终端里 dsh plugin add）：旧结论不能再生效，否则会显示
  // 「1.2.0 → 1.1.0」的降级箭头、黄色按钮真把插件装回旧版。
  it('ignores a stale manual check and falls back to the background result', () => {
    const updates = resolveCardUpdates(
      [plugin('demo', '1.2.0')],
      { demo: { status: 'done', latest: '1.1.0', hasUpdate: true, current: '1.0.0' } },
      { demo: { latestVersion: '1.3.0', hasUpdate: true } }
    )
    expect(updates.demo).toEqual({ hasUpdate: true, latest: '1.3.0' })
  })

  it('drops a stale manual check when the background has nothing either', () => {
    const updates = resolveCardUpdates(
      [plugin('demo', '1.2.0')],
      { demo: { status: 'done', latest: '1.1.0', hasUpdate: true, current: '1.0.0' } },
      {}
    )
    expect(updates.demo).toEqual({ hasUpdate: false, latest: '' })
  })

  it('requires the background latest to differ from the installed version', () => {
    const updates = resolveCardUpdates(
      [plugin('demo', '1.1.0')],
      {},
      { demo: { latestVersion: '1.1.0', hasUpdate: true } }
    )
    expect(updates.demo).toEqual({ hasUpdate: false, latest: '' })
  })

  // 手动检查的结论（即使「已是最新」）优先于后台缓存 —— 后者可能还没到下一次小时级检测。
  it('lets a fresh "no update" manual result override the background result', () => {
    const updates = resolveCardUpdates(
      [plugin('demo', '2.0.0')],
      { demo: { status: 'done', hasUpdate: false, current: '2.0.0' } },
      { demo: { latestVersion: '2.1.0', hasUpdate: true } }
    )
    expect(updates.demo).toEqual({ hasUpdate: false, latest: '' })
  })

  it('treats a manual result without current as usable (older backend / state)', () => {
    const updates = resolveCardUpdates(
      [plugin('demo', '1.0.0')],
      { demo: { status: 'done', latest: '1.1.0', hasUpdate: true } },
      {}
    )
    expect(updates.demo).toEqual({ hasUpdate: true, latest: '1.1.0' })
  })

  it('returns one entry per plugin, defaulting to no update', () => {
    const updates = resolveCardUpdates([plugin('a', '1.0.0'), plugin('b', '2.0.0')], {}, {})
    expect(updates).toEqual({
      a: { hasUpdate: false, latest: '' },
      b: { hasUpdate: false, latest: '' }
    })
  })
})
