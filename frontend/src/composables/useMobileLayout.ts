// src/composables/useMobileLayout.ts — 「是否按移动端（触屏）布局渲染」的判定。
//
// 用途：触屏专属 UI 的显隐判据（当前只有终端页的底部辅助键条 KeypadBar）。
//
// 为什么不能按视口宽度判定：iPad 的 CSS 宽度是 768 / 834 / 1024px，全部 ≥ Tailwind 的
// md 断点（768px），用 `md:hidden` 这类宽度断点会把 iPad 判成桌面 —— 辅助键条被隐藏，
// 触屏上就再也没有 ESC/Tab/Ctrl/Alt/方向键可用。屏幕大不等于有键盘：大屏触屏必须按
// 「输入能力」判定，宽度只作为「窄窗口也当移动端用」的补充。
//
// 三条判据，任一成立即按移动端布局：
//   ① 视口窄（<768px）：延续既有行为——桌面浏览器的小窗口 / 窄窗口仍按移动端渲染；
//   ② 主指针为粗指针且无悬停：手机 / 平板 / iPad 的常规状态；
//   ③ 触屏平台的 UA 兜底：触屏设备一旦接上鼠标 / 触控板，WebKit、Blink 都会把**主**
//      指针改报成 (pointer: fine) + hover: hover（iPad 接妙控键盘/触控板即如此），
//      ② 随即失效。此时用「UA 是移动/平板平台 且 maxTouchPoints > 0」兜底：
//      真 Mac 的 UA 同样是 Macintosh，但 maxTouchPoints 为 0；Windows / Linux 触屏
//      笔记本的 UA 不含这些平台标识，判定不受影响，仍是桌面布局。
import { readonly, ref, type Ref } from 'vue'

// 与 Tailwind md 断点互补：md 是 `(width >= 768px)`，这里取 `(max-width: 767px)`。
const MOBILE_LAYOUT_QUERY = '(max-width: 767px), (hover: none) and (pointer: coarse)'

// 移动 / 平板平台标识（iPadOS 的 Safari UA 是 "Macintosh; Intel Mac OS X"，
// 与 macOS 无法从 UA 区分，只能靠 maxTouchPoints 配合，见上）。
const TOUCH_PLATFORM_RE = /Android|iPhone|iPad|iPod|Macintosh/i

function isTouchPlatform(): boolean {
  if (typeof navigator === 'undefined') return false
  return (
    (navigator.maxTouchPoints ?? 0) > 0 && TOUCH_PLATFORM_RE.test(navigator.userAgent || '')
  )
}

const query =
  typeof window !== 'undefined' && window.matchMedia
    ? window.matchMedia(MOBILE_LAYOUT_QUERY)
    : null

const isMobileLayout = ref((query?.matches ?? false) || isTouchPlatform())

// 视口跨过 768px（旋转 / 缩放窗口）或输入能力变化时同步；③ 的 UA 判定是静态的，
// 每次重算一遍即可（幂等且无副作用）。
query?.addEventListener('change', () => {
  isMobileLayout.value = query.matches || isTouchPlatform()
})

/**
 * 只读：当前是否按移动端（触屏）布局渲染。
 * 全局设备属性（同一页面所有调用者共享一份结果），无需生命周期钩子。
 */
export function useMobileLayout(): Readonly<Ref<boolean>> {
  return readonly(isMobileLayout)
}

// ---------- 「宽布局」判定（平板档，用于键条双页并排） ----------
//
// 判据就是 Tailwind 的 md 断点（>=768px）：手机竖屏 <768 → 单页 + 左右切换；
// 平板（iPad 768/834/1024）及以上 → 两页并排同时显示。
// 不要另发明断点数值：这里真正要问的是「宽度够不够并排摆下两页」。
const WIDE_LAYOUT_QUERY = '(min-width: 768px)'

const wideQuery =
  typeof window !== 'undefined' && window.matchMedia
    ? window.matchMedia(WIDE_LAYOUT_QUERY)
    : null

const isWideLayout = ref(wideQuery?.matches ?? false)

wideQuery?.addEventListener('change', () => {
  isWideLayout.value = wideQuery.matches
})

/**
 * 只读：当前是否宽布局（>=md 断点）。键条据此决定「两页并排」还是「单页 + 切换」。
 */
export function useWideLayout(): Readonly<Ref<boolean>> {
  return readonly(isWideLayout)
}
