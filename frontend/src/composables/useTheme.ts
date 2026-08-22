// src/composables/useTheme.ts
import { ref, onMounted, onBeforeUnmount } from 'vue'

type ThemeMode = 'light' | 'dark' | 'system'

// 从 localStorage 读取，若无效则默认为 'system'
const themeMode = ref<ThemeMode>((localStorage.getItem('theme-mode') as ThemeMode) || 'system')
const systemPrefersDark = ref(window.matchMedia('(prefers-color-scheme: dark)').matches)

function applyTheme(mode: ThemeMode) {
  const isDark = mode === 'dark' || (mode === 'system' && systemPrefersDark.value)
  const root = document.documentElement
  if (isDark) {
    root.classList.add('dark')
    root.style.colorScheme = 'dark'
  } else {
    root.classList.remove('dark')
    root.style.colorScheme = 'light'
  }
  // 调试日志：可注释掉
  console.log(`[Theme] applied: ${mode}, isDark: ${isDark}, classList: ${root.classList}`)
}

function setTheme(mode: ThemeMode) {
  themeMode.value = mode
  localStorage.setItem('theme-mode', mode)
  applyTheme(mode)
  // 调试日志
  console.log(`[Theme] set to: ${mode}`)
}

let mediaQuery: MediaQueryList | null = null

export function useTheme() {
  const cycleTheme = () => {
    const modes: ThemeMode[] = ['light', 'dark', 'system']
    const idx = modes.indexOf(themeMode.value)
    const next = modes[(idx + 1) % modes.length]
    setTheme(next)
  }

  onMounted(() => {
    // 确保初始主题与 localStorage 一致
    const saved = localStorage.getItem('theme-mode') as ThemeMode | null
    if (saved && ['light', 'dark', 'system'].includes(saved)) {
      themeMode.value = saved
    } else {
      themeMode.value = 'system'
    }
    applyTheme(themeMode.value)

    // 监听系统主题变化
    mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
    const handler = (e: MediaQueryListEvent) => {
      systemPrefersDark.value = e.matches
      if (themeMode.value === 'system') {
        applyTheme('system')
      }
    }
    mediaQuery.addEventListener('change', handler)
    onBeforeUnmount(() => {
      mediaQuery?.removeEventListener('change', handler)
    })
  })

  return {
    themeMode, // 只读，使用 setTheme 修改
    setTheme,
    cycleTheme,
    applyTheme,
  }
}