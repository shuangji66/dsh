// src/utils/clipboard.ts —— 剪贴板写入的唯一实现（终端复制、插件名点击复制都用它）。
//
// 优先用异步 Clipboard API；它在**非安全上下文**下不可用 —— 本项目常见：经反代端口以
// http 访问时 window.isSecureContext === false，navigator.clipboard 直接不存在。
// 因此退回 execCommand('copy') 的临时 textarea 方案；Clipboard API 自身也可能因权限
// 策略 reject，同样回落到兜底实现。
//
// 兜底用的 textarea 放在屏幕外：终端的「长按粘贴」也依赖临时 textarea，但那是另一个
// 场景（唤起系统编辑菜单），两者互不影响。
export function copyText(text: string) {
  if (navigator.clipboard && window.isSecureContext) {
    navigator.clipboard.writeText(text).catch(() => legacyCopy(text))
  } else {
    legacyCopy(text)
  }
}

function legacyCopy(text: string) {
  const ta = document.createElement('textarea')
  ta.value = text
  ta.style.position = 'fixed'
  ta.style.left = '-9999px'
  ta.style.top = '-9999px'
  ta.style.opacity = '0'
  document.body.appendChild(ta)
  ta.focus()
  ta.select()
  try {
    document.execCommand('copy')
  } catch {
    /* 忽略 */
  }
  document.body.removeChild(ta)
}
