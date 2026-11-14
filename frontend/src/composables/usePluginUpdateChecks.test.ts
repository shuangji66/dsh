import { beforeEach, describe, expect, it } from 'vitest'
import { usePluginUpdateChecks } from '@/composables/usePluginUpdateChecks'

describe('usePluginUpdateChecks', () => {
  const { checks } = usePluginUpdateChecks()

  beforeEach(() => {
    checks.value = {}
  })

  // 模块级单例：PluginsView 卸载再挂载（切子页面）后，手动检查的结论仍在。
  it('keeps manual check results across composable calls', () => {
    checks.value = { demo: { status: 'done', latest: '1.1.0', hasUpdate: true } }
    const afterRemount = usePluginUpdateChecks()
    expect(afterRemount.checks.value.demo).toEqual({ status: 'done', latest: '1.1.0', hasUpdate: true })
  })
})
