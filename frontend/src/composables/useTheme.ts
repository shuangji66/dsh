// src/composables/useTheme.ts
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'

type ThemeMode = 'light' | 'dark' | 'system'

// 主题色的两套值：与 index.html 首屏内联脚本、style.css 的页面底色保持一致。
// 深色是 App.vue 根节点的 #0B0B0F，浅色是 body 的 #FAFAFA。
const THEME_COLOR_DARK = '#0B0B0F'
const THEME_COLOR_LIGHT = '#FAFAFA'

function isThemeMode(v: unknown): v is ThemeMode {
  return v === 'light' || v === 'dark' || v === 'system'
}

// localStorage 在隐私模式/被禁用时会抛错：主题回退到 system，而不是让整个应用起不来。
function readStoredTheme(): ThemeMode {
  try {
    const v = localStorage.getItem('theme-mode')
    return isThemeMode(v) ? v : 'system'
  } catch {
    return 'system'
  }
}

// 从 localStorage 读取，若无效则默认为 'system'
const themeMode = ref<ThemeMode>(readStoredTheme())
const systemPrefersDark = ref(window.matchMedia('(prefers-color-scheme: dark)').matches)

// 控制台是否处于深色模式（供各视图 —— 尤其终端 xterm 配色 —— 跟随切换）
const isDark = computed(
  () => themeMode.value === 'dark' || (themeMode.value === 'system' && systemPrefersDark.value)
)

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
  // 移动端地址栏/状态栏颜色跟随主题；首屏那次由 index.html 的内联脚本先做。
  const meta = document.querySelector('meta[name="theme-color"]')
  if (meta) meta.setAttribute('content', isDark ? THEME_COLOR_DARK : THEME_COLOR_LIGHT)
}

function setTheme(mode: ThemeMode) {
  themeMode.value = mode
  try {
    localStorage.setItem('theme-mode', mode)
  } catch {
    /* 隐私模式：本次会话内仍生效 */
  }
  applyTheme(mode)
}

// mediaQuery 放在 useTheme 内部：App / 终端页 / 设置页都会调用它，模块级变量会让
// 后挂载的那个覆盖前一个 —— 先挂载的组件卸载时删掉的是别人的监听器。
export function useTheme() {
  let mediaQuery: MediaQueryList | null = null

  const cycleTheme = () => {
    const modes: ThemeMode[] = ['light', 'dark', 'system']
    const idx = modes.indexOf(themeMode.value)
    const next = modes[(idx + 1) % modes.length]
    setTheme(next)
  }

  onMounted(() => {
    // 只应用模块级已读到的主题，**不要**在这里重读 localStorage：存储被禁用时
    // readStoredTheme() 会回退 system，把用户本次会话里刚选的主题顶掉（与 setTheme 的注释矛盾）。
    applyTheme(themeMode.value)

    // 监听系统主题变化
    mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
    const query = mediaQuery
    const handler = (e: MediaQueryListEvent) => {
      systemPrefersDark.value = e.matches
      if (themeMode.value === 'system') {
        applyTheme('system')
      }
    }
    query.addEventListener('change', handler)
    onBeforeUnmount(() => {
      query.removeEventListener('change', handler)
    })
  })

  return {
    themeMode, // 只读，使用 setTheme 修改
    isDark, // 深色模式指示（终端配色等跟随控制台切换）
    setTheme,
    cycleTheme,
    applyTheme,
  }
}