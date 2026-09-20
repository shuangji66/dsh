// 弹窗打开时锁定页面滚动：阻止在弹窗背后继续滚动/拖动页面。
//
// 实现要点（不要退化成单纯给 html/body 加 overflow: hidden）：
// 本项目 html/body 是 height:100% 的布局，根元素一旦 overflow:hidden，视口没有
// 可滚动区域，浏览器会把滚动位置直接夹到 0 —— 打开弹窗时背景会“跳回顶部”，
// 关闭后也回不到原位。因此这里改用「body 固定定位 + 负 top 抵消」：
// 背景视觉上纹丝不动，滚动彻底失效（含触摸设备的橡皮筋滚动），关闭时再把
// 滚动位置还原。
//
// 用模块级引用计数而不是单个组件直接写死样式：控制台里的弹窗会叠加（如“更新”
// 弹窗之上再弹“取消二次确认”），必须等最后一个弹窗关闭才解锁，否则先关掉的
// 那层会提前把页面滚动放回来。
import { getCurrentScope, onScopeDispose, toRef, watch } from 'vue'
import type { MaybeRefOrGetter } from 'vue'

let lockCount = 0
let savedScrollY = 0
// 锁定前的内联样式，解锁时逐个还原（不覆盖调用方自己写的样式）
let savedHtmlOverflow = ''
let savedBodyOverflow = ''
let savedBodyPosition = ''
let savedBodyTop = ''
let savedBodyLeft = ''
let savedBodyRight = ''
let savedBodyWidth = ''
let savedBodyPaddingRight = ''

function acquire() {
  const html = document.documentElement
  const body = document.body
  // 先量滚动条宽度：设置 overflow 之后滚动条会消失，量不到差值
  const scrollbarGap = window.innerWidth - html.clientWidth
  savedScrollY = window.scrollY
  savedHtmlOverflow = html.style.overflow
  savedBodyOverflow = body.style.overflow
  savedBodyPosition = body.style.position
  savedBodyTop = body.style.top
  savedBodyLeft = body.style.left
  savedBodyRight = body.style.right
  savedBodyWidth = body.style.width
  savedBodyPaddingRight = body.style.paddingRight

  html.style.overflow = 'hidden'
  body.style.position = 'fixed'
  body.style.top = `-${savedScrollY}px`
  body.style.left = '0'
  body.style.right = '0'
  body.style.width = '100%'
  // 滚动条消失会让页面变宽，用等宽 padding 补回来，避免内容横向跳动
  if (scrollbarGap > 0) body.style.paddingRight = `${scrollbarGap}px`
}

function release() {
  const html = document.documentElement
  const body = document.body
  html.style.overflow = savedHtmlOverflow
  body.style.overflow = savedBodyOverflow
  body.style.position = savedBodyPosition
  body.style.top = savedBodyTop
  body.style.left = savedBodyLeft
  body.style.right = savedBodyRight
  body.style.width = savedBodyWidth
  body.style.paddingRight = savedBodyPaddingRight
  // 样式还原后文档才能滚动，此时把位置放回弹窗打开前的那一屏
  if (savedScrollY) window.scrollTo(0, savedScrollY)
}

/**
 * 页面滚动锁：`active` 为真时锁定滚动，为假时释放。
 *
 * @param active 弹窗（或任意浮层）是否打开；支持普通布尔、ref、computed、getter
 * @example
 * const dialogOpen = ref(false)
 * useBodyScrollLock(dialogOpen)
 * useBodyScrollLock(() => a.value || b.value) // 多个弹窗合并判断
 */
export function useBodyScrollLock(active: MaybeRefOrGetter<boolean>) {
  if (typeof document === 'undefined') return
  let locked = false
  watch(
    toRef(active),
    (on) => {
      if (on === locked) return
      locked = on
      if (on) {
        if (lockCount++ === 0) acquire()
      } else if (lockCount > 0 && --lockCount === 0) {
        release()
      }
    },
    { immediate: true }
  )
  // 弹窗还开着时组件被卸载（例如切换子页面），必须自己释放，否则页面会永久锁死
  if (getCurrentScope()) {
    onScopeDispose(() => {
      if (!locked) return
      locked = false
      if (lockCount > 0 && --lockCount === 0) release()
    })
  }
}
