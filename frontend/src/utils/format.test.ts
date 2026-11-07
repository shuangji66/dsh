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

  it('formats datetimes in the local timezone and tolerates invalid input', () => {
    // **不要写死「+08:00 → 02:00:00」**：formatDateTime 的契约是「按本机时区渲染」，
    // 写死某个时区的钟表值会让 UTC 的 CI / 容器直接失败（本测试在 TZ=UTC 下曾报
    // `2026-10-29 18:00:00` vs `2026-10-30 02:00:00`）。
    // 改成：按本地时间构造 → toISOString() 转成同一瞬间 → 格式化回来。任何时区都成立，
    // 而在非 UTC 机器上，一旦有人把 getHours() 改成 getUTCHours()，第一条就会挂。
    const local = new Date(2026, 9, 30, 14, 5, 9)
    expect(formatDateTime(local.toISOString())).toBe('2026-10-30 14:05:09')
    // 顺带覆盖补零（月份/日/时/分/秒都补齐两位）。
    const padded = new Date(2026, 0, 2, 3, 4, 5)
    expect(formatDateTime(padded.toISOString())).toBe('2026-01-02 03:04:05')
    expect(formatDateTime('')).toBe('—')
    expect(formatDateTime('not-a-date')).toBe('—')
  })
})
