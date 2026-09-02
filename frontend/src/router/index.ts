import { createRouter, createWebHistory } from 'vue-router'

// 所有子页面都通过 / 路径下的 ?view= 查询参数切换，
// 避免 /directory、/terminal 等独立子路径产生真实的历史记录，从而被浏览器后退切页。
// 为兼容旧的 /directory、/terminal 直达链接，做 302 重定向到对应的 /?view=xxx。
const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      name: 'view',
      component: () => import('@/views/ViewSwitcher.vue'),
    },
    { path: '/directory', redirect: { path: '/', query: { view: 'directory' } } },
    { path: '/plugins', redirect: { path: '/', query: { view: 'plugins' } } },
    { path: '/terminal', redirect: { path: '/', query: { view: 'terminal' } } },
    { path: '/logs', redirect: { path: '/', query: { view: 'logs' } } },
  ],
})

export default router