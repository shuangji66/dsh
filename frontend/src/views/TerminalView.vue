<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, nextTick } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'

const el = ref<HTMLElement | null>(null)
const wsProtocol = location.protocol === 'https:' ? 'wss:' : 'ws:'

const basePath = document.baseURI ? new URL(document.baseURI).pathname.replace(/\/$/, '') : ''
const wsEndpoint = basePath + '/terminal'

let term: Terminal | null = null
let fitAddon: FitAddon | null = null
let sock: WebSocket | null = null

// 修饰键状态
const ctrlPressed = ref(false)
const altPressed = ref(false)
const shiftPressed = ref(false)

function wsUrl(): string {
  const u = new URL(wsEndpoint, location.href)
  u.protocol = wsProtocol
  return u.toString()
}

function scrollToBottom() {
  if (!term) return
  nextTick(() => {
    term?.scrollToBottom()
  })
}

function sendKey(key: string) {
  if (term) {
    term.focus()
    if (sock && sock.readyState === WebSocket.OPEN) {
      sock.send(key)
    }
    scrollToBottom()
  }
}

function toggleModifier(mod: 'ctrl' | 'alt' | 'shift') {
  if (mod === 'ctrl') ctrlPressed.value = !ctrlPressed.value
  else if (mod === 'alt') altPressed.value = !altPressed.value
  else if (mod === 'shift') shiftPressed.value = !shiftPressed.value
  term?.focus()
}

onMounted(() => {
  if (!el.value) return

  const theme = {
    background: '#1e1e2e',
    foreground: '#cdd6f4',
    cursor: '#f5e0dc',
    cursorAccent: '#1e1e2e',
    selection: '#585b70',
    black: '#45475a',
    red: '#f38ba8',
    green: '#a6e3a1',
    yellow: '#f9e2af',
    blue: '#89b4fa',
    magenta: '#cba6f7',
    cyan: '#94e2d5',
    white: '#bac2de',
    brightBlack: '#585b70',
    brightRed: '#f38ba8',
    brightGreen: '#a6e3a1',
    brightYellow: '#f9e2af',
    brightBlue: '#89b4fa',
    brightMagenta: '#cba6f7',
    brightCyan: '#94e2d5',
    brightWhite: '#a6adc8',
  }

  term = new Terminal({
    cursorBlink: true,
    fontSize: 13,
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
    theme,
    scrollback: 1000,
  })
  fitAddon = new FitAddon()
  term.loadAddon(fitAddon)
  term.open(el.value)
  fitAddon.fit()
  scrollToBottom()

  // WebSocket 连接
  sock = new WebSocket(wsUrl())
  sock.binaryType = 'arraybuffer'

  sock.onmessage = (ev) => {
    const data = typeof ev.data === 'string' ? ev.data : new TextDecoder().decode(ev.data)
    term?.write(data)
    scrollToBottom()
  }
  sock.onclose = () => {
    term?.writeln('\r\n\x1b[31m连接已关闭。刷新页面重连。\x1b[0m')
    scrollToBottom()
  }
  sock.onerror = () => {
    term?.writeln('\r\n\x1b[31mWebSocket 错误。\x1b[0m')
    scrollToBottom()
  }

  // ===== 核心修改：在 onData 中处理组合键 =====
  term.onData((data) => {
    if (!sock || sock.readyState !== WebSocket.OPEN) return

    let toSend = data

    // Ctrl 组合键：将字母转换为控制字符
    if (ctrlPressed.value && data.length === 1) {
      const code = data.charCodeAt(0)
      if (code >= 97 && code <= 122) { // a-z
        toSend = String.fromCharCode(code - 96)
      } else if (code >= 65 && code <= 90) { // A-Z
        toSend = String.fromCharCode(code - 64)
      }
      // 如果转换后发送，终端会显示 ^C 等，由 bash 回显
      sock.send(toSend)
      return
    }

    // Alt 组合键：添加 ESC 前缀
    if (altPressed.value && data.length === 1) {
      toSend = '\x1b' + data
      sock.send(toSend)
      return
    }

    // 普通输入
    sock.send(data)
  })

  const onResize = () => {
    fitAddon?.fit()
    scrollToBottom()
  }
  window.addEventListener('resize', onResize)
  const disposer = () => window.removeEventListener('resize', onResize)
  ;(term as unknown as { _resizeDisposer?: () => void })._resizeDisposer = disposer
})

onBeforeUnmount(() => {
  sock?.close()
  if (term) {
    const d = (term as unknown as { _resizeDisposer?: () => void })._resizeDisposer
    d?.()
    term.dispose()
  }
})

const reconnect = () => {
  sock?.close()
  setTimeout(() => {
    sock = new WebSocket(wsUrl())
    sock.onmessage = (ev) => {
      const data = typeof ev.data === 'string' ? ev.data : new TextDecoder().decode(ev.data)
      term?.write(data)
      scrollToBottom()
    }
    sock.onopen = () => {
      term?.writeln('已重新连接')
      scrollToBottom()
    }
    sock.onclose = () => {
      term?.writeln('\r\n\x1b[31m连接已关闭。刷新页面重连。\x1b[0m')
      scrollToBottom()
    }
    sock.onerror = () => {
      term?.writeln('\r\n\x1b[31mWebSocket 错误。\x1b[0m')
      scrollToBottom()
    }
  }, 200)
}

function clearTerminal() {
  if (term) {
    term.clear()
    scrollToBottom()
  }
}
</script>

<template>
  <!-- 移动端减去底部导航栏高度 (h-14 = 56px)，桌面端保持全高 -->
  <div class="flex flex-col h-[calc(100dvh-56px)] md:h-[100dvh]">
    <!-- 顶部工具栏 -->
    <div class="flex items-center justify-between px-4 py-2 bg-white dark:bg-slate-900 border-b border-slate-200 dark:border-slate-800">
      <div class="text-sm font-medium text-slate-900 dark:text-slate-100">终端</div>
      <div class="flex gap-2">
        <button class="px-3 py-1 text-xs bg-slate-200 dark:bg-slate-800 hover:bg-slate-300 dark:hover:bg-slate-700 rounded-md text-slate-800 dark:text-slate-100" @click="reconnect">重连</button>
        <button class="px-3 py-1 text-xs bg-slate-200 dark:bg-slate-800 hover:bg-slate-300 dark:hover:bg-slate-700 rounded-md text-slate-800 dark:text-slate-100" @click="clearTerminal">清空</button>
      </div>
    </div>

    <!-- 终端主体 -->
    <div ref="el" class="flex-1 term-container p-2 overflow-hidden"></div>

    <!-- 移动端功能键栏 (仅在移动端显示) -->
    <div class="flex md:hidden items-center justify-around gap-1 px-2 py-1 bg-slate-100 dark:bg-slate-800 border-t border-slate-200 dark:border-slate-700 flex-wrap">
      <button class="px-2 py-1 text-xs rounded hover:bg-slate-300 dark:hover:bg-slate-600 text-slate-800 dark:text-slate-200"
        :class="ctrlPressed ? 'bg-indigo-500 dark:bg-indigo-600 text-white' : 'bg-slate-200 dark:bg-slate-700'"
        @click="toggleModifier('ctrl')">Ctrl</button>
      <button class="px-2 py-1 text-xs rounded hover:bg-slate-300 dark:hover:bg-slate-600 text-slate-800 dark:text-slate-200"
        :class="altPressed ? 'bg-indigo-500 dark:bg-indigo-600 text-white' : 'bg-slate-200 dark:bg-slate-700'"
        @click="toggleModifier('alt')">Alt</button>
      <button class="px-2 py-1 text-xs rounded hover:bg-slate-300 dark:hover:bg-slate-600 text-slate-800 dark:text-slate-200"
        :class="shiftPressed ? 'bg-indigo-500 dark:bg-indigo-600 text-white' : 'bg-slate-200 dark:bg-slate-700'"
        @click="toggleModifier('shift')">Shift</button>
      <button class="px-2 py-1 text-xs bg-slate-200 dark:bg-slate-700 rounded hover:bg-slate-300 dark:hover:bg-slate-600 text-slate-800 dark:text-slate-200" @click="sendKey('\t')">Tab</button>
      <button class="px-2 py-1 text-xs bg-slate-200 dark:bg-slate-700 rounded hover:bg-slate-300 dark:hover:bg-slate-600 text-slate-800 dark:text-slate-200" @click="sendKey('\x1b')">ESC</button>
      <button class="px-2 py-1 text-xs bg-slate-200 dark:bg-slate-700 rounded hover:bg-slate-300 dark:hover:bg-slate-600 text-slate-800 dark:text-slate-200" @click="sendKey('\x1b[2~')">Insert</button>
    </div>
  </div>
</template>