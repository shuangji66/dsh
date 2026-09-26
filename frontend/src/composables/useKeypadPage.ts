// src/composables/useKeypadPage.ts — 终端页底部辅助键条的当前页（模块级共享）。
//
// 为什么共享而不是每个实例各存一份：键条虽然当前只有终端页一个实例，但切到其他页面
// 再切回来（KeepAlive 缓存终端视图）时不应跳回第一页——正在打标点时切走再切回，页就没了。
// 两页：0 = 功能键/方向键/常用符号，1 = 常见标点。
// 切页由键条两侧的竖长条按钮触发，**不循环**（到边界即停）：第一页只有「下一页」按钮，
// 第二页只有「上一页」按钮，UI 上也不存在越界操作。
import { ref } from 'vue'

export const KEYPAD_PAGES = 2

const page = ref(0)

/** 只读：当前页序号（0 起） */
export function useKeypadPage() {
  /** 翻页：dir = -1 上一页 / +1 下一页（到边界即停，不回绕） */
  function step(dir: number) {
    page.value = Math.min(KEYPAD_PAGES - 1, Math.max(0, page.value + dir))
  }
  return { page, step }
}
