<script setup lang="ts">
import { onMounted, onBeforeUnmount, onActivated, onDeactivated, ref, nextTick } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'

defineOptions({ name: 'TerminalView' })

const el = ref<HTMLElement | null>(null)
const wsProtocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
const basePath = document.baseURI ? new URL(document.baseURI).pathname.replace(/\/$/, '') : ''
const wsEndpoint = basePath + '/terminal'

let term: Terminal | null = null
let fitAddon: FitAddon | null = null
let sock: WebSocket | null = null
let resizeObserver: ResizeObserver | null = null
let resizeTimeout: number | null = null
let fitRetryTimer: number | null = null
let onPasteEvent: ((ev: ClipboardEvent) => void) | null = null

// 修饰键
const ctrlPressed = ref(false)
const altPressed = ref(false)
const shiftPressed = ref(false)

// 方向键重复
let repeatTimer: number | null = null

// 长按粘贴相关
let longPressTimer: number | null = null
let touchStartX = 0
let touchStartY = 0
let touchStartTime = 0
let pasteHelper: HTMLTextAreaElement | null = null

function wsUrl(): string {
  const u = new URL(wsEndpoint, location.href)
  u.protocol = wsProtocol
  return u.toString()
}

function scrollToBottom() {
  if (!term) return
  nextTick(() => term?.scrollToBottom())
}

function sendKey(key: string) {
  if (term) {
    term.focus()
    if (sock && sock.readyState === WebSocket.OPEN) sock.send(key)
    scrollToBottom()
  }
}

function toggleModifier(mod: 'ctrl' | 'alt' | 'shift') {
  if (mod === 'ctrl') ctrlPressed.value = !ctrlPressed.value
  else if (mod === 'alt') altPressed.value = !altPressed.value
  else if (mod === 'shift') shiftPressed.value = !shiftPressed.value
  term?.focus()
}

function sendResize(cols: number, rows: number) {
  if (!sock || sock.readyState !== WebSocket.OPEN) return
  sock.send(`\x1b]resize;${cols};${rows}\x07`)
}

function fitAndResize() {
  if (!fitAddon || !term || !el.value) return
  try {
    const container = el.value
    // 容器不可见或尚无尺寸时，稍后重试，避免 fit 把列数算成 0 导致不换行
    if (container.clientWidth <= 0 || container.clientHeight <= 0) {
      if (fitRetryTimer === null) {
        fitRetryTimer = window.setTimeout(() => {
          fitRetryTimer = null
          fitAndResize()
        }, 120)
      }
      return
    }
    fitAddon.fit()
    const cols = term.cols
    const rows = term.rows
    if (cols > 0 && rows > 0) {
      sendResize(cols, rows)
      scrollToBottom()
    }
  } catch (e) {
    console.warn('fitAndResize error:', e)
  }
}

function debouncedFit() {
  if (resizeTimeout) cancelAnimationFrame(resizeTimeout)
  resizeTimeout = requestAnimationFrame(() => {
    fitAndResize()
    resizeTimeout = null
  })
}

function startRepeat(key: string) {
  if (repeatTimer) return
  sendKey(key)
  repeatTimer = window.setInterval(() => sendKey(key), 100)
}

function stopRepeat() {
  if (repeatTimer) {
    clearInterval(repeatTimer)
    repeatTimer = null
  }
}

// ---------- 长按粘贴：调用系统文本编辑菜单 ----------
function showPasteMenu() {
  if (pasteHelper) {
    document.body.removeChild(pasteHelper)
    pasteHelper = null
  }
  const ta = document.createElement('textarea')
  ta.style.position = 'fixed'
  ta.style.left = '-9999px'
  ta.style.top = '-9999px'
  ta.style.width = '1px'
  ta.style.height = '1px'
  ta.style.opacity = '0'
  ta.style.pointerEvents = 'none'
  ta.style.zIndex = '-1'
  ta.setAttribute('autocorrect', 'off')
  ta.setAttribute('autocapitalize', 'off')
  ta.setAttribute('spellcheck', 'false')
  document.body.appendChild(ta)
  pasteHelper = ta

  ta.focus()
  ta.select()

  const cleanup = () => {
    if (pasteHelper) {
      pasteHelper.removeEventListener('paste', onPaste)
      pasteHelper.removeEventListener('blur', onBlur)
      pasteHelper.removeEventListener('touchstart', onTouchOutside)
      document.body.removeChild(pasteHelper)
      pasteHelper = null
    }
  }

  const onPaste = (ev: ClipboardEvent) => {
    const text = ev.clipboardData?.getData('text/plain')
    if (text && sock && sock.readyState === WebSocket.OPEN) {
      sock.send(text)
      term?.focus()
    }
    cleanup()
  }

  const onBlur = () => {
    setTimeout(cleanup, 200)
  }

  const onTouchOutside = (e: TouchEvent) => {
    if (!pasteHelper?.contains(e.target as Node)) {
      cleanup()
    }
  }

  ta.addEventListener('paste', onPaste)
  ta.addEventListener('blur', onBlur)
  document.addEventListener('touchstart', onTouchOutside, { once: true })
  setTimeout(() => {
    if (pasteHelper) cleanup()
  }, 10000)
}

function onTouchStart(e: TouchEvent) {
  if (!term || !sock || sock.readyState !== WebSocket.OPEN) return
  if (e.touches.length !== 1) return
  const target = e.target as HTMLElement
  if (target.closest('.md\\:hidden')) return
  const touch = e.touches[0]
  touchStartX = touch.clientX
  touchStartY = touch.clientY
  touchStartTime = Date.now()
  longPressTimer = window.setTimeout(() => {
    showPasteMenu()
    longPressTimer = null
  }, 600)
}

function onTouchMove(e: TouchEvent) {
  if (longPressTimer) {
    const touch = e.touches[0]
    const dx = touch.clientX - touchStartX
    const dy = touch.clientY - touchStartY
    if (Math.abs(dx) > 10 || Math.abs(dy) > 10) {
      clearTimeout(longPressTimer)
      longPressTimer = null
    }
  }
}

function onTouchEnd() {
  if (longPressTimer) {
    clearTimeout(longPressTimer)
    longPressTimer = null
  }
}
// ------------------------------------------------

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
    letterSpacing: 0,
  })
  fitAddon = new FitAddon()
  term.loadAddon(fitAddon)
  term.open(el.value)

  nextTick(() => {
    requestAnimationFrame(fitAndResize)
  })

  term.onResize(({ cols, rows }) => {
    // 不重复发送，由 fitAndResize 统一处理
  })

  if (window.ResizeObserver) {
    resizeObserver = new ResizeObserver(debouncedFit)
    resizeObserver.observe(el.value)
  } else {
    window.addEventListener('resize', debouncedFit)
  }

  // 桌面端原生粘贴：Ctrl+Shift+V / 右键粘贴 触发 paste 事件时直接发送到终端
  onPasteEvent = (ev: ClipboardEvent) => {
    if (!sock || sock.readyState !== WebSocket.OPEN) return
    const text = ev.clipboardData?.getData('text/plain')
    if (text) {
      ev.preventDefault()
      sock.send(text)
      scrollToBottom()
    }
  }
  el.value.addEventListener('paste', onPasteEvent)

  // WebSocket
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

  term.onData((data) => {
    if (!sock || sock.readyState !== WebSocket.OPEN) return
    let toSend = data
    if (ctrlPressed.value && data.length === 1) {
      const code = data.charCodeAt(0)
      if (code >= 97 && code <= 122) toSend = String.fromCharCode(code - 96)
      else if (code >= 65 && code <= 90) toSend = String.fromCharCode(code - 64)
      sock.send(toSend)
      return
    }
    if (altPressed.value && data.length === 1) {
      sock.send('\x1b' + data)
      return
    }
    sock.send(data)
  })
})

// 页面切换回来时（KeepAlive 激活）：重新适配尺寸、滚动到底部
onActivated(() => {
  nextTick(() => {
    requestAnimationFrame(fitAndResize)
    scrollToBottom()
  })
})

// 页面切走时：释放长按粘贴等临时资源
onDeactivated(() => {
  if (fitRetryTimer) {
    clearTimeout(fitRetryTimer)
    fitRetryTimer = null
  }
  if (longPressTimer) {
    clearTimeout(longPressTimer)
    longPressTimer = null
  }
  if (pasteHelper) {
    document.body.removeChild(pasteHelper)
    pasteHelper = null
  }
})

onBeforeUnmount(() => {
  if (fitRetryTimer) {
    clearTimeout(fitRetryTimer)
    fitRetryTimer = null
  }
  if (onPasteEvent && el.value) {
    el.value.removeEventListener('paste', onPasteEvent)
    onPasteEvent = null
  }
  sock?.close()
  if (resizeObserver) {
    resizeObserver.disconnect()
    resizeObserver = null
  } else {
    window.removeEventListener('resize', debouncedFit)
  }
  if (resizeTimeout) cancelAnimationFrame(resizeTimeout)
  if (repeatTimer) clearInterval(repeatTimer)
  if (longPressTimer) clearTimeout(longPressTimer)
  if (pasteHelper) {
    document.body.removeChild(pasteHelper)
    pasteHelper = null
  }
  if (toastTimer) clearTimeout(toastTimer)
  if (term) term.dispose()
})

const reconnect = () => {
  sock?.close()
  setTimeout(() => {
    sock = new WebSocket(wsUrl())
    sock.binaryType = 'arraybuffer'
    sock.onmessage = (ev) => {
      const data = typeof ev.data === 'string' ? ev.data : new TextDecoder().decode(ev.data)
      term?.write(data)
      scrollToBottom()
    }
    sock.onopen = () => {
      term?.writeln('已重新连接')
      scrollToBottom()
      fitAndResize()
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

// ---------- 复制：把终端选中内容复制到剪贴板 ----------
async function copySelection() {
  if (!term) return
  const sel = term.getSelection()
  if (!sel) {
    showToast('没有选中内容')
    return
  }
  try {
    await navigator.clipboard.writeText(sel)
    showToast('已复制到剪贴板')
  } catch {
    // 剪贴板 API 不可用（如非安全上下文）时退回选中态
    showToast('请直接框选文本复制')
  }
  term.focus()
}

// ---------- 粘贴：读取剪贴板并发送到终端 ----------
async function pasteClipboard() {
  if (!term || !sock || sock.readyState !== WebSocket.OPEN) {
    showToast('终端未连接')
    return
  }
  try {
    const text = await navigator.clipboard.readText()
    if (text) {
      sock.send(text)
      term.focus()
      scrollToBottom()
    }
  } catch {
    // 剪贴板读取失败时，调用系统粘贴菜单兜底
    showPasteMenu()
  }
}

let toastTimer: number | null = null
function showToast(msg: string) {
  const hint = document.querySelector('.term-copy-toast') as HTMLElement | null
  if (!hint) return
  hint.textContent = msg
  hint.classList.remove('opacity-0', 'pointer-events-none')
  if (toastTimer) clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => {
    hint.classList.add('opacity-0', 'pointer-events-none')
    toastTimer = null
  }, 1400)
}
</script>

<template>
  <div class="flex flex-col h-[calc(100dvh-56px)] md:h-[100dvh]">
    <!-- 工具栏 -->
    <div class="flex items-center justify-between px-4 py-2.5 bg-surface dark:bg-[#111115] border-b border-line dark:border-[#2A2A32]">
      <div class="flex items-center gap-2">
        <span class="w-2 h-2 rounded-full bg-success animate-pulse"></span>
        <span class="text-sm font-medium text-ink dark:text-white">终端</span>
      </div>
      <div class="flex items-center gap-1">
        <button class="g-btn-ghost hidden sm:inline-flex" title="复制选中内容" @click="copySelection">复制</button>
        <button class="g-btn-ghost" title="粘贴到终端" @click="pasteClipboard">粘贴</button>
        <button class="g-btn-ghost" @click="reconnect">重连</button>
        <button class="g-btn-ghost" @click="clearTerminal">清空</button>
      </div>
    </div>

    <!-- 终端容器，绑定触摸事件（相对定位，承载复制提示气泡） -->
    <div
      ref="el"
      class="flex-1 term-container p-2 overflow-hidden relative"
      @touchstart="onTouchStart"
      @touchmove="onTouchMove"
      @touchend="onTouchEnd"
      @touchcancel="onTouchEnd"
    >
      <!-- 复制/粘贴反馈提示 -->
      <div class="term-copy-toast absolute bottom-2 right-2 z-20 px-3 py-1.5 rounded-md bg-black/70 dark:bg-white/85 text-white dark:text-black text-xs font-medium shadow-card opacity-0 pointer-events-none transition-opacity duration-200 whitespace-nowrap"></div>
    </div>

    <!-- 移动端功能键（两行） -->
    <div
      class="flex md:hidden flex-col gap-1 px-3 py-2.5 pb-4 bg-bg dark:bg-[#0B0B0F] border-t border-line dark:border-[#2A2A32]"
      style="padding-bottom: calc(env(safe-area-inset-bottom, 0px) + 10px);"
    >
      <div class="flex items-center justify-around gap-1 flex-wrap">
        <button class="px-2.5 py-1 text-xs font-medium rounded-full transition" :class="ctrlPressed ? 'bg-brand text-white' : 'bg-black/5 dark:bg-white/10 text-ink-soft dark:text-[#A6A6AD]'" @click="toggleModifier('ctrl')">Ctrl</button>
        <button class="px-2.5 py-1 text-xs font-medium rounded-full transition" :class="altPressed ? 'bg-brand text-white' : 'bg-black/5 dark:bg-white/10 text-ink-soft dark:text-[#A6A6AD]'" @click="toggleModifier('alt')">Alt</button>
        <button class="px-2.5 py-1 text-xs font-medium rounded-full transition" :class="shiftPressed ? 'bg-brand text-white' : 'bg-black/5 dark:bg-white/10 text-ink-soft dark:text-[#A6A6AD]'" @click="toggleModifier('shift')">Shift</button>
        <button class="px-2.5 py-1 text-xs font-medium bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] hover:bg-black/10 dark:hover:bg-white/15 transition" @click="sendKey('\t')">Tab</button>
        <button class="px-2.5 py-1 text-xs font-medium bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] hover:bg-black/10 dark:hover:bg-white/15 transition" @click="sendKey('\x1b')">ESC</button>
        <button class="px-2.5 py-1 text-xs font-medium bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] hover:bg-black/10 dark:hover:bg-white/15 transition" @click="sendKey('\x1b[2~')">Insert</button>
        <!-- 移动端粘贴：读取剪贴板并发送到终端 -->
        <button class="px-2.5 py-1 text-xs font-medium bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] hover:bg-black/10 dark:hover:bg-white/15 transition select-none" @click="pasteClipboard">粘贴</button>
      </div>
      <div class="flex items-center justify-around gap-1 flex-wrap">
        <button class="px-2.5 py-1 text-xs font-medium bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] hover:bg-black/10 dark:hover:bg-white/15 transition select-none" @mousedown="startRepeat('\x1b[A')" @mouseup="stopRepeat" @mouseleave="stopRepeat" @touchstart.prevent="startRepeat('\x1b[A')" @touchend="stopRepeat" @touchcancel="stopRepeat">↑</button>
        <button class="px-2.5 py-1 text-xs font-medium bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] hover:bg-black/10 dark:hover:bg-white/15 transition select-none" @mousedown="startRepeat('\x1b[B')" @mouseup="stopRepeat" @mouseleave="stopRepeat" @touchstart.prevent="startRepeat('\x1b[B')" @touchend="stopRepeat" @touchcancel="stopRepeat">↓</button>
        <button class="px-2.5 py-1 text-xs font-medium bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] hover:bg-black/10 dark:hover:bg-white/15 transition select-none" @mousedown="startRepeat('\x1b[D')" @mouseup="stopRepeat" @mouseleave="stopRepeat" @touchstart.prevent="startRepeat('\x1b[D')" @touchend="stopRepeat" @touchcancel="stopRepeat">←</button>
        <button class="px-2.5 py-1 text-xs font-medium bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] hover:bg-black/10 dark:hover:bg-white/15 transition select-none" @mousedown="startRepeat('\x1b[C')" @mouseup="stopRepeat" @mouseleave="stopRepeat" @touchstart.prevent="startRepeat('\x1b[C')" @touchend="stopRepeat" @touchcancel="stopRepeat">→</button>
        <button class="px-2.5 py-1 text-xs font-medium bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] hover:bg-black/10 dark:hover:bg-white/15 transition" @click="sendKey('.')">.</button>
        <button class="px-2.5 py-1 text-xs font-medium bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] hover:bg-black/10 dark:hover:bg-white/15 transition" @click="sendKey('/')">/</button>
        <!-- 新增 " 和 = 键，注意单引号包裹 -->
        <button class="px-2.5 py-1 text-xs font-medium bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] hover:bg-black/10 dark:hover:bg-white/15 transition" @click='sendKey("\"")'>"</button>
        <button class="px-2.5 py-1 text-xs font-medium bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] hover:bg-black/10 dark:hover:bg-white/15 transition" @click="sendKey('=')">=</button>
      </div>
    </div>
  </div>
</template>