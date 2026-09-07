<script setup lang="ts">
import { RouterView, useRoute } from 'vue-router'
import { computed, ref, watch, onMounted, onBeforeUnmount } from 'vue'
import { useTheme } from '@/composables/useTheme'
import { useI18n } from '@/composables/useI18n'
import { useConsolePrefs } from '@/composables/useConsolePrefs'
import { useSettingsStore } from '@/stores/settings'
import Toast from '@/components/Toast.vue'
import router from './router'

const route = useRoute()
const { t, locale, setLocale } = useI18n()
const settings = useSettingsStore()
const { defaultPage } = useConsolePrefs()
const { themeMode, cycleTheme } = useTheme() // 初始化/跟随系统主题 + 侧边栏主题切换

// 侧边栏折叠状态
const collapsed = ref(localStorage.getItem('sidebar-collapsed') === 'true')
watch(collapsed, (val) => {
  localStorage.setItem('sidebar-collapsed', String(val))
})

const toggleCollapse = () => {
  collapsed.value = !collapsed.value
}

// 导航项：label 用 i18n 翻译
const nav = [
  { name: 'overview', labelKey: 'nav_overview', icon: 'overview' },
  { name: 'settings', labelKey: 'nav_settings', icon: 'settings' },
  { name: 'directory', labelKey: 'nav_directory', icon: 'folder' },
  { name: 'plugins', labelKey: 'nav_plugins', icon: 'plugin' },
  { name: 'terminal', labelKey: 'nav_terminal', icon: 'terminal' },
  { name: 'logs', labelKey: 'nav_logs', icon: 'logs' }
]

// 打开时的默认页面
function defaultView(): string {
  if (defaultPage.value === 'last') {
    return localStorage.getItem('last-view') || 'overview'
  }
  if (defaultPage.value) return defaultPage.value
  return 'overview'
}

// 当前子页面：优先 URL 查询参数，否则用配置的默认页面
const current = computed(() => {
  const q = route.query.view as string | undefined
  if (q && nav.some((n) => n.name === q)) return q
  return defaultView()
})

// 控制台 HTML 标题随子页面切换动态显示（只显示子页面标题，如“概览/设置”，无前缀）
watch(
  [current, () => locale.value],
  () => {
    const item = nav.find((n) => n.name === current.value)
    document.title = item ? t(item.labelKey) : ''
  },
  { immediate: true }
)

// 子页面统一在 / 路径下通过 ?view= 切换；用 replace 避免产生历史记录。
// 同时记录“最后访问的页面”，供“保持退出时的页面”使用。
function navigate(name: string) {
  localStorage.setItem('last-view', name)
  router.replace({ path: '/', query: { view: name } })
}

let appliedDefault = false
// 配置加载完成后，若 URL 未指定子页面，则跳转到默认页面
watch(
  () => settings.config,
  (c) => {
    if (appliedDefault) return
    appliedDefault = true
    if (!route.query.view) {
      router.replace({ path: '/', query: { view: defaultView() } })
    }
  },
  { immediate: true }
)

// ===== 细线风格 SVG 图标（stroke 1.5，与设计语言一致） =====
const icons = {
  // 导航图标
  overview: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/></svg>`,
  settings: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/></svg>`,
  plugin: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M19.439 7.85c-.049.322.059.648.289.878l1.568 1.568c.47.47.706 1.087.706 1.704s-.235 1.233-.706 1.704l-1.611 1.611a.98.98 0 0 1-.837.276c-.47-.07-.802-.48-.968-.925a2.501 2.501 0 1 0-3.214 3.214c.446.166.855.497.925.968a.979.979 0 0 1-.276.837l-1.61 1.61a2.404 2.404 0 0 1-1.705.707 2.402 2.402 0 0 1-1.704-.706l-1.568-1.568a1.026 1.026 0 0 0-.877-.29c-.493.074-.84.504-1.02.968a2.5 2.5 0 1 1-3.237-3.237c.464-.18.894-.527.967-1.02a1.026 1.026 0 0 0-.289-.877l-1.568-1.568A2.402 2.402 0 0 1 1.998 12c0-.617.236-1.234.706-1.704L4.315 8.685a.979.979 0 0 1 .837-.276c.47.07.802.48.968.925a2.501 2.501 0 1 0 3.214-3.214c-.166-.446-.477-.855-.968-.925a.979.979 0 0 1-.837-.276L4.315 3.6a2.403 2.403 0 0 1-.706-1.704"/></svg>`,
  terminal: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/></svg>`,
  folder: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>`,
  logs: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M4 4h16v16H4z"/><path d="M4 9h16"/><path d="M4 14h16"/><path d="M7 11.5h0M7 16.5h0"/></svg>`,
  // 汉堡折叠图标
  menu: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><line x1="3" y1="6" x2="21" y2="6"/><line x1="3" y1="12" x2="21" y2="12"/><line x1="3" y1="18" x2="21" y2="18"/></svg>`,
  close: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>`,
  // 主题切换图标：按当前模式显示 浅色(太阳)/深色(月亮)/跟随系统(显示器)
  sun: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/></svg>`,
  moon: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/></svg>`,
  monitor: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect width="14" height="10" x="5" y="4" rx="2"/><line x1="12" x2="12" y1="20" y2="16"/><line x1="8" x2="16" y1="20" y2="20"/></svg>`,
  // 语言切换图标：地球
  globe: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20"/><path d="M2 12h20"/></svg>`
}

const menuIcon = computed(() => collapsed.value ? icons.menu : icons.close)

// 主题图标与提示文案随当前模式变化：light=太阳 / dark=月亮 / system=显示器
const themeIcon = computed(() =>
  themeMode.value === 'dark' ? icons.moon : themeMode.value === 'light' ? icons.sun : icons.monitor
)
const themeTitle = computed(() => t('theme_' + themeMode.value))

// 语言切换：点击在 中文 ↔ English 之间循环（与设置页语言选择共享同一 locale）
function cycleLanguage() {
  setLocale(locale.value === 'zh' ? 'en' : 'zh')
}
// 语言提示文案：显示当前语言名
const languageTitle = computed(() => t(locale.value === 'zh' ? 'lang_zh' : 'lang_en'))

// 阻止浏览器后退/滑动返回切换子页面
onMounted(() => {
  // 用 replace 切换子页面，URL 始终保持在 / 路径下；兜底处理浏览器后退/手势，
  // 若触发 popstate 则把当前子页面重新压回，阻断切页
  const blockBack = () => {
    router.replace({ path: '/', query: { view: current.value } })
  }
  window.addEventListener('popstate', blockBack)
  onBeforeUnmount(() => {
    window.removeEventListener('popstate', blockBack)
  })
})
</script>

<template>
  <div class="min-h-screen bg-bg dark:bg-[#0B0B0F] text-ink dark:text-[#EDEDF0] transition-colors">
    <Toast />
    <!-- ===== 桌面端：左侧边栏 (md 及以上) ===== -->
    <aside
      class="hidden md:flex fixed inset-y-0 left-0 bg-surface dark:bg-[#111115] border-r border-line dark:border-[#2A2A32] flex-col py-4 transition-all duration-300 z-10"
      :class="collapsed ? 'w-16' : 'w-44'">
      <!-- 汉堡折叠按钮 -->
      <button
        @click="toggleCollapse"
        class="w-9 h-9 rounded-lg flex items-center justify-center text-lg transition hover:bg-black/5 dark:hover:bg-white/5 text-ink-soft dark:text-[#A6A6AD] mb-5 mx-auto"
        :title="collapsed ? t('sidebar_expand') : t('sidebar_collapse')"
        v-html="menuIcon"
      />

      <!-- 导航链接：展开与折叠均居中对齐 -->
      <nav class="w-full flex flex-col space-y-1.5 px-2">
        <button v-for="item in nav" :key="item.name" @click="navigate(item.name)"
          class="flex items-center justify-center gap-2.5 w-full px-2 py-2.5 rounded-lg text-sm font-medium transition-all duration-150 cursor-pointer"
          :class="[
            current === item.name
              ? 'bg-brand text-white shadow-glow'
              : 'text-ink-soft dark:text-[#A6A6AD] hover:bg-black/5 dark:hover:bg-white/5 hover:text-ink dark:hover:text-white'
          ]" :title="t(item.labelKey)">
          <span class="w-5 h-5 flex-shrink-0 inline-block" v-html="icons[item.icon]"></span>
          <span v-if="!collapsed" class="whitespace-nowrap">{{ t(item.labelKey) }}</span>
        </button>
      </nav>

      <!-- 底部：主题切换按钮（折叠仅图标；展开图标+“主题”，居中；点击循环 浅色→深色→跟随系统） -->
      <button
        class="mt-auto flex items-center justify-center gap-2.5 w-full px-2 py-2.5 rounded-lg text-sm font-medium transition-all duration-150 cursor-pointer text-ink-soft dark:text-[#A6A6AD] hover:bg-black/5 dark:hover:bg-white/5 hover:text-ink dark:hover:text-white"
        :title="themeTitle"
        @click="cycleTheme">
        <span class="w-5 h-5 flex-shrink-0 inline-block" v-html="themeIcon"></span>
        <span v-if="!collapsed" class="whitespace-nowrap">{{ t('theme_label') }}</span>
      </button>

      <!-- 底部：语言切换按钮（折叠仅图标；展开图标+“语言”，居中；点击循环 中文↔English） -->
      <button
        class="mt-1 flex items-center justify-center gap-2.5 w-full px-2 py-2.5 rounded-lg text-sm font-medium transition-all duration-150 cursor-pointer text-ink-soft dark:text-[#A6A6AD] hover:bg-black/5 dark:hover:bg-white/5 hover:text-ink dark:hover:text-white"
        :title="languageTitle"
        @click="cycleLanguage">
        <span class="w-5 h-5 flex-shrink-0 inline-block" v-html="icons.globe"></span>
        <span v-if="!collapsed" class="whitespace-nowrap">{{ t('language_label') }}</span>
      </button>
    </aside>

    <!-- ===== 移动端：底部导航栏 (小于 md) ===== -->
    <nav
      class="flex md:hidden fixed bottom-0 left-0 right-0 h-14 bg-surface dark:bg-[#111115] border-t border-line dark:border-[#2A2A32] items-center justify-center z-10 gap-[18px]"
      style="padding-left: calc(env(safe-area-inset-left) + 1rem); padding-right: calc(env(safe-area-inset-right) + 1rem); padding-bottom: env(safe-area-inset-bottom);">
      <button v-for="item in nav" :key="item.name" @click="navigate(item.name)"
        class="flex flex-col items-center justify-center w-12 h-12 rounded-lg text-xl transition hover:bg-black/5 dark:hover:bg-white/5 cursor-pointer"
        :class="current === item.name ? 'text-brand' : 'text-ink-soft dark:text-[#A6A6AD]'"
        :title="t(item.labelKey)">
        <span class="w-6 h-6 inline-block" v-html="icons[item.icon]"></span>
      </button>
    </nav>

    <!-- ===== 主内容区域（使用 KeepAlive 缓存终端组件） ===== -->
    <main class="pl-0 pb-14 md:pb-0 transition-all duration-300" :class="collapsed ? 'md:pl-16' : 'md:pl-44'">
      <KeepAlive>
        <RouterView />
      </KeepAlive>
    </main>
  </div>
</template>