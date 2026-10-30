import { describe, expect, it } from 'vitest'
import {
  balanceLevel,
  currencySymbol,
  formatBalance,
  formatNonZeroWallets,
  formatWallets,
} from '@/utils/money'

// 这些规则与 dsh 自己的账号面板保持一致（截断而非四舍五入），改动前先看 money.ts 的注释。
describe('money', () => {
  it('currencySymbol', () => {
    expect(currencySymbol('CNY')).toBe('¥')
    expect(currencySymbol('USD')).toBe('$')
  })

  it('formats zero / positive / negative balances', () => {
    expect(formatBalance('0', 'CNY')).toBe('¥0.00')
    expect(formatBalance('12.345', 'CNY')).toBe('¥12.34')
    expect(formatBalance('1234.5', 'CNY')).toBe('¥1,234.50')
    expect(formatBalance('-12.345', 'CNY')).toBe('-¥12.34')
    expect(formatBalance('-0.001', 'CNY')).toBe('-¥0.01')
  })

  it('keeps sub-cent positive balances visible', () => {
    expect(formatBalance('0.001', 'CNY')).toBe('<¥0.01')
  })

  it('renders an unreadable amount as an em dash, never as 0.00 or a bare currency sign', () => {
    expect(formatBalance('--', 'CNY')).toBe('—')
    expect(formatBalance('', 'CNY')).toBe('—')
    expect(formatBalance('   ', 'CNY')).toBe('—')
    expect(formatBalance('NaN', 'USD')).toBe('—')
    // 多个钱包里有一个读不出来：其余照常显示，不整体降级
    expect(formatWallets([
      { currency: 'CNY', balance: '1.20' },
      { currency: 'USD', balance: '--' },
    ])).toBe('¥1.20 + —')
  })

  it('joins multiple wallets and hides zero bonus', () => {
    expect(formatWallets([
      { currency: 'CNY', balance: '1.20' },
      { currency: 'USD', balance: '3.40' },
    ])).toBe('¥1.20 + $3.40')
    expect(formatNonZeroWallets([
      { currency: 'CNY', balance: '0' },
      { currency: 'CNY', balance: '2.00' },
    ])).toBe('¥2.00')
  })

  it('balanceLevel uses the lowest wallet and unknown for unreadable values', () => {
    expect(balanceLevel([{ currency: 'CNY', balance: '10' }])).toBe('high')
    expect(balanceLevel([{ currency: 'CNY', balance: '3' }])).toBe('mid')
    expect(balanceLevel([{ currency: 'CNY', balance: '2.99' }])).toBe('low')
    expect(balanceLevel([{ currency: 'CNY', balance: '--' }])).toBe('unknown')
    expect(balanceLevel([])).toBe('unknown')
  })
})
