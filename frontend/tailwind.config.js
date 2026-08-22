/** @type {import('tailwindcss').Config} */
export default {
  darkMode: 'class', // 必须显式设置为 class 模式
  content: [
    './index.html',
    './src/**/*.{vue,js,ts,jsx,tsx}'
  ],
  theme: {
    extend: {
      // ===== Genesis 设计令牌 =====
      colors: {
        // 品牌主色（indigo）
        brand: {
          DEFAULT: '#6366F1',
          hover: '#4F46E5',
          soft: 'rgba(99,102,241,0.12)',
        },
        // 语义色
        success: { DEFAULT: '#10B981' },
        warning: { DEFAULT: '#F59E0B' },
        danger: { DEFAULT: '#EF4444' },
        // 中性层：背景 / 表面 / 边框 / 文本
        bg: { DEFAULT: '#FAFAFA' },
        surface: { DEFAULT: '#FFFFFF' },
        line: { DEFAULT: '#E8E8EC' },
        ink: {
          DEFAULT: '#0A0A0A',
          soft: '#6B6B6B',
          faint: '#9C9C9C',
        },
      },
      fontFamily: {
        sans: ['"DM Sans"', 'ui-sans-serif', 'system-ui', '-apple-system', 'Segoe UI', 'Roboto', 'Helvetica Neue', 'Arial', 'sans-serif'],
        display: ['"General Sans"', '"DM Sans"', 'ui-sans-serif', 'system-ui', 'sans-serif'],
        mono: ['"JetBrains Mono"', 'ui-monospace', 'SFMono-Regular', 'Menlo', 'Consolas', 'monospace'],
      },
      boxShadow: {
        // 卡片悬浮：+2px 上移的轻投影
        card: '0 8px 30px rgba(0,0,0,0.08)',
        // 主按钮悬浮辉光
        glow: '0 4px 12px rgba(99,102,241,0.35)',
        // 下拉 / 弹层
        pop: '0 10px 40px rgba(0,0,0,0.12)',
      },
      ringColor: {
        brand: '#6366F1',
      },
      maxWidth: {
        content: '1280px',
      },
    }
  },
  plugins: []
}