/** @type {import('tailwindcss').Config} */
export default {
  darkMode: 'class', // 必须显式设置为 class 模式
  content: [
    './index.html',
    './src/**/*.{vue,js,ts,jsx,tsx}'
  ],
  theme: {
    extend: {}
  },
  plugins: []
}