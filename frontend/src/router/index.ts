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
    // 旧 `/terminal` 直达链接：有终端构建时进终端页；无终端构建时回概览，
    // 而不是落到一条没有匹配的路由（那会白屏）。
    {
      path: '/terminal',
      redirect: { path: '/', query: { view: __TERMINAL_ENABLED__ ? 'terminal' : 'overview' } },
    },
    { path: '/logs', redirect: { path: '/', query: { view: 'logs' } } },
  ],
})

export default router