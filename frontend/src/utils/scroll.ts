// 滚动位置记忆：给「列表自动刷新」用（不是给页面切换用）。
//
// 为什么需要它：插件页在启停 / 卸载 / 重置之后会主动重拉列表，而重拉期间列表被加载态
// 替换，页面高度骤降，浏览器随即把滚动位置夹到 0 —— 用户操作完就被甩回列表顶部。
// 这里不改变加载态本身，只在重拉前后把位置记下来再放回去。内容变短（卸载掉一张卡片）时
// 浏览器自己会夹住，因此还原的是「大致位置」而不是逐像素的原位。
//
// 滚动容器有两只（见 App.vue 的 [data-scroll-lock] 说明）：桌面端是文档（window），
// 移动端是标了 [data-scroll-lock] 的 <main>（高 = 100dvh − 底栏高、overflow-y: auto）。
// 两端都记，还原时各写各的 —— 另一端通常本来就是 0，先比较再写，避免无意义的滚动。
const SCROLLER_SELECTOR = '[data-scroll-lock]'

export interface ScrollSnapshot {
  windowY: number
  scrollerTop: number
}

// 取「真正的滚动容器」：桌面端 <main> 是 overflow: visible（滚动在文档上），此时返回 null，
// 与 useBodyScrollLock 用的是同一条判据。
function scrollContainer(): HTMLElement | null {
  const el = document.querySelector<HTMLElement>(SCROLLER_SELECTOR)
  if (!el) return null
  const overflowY = getComputedStyle(el).overflowY
  return overflowY === 'auto' || overflowY === 'scroll' ? el : null
}

/** 记录当前滚动位置（文档 + 移动端滚动容器）。 */
export function captureScroll(): ScrollSnapshot {
  return { windowY: window.scrollY, scrollerTop: scrollContainer()?.scrollTop ?? 0 }
}

/**
 * 还原滚动位置。必须在新内容渲染完成之后调用（还原前 `await nextTick()`）：
 * 内容还没长回来时写回的 scrollTop / scrollY 会被浏览器夹成更小的值。
 */
export function restoreScroll(snapshot: ScrollSnapshot | null): void {
  if (!snapshot) return
  if (window.scrollY !== snapshot.windowY) window.scrollTo({ top: snapshot.windowY })
  const el = scrollContainer()
  if (el && el.scrollTop !== snapshot.scrollerTop) el.scrollTop = snapshot.scrollerTop
}
