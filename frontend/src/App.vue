<script setup lang="ts">
import { RouterView, useRoute } from 'vue-router'
import { computed, ref, watch, onMounted, onBeforeUnmount } from 'vue'
import { useTheme } from '@/composables/useTheme'
import { useI18n } from '@/composables/useI18n'
import { useConsolePrefs } from '@/composables/useConsolePrefs'
import { useKeyboardInset } from '@/composables/useKeyboardInset'
import { useSettingsStore } from '@/stores/settings'
import Toast from '@/components/Toast.vue'
import router from './router'
import { icons } from '@/utils/icons'

const route = useRoute()
const { t, locale, setLocale } = useI18n()
const settings = useSettingsStore()
const { defaultPage } = useConsolePrefs()
const { themeMode, cycleTheme } = useTheme() // 初始化/跟随系统主题 + 侧边栏主题切换

// 软键盘遮挡量（--kb-inset）：底部导航栏与终端页据此抬到键盘之上，
// 否则固定底栏会停在键盘背后（见 composable 内的说明）。
useKeyboardInset()

// 侧边栏折叠状态
const collapsed = ref(localStorage.getItem('sidebar-collapsed') === 'true')
watch(collapsed, (val) => {
  localStorage.setItem('sidebar-collapsed', String(val))
})

const toggleCollapse = () => {
  collapsed.value = !collapsed.value
}

// 导航项：label 用 i18n 翻译。用 as const 固定 icon 为字面量联合类型，
// 模板里 icons[item.icon] 才能被 vue-tsc 正确推导（否则 icon 退化为 string）。
const nav = [
  { name: 'overview', labelKey: 'nav_overview', icon: 'overview' },
  { name: 'settings', labelKey: 'nav_settings', icon: 'settings' },
  { name: 'directory', labelKey: 'nav_directory', icon: 'folder' },
  { name: 'plugins', labelKey: 'nav_plugins', icon: 'plugin' },
  { name: 'terminal', labelKey: 'nav_terminal', icon: 'terminal' },
  { name: 'logs', labelKey: 'nav_logs', icon: 'logs' }
] as const

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

// ===== 图标：统一放在 utils/icons.ts（侧边栏与子页面标题栏共用同一套） =====

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

  // 全局禁止鼠标「按住拖动」触发原生拖拽：控制台是应用式 UI，拖动任何东西都不该冒出
  // 浏览器的半透明拖拽快照（最典型的是拖内联 SVG 图标 —— Chromium 把内联 <svg> 当图片
  // 一样可拖，而整个控制台到处是 v-html 注入的图标）。这里在**捕获阶段**拦掉 dragstart，
  // 比逐个元素写 draggable="false" 可靠：新加的元素自动生效。
  //
  // 只拦 dragstart，不碰 dragover/drop —— 从系统里拖文件进页面（将来若做拖放上传）不受影响；
  // 将来要加拖拽排序之类的功能，给对应元素标上 data-allow-drag 即可放行。
  const blockDrag = (ev: DragEvent) => {
    const target = ev.target as HTMLElement | null
    if (target && typeof target.closest === 'function' && target.closest('[data-allow-drag]')) return
    ev.preventDefault()
  }
  document.addEventListener('dragstart', blockDrag, true)
  onBeforeUnmount(() => {
    document.removeEventListener('dragstart', blockDrag, true)
  })
})
</script>

<template>
  <div class="min-h-screen bg-bg dark:bg-[#0B0B0F] text-ink dark:text-[#EDEDF0] transition-colors">
    <Toast />
    <!-- ===== 桌面端：左侧边栏 (md 及以上) =====
         浮动圆角面板（与页面留出边距），宽度在 w-16 / w-48 之间过渡折叠。
         折叠动效统一走「图标不动、文字用 max-w + opacity 收起」：
         直接 v-if 掉文字会让宽度骤变、文字闪现，这里改用可过渡的最大宽度。 -->
    <aside
      class="hidden md:flex fixed inset-y-0 left-0 my-3 ml-3 bg-surface dark:bg-[#111115] border border-line dark:border-[#2A2A32] rounded-2xl shadow-card flex-col z-10 overflow-hidden transition-all duration-300"
      :class="collapsed ? 'w-16' : 'w-48'">

      <!-- 顶部：Logo + 标题（整行可点击折叠/展开），右侧箭头仅在展开时出现 -->
      <div class="flex items-center gap-1 px-2 h-14 shrink-0 border-b border-line dark:border-[#2A2A32]">
        <button
          @click="toggleCollapse"
          class="flex items-center min-w-0 flex-1 rounded-xl py-2 transition-all duration-300 hover:bg-black/5 dark:hover:bg-white/5 group"
          :class="collapsed ? 'justify-center px-1' : 'justify-start px-1.5'"
          :title="collapsed ? t('sidebar_expand') : t('sidebar_collapse')"
        >
          <!-- 「展开/折叠」按钮的图标：鲸鱼（icons.whale，自带配色，父级的 text-* 不影响它）。
               源 SVG 带 800px 固定尺寸，已去掉并改为由这里的 w-6 h-6（24px）控制 ——
               在 32px 的圆角方框里留出呼吸感，与导航图标（20px）比例协调。 -->
          <span
            class="w-8 h-8 rounded-xl bg-brand/10 flex items-center justify-center shrink-0 transition-transform duration-500 group-hover:scale-110"
          >
            <span class="w-6 h-6 inline-block" v-html="icons.whale"></span>
          </span>
          <span
            class="font-display text-sm font-semibold whitespace-nowrap overflow-hidden text-ink dark:text-white transition-all duration-300 ease-in-out"
            :class="collapsed ? 'opacity-0 max-w-0 ml-0' : 'opacity-100 max-w-[8rem] ml-2.5'"
          >Harness</span>
        </button>
        <button
          v-if="!collapsed"
          @click="toggleCollapse"
          class="w-7 h-7 shrink-0 rounded-lg flex items-center justify-center text-ink-soft dark:text-[#A6A6AD] hover:text-ink dark:hover:text-white hover:bg-black/5 dark:hover:bg-white/5 transition-all"
          :title="t('sidebar_collapse')"
          v-html="icons.chevronLeft"
        ></button>
      </div>

      <!-- 导航链接：展开时左对齐 + 悬停右移，折叠时图标居中 + 悬停放大 -->
      <nav class="flex-1 min-h-0 overflow-y-auto flex flex-col gap-1 p-2">
        <button v-for="item in nav" :key="item.name" @click="navigate(item.name)"
          class="w-full flex items-center rounded-xl text-sm font-medium transition-all duration-300 active:scale-95"
          :class="[
            current === item.name
              ? 'bg-brand text-white shadow-glow font-semibold'
              : 'text-ink-soft dark:text-[#A6A6AD] hover:bg-black/5 dark:hover:bg-white/5 hover:text-ink dark:hover:text-white',
            collapsed ? 'px-2.5 py-2 justify-center hover:scale-105' : 'px-3.5 py-2.5 justify-start hover:translate-x-1'
          ]" :title="t(item.labelKey)">
          <span class="w-5 h-5 flex-shrink-0 inline-block" v-html="icons[item.icon]"></span>
          <span
            class="whitespace-nowrap overflow-hidden transition-all duration-300 ease-in-out"
            :class="collapsed ? 'opacity-0 max-w-0 ml-0' : 'opacity-100 max-w-[8rem] ml-3'"
          >{{ t(item.labelKey) }}</span>
        </button>
      </nav>

      <!-- 底部：主题 / 语言（展开时左右并排，折叠时上下堆叠；文字同样用 max-w 过渡收起） -->
      <div class="border-t border-line dark:border-[#2A2A32] p-2 shrink-0">
        <div class="flex w-full transition-all duration-300"
          :class="collapsed ? 'flex-col items-center gap-1.5' : 'flex-row gap-2'">
          <button
            class="flex items-center justify-center rounded-xl bg-black/[0.03] dark:bg-white/[0.06] text-ink-soft dark:text-[#A6A6AD] hover:text-ink dark:hover:text-white hover:bg-black/5 dark:hover:bg-white/10 transition-all duration-300 active:scale-95 group overflow-hidden"
            :class="collapsed ? 'w-10 h-10' : 'flex-1 h-10 px-2.5'"
            :title="themeTitle"
            @click="cycleTheme">
            <span class="w-5 h-5 flex-shrink-0 inline-block transition-transform duration-300 group-hover:scale-110 group-hover:rotate-12" v-html="themeIcon"></span>
            <span
              class="text-xs font-medium whitespace-nowrap overflow-hidden transition-all duration-300 ease-in-out"
              :class="collapsed ? 'opacity-0 max-w-0 ml-0' : 'opacity-100 max-w-[5rem] ml-2'"
            >{{ t('theme_label') }}</span>
          </button>
          <button
            class="flex items-center justify-center rounded-xl bg-black/[0.03] dark:bg-white/[0.06] text-ink-soft dark:text-[#A6A6AD] hover:text-ink dark:hover:text-white hover:bg-black/5 dark:hover:bg-white/10 transition-all duration-300 active:scale-95 group overflow-hidden"
            :class="collapsed ? 'w-10 h-10' : 'flex-1 h-10 px-2.5'"
            :title="languageTitle"
            @click="cycleLanguage">
            <span class="w-5 h-5 flex-shrink-0 inline-block transition-transform duration-300 group-hover:scale-110 group-hover:-rotate-12" v-html="icons.globe"></span>
            <span
              class="text-xs font-medium whitespace-nowrap overflow-hidden transition-all duration-300 ease-in-out"
              :class="collapsed ? 'opacity-0 max-w-0 ml-0' : 'opacity-100 max-w-[5rem] ml-2'"
            >{{ t('language_label') }}</span>
          </button>
        </div>
      </div>
    </aside>

    <!-- ===== 移动端：底部导航栏 (小于 md) =====
         高度 = 3.5rem 内容高度 + 底部安全区（--bottom-nav-h，与 main / 终端页一致）：
         box-sizing: border-box 下 padding-bottom 由安全区吃掉，图标仍居中在安全区之上；
         左右安全区（横屏刘海）一并留出，避免图标贴边或被遮挡。
         bottom 恒为 0：底栏必须稳稳贴底 —— 拖动页面时地址栏伸缩会让视口高度变化，
         一旦拿视口量当偏移量，底栏就会上下乱窜。键盘弹起时整条底栏由 style.css 的
         html[data-kb] 规则隐去，终端页改为贴合键盘（功能键栏落在键盘顶边）。 -->
    <nav
      class="mobile-bottom-nav flex md:hidden fixed bottom-0 left-0 right-0 bg-surface dark:bg-[#111115] border-t border-line dark:border-[#2A2A32] items-center justify-center z-10 gap-[18px]"
      style="height: var(--bottom-nav-h); padding-left: calc(env(safe-area-inset-left) + 1rem); padding-right: calc(env(safe-area-inset-right) + 1rem); padding-bottom: env(safe-area-inset-bottom);">
      <button v-for="item in nav" :key="item.name" @click="navigate(item.name)"
        class="flex flex-col items-center justify-center w-12 h-12 rounded-lg text-xl transition-all duration-200 active:scale-90 cursor-pointer"
        :class="current === item.name ? 'text-brand scale-105' : 'text-ink-soft dark:text-[#A6A6AD] hover:bg-black/5 dark:hover:bg-white/5'"
        :title="t(item.labelKey)">
        <span class="w-6 h-6 inline-block" v-html="icons[item.icon]"></span>
      </button>
    </nav>

    <!-- ===== 主内容区域（使用 KeepAlive 缓存终端组件） =====
         底部预留 = 底部导航高度（含安全区）+ 键盘遮挡高度。键盘弹起时 --bottom-nav-h
         被归零（底栏同时隐藏），这里就只剩键盘遮挡高度，尾部内容不会被键盘盖住。
         左侧留白 = 侧边栏左边距(0.75rem) + 侧边栏宽度 + 间隙(0.75rem)：展开 13.5rem、
         折叠 5.5rem，与 aside 的 w-48 / w-16 严格对应，两侧必须同步改。 -->
    <main class="pl-0 pb-[calc(var(--bottom-nav-h)_+_var(--kb-inset,0px))] md:pb-0 transition-all duration-300" :class="collapsed ? 'md:pl-[5.5rem]' : 'md:pl-[13.5rem]'">
      <KeepAlive>
        <RouterView />
      </KeepAlive>
    </main>
  </div>
</template>