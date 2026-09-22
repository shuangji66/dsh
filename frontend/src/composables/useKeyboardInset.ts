// 移动端软键盘桥接：识别「软键盘是否弹起」以及「它遮住了多高 / 还能画多大」，把结果
// 落到三个全局量上：
//
//	html[data-kb]（属性）：键盘弹起时存在。style.css 据此隐藏移动端底部导航栏、把底部
//	  预留（--bottom-nav-h）归零，并把终端页高度切成 --vv-h；
//	--kb-inset（CSS 变量，px）：被键盘遮住的高度（只在「布局视口不随键盘收缩」的浏览器里
//	  非零），主内容区据此预留底部空间；
//	--vv-h（CSS 变量，px）：键盘弹起时的可视视口高度，即键盘顶边以上的全部可用空间。
//
// 为什么不能用「视口变矮」单条判据：滚动手势会让 Safari 收起/展开地址栏，可视视口跟着
// 变几十到上百像素，只看高度就会在滚动时误判成键盘弹起 —— 底栏忽隐忽现、预留值来回跳，
// 表现为底栏「上下乱窜」。因此键盘判据取「有可编辑元素获得焦点 + 视口比无键盘基准矮了
// 至少 KEYBOARD_MIN」两条同时成立；且未判定为键盘时一律把 --kb-inset 归零（滚动过程中
// 视口变化不再影响任何布局量，底栏稳稳贴在底部）。
//
// 基准 baseline 取历史最大的布局视口高度（document.documentElement.clientHeight）：
//   - 浏览器只缩可视视口（iOS Safari / Chrome resizes-visual）：clientHeight 不变，
//     baseline - 可视底边 就是键盘高度，--kb-inset 也等于它；
//   - 浏览器连布局视口一起缩（Android WebView adjustResize / resizes-content）：
//     clientHeight 跟着变小，--kb-inset 自然为 0（fixed 元素本就被浏览器顶上去了，
//     不需要再抬）；键盘判据仍成立，因为 baseline 记住的是收缩前的值。
//
// 只在 App 根部调用一次（全局变量，多组件重复调用会互相覆盖）。
import { onBeforeUnmount, onMounted } from 'vue'

// 1px 容差：键盘收起时个别浏览器会留下亚像素差值，避免底栏抖动。
const EPSILON = 1
// 键盘至少遮住这么高（最矮的横屏键盘也在 150px 上下）：与「可编辑元素有焦点」一起用，
// 把地址栏收起/展开这类几十像素的视口变化排除在外。
const KEYBOARD_MIN = 120
// 宽度变化超过这么多像素视为旋转/窗口尺寸变化：无键盘基准要重新学习，否则横屏后
// 视口比竖屏基准矮一大截，会被一直误判成「键盘还弹着」（底栏就再也不出现了）。
const WIDTH_RESET = 40

let active = 0
let baseline = 0
let lastWidth = 0

// editableFocused 报告当前是否聚焦在会唤起软键盘的控件上（含终端里 xterm 的隐藏
// textarea 与粘贴用的临时 textarea）。
function editableFocused(): boolean {
  const el = document.activeElement as HTMLElement | null
  if (!el) return false
  if (el.isContentEditable) return true
  const tag = el.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA'
}

export function useKeyboardInset() {
  let raf = 0

  const apply = () => {
    raf = 0
    const vv = window.visualViewport
    if (!vv) return
    // 双指缩放（scale ≠ 1）不是键盘：跳过更新，免得底栏随缩放浮动。
    if (Math.abs(vv.scale - 1) > 0.01) return

    const width = window.innerWidth
    if (Math.abs(width - lastWidth) > WIDTH_RESET) {
      lastWidth = width
      baseline = 0
    }

    const layoutBottom = document.documentElement.clientHeight
    // offsetTop 也要算进去：iOS 键盘弹起时可能整体平移可视视口（页面被顶上去）。
    const visibleBottom = vv.height + vv.offsetTop
    if (layoutBottom > baseline) baseline = layoutBottom

    const open = editableFocused() && baseline - visibleBottom > KEYBOARD_MIN
    const root = document.documentElement

    if (open) {
      // 这里才写值：滚动、地址栏伸缩都不再改变布局量，底栏因此不会上下乱窜。
      const overlap = layoutBottom - visibleBottom
      root.style.setProperty('--kb-inset', overlap > EPSILON ? Math.round(overlap) + 'px' : '0px')
      root.style.setProperty('--vv-h', Math.round(vv.height) + 'px')
      root.setAttribute('data-kb', '1')
    } else {
      root.style.setProperty('--kb-inset', '0px')
      root.style.removeProperty('--vv-h')
      root.removeAttribute('data-kb')
    }
  }

  // 键盘弹起/收起、页面滚动时 visualViewport 都会连续触发 resize/scroll，
  // 用 rAF 合并到每帧一次。
  const schedule = () => {
    if (!raf) raf = requestAnimationFrame(apply)
  }

  onMounted(() => {
    active++
    schedule()
    // 聚焦/失焦时立刻重算（键盘的出现/消失本身会触发 visualViewport 事件，这里只是
    // 让状态切换更跟手）；旋转与窗口尺寸变化可能不经过 visualViewport，故额外监听。
    document.addEventListener('focusin', schedule)
    document.addEventListener('focusout', schedule)
    window.addEventListener('resize', schedule)
    const vv = window.visualViewport
    if (vv) {
      vv.addEventListener('resize', schedule)
      vv.addEventListener('scroll', schedule)
    }
  })

  onBeforeUnmount(() => {
    document.removeEventListener('focusin', schedule)
    document.removeEventListener('focusout', schedule)
    window.removeEventListener('resize', schedule)
    const vv = window.visualViewport
    if (vv) {
      vv.removeEventListener('resize', schedule)
      vv.removeEventListener('scroll', schedule)
    }
    if (raf) {
      cancelAnimationFrame(raf)
      raf = 0
    }
    active--
    if (active <= 0) {
      active = 0
      baseline = 0
      lastWidth = 0
      const root = document.documentElement
      root.style.removeProperty('--kb-inset')
      root.style.removeProperty('--vv-h')
      root.removeAttribute('data-kb')
    }
  })
}
