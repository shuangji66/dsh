import { describe, expect, it } from 'vitest'
import { isAlphaVersion, visibleVersions } from '@/utils/versionFilter'
import type { DshVersionEntry } from '@/serverapi'

const v = (version: string, extra: Partial<DshVersionEntry> = {}): DshVersionEntry => ({ version, ...extra })

describe('visibleVersions', () => {
  it('关闭时原样返回', () => {
    const rows = [v('0.2.2-rc.3'), v('0.2.2-alpha.3')]
    expect(visibleVersions(rows, false)).toBe(rows)
  })

  it('按版本号识别 alpha（不看 dist-tag）', () => {
    expect(isAlphaVersion('0.2.2-alpha.3')).toBe(true)
    expect(isAlphaVersion('0.2.3-alpha.1')).toBe(true)
    expect(isAlphaVersion('0.2.2-rc.3')).toBe(false)
    expect(isAlphaVersion('0.2.1')).toBe(false)
  })

  it('藏掉未安装的 alpha，保留其它版本（alpha 行可以没有任何标签）', () => {
    const rows = [
      v('0.2.2-rc.3', { tags: ['next'] }),
      v('0.2.2-alpha.3'),
      v('0.2.2-alpha.2'),
      v('0.2.1-rc.5'),
    ]
    expect(visibleVersions(rows, true).map((r) => r.version)).toEqual(['0.2.2-rc.3', '0.2.1-rc.5'])
  })

  it('挂着 alpha 标签、但版本号不含 alpha 的行照旧显示', () => {
    const rows = [v('0.2.2-rc.4', { tags: ['alpha'] })]
    expect(visibleVersions(rows, true).map((r) => r.version)).toEqual(['0.2.2-rc.4'])
  })

  it('已安装 / 使用中的 alpha 不藏（否则看不到、也切不走）', () => {
    const rows = [
      v('0.2.2-alpha.3', { installed: true }),
      v('0.2.2-alpha.2', { installed: true, active: true }),
      v('0.2.2-alpha.1'),
    ]
    expect(visibleVersions(rows, true).map((r) => r.version)).toEqual(['0.2.2-alpha.3', '0.2.2-alpha.2'])
  })

  it('正在下载的那一版即使被藏也保留（进度条不能凭空消失）', () => {
    const rows = [v('0.2.2-alpha.4')]
    expect(visibleVersions(rows, true, ['0.2.2-alpha.4']).map((r) => r.version)).toEqual(['0.2.2-alpha.4'])
    expect(visibleVersions(rows, true)).toEqual([])
  })

  it('红点指向的最高版本即使未安装也保留（否则红点指向看不见的行）', () => {
    const rows = [
      v('0.2.3-alpha.2'),
      v('0.2.3-alpha.1'),
      v('0.2.2-rc.3', { tags: ['next'], installed: true, active: true }),
    ]
    expect(visibleVersions(rows, true, ['0.2.3-alpha.2']).map((r) => r.version))
      .toEqual(['0.2.3-alpha.2', '0.2.2-rc.3'])
  })

  it('没有 tags 的行一律保留', () => {
    expect(visibleVersions([v('9.9.9')], true).map((r) => r.version)).toEqual(['9.9.9'])
  })
})
