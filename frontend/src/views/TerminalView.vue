<script setup lang="ts">
import { onMounted, onBeforeUnmount, onActivated, onDeactivated, ref, watch, nextTick } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { Unicode11Addon } from '@xterm/addon-unicode11'
import { ClipboardAddon } from '@xterm/addon-clipboard'
import '@xterm/xterm/css/xterm.css'
import { useI18n } from '@/composables/useI18n'
import { useTheme } from '@/composables/useTheme'
import { useToastStore } from '@/stores/toast'
import { api, type QuickCmd } from '@/serverapi'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import QuickCmdsDialog from '@/components/QuickCmdsDialog.vue'
import QuickCmdEditDialog from '@/components/QuickCmdEditDialog.vue'

defineOptions({ name: 'TerminalView' })
const { t } = useI18n()
const toast = useToastStore()
const { isDark } = useTheme() // 终端配色跟随控制台浅色/深色切换

// ---------- xterm 主题配色（随控制台主题切换） ----------
// 深色模式：黑底 #1A1A1A 背景、#4EC9B0 绿字（经典绿色终端风）
const DARK_PALETTE = {
  background: '#1A1A1A',
  foreground: '#4EC9B0',
  cursor: '#4EC9B0',
  cursorAccent: '#1A1A1A',
  selectionBackground: 'rgba(78, 201, 176, 0.35)',
  black: '#4d4d4d',
  red: '#ff5555',
  green: '#4EC9B0',
  yellow: '#ffdd33',
  blue: '#5555ff',
  magenta: '#ff55ff',
  cyan: '#55ffff',
  white: '#bbbbbb',
  brightBlack: '#878787',
  brightRed: '#ff7777',
  brightGreen: '#66ff88',
  brightYellow: '#ffee66',
  brightBlue: '#7777ff',
  brightMagenta: '#ff88ff',
  brightCyan: '#88ffff',
  brightWhite: '#ffffff'
}
// 浅色模式：温和的米白背景 + 黑字
const LIGHT_PALETTE = {
  background: '#faf5e9',
  foreground: '#1a1814',
  cursor: '#1a1814',
  cursorAccent: '#faf5e9',
  selectionBackground: 'rgba(181, 141, 63, 0.35)',
  black: '#37352f',
  red: '#c14a3a',
  green: '#4d7f38',
  yellow: '#a5711f',
  blue: '#3b6ea5',
  magenta: '#8f4f9e',
  cyan: '#2e7d78',
  white: '#665f52',
  brightBlack: '#8a8377',
  brightRed: '#d96a55',
  brightGreen: '#5c9444',
  brightYellow: '#c08a2e',
  brightBlue: '#4f82bd',
  brightMagenta: '#a862b5',
  brightCyan: '#3d918b',
  brightWhite: '#948c7d'
}

const el = ref<HTMLElement | null>(null)
const wsProtocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
const basePath = document.baseURI ? new URL(document.baseURI).pathname.replace(/\/$/, '') : ''
const wsEndpoint = basePath + '/terminal'

// 后端会话 id：新建会话在 WS 控制帧 \x1b]id;<id>\x07 回填；
// 恢复历史时从 /api/sessions 取得非退出会话的 id 并以 ?id= 挂载回放。
const sessionId = ref<string | null>(null)

// 会话不存在标记：收到 "session not found" 后丢弃旧 id，连接关闭时自动重建
let recreateOnClose = false

let term: Terminal | null = null
let fitAddon: FitAddon | null = null
let sock: WebSocket | null = null
let termOpened = false

// 桌面端鼠标选中自动复制的去重/节流状态
let lastAutoCopied = ''
let lastAutoCopyToastAt = 0

// 心跳间隔(ms)：定期发送控制消息，防止连接被代理 / NAT / 负载均衡的空闲超时断开
const HEARTBEAT_INTERVAL_MS = 20000
let heartbeatTimer: number | null = null
let resizeObserver: ResizeObserver | null = null
let resizeTimeout: number | null = null
let fitRetryTimer: number | null = null
let onPasteEvent: ((ev: ClipboardEvent) => void) | null = null
let toastTimer: number | null = null

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

// 快捷指令弹窗状态
const quickCmdsVisible = ref(false)
const editVisible = ref(false)
const editingCmd = ref<QuickCmd | null>(null)
const quickCmds = ref<QuickCmd[]>([])
const deleteTarget = ref<QuickCmd | null>(null)
const deleteDialogVisible = ref(false)
const cmdLoading = ref(false)

function wsUrl(): string {
  const u = new URL(wsEndpoint, location.href)
  u.protocol = wsProtocol
  if (sessionId.value) u.searchParams.set('id', sessionId.value)
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

// ---------- 心跳保活：与后端约定的 OSC 控制消息，不会写入 PTY ----------
function sendHeartbeat() {
  if (!sock || sock.readyState !== WebSocket.OPEN) return
  sock.send('\x1b]ping\x07')
}

function startHeartbeat() {
  stopHeartbeat()
  sendHeartbeat()
  heartbeatTimer = window.setInterval(sendHeartbeat, HEARTBEAT_INTERVAL_MS)
}

function stopHeartbeat() {
  if (heartbeatTimer !== null) {
    clearInterval(heartbeatTimer)
    heartbeatTimer = null
  }
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

  term = new Terminal({
    cursorBlink: true,
    fontSize: 13,
    // 字体需带 CJK 回退，否则中文按 fallsback 字体的度量渲染，导致列宽错位
    fontFamily:
      'ui-monospace, SFMono-Regular, Menlo, Consolas, "Cascadia Mono", "Noto Sans Mono CJK SC", "PingFang SC", "Microsoft YaHei", "WenQuanYi Micro Hei", monospace',
    // 终端的底色/字体色跟随控制台浅色/深色主题切换
    theme: isDark.value ? DARK_PALETTE : LIGHT_PALETTE,
    scrollback: 1000,
    letterSpacing: 0,
    // xterm 5.x 中 term.unicode API 属 proposed API，访问前必须显式开启，
    // 否则 term.unicode.activeVersion = '11' 会直接抛错导致终端页初始化中断。
    allowProposedApi: true,
  })
  // Unicode 11 宽度表：xterm 默认的 Unicode 6 规则对大量 CJK 字符宽度计算错误，
  // 会造成中文输入时光标/渲染错位。addon 需在构造后加载并切换 activeVersion
  // （构造参数里直接写 unicodeVersion: '11' 会因 provider 尚未注册而抛错）。
  term.loadAddon(new Unicode11Addon())
  term.unicode.activeVersion = '11'
  fitAddon = new FitAddon()
  term.loadAddon(fitAddon)
  // 剪贴板 addon：注册 OSC 52 剪贴板读写（桌面端鼠标选中自动复制）
  try {
    term.loadAddon(new ClipboardAddon())
  } catch (e) {
    console.warn('clipboard addon:', e)
  }

  // 等待等宽字体加载完成后再 open + fit，确保 xterm 首次测量到的字符
  // 单元格宽度是准确的。若过早 open，xterm 会用一个尚未加载完成的回退
  // 字体测量 cell 宽度并缓存，导致 cols 计算错误（文字提前换行 / 命令重叠）。
  const openAndFit = () => {
    try {
      term?.open(el.value as HTMLElement)
      termOpened = true
    } catch {
      /* 已 open 则忽略 */
    }
    nextTick(() => {
      requestAnimationFrame(() => {
        fitAndResize()
      })
    })
  }
  if (typeof document !== 'undefined' && document.fonts && document.fonts.ready) {
    const fallback = window.setTimeout(() => {
      if (!termOpened) openAndFit()
    }, 800)
    document.fonts.ready.then(() => {
      window.clearTimeout(fallback)
      if (!termOpened) openAndFit()
    })
  } else {
    openAndFit()
  }

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

  // 先恢复会话（若有未退出的活动会话则以 ?id= 挂载回放历史），再建立 WebSocket
  restoreSession().then(() => connect())

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

  // 桌面端：鼠标框选（或双击选词）后自动把选中文本复制进剪贴板，并 toast 提示。
  // 连续选择变化时按文本去重 + 1.2s 节流，避免刷屏。
  term.onSelectionChange(() => {
    const sel = term?.getSelection()
    if (!sel || !sel.trim() || sel === lastAutoCopied) return
    lastAutoCopied = sel
    copyText(sel)
    const now = Date.now()
    if (now - lastAutoCopyToastAt > 1200) {
      lastAutoCopyToastAt = now
      showToast('' + t('term_copied') + '')
    }
  })
})

// 处理来自后端的数据：剥离会话控制帧（\x1b]id;、\x1b]ready\x07、\x1b]exit\x07），
// 其余内容写入终端。
function handleData(raw: string) {
  if (!raw) return
  let rest = raw
  rest = rest.replace(/\x1b\]id;([0-9a-f]+)\x07/g, (_, id: string) => {
    sessionId.value = id
    return ''
  })
  rest = rest.replace(/\x1b\]ready\x07/g, () => '')
  rest = rest.replace(/\x1b\]exit\x07/g, () => '')
  // 会话已不存在（如后端重启）：丢弃旧 id，稍后自动重建新会话
  if (rest.includes('session not found')) {
    recreateOnClose = true
    sessionId.value = null
    rest = '\r\n\x1b[33m' + t('term_conn_closed') + '\x1b[0m\r\n'
  }
  if (rest) {
    term?.write(rest)
    scrollToBottom()
  }
}

// 建立 WebSocket：sessionId 为空则新建会话（后端回 \x1b]id;<id>\x07 回填），
// 否则以 ?id= 挂载已有会话（后端回放历史 + \x1b]ready\x07 后进入实时流）。
function connect() {
  if (!el.value) return
  disconnect()
  sock = new WebSocket(wsUrl())
  sock.binaryType = 'arraybuffer'

  sock.onopen = () => {
    // 连接建立后重新 fit 并同步 PTY 尺寸，确保后端 bash 的 cols/rows 与
    // 前端渲染一致，避免换行错位、命令重叠。
    fitAndResize()
    startHeartbeat()
  }
  sock.onmessage = (ev) => {
    const data = typeof ev.data === 'string' ? ev.data : new TextDecoder().decode(ev.data)
    handleData(data)
  }
  sock.onclose = () => {
    stopHeartbeat()
    if (recreateOnClose) {
      recreateOnClose = false
      connect()
      return
    }
    term?.writeln('\r\n\x1b[31m' + t('term_conn_closed') + '\x1b[0m')
    scrollToBottom()
  }
  sock.onerror = () => {
    term?.writeln('\r\n\x1b[31m' + t('term_ws_error') + '\x1b[0m')
    scrollToBottom()
  }
}

function disconnect() {
  if (heartbeatTimer !== null) {
    clearInterval(heartbeatTimer)
    heartbeatTimer = null
  }
  if (sock) {
    sock.onmessage = null
    sock.onclose = null
    sock.onerror = null
    sock.onopen = null
    sock.close()
    sock = null
  }
}

// 启动时恢复会话：列出后端活动会话，取第一个未退出的会话 id 以 ?id= 挂载，
// 从而回放其历史消息；无会话则新建。
async function restoreSession() {
  try {
    const res = await api.sessions()
    const list = res.sessions || []
    const live = list.find((s) => !s.exited)
    if (live) sessionId.value = live.id
  } catch (e) {
    console.warn('restore sessions error:', e)
  }
}

// 控制台主题（浅色/深色/跟随系统）切换时，实时更新终端底色与字体色
watch(
  () => isDark.value,
  (d) => {
    if (term) term.options.theme = d ? DARK_PALETTE : LIGHT_PALETTE
  }
)

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
  stopHeartbeat()
  if (fitRetryTimer) {
    clearTimeout(fitRetryTimer)
    fitRetryTimer = null
  }
  if (onPasteEvent && el.value) {
    el.value.removeEventListener('paste', onPasteEvent)
    onPasteEvent = null
  }
  disconnect()
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
  // 重连 = 重置终端显示，随后重新挂载会话——后端会从临时历史文件回放全部内容
  // 后再进入实时流，实现「重连同同步加载会话历史消息」。
  term?.reset()
  connect()
}

function clearTerminal() {
  if (term) {
    term.clear()
    scrollToBottom()
  }
  // 清屏同步清空后端临时历史文件：重连/刷新后不再回放已清除的内容
  if (sessionId.value) {
    api.clearSessionHistory(sessionId.value).catch((e) => console.warn('clear session history:', e))
  }
}

// ---------- 快捷指令：列表 / 新增 / 编辑 / 删除 / 执行 ----------
async function loadQuickCmds() {
  cmdLoading.value = true
  try {
    const res = await api.listQuickCmds()
    quickCmds.value = res.commands || []
  } catch (e) {
    toast.show(t('qc_load_failed'), 'error')
    console.warn('load quick cmds error:', e)
  } finally {
    cmdLoading.value = false
  }
}

function openQuickCmds() {
  quickCmdsVisible.value = true
  loadQuickCmds()
}

// 将命令内容写入终端；勾选“自动执行”的命令在输入后自动回车执行，
// 否则仅输入命令文本，由用户自行回车确认。
function runQuickCmd(cmd: QuickCmd) {
  if (!sock || sock.readyState !== WebSocket.OPEN) {
    toast.show(t('qc_not_connected'), 'error')
    return
  }
  // 多行内容统一转成回车（换行即输入命令的一部分）
  const payload = cmd.content.replace(/\r?\n/g, '\r') + (cmd.auto ? '\r' : '')
  sock.send(payload)
  scrollToBottom()
  // 点击命令卡片后自动关闭弹窗，并把光标聚焦到终端
  quickCmdsVisible.value = false
  term?.focus()
}

function onQuickCmdAdd() {
  editingCmd.value = null
  editVisible.value = true
}

function onQuickCmdEdit(cmd: QuickCmd) {
  // 直接持有列表中的引用，保存时据此更新对应卡片
  editingCmd.value = cmd
  editVisible.value = true
}

function onQuickCmdDelete(cmd: QuickCmd) {
  deleteTarget.value = cmd
  deleteDialogVisible.value = true
}

async function confirmDeleteQuickCmd() {
  const target = deleteTarget.value
  if (!target) return
  quickCmds.value = quickCmds.value.filter((c) => c.id !== target.id)
  deleteTarget.value = null
  deleteDialogVisible.value = false
  await persistQuickCmds()
  toast.show(t('qc_deleted'), 'success')
}

// 把当前列表整体写回持久化文件；失败时回滚本地列表（深拷贝快照，编辑场景也能正确回滚）
async function persistQuickCmds() {
  const snapshot = quickCmds.value.map((c) => ({ ...c }))
  try {
    const res = await api.saveQuickCmds(quickCmds.value)
    quickCmds.value = res.commands || quickCmds.value
  } catch (e) {
    quickCmds.value = snapshot
    toast.show(t('qc_save_failed'), 'error')
    console.warn('save quick cmds error:', e)
    throw e
  }
}

async function onQuickCmdSave(payload: { name: string; content: string; auto: boolean }) {
  if (editingCmd.value) {
    editingCmd.value.name = payload.name
    editingCmd.value.content = payload.content
    editingCmd.value.auto = payload.auto
  } else {
    quickCmds.value.push({
      id: crypto.randomUUID(),
      name: payload.name,
      content: payload.content,
      auto: payload.auto
    })
  }
  editVisible.value = false
  try {
    await persistQuickCmds()
    toast.show(t('qc_saved'), 'success')
  } catch {
    // persistQuickCmds 已提示失败
  }
}

// 快捷指令上移/下移：QuickCmdsDialog 已算出新顺序，这里整体写回持久化文件
async function onQuickCmdReorder(ordered: QuickCmd[]) {
  quickCmds.value = ordered
  try {
    await persistQuickCmds()
  } catch {
    // persistQuickCmds 已提示失败并回滚
  }
}

// ---------- 复制：桌面端鼠标选中已自动复制，不再提供复制按钮入口 ----------
// 剪贴板写入：优先异步 Clipboard API；非安全上下文（http 反代）时用 execCommand 兜底
function legacyCopy(text: string) {
  const ta = document.createElement('textarea')
  ta.value = text
  ta.style.position = 'fixed'
  ta.style.left = '-9999px'
  ta.style.top = '-9999px'
  ta.style.opacity = '0'
  document.body.appendChild(ta)
  ta.focus()
  ta.select()
  try {
    document.execCommand('copy')
  } catch {
    /* 忽略 */
  }
  document.body.removeChild(ta)
}

function copyText(text: string) {
  if (navigator.clipboard && window.isSecureContext) {
    navigator.clipboard.writeText(text).catch(() => legacyCopy(text))
  } else {
    legacyCopy(text)
  }
}

// ---------- 粘贴：读取剪贴板并发送到终端 ----------
async function pasteClipboard() {
  if (!term || !sock || sock.readyState !== WebSocket.OPEN) {
    showToast('' + t('term_not_connected') + '')
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
    <div
      class="flex items-center justify-between px-4 py-2.5 bg-surface dark:bg-[#111115] border-b border-line dark:border-[#2A2A32]">
      <div class="flex items-center gap-2">
        <span class="text-ink-faint dark:text-[#8A8A92] text-sm font-medium uppercase tracking-widest">{{ t('terminal_title') }}</span>
      </div>
      <div class="flex items-center gap-1">
        <button class="g-btn-ghost" :title="t('term_quick_cmds')" @click="openQuickCmds">{{ t('term_quick_cmds') }}</button>
        <button class="g-btn-ghost" :title="t('term_paste')" @click="pasteClipboard">{{ t('term_paste') }}</button>
        <button class="g-btn-ghost" @click="reconnect">{{ t('term_reconnect') }}</button>
        <button class="g-btn-ghost" @click="clearTerminal">{{ t('term_clear') }}</button>
      </div>
    </div>

    <!-- 终端容器，绑定触摸事件（相对定位，承载复制提示气泡）
         深色模式：#1A1A1A 底 / #4EC9B0 字；浅色模式：#faf5e9 底 / #1a1814 字（跟随控制台主题） -->
    <div ref="el" class="flex-1 term-container overflow-hidden relative border-l-[10px] border-b-[10px]
      bg-[#faf5e9] border-[#faf5e9] dark:bg-[#1A1A1A] dark:border-[#1A1A1A]" @touchstart="onTouchStart"
      @touchmove="onTouchMove" @touchend="onTouchEnd" @touchcancel="onTouchEnd">
      <!-- 复制/粘贴反馈提示 -->
      <div
        class="term-copy-toast absolute bottom-2 right-2 z-20 px-3 py-1.5 rounded-md bg-black/70 dark:bg-white/85 text-white dark:text-black text-xs font-medium shadow-card opacity-0 pointer-events-none transition-opacity duration-200 whitespace-nowrap">
      </div>
    </div>

    <!-- 移动端功能键（两行） -->
    <div
      class="flex md:hidden flex-col gap-1 px-3 py-2.5 pb-4 bg-bg dark:bg-[#0B0B0F] border-t border-line dark:border-[#2A2A32]"
      style="padding-bottom: calc(env(safe-area-inset-bottom, 0px) + 10px);">
      <!-- 第一行 -->
      <div class="flex items-center justify-around gap-1 flex-wrap">
        <!-- ESC -->
        <button class="px-2.5 py-1 text-xs font-medium bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] hover:bg-black/10 dark:hover:bg-white/15 transition" @click="sendKey('\x1b')">ESC</button>
        <!-- ↑ 上方向键（长按重复） -->
        <button class="px-2.5 py-1 text-xs font-medium bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] hover:bg-black/10 dark:hover:bg-white/15 transition select-none" @mousedown="startRepeat('\x1b[A')" @mouseup="stopRepeat" @mouseleave="stopRepeat" @touchstart.prevent="startRepeat('\x1b[A')" @touchend="stopRepeat" @touchcancel="stopRepeat">↑</button>
        <!-- Tab -->
        <button class="px-2.5 py-1 text-xs font-medium bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] hover:bg-black/10 dark:hover:bg-white/15 transition" @click="sendKey('\t')">Tab</button>
        <!-- Ctrl -->
        <button class="px-2.5 py-1 text-xs font-medium rounded-full transition" :class="ctrlPressed ? 'bg-brand text-white' : 'bg-black/5 dark:bg-white/10 text-ink-soft dark:text-[#A6A6AD]'" @click="toggleModifier('ctrl')">Ctrl</button>
        <!-- Shift -->
        <button class="px-2.5 py-1 text-xs font-medium rounded-full transition" :class="shiftPressed ? 'bg-brand text-white' : 'bg-black/5 dark:bg-white/10 text-ink-soft dark:text-[#A6A6AD]'" @click="toggleModifier('shift')">Shift</button>
        <!-- Alt -->
        <button class="px-2.5 py-1 text-xs font-medium rounded-full transition" :class="altPressed ? 'bg-brand text-white' : 'bg-black/5 dark:bg-white/10 text-ink-soft dark:text-[#A6A6AD]'" @click="toggleModifier('alt')">Alt</button>
        <!-- Insert -->
        <button class="px-2.5 py-1 text-xs font-medium bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] hover:bg-black/10 dark:hover:bg-white/15 transition" @click="sendKey('\x1b[2~')">Insert</button>
      </div>

      <!-- 第二行 -->
      <div class="flex items-center justify-around gap-1 flex-wrap">
        <!-- ← 左方向键 -->
        <button class="px-2.5 py-1 text-xs font-medium bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] hover:bg-black/10 dark:hover:bg-white/15 transition select-none" @mousedown="startRepeat('\x1b[D')" @mouseup="stopRepeat" @mouseleave="stopRepeat" @touchstart.prevent="startRepeat('\x1b[D')" @touchend="stopRepeat" @touchcancel="stopRepeat">←</button>
        <!-- ↓ 下方向键 -->
        <button class="px-2.5 py-1 text-xs font-medium bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] hover:bg-black/10 dark:hover:bg-white/15 transition select-none" @mousedown="startRepeat('\x1b[B')" @mouseup="stopRepeat" @mouseleave="stopRepeat" @touchstart.prevent="startRepeat('\x1b[B')" @touchend="stopRepeat" @touchcancel="stopRepeat">↓</button>
        <!-- → 右方向键 -->
        <button class="px-2.5 py-1 text-xs font-medium bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] hover:bg-black/10 dark:hover:bg-white/15 transition select-none" @mousedown="startRepeat('\x1b[C')" @mouseup="stopRepeat" @mouseleave="stopRepeat" @touchstart.prevent="startRepeat('\x1b[C')" @touchend="stopRepeat" @touchcancel="stopRepeat">→</button>
        <!-- . -->
        <button class="px-2.5 py-1 text-xs font-medium bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] hover:bg-black/10 dark:hover:bg-white/15 transition" @click="sendKey('.')">.</button>
        <!-- / -->
        <button class="px-2.5 py-1 text-xs font-medium bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] hover:bg-black/10 dark:hover:bg-white/15 transition" @click="sendKey('/')">/</button>
        <!-- - 新增 -->
        <button class="px-2.5 py-1 text-xs font-medium bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] hover:bg-black/10 dark:hover:bg-white/15 transition" @click="sendKey('-')">-</button>
        <!-- " -->
        <button class="px-2.5 py-1 text-xs font-medium bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] hover:bg-black/10 dark:hover:bg-white/15 transition" @click='sendKey("\"")'>"</button>
        <!-- = -->
        <button class="px-2.5 py-1 text-xs font-medium bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] hover:bg-black/10 dark:hover:bg-white/15 transition" @click="sendKey('=')">=</button>
      </div>
    </div>
  </div>

  <!-- 快捷指令列表弹窗 -->
  <QuickCmdsDialog
    v-model:visible="quickCmdsVisible"
    :commands="quickCmds"
    :loading="cmdLoading"
    @add="onQuickCmdAdd"
    @edit="onQuickCmdEdit"
    @delete="onQuickCmdDelete"
    @run="runQuickCmd"
    @reorder="onQuickCmdReorder"
  />

  <!-- 新增 / 编辑快捷指令弹窗 -->
  <QuickCmdEditDialog
    v-model:visible="editVisible"
    :cmd="editingCmd"
    @save="onQuickCmdSave"
  />

  <!-- 删除快捷指令确认 -->
  <ConfirmDialog
    v-model:visible="deleteDialogVisible"
    :title="t('qc_delete_confirm_title')"
    :message="t('qc_delete_confirm_msg', { name: deleteTarget?.name || '' })"
    :confirm-text="t('qc_delete')"
    :cancel-text="t('qc_cancel')"
    danger
    @confirm="confirmDeleteQuickCmd"
  />
</template>