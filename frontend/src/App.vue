<script setup lang="ts">
  import { RouterView, useRoute } from 'vue-router'
import { computed, ref, watch, onMounted, onBeforeUnmount } from 'vue'
import { useTheme } from '@/composables/useTheme'
import Toast from '@/components/Toast.vue'
import router from './router' 

const route = useRoute()
const current = computed(() => (route.query.view as string) || 'settings')
const { themeMode, cycleTheme } = useTheme()

// ===== 侧边栏折叠状态 =====
const collapsed = ref(localStorage.getItem('sidebar-collapsed') === 'true')
watch(collapsed, (val) => {
  localStorage.setItem('sidebar-collapsed', String(val))
})

const toggleCollapse = () => {
  collapsed.value = !collapsed.value
}

const nav = [
  { name: 'settings', label: '控制台', icon: 'settings' },
  { name: 'directory', label: '工作区', icon: 'folder' },
  { name: 'terminal', label: '终端', icon: 'terminal' },
  { name: 'logs', label: '日志', icon: 'logs' }
]

// 子页面统一在 / 路径下通过 ?view= 切换；用 replace 避免产生历史记录
function navigate(name: string) {
  router.replace({ path: '/', query: { view: name } })
}

const themeIconName = computed(() => {
  switch (themeMode.value) {
    case 'light': return 'sun'
    case 'dark': return 'moon'
    default: return 'auto'
  }
})

// ===== 细线风格 SVG 图标（stroke 1.5，与设计语言一致） =====
const icons = {
  // 导航图标
  settings: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/></svg>`,
  folder: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>`,
  terminal: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/></svg>`,
  logs: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M4 4h16v16H4z"/><path d="M4 9h16"/><path d="M4 14h16"/><path d="M7 11.5h0M7 16.5h0"/></svg>`,
  // 主题图标
  sun: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></svg>`,
  moon: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>`,
  auto: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M8 16 L12 8 L16 16 M10 14 L14 14"/></svg>`,
  // 汉堡折叠图标
  menu: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><line x1="3" y1="6" x2="21" y2="6"/><line x1="3" y1="12" x2="21" y2="12"/><line x1="3" y1="18" x2="21" y2="18"/></svg>`,
  close: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>`
}

const menuIcon = computed(() => collapsed.value ? icons.close : icons.menu)

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
        :title="collapsed ? '展开' : '折叠'"
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
          ]" :title="item.label">
          <span class="w-5 h-5 flex-shrink-0 inline-block" v-html="icons[item.icon]"></span>
          <span v-if="!collapsed" class="whitespace-nowrap">{{ item.label }}</span>
        </button>
      </nav>

      <!-- 底部主题切换 -->
      <div class="mt-auto w-full px-2">
        <button
          @click="cycleTheme"
          class="flex items-center justify-center gap-2.5 w-full px-2 py-2.5 rounded-lg text-sm font-medium transition hover:bg-black/5 dark:hover:bg-white/5 text-ink-soft dark:text-[#A6A6AD]"
          :title="`主题：${themeMode}`"
        >
          <span class="w-5 h-5 flex-shrink-0 inline-block" v-html="icons[themeIconName]"></span>
          <span v-if="!collapsed" class="whitespace-nowrap">主题</span>
        </button>
      </div>
    </aside>

    <!-- ===== 移动端：底部导航栏 (小于 md) ===== -->
    <nav
      class="flex md:hidden fixed bottom-0 left-0 right-0 h-14 bg-surface dark:bg-[#111115] border-t border-line dark:border-[#2A2A32] items-center justify-around z-10 px-2"
      style="padding-bottom: env(safe-area-inset-bottom);">
      <button v-for="item in nav" :key="item.name" @click="navigate(item.name)"
        class="flex flex-col items-center justify-center w-12 h-12 rounded-lg text-xl transition hover:bg-black/5 dark:hover:bg-white/5 cursor-pointer"
        :class="current === item.name ? 'text-brand' : 'text-ink-soft dark:text-[#A6A6AD]'"
        :title="item.label">
        <span class="w-6 h-6 inline-block" v-html="icons[item.icon]"></span>
      </button>
      <!-- 主题切换按钮（移动端） -->
      <button
        @click="cycleTheme"
        class="flex flex-col items-center justify-center w-12 h-12 rounded-lg text-xl transition hover:bg-black/5 dark:hover:bg-white/5 text-ink-soft dark:text-[#A6A6AD]"
        :title="`主题：${themeMode}`"
      >
        <span class="w-6 h-6 inline-block" v-html="icons[themeIconName]"></span>
      </button>
    </nav>

    <!-- ===== 主内容区域 ===== -->
    <main class="pl-0 pb-14 md:pb-0 transition-all duration-300" :class="collapsed ? 'md:pl-16' : 'md:pl-44'">
      <RouterView />
    </main>
  </div>
</template>