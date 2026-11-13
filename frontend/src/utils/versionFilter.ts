import type { DshVersionEntry } from '@/serverapi'

/** 版本弹窗里「隐藏 alpha」的过滤规则（纯函数，便于单元测试）。
 *
 *  只藏**本地用不着**的 alpha 行：已安装 / 使用中 / 正在下载的那一版一律保留 ——
 *  否则用户在列表里看不到自己在跑的那一版，也就没法切走或删掉它；正在下载的 alpha
 *  被藏掉，进度条会凭空消失。
 *
 *  `keep` 里还要放**列表里版本号最高的那一版**（`ServerVersions.latest`）：概览页的
 *  「有更新」红点就是按这一版算的（后端 refreshDshStatus），若它恰好是未安装的 alpha
 *  而被藏掉，红点就会指向一个看不见、也下载不了的行。
 */
/** 版本号里带 alpha 预发布标识（`0.2.2-alpha.3`）即为 alpha 版。
 *
 *  刻意**不看 npm dist-tag**：`alpha` 标签只挂在其中一版上（其余 `-alpha.x` 版本没有
 *  任何标签），靠 tags 判断会漏掉大半 alpha 版本；反过来手动把 `alpha` 标签指到别的
 *  版本上也不该改变「哪一版是 alpha」这个事实。
 */
export function isAlphaVersion(version: string): boolean {
  return /(^|[-.])alpha([.-]|$)/i.test(version)
}

export function visibleVersions(
  rows: DshVersionEntry[],
  hideAlpha: boolean,
  keep: string[] = [],
): DshVersionEntry[] {
  if (!hideAlpha) return rows
  const kept = new Set(keep.filter((v) => v !== ''))
  return rows.filter((row) =>
    row.installed === true
    || row.active === true
    || kept.has(row.version)
    || !isAlphaVersion(row.version),
  )
}
