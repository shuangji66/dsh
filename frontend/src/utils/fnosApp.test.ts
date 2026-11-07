import { describe, expect, it } from 'vitest'
import {
  FNOS_APP_NATIVE_DIRECTORY_FIX,
  fnosAppNeedsWebDirectoryAuth,
  fnosAppVersion,
  isFnosApp,
  isFnosAppAtLeast,
} from '@/utils/fnosApp'

const appUa = (version: string) => 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) '
  + 'AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 '
  + `FNOS/1.2.1010 FNAppType/iOS FNAppVer/${version}`

describe('fnosApp', () => {
  it('detects the app WebView and reads its version', () => {
    expect(isFnosApp(appUa('1.37.3'))).toBe(true)
    expect(fnosAppVersion(appUa('1.37.3'))).toBe('1.37.3')
    expect(fnosAppVersion('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)')).toBeNull()
    expect(fnosAppVersion('Mozilla/5.0 FNAppType/iOS')).toBeNull()
  })

  it('compares versions numerically, not as strings', () => {
    expect(FNOS_APP_NATIVE_DIRECTORY_FIX).toBe('1.38.0')
    // 1.37.10 比 1.38.0 老（字符串比较会得出相反结论）。
    expect(isFnosAppAtLeast('1.38.0', appUa('1.37.10'))).toBe(false)
    expect(isFnosAppAtLeast('1.38.0', appUa('1.37.3'))).toBe(false)
    expect(isFnosAppAtLeast('1.38.0', appUa('1.38'))).toBe(true)
    expect(isFnosAppAtLeast('1.38.0', appUa('1.38.0.1'))).toBe(true)
    // 没有版本号就不是「够新」。
    expect(isFnosAppAtLeast('1.38.0', 'Mozilla/5.0 (Macintosh)')).toBe(false)
  })

  it('keeps web authorization only for app builds before 1.38.0', () => {
    expect(fnosAppNeedsWebDirectoryAuth(appUa('1.37.3'))).toBe(true)
    expect(fnosAppNeedsWebDirectoryAuth(appUa('1.38.0'))).toBe(false)
    // 不是 App：走桌面/浏览器分支，与这个兼容开关无关。
    expect(fnosAppNeedsWebDirectoryAuth('Mozilla/5.0 (Macintosh)')).toBe(false)
    // App 但 UA 里没有版本号：保守走网页授权。
    expect(fnosAppNeedsWebDirectoryAuth('Mozilla/5.0 FNAppType/iOS')).toBe(true)
  })
})
