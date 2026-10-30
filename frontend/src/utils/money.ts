// money.ts —— 余额展示：规则照抄 dsh 自己的账号面板（`dsh-client-ui-settings-account`
// 的 `formatBalance` / 调用点），否则同一个账号在 dsh 与控制台里会显示成两个数。
//
// dsh 的规则（原实现基于 Big.js）：
//   0            → `¥0.00`
//   < 0 且 > -0.01 → `-¥0.01`（欠款保底显示，不显示成 -0.00）
//   < 0 其余     → `-¥12.34`
//   0 < x < 0.01 → `<¥0.01`（子分余额，不四舍五入成 0.00）
//   其余         → `¥1,234.56`（**截断**到 2 位，不是四舍五入）
// 货币符号：CNY → ¥，其余 → $。
//
// 两点实现取舍：
//   1. 「是否 0 / 正负 / 是否小于 0.01」用 Number 判断 —— 这三个判断与浮点误差无关
//      （`Number('0.01') < 0.01` 为 false，边界稳定），不值得为此手写十进制比较器；
//   2. **输出**一律走字符串截断 + 手工千分位：`toFixed(2)` 会把 12.345 变成 12.35，
//      而 dsh 用 Big.roundDown（截断），两边金额对不上账；`toLocaleString` 又依赖运行时
//      区域设置（服务端渲染/老浏览器上未必存在），所以千分位自己插。

export interface WalletView {
  currency: string
  balance: string
}

/** 货币符号：与 dsh 的调用点一致（CNY → ¥，其余 → $）。 */
export function currencySymbol(currency: string): string {
  return currency === 'CNY' ? '¥' : '$'
}

/** 把无符号的数字文本截断到 2 位小数，并加千分位。 */
function fixed2Text(raw: string): string {
  const body = raw.replace(/^[+-]/, '')
  const [intRaw = '0', fracRaw = ''] = body.split('.')
  const int = intRaw.replace(/^0+(?=\d)/, '') || '0'
  const frac = (fracRaw + '00').slice(0, 2)
  return `${groupThousands(int)}.${frac}`
}

/** 三位一插的千分位（从右往左）。 */
function groupThousands(int: string): string {
  let out = ''
  for (let i = int.length; i > 0; i -= 3) {
    const chunk = int.slice(Math.max(0, i - 3), i)
    out = out ? `${chunk},${out}` : chunk
  }
  return out || '0'
}

/**
 * 格式化一个余额（平台给的是十进制字符串）。
 * 读不出数值（空串、`--`、平台换字段）时返回「—」：既不显示成「0.00」骗人，也不拼出
 * `¥--` 这种既不像金额又带着货币符号的串（与概览页「读不到就是 —」的约定一致）。
 */
export function formatBalance(amount: string, currency: string): string {
  const symbol = currencySymbol(currency)
  const raw = (amount ?? '').trim()
  const value = Number(raw)
  if (!raw || !Number.isFinite(value)) return '—'
  if (value === 0) return `${symbol}0.00`
  if (value < 0) return value > -0.01 ? `-${symbol}0.01` : `-${symbol}${fixed2Text(raw)}`
  if (value < 0.01) return `<${symbol}0.01`
  return `${symbol}${fixed2Text(raw)}`
}

/**
 * 一行展示多个钱包：dsh 用 ` + ` 连接（CSS `:before{content:" + "}`），这里保持同一形态。
 * 空列表返回空串，调用方据此回退到「已登录」这类文案。
 */
export function formatWallets(wallets?: WalletView[] | null): string {
  return (wallets ?? []).map((w) => formatBalance(w.balance, w.currency)).join(' + ')
}

/** 解析金额文本：只有非空且能解析成有限数字时才返回数值，否则 undefined。 */
function parseAmount(amount: string): number | undefined {
  const raw = (amount ?? '').trim()
  if (!raw) return undefined
  const value = Number(raw)
  return Number.isFinite(value) ? value : undefined
}

/** 金额文本是否为 0：只认能解析成有限数字且等于 0 的文本。 */
function isZeroAmount(amount: string): boolean {
  return parseAmount(amount) === 0
}

/**
 * 同 `formatWallets`，但金额为 0 的钱包不参与展示（赠送金用：没赠送就不占一行）。
 * 空串 / `--` 这类读不出数值的金额**不算 0** —— 平台一换字段宁可照常显示，别静默藏掉。
 */
export function formatNonZeroWallets(wallets?: WalletView[] | null): string {
  return formatWallets((wallets ?? []).filter((w) => !isZeroAmount(w.balance)))
}

/** 余额档位：页面据此上色（见 AccountCard）。 */
export type BalanceLevel = 'high' | 'mid' | 'low' | 'unknown'

/**
 * 余额档位：≥10 元 high（绿）/ 3–10 元 mid（黄）/ <3 元（含欠款）low（红）。
 * 多币种取**最低**的一档：宁可提示余额偏低，也别让另一种货币把颜色抬成绿色。
 * 没有可解析的金额（空列表 / `--`）返回 unknown —— 上色必须基于确定的数字。
 */
export function balanceLevel(wallets?: WalletView[] | null): BalanceLevel {
  const values = (wallets ?? [])
    .map((w) => parseAmount(w.balance))
    .filter((v): v is number => v !== undefined)
  if (!values.length) return 'unknown'
  const lowest = Math.min(...values)
  if (lowest >= 10) return 'high'
  if (lowest >= 3) return 'mid'
  return 'low'
}
