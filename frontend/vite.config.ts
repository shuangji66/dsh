import { defineConfig, type Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'
import { readFileSync, readdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { gzipSync } from 'node:zlib'

// The frontend is embedded into the Go binary at build time and served over the
// admin unix socket under a baseurl prefix fronted by nginx. The baseurl is NOT
// known at build time and must NOT be baked into the binary — it is resolved at
// runtime from the environment (HARNESS_ADMIN_BASEURL). So we build with a
// RELATIVE base (./assets/...), and the backend prepends the runtime baseurl
// to asset URLs when it serves index.html.
// 终端功能可在编译期整体剔除：构建时置 TERMINAL=0（make / build.sh 会同时给 Go 传
// -tags noterminal，并把 VITE_TERMINAL 置 0）。这里负责前端那一半 —— 关闭时用占位页
// 顶替 TerminalView，xterm 等终端依赖因此**不会进入产物**（不是隐藏入口，是真不打包）。
// 页面入口（侧边栏 / 旧路径）由 __TERMINAL_ENABLED__ 在编译期一并收起。
const terminalEnabled = process.env.VITE_TERMINAL !== '0'

// 构建期为 assets 生成 .gz 兄弟文件（构建产物是 Go //go:embed 的，运行时不再压缩）。
// 后端的 spaHandler/serveBytes 在 Accept-Encoding 含 gzip 时直接发这份预压缩字节，
// 并带上 Vary: Accept-Encoding；未压缩的原始文件仍然保留给不支持 gzip 的客户端。
// 只压 hashed assets（js/css），不压 index.html（它每次要注入 <base>，不能预压缩）。
function precompressAssets(): Plugin {
  const assetsDir = fileURLToPath(new URL('./dist/assets', import.meta.url))
  return {
    name: 'harness-precompress-assets',
    apply: 'build',
    closeBundle() {
      let files: string[]
      try {
        files = readdirSync(assetsDir)
      } catch {
        return
      }
      for (const name of files) {
        if (!/\.(js|css)$/.test(name)) continue
        const file = join(assetsDir, name)
        writeFileSync(file + '.gz', gzipSync(readFileSync(file), { level: 9 }))
      }
    }
  }
}

export default defineConfig({
  plugins: [vue(), precompressAssets()],
  define: {
    __TERMINAL_ENABLED__: JSON.stringify(terminalEnabled)
  },
  resolve: {
    // 顺序要紧：Vite/Rollup 的 alias 按声明顺序匹配，'@' 会先把 '@/views/TerminalView.vue'
    // 改写成绝对路径，更具体的这条就再也匹配不上了 —— 所以终端那条必须放在前面。
    alias: {
      // 只在无终端构建里生效：ViewSwitcher 里那条静态 import 被指向占位页。
      ...(terminalEnabled
        ? {}
        : {
            '@/views/TerminalView.vue': fileURLToPath(
              new URL('./src/views/TerminalViewDisabled.vue', import.meta.url)
            )
          }),
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