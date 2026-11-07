// src/composables/useAccountCardVisible.ts —— 概览页「DeepSeek 账号」卡片在**移动端**是否显示。
//
// 用途只有一个：不需要登录 DeepSeek 官方账号的用户，可以在移动端把这张卡片收起来，
// 免得概览页第一屏被一个用不上的卡片占掉。开关在设置页「控制台设置」卡片里（那张卡片
// 本身只在 <md 出现，见 SettingsView）。
//
// 三条边界（改之前先看清）：
//   1. **只管显示/隐藏卡片，不动登录状态**：凭据、账号库、轮询与已登录的账号一概不变，
//      关掉开关不等于退出登录；重新打开开关，卡片照旧显示当前账号。
//   2. **只影响移动端**：桌面端（>=md）始终显示。判定在 OverviewView 里用
//      `useWideLayout()`（就是 md 断点）与这里的开关取「或」，因此桌面端永远为真 ——
//      桌面有地方摆这张卡片，也没必要多一个「关了以后找不回来」的状态。
//   3. **只存这个浏览器**（localStorage，不落盘到后端）：与主题 / 语言同一类偏好，
//      换浏览器/清缓存即回默认值（默认显示）。
import { readonly, ref, type Ref } from 'vue'

const STORAGE_KEY = 'overview-account-card'

// localStorage 在隐私模式 / 被禁用的 WebView 里连读都会抛错：读不到就按默认值（显示），
// 不让一个偏好设置把整个应用拖崩。
function readStored(): boolean {
  try {
    // 只有显式存过 '0' 才算关闭；没存过（null）或存了别的东西都回默认的「显示」。
    return localStorage.getItem(STORAGE_KEY) !== '0'
  } catch {
    return true
  }
}

// 模块级共享：设置页的开关与概览页的卡片必须看到同一份值（跨组件、跨页面切换）。
const accountCardVisible = ref(readStored())

function setAccountCardVisible(visible: boolean) {
  accountCardVisible.value = visible
  try {
    localStorage.setItem(STORAGE_KEY, visible ? '1' : '0')
  } catch {
    // 存储被禁用：本次会话内仍然生效，只是刷新后回到默认值。
  }
}

/** 只读：移动端是否显示概览页的 DeepSeek 账号卡片（默认 true）。 */
export function useAccountCardVisible(): Readonly<Ref<boolean>> {
  return readonly(accountCardVisible)
}

/** 写入偏好（设置页开关调用）：立即生效 + 写入 localStorage。 */
export { setAccountCardVisible }
