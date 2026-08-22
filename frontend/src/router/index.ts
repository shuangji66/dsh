import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'settings', component: () => import('@/views/SettingsView.vue') },
    { path: '/directory', name: 'directory', component: () => import('@/views/DirectoryView.vue') },
    { path: '/terminal', name: 'terminal', component: () => import('@/views/TerminalView.vue') }
  ]
})

export default router