import { describe, expect, it } from 'vitest'
import { formatBytes, formatDateTime } from '@/utils/format'

describe('format', () => {
  it('formats bytes with 1024 base', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(1023)).toBe('1023 B')
    expect(formatBytes(1024)).toBe('1.0 KB')
    expect(formatBytes(1024 * 1024 * 1.5)).toBe('1.5 MB')
    expect(formatBytes(-1)).toBe('—')
  })

  it('formats datetimes and tolerates invalid input', () => {
    expect(formatDateTime('2026-10-30T02:00:00+08:00')).toBe('2026-10-30 02:00:00')
    expect(formatDateTime('')).toBe('—')
    expect(formatDateTime('not-a-date')).toBe('—')
  })
})
