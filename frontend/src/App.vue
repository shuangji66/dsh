<script setup lang="ts">
  import { RouterView, useRoute } from 'vue-router'
import { computed, ref, watch, onMounted } from 'vue'
import { useTheme } from '@/composables/useTheme'
import Toast from '@/components/Toast.vue'
import router from './router' 

const route = useRoute()
const current = computed(() => route.name as string)
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
  { name: 'settings', label: '控制台', to: '/', icon: 'settings' },
  { name: 'directory', label: '工作区', to: '/directory', icon: 'folder' },
  { name: 'terminal', label: '终端', to: '/terminal', icon: 'terminal' }
]

const themeIconName = computed(() => {
  switch (themeMode.value) {
    case 'light': return 'sun'
    case 'dark': return 'moon'
    default: return 'auto'
  }
})

// ===== Icon8 风格 SVG 图标 =====
const icons = {
  // 导航图标
  settings: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/></svg>`,
  folder: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>`,
  terminal: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/></svg>`,
  // 主题图标
  sun: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></svg>`,
  moon: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>`,
  auto: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M8 16 L12 8 L16 16 M10 14 L14 14"/></svg>`,
  // 汉堡折叠图标
  menu: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><line x1="3" y1="6" x2="21" y2="6"/><line x1="3" y1="12" x2="21" y2="12"/><line x1="3" y1="18" x2="21" y2="18"/></svg>`,
  close: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>`
}

const menuIcon = computed(() => collapsed.value ? icons.close : icons.menu)

// 阻止浏览器后退/滑动返回
onMounted(() => {
  // 每次导航完成后，替换历史记录中的当前条目为当前 URL，使后退失效
  router.afterEach(() => {
    window.history.replaceState(null, '', window.location.href)
  })
})
</script>

<template>
  <div class="min-h-screen bg-slate-50 dark:bg-slate-950 text-slate-900 dark:text-slate-100 transition-colors">
    <Toast />
    <!-- ===== 桌面端：左侧边栏 (md 及以上) ===== -->
    <aside
      class="hidden md:flex fixed inset-y-0 left-0 bg-white dark:bg-slate-900 border-r border-slate-200 dark:border-slate-800 flex-col items-center py-4 transition-all duration-300 z-10"
      :class="collapsed ? 'w-16' : 'w-48'">
      <!-- 汉堡折叠按钮（图标缩小为 text-xl） -->
      <button
        @click="toggleCollapse"
        class="w-10 h-10 rounded-lg flex items-center justify-center text-xl transition hover:bg-slate-100 dark:hover:bg-slate-800 text-slate-600 dark:text-slate-400 mb-4"
        :title="collapsed ? '展开' : '折叠'"
        v-html="menuIcon"
      />

      <!-- 导航链接，间距增大到 space-y-3 -->
      <div class="w-full flex flex-col space-y-3">
        <router-link v-for="item in nav" :key="item.name" :to="item.to"
          class="flex items-center gap-3 w-full px-3 py-2 rounded-lg transition hover:bg-slate-100 dark:hover:bg-slate-800"
          :class="[
            current === item.name ? 'bg-indigo-600 text-white' : 'text-slate-600 dark:text-slate-400',
            collapsed ? 'justify-center' : 'justify-start'
          ]" :title="item.label">
          <span class="w-6 h-6 flex-shrink-0 inline-block" v-html="icons[item.icon]"></span>
          <span v-if="!collapsed" class="text-sm whitespace-nowrap">{{ item.label }}</span>
        </router-link>
      </div>

      <!-- 底部主题切换 -->
      <div class="mt-auto w-full px-3">
        <button
          @click="cycleTheme"
          class="flex items-center gap-3 w-full px-3 py-2 rounded-lg transition hover:bg-slate-100 dark:hover:bg-slate-800 text-slate-600 dark:text-slate-400"
          :class="collapsed ? 'justify-center' : 'justify-start'"
          :title="`主题：${themeMode}`"
        >
          <span class="w-6 h-6 flex-shrink-0 inline-block" v-html="icons[themeIconName]"></span>
          <span v-if="!collapsed" class="text-sm whitespace-nowrap">主题</span>
        </button>
      </div>
    </aside>

    <!-- ===== 移动端：底部导航栏 (小于 md) ===== -->
    <nav
      class="flex md:hidden fixed bottom-0 left-0 right-0 h-14 bg-white dark:bg-slate-900 border-t border-slate-200 dark:border-slate-800 items-center justify-around z-10 px-2">
      <router-link v-for="item in nav" :key="item.name" :to="item.to"
        class="flex flex-col items-center justify-center w-12 h-12 rounded-lg text-xl transition hover:bg-slate-100 dark:hover:bg-slate-800"
        :class="current === item.name ? 'text-indigo-600 dark:text-indigo-400' : 'text-slate-600 dark:text-slate-400'"
        :title="item.label">
        <span class="w-6 h-6 inline-block" v-html="icons[item.icon]"></span>
      </router-link>
      <!-- 主题切换按钮（移动端） -->
      <button
        @click="cycleTheme"
        class="flex flex-col items-center justify-center w-12 h-12 rounded-lg text-xl transition hover:bg-slate-100 dark:hover:bg-slate-800 text-slate-600 dark:text-slate-400"
        :title="`主题：${themeMode}`"
      >
        <span class="w-6 h-6 inline-block" v-html="icons[themeIconName]"></span>
      </button>
    </nav>

    <!-- ===== 主内容区域 ===== -->
    <main class="pl-0 md:pl-16 pb-14 md:pb-0 transition-all duration-300" :class="collapsed ? 'md:pl-16' : 'md:pl-48'">
      <RouterView />
    </main>
  </div>
</template>