// useDialogA11y —— 弹窗的键盘/读屏基础能力（角色、焦点、Escape、背景 inert）。
//
// 控制台的弹窗都是「Teleport 到 body 的遮罩 + 面板」，此前只有鼠标能用：实测打开
// dsh 版本弹窗后焦点仍停在触发按钮上、没有 role/aria-modal、按 Escape 不关闭，
// Tab 还能跑到弹窗背后。这里把共性抽出来，各弹窗只需：
//   1. 在面板上挂 `ref="panel"`、`tabindex="-1"`、`role="dialog"`、`aria-modal="true"`、
//      `:aria-label="..."`；
//   2. 调一次 useDialogA11y(() => 可见性, panel, { onClose })。
//
// 三个实现要点：
//   - **模块级 stack**：弹窗会叠加（版本列表 → 切换确认），Escape 只能由最上层处理；
//   - **#app inert**：面板 Teleport 在 body，把 #app 设成 inert 就能一次性挡住背景的
//     焦点/读屏，不需要手写「把所有兄弟节点 tabindex=-1」；用引用计数支持叠加；
//   - **焦点归还**：打开前记下 activeElement，关闭时还原（组件被卸载也会走 onScopeDispose）。
import { getCurrentScope, nextTick, onScopeDispose, toValue, watch } from 'vue'
import type { MaybeRefOrGetter, Ref } from 'vue'

type InertElement = HTMLElement & { inert: boolean }

const stack: symbol[] = []
let inertCount = 0
let appWasInert = false

function appRoot(): InertElement | null {
  return (document.getElementById('app') as InertElement | null) ?? null
}

function lockBackground() {
  if (inertCount++ > 0) return
  const app = appRoot()
  if (!app) return
  appWasInert = app.inert
  app.inert = true
}

function unlockBackground() {
  if (inertCount === 0) return
  if (--inertCount > 0) return
  const app = appRoot()
  if (app) app.inert = appWasInert
}

// 可聚焦元素：排除 disabled / tabindex=-1 / 不可见（getClientRects 为空）的节点。
function focusable(panel: HTMLElement): HTMLElement[] {
  const nodes = panel.querySelectorAll<HTMLElement>(
    'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), ' +
      'select:not([disabled]), [tabindex]:not([tabindex="-1"])'
  )
  return Array.from(nodes).filter((el) => el.getClientRects().length > 0)
}

export interface DialogA11yOptions {
  /** Escape 时执行（通常是弹窗自己的 close()）。 */
  onClose?: () => void
}

export function useDialogA11y(
  visible: MaybeRefOrGetter<boolean>,
  panel: Ref<HTMLElement | null>,
  options: DialogA11yOptions = {}
) {
  if (typeof document === 'undefined') return

  const token = Symbol('dialog')
  let active = false
  let previousFocus: HTMLElement | null = null

  function onKeydown(e: KeyboardEvent) {
    // 只有最上层弹窗处理键盘：叠加时 Escape 不能把下面那层也关掉。
    if (stack[stack.length - 1] !== token) return
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      options.onClose?.()
      return
    }
    if (e.key !== 'Tab') return
    const el = panel.value
    if (!el) return
    const items = focusable(el)
    if (items.length === 0) {
      e.preventDefault()
      el.focus({ preventScroll: true })
      return
    }
    const first = items[0]
    const last = items[items.length - 1]
    const current = document.activeElement as HTMLElement | null
    // 焦点在面板本身（刚打开时）或不在面板里：显式送到首/末项，避免 Shift+Tab 跑到浏览器 UI。
    if (!current || current === el || !el.contains(current)) {
      e.preventDefault()
      ;(e.shiftKey ? last : first).focus()
      return
    }
    if (e.shiftKey && current === first) {
      e.preventDefault()
      last.focus()
    } else if (!e.shiftKey && current === last) {
      e.preventDefault()
      first.focus()
    }
  }

  async function activate() {
    if (active) return
    active = true
    // 必须在设置 #app inert 之前记住：inert 会让当前焦点落到 body。
    previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    stack.push(token)
    lockBackground()
    document.addEventListener('keydown', onKeydown, true)
    await nextTick()
    // 期间可能已经关闭，或被另一个弹窗盖住：只看最上层，避免抢焦点。
    if (!active || stack[stack.length - 1] !== token) return
    // 表单类弹窗用 data-autofocus 指定初始焦点（如快捷指令的「名称」输入框）；
    // 其余弹窗聚焦面板本身，由读屏器报出 aria-label，Tab 再进入具体控件。
    const target =
      panel.value?.querySelector<HTMLElement>('[data-autofocus]') ?? panel.value ?? null
    target?.focus({ preventScroll: true })
  }

  function deactivate() {
    if (!active) return
    active = false
    document.removeEventListener('keydown', onKeydown, true)
    const i = stack.lastIndexOf(token)
    if (i >= 0) stack.splice(i, 1)
    unlockBackground()
    const prev = previousFocus
    previousFocus = null
    if (prev?.isConnected) prev.focus({ preventScroll: true })
  }

  watch(
    () => toValue(visible),
    (on) => {
      if (on) void activate()
      else deactivate()
    },
    { immediate: true }
  )

  // 弹窗还开着就被卸载（切页 / v-if 关闭）：必须自己释放，否则 #app 会永久 inert。
  if (getCurrentScope()) onScopeDispose(deactivate)
}
