// ESLint flat config —— 只做「能抓出真问题」的规则，不引入格式化风格争论。
// 覆盖：Vue 3 + TypeScript（<script setup>）、浏览器全局、构建期常量。
// 运行：npm run lint
import js from '@eslint/js'
import pluginVue from 'eslint-plugin-vue'
import tseslint from 'typescript-eslint'
import globals from 'globals'

export default tseslint.config(
  {
    ignores: ['dist/**', 'node_modules/**', 'src/public/**']
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  ...pluginVue.configs['flat/essential'],
  {
    files: ['**/*.{ts,vue}'],
    languageOptions: {
      globals: {
        ...globals.browser,
        // vite.config.ts 的 define 注入（env.d.ts 已有类型声明）
        __TERMINAL_ENABLED__: 'readonly'
      }
    }
  },
  {
    // .vue 的 <script lang="ts"> 需要把 TS parser 交给 vue-eslint-parser
    files: ['**/*.vue'],
    languageOptions: {
      parserOptions: {
        parser: tseslint.parser
      }
    }
  },
  {
    rules: {
      // 项目约定：v-html 只用于本地静态 SVG 与经过转义的 Markdown（见 useMarkdown.ts）
      'vue/no-v-html': 'off',
      // 单文件页面组件（ViewSwitcher / TerminalViewDisabled 等）是既有约定
      'vue/multi-word-component-names': 'off',
      // 现有 API 层存在少量 `as unknown as` 与 any 边界的局部断言，不阻塞 lint
      '@typescript-eslint/no-explicit-any': 'off',
      '@typescript-eslint/no-unused-vars': [
        'warn',
        { argsIgnorePattern: '^_', varsIgnorePattern: '^_', caughtErrors: 'none' }
      ],
      // 允许 `catch { /* 说明 */ }` 这种刻意的空处理（项目里大量用于「忽略非关键失败」）
      'no-empty': ['error', { allowEmptyCatch: true }]
    }
  }
)
