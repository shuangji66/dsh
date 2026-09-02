// 控制台偏好（默认页面）——仅保存在浏览器 localStorage，
// 实时生效，不随“保存配置”持久化到后端。(主题由 useTheme、语言由 useI18n 管理)
import { ref } from 'vue'

export type DefaultPage = 'overview' | 'last' | 'settings' | 'directory' | 'plugins' | 'terminal' | 'logs'

const DEFAULT_PAGE_KEY = 'console-default-page'

const VALID_PAGES: DefaultPage[] = ['overview', 'last', 'settings', 'directory', 'plugins', 'terminal', 'logs']

function validDefaultPage(v: string | null): DefaultPage {
  return (VALID_PAGES as string[]).includes(v || '') ? (v as DefaultPage) : 'overview'
}

// 默认页面偏好（模块级共享，reactive）
const defaultPage = ref<DefaultPage>(validDefaultPage(localStorage.getItem(DEFAULT_PAGE_KEY)))

export function setDefaultPage(v: DefaultPage) {
  defaultPage.value = v
  localStorage.setItem(DEFAULT_PAGE_KEY, v)
}

export function useConsolePrefs() {
  return { defaultPage, setDefaultPage, validDefaultPage }
}