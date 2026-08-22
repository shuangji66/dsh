import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

// The frontend is embedded into the Go binary at build time and served over the
// admin unix socket under a baseurl prefix fronted by nginx. The baseurl is NOT
// known at build time and must NOT be baked into the binary — it is resolved at
// runtime from the environment (HARNESS_ADMIN_BASEURL). So we build with a
// RELATIVE base (./assets/...), and the backend prepends the runtime baseurl
// to asset URLs when it serves index.html.
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url))
    }
  },
  base: './',
  // 将 src/public 目录作为 Vite 的静态资源根目录，使 callback.html 等文件被复制到 dist
  publicDir: 'src/public',
  build: {
    outDir: 'dist',
    emptyOutDir: true
  },
  optimizeDeps: {
    include: ['@trimjs/web-app']
  }
})