// src/constants/repos.ts —— 各组件外链的 GitHub 仓库（全站唯一一份）。
//
// 三个更新弹窗组件（dsh 服务在 ServerVersionsDialog.vue、插件市场与增强插件共用
// ManagedPluginDialog.vue）标题右侧都有指向对应仓库的裸图标外链；概览页「关于」弹窗
// 也用它指向本仓库（见 GithubIconLink.vue）。集中到这里，新增目标时补一行即可。
//
// 注意：这是**展示用**的常量，与后端更新链路里的仓库地址无关，不要互相替换。
export const repoSlugs = {
  // 本仓库（控制台与增强插件的源码；概览页「关于」弹窗用它）
  harness: 'shuangji66/dsh',
  // dsh 服务：上游 DeepSeek Harness 仓库
  dsh: 'deepseek-ai/deepseek-harness',
  // 插件市场：dsh-market
  market: 'dsh-market/dsh-market',
  // 增强插件（dsh-fnos-advanced）：本仓库（包在该目录下）
  advanced: 'shuangji66/dsh'
} as const

// 仓库的 HTTPS 地址（GithubIconLink 的 href）
export function repoURL(slug: string): string {
  return `https://github.com/${slug}`
}
