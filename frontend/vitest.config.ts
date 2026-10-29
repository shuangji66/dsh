import { defineConfig } from 'vitest/config'
import { fileURLToPath, URL } from 'node:url'

// 前端单元测试只覆盖「纯逻辑 / 可脱离后端验证」的部分：
// 金额格式化、Markdown 安全性、i18n 参数替换与回退。
// 组件行为仍以 vue-tsc + 构建 + 手动/Playwright 回归为主（见 AGENTS.md）。
export default defineConfig({
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url))
    }
  },
  test: {
    environment: 'happy-dom',
    include: ['src/**/*.test.ts']
  }
})
