// src/constants/repos.ts —— 各更新目标在上游的 GitHub 仓库（全站唯一一份）。
//
// 三个更新目标分处三个弹窗组件（harness 在 UpdateSection.vue、dsh 服务在
// ServerVersionsDialog.vue、插件市场在 MarketDialog.vue），每个弹窗标题右侧都有一个
// 指向对应仓库的裸图标外链（见 GithubIconLink.vue）。三个仓库地址以前只写在
// UpdateSection.vue 里，拆出独立弹窗后另外两个就没有图标可挂 —— 因此集中到这里，
// 新增目标时补一行即可。
//
// 注意：这是**展示用**的常量，与后端更新链路用的仓库地址（update.go 的 updateRepoURL）
// 无关，不要把它们互相替换。
export const repoSlugs = {
  // harness 控制台：本仓库（harness-* 与 dsh-* 两类发布资产都在这里）
  harness: 'shuangji66/dsh',
  // dsh 服务：上游 DeepSeek Harness 仓库
  dsh: 'deepseek-ai/deepseek-harness',
  // 插件市场：dsh-market
  market: 'dsh-market/dsh-market'
} as const

// 仓库的 HTTPS 地址（GithubIconLink 的 href）
export function repoURL(slug: string): string {
  return `https://github.com/${slug}`
}
