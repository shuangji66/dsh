// src/utils/randomId.ts

/**
 * 生成一个 v4 UUID 字符串（用于本地列表 key、持久化条目的唯一 id）。
 *
 * 背景：`crypto.randomUUID` 是**安全上下文（secure context）专属** API —— 通过
 * 明文 HTTP + LAN 地址或域名访问控制台时，该方法是 `undefined`，
 * 直接 `crypto.randomUUID()` 会抛 `TypeError`。
 * 控制台页面由 `admin.go` 直接提供、不经 `proxy.go` 的反代注入（那里只对 dsh
 * 页面注入 polyfill），因此这里必须自带兜底。
 *
 * `crypto.getRandomValues` 不受安全上下文限制，故优先用它自行拼装 UUID；
 * 仅当它也不可用时才退化为时间戳 + 随机数（非加密强度，但足以做本地 key）。
 */
export function randomId(): string {
  const c = globalThis.crypto

  // 安全上下文（localhost / HTTPS）：原生实现最优先。
  if (c && typeof c.randomUUID === 'function') return c.randomUUID()

  // 非安全上下文：getRandomValues 仍可用，按 RFC 4122 v4 手工拼装。
  if (c && typeof c.getRandomValues === 'function') {
    const b = c.getRandomValues(new Uint8Array(16))
    b[6] = (b[6] & 0x0f) | 0x40 // version 4
    b[8] = (b[8] & 0x3f) | 0x80 // variant 10
    const hex = Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('')
    return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
  }

  // 极端兜底：Web Crypto 完全不可用。非加密强度，仅保证唯一性。
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
}
