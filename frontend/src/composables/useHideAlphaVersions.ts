// src/composables/useHideAlphaVersions.ts —— dsh 版本弹窗里的「隐藏alpha」开关。
//
// 与主题 / 语言同一类**纯浏览器偏好**：只写 localStorage，不落后端、不需要点保存，
// 换浏览器或清缓存就回到默认（显示 alpha）。设置项就在弹窗「刷新」那一行的左侧。
import { readonly, ref, type Ref } from 'vue'

const STORAGE_KEY = 'dsh-versions-hide-alpha'

// localStorage 在隐私模式 / 被禁用的 WebView 里连读都会抛错：读不到就按默认值（显示），
// 不让一个显示偏好把整个弹窗拖崩。
function readStored(): boolean {
  try {
    // 只有显式存过 '1' 才算开启；没存过（null）或存了别的东西都回默认的「显示」。
    return localStorage.getItem(STORAGE_KEY) === '1'
  } catch {
    return false
  }
}

// 模块级共享：弹窗反复打开、跨页面切换都读同一份值。
const hideAlpha = ref(readStored())

function setHideAlpha(value: boolean) {
  hideAlpha.value = value
  try {
    localStorage.setItem(STORAGE_KEY, value ? '1' : '0')
  } catch {
    // 存储被禁用：本次会话内仍生效，刷新后回默认。
  }
}

/** 只读：是否隐藏未安装的 alpha 版本（默认 false）。 */
export function useHideAlphaVersions(): Readonly<Ref<boolean>> {
  return readonly(hideAlpha)
}

export { setHideAlpha }
