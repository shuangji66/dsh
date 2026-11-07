// fnosApp.ts —— 判断当前页面是否运行在「飞牛 App」的内置 WebView 里，并读取 App 版本。
//
// 飞牛 App 的 WebView 在 UA 里带 `FNAppType/<类型>` 与 `FNAppVer/<版本>`；桌面浏览器与
// 普通手机浏览器都没有。用途：App 内某些依赖宿主桥接的能力（如目录授权）与浏览器行为
// 不同 —— **1.38.0 之前**的 App 里目录授权改走平台的网页授权页（见 utils/fnosAuth.ts），
// 1.38.0 起官方修复了原生 pickUserFile 的个人目录授权，直接用宿主选择器。

/** 飞牛 App 原生目录选择器修复个人目录授权的首个版本。 */
export const FNOS_APP_NATIVE_DIRECTORY_FIX = '1.38.0'

export function isFnosApp(userAgent = navigator.userAgent || ''): boolean {
  return /FNAppType\//.test(userAgent)
}

/** 从 UA 读 App 版本（`FNAppVer/1.37.3`）；不在 App 内或 UA 缺该字段时为 null。 */
export function fnosAppVersion(userAgent = navigator.userAgent || ''): string | null {
  const match = /FNAppVer\/(\d+(?:\.\d+)*)/.exec(userAgent)
  return match ? match[1] : null
}

/** 版本按数字逐段比较（1.37.10 < 1.38.0；1.38 等价 1.38.0）；读不到版本一律 false。 */
export function isFnosAppAtLeast(version: string, userAgent = navigator.userAgent || ''): boolean {
  const current = fnosAppVersion(userAgent)
  if (!current) return false
  const left = current.split('.').map((part) => Number.parseInt(part, 10) || 0)
  const right = version.split('.').map((part) => Number.parseInt(part, 10) || 0)
  const length = Math.max(left.length, right.length)
  for (let i = 0; i < length; i += 1) {
    const a = left[i] ?? 0
    const b = right[i] ?? 0
    if (a !== b) return a > b
  }
  return true
}

/**
 * 目录授权是否必须绕行网页授权页。
 *
 * 只有 App 分支需要：1.38.0 之前原生 pickUserFile 会把个人目录按共享授权处理并报
 * E_APP_AUTH；桌面浏览器 / 桌面宿主本来就走各自的流程，与此无关。
 */
export function fnosAppNeedsWebDirectoryAuth(userAgent = navigator.userAgent || ''): boolean {
  return isFnosApp(userAgent) && !isFnosAppAtLeast(FNOS_APP_NATIVE_DIRECTORY_FIX, userAgent)
}
