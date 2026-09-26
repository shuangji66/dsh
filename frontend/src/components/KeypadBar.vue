<script setup lang="ts">
// 终端页底部辅助键条（仅在「移动端布局」下渲染：触屏设备或窄视口，见 useMobileLayout）：
//   【第一页】功能键与常用符号
//     第一行：ESC | ↑ | Tab | Ctrl | Alt | Shift | Insert（↑ 在第二位，与第二行的
//             ← ↓ → 组成倒 T 型方向键组合；Insert 在最右）
//     第二行：← | ↓ | → | / | - | = | " | .（`.` 放在 `"` 右侧）
//   Shift **锁定态**（一直有效，直到再点一次 Shift 才解除）：方向键换成
//     ←→ Home/End、↑↓ PageUp/PageDown（键面显示 HM/ED/PU/PD，序列见 SHIFT_CURSOR）；
//   符号键发上档字符，
//     **键面也显示上档字符**，并换成品牌色底/字表示「已上档」。映射见 SHIFT_MAP
//     （.→<  ,→>  /→?  ;→:  "→'  [→{  ]→}  \→|  -→_  =→+  `→~）。
//   【第二页】常见标点（两行、每行 8 个）
//     第一行：! @ # $ % ^ & *    第二行：, ; [ ] \ ` ( )（8 个）
//   切页：**点两侧的竖长条按钮**（触屏与鼠标都可用）。
//     第一页只在**右侧**显示「›」（进入第二页）；第二页只在**左侧**显示「‹」（回到第一页）；
//     不循环、不支持滑动切页。按钮是细长竖条，纵向高度与两行标点一致。
//   **平板（≥md 断点）两页并排同时显示**，不显示切页按钮。
// 方向键支持长按连发；所有按键统一 emit('key') 交给终端页发送。
// 交互策略：@click 覆盖桌面/部分移动端；同时用 @touchstart.prevent 作为移动端
//   保底（避免浏览器在 prevent.stop 容器中不合成 click 导致按键无效）。
// 显隐不能交给 md:hidden（宽度断点）：iPad 宽度 ≥768px 会被判成桌面而丢掉整条辅助键，
//   改用 composables/useMobileLayout.ts 的「触屏或窄视口」判据；「是否平板档」用同文件的
//   useWideLayout()（就是 md 断点，别另发明数值）。
import { onBeforeUnmount, watch } from 'vue'
import { useMobileLayout, useWideLayout } from '@/composables/useMobileLayout'
import { useKeypadPage } from '@/composables/useKeypadPage'
import { useI18n } from '@/composables/useI18n'

const { t } = useI18n()
const mobileLayout = useMobileLayout()
// 平板档：两页并排显示（宽度够，不需要切换）
const wide = useWideLayout()
const { page, step } = useKeypadPage()

const props = defineProps<{
  ctrl: boolean
  alt: boolean
  shift: boolean
}>()

const emit = defineEmits<{
  (e: 'key', data: string): void
  (e: 'toggle', mod: 'ctrl' | 'alt' | 'shift'): void
}>()

// 方向键长按连发
let repeatTimer: number | null = null

function startRepeat(name: CursorName) {
  if (repeatTimer) return
  emit('key', cursorSeq(name))
  repeatTimer = window.setInterval(() => emit('key', cursorSeq(name)), 100)
}
function stopRepeat() {
  if (repeatTimer) {
    clearInterval(repeatTimer)
    repeatTimer = null
  }
}

// 连发定时器必须跟着生命周期走：按键所在容器是 `v-if="wide || page === 0"`，
// 切页或切布局档会把按住的按钮整体卸载 —— 之后 touchend/mouseup 不再到达该节点，
// 100ms 的连发会一直往 PTY 灌 ESC 序列（组件卸载也不会自动停）。
watch([page, wide], stopRepeat)
onBeforeUnmount(stopRepeat)

// 单次按键：@click + @touchstart.prevent 双保险
// touchstart 用于移动端保底；touchend 时不重复触发（点击只触发一次）。
// click 是桌面端主要路径；touchstart 在移动端会抢先触发一次，click 因
// 被 prevent 默认行为抑制后不合成，恰好保证只发一次。
function send(data: string) {
  emit('key', data)
}

// 键码映射（避免模板内联引号转义问题）
const K = {
  esc: '\x1b',
  tab: '\t',
  insert: '\x1b[2~',
  left: '\x1b[D',
  up: '\x1b[A',
  down: '\x1b[B',
  right: '\x1b[C'
}

// 第一页第二行的符号（顺序即显示顺序：`.` 在 `"` 右侧）
const SYM_DOT = '.'
const SYM_SLASH = '/'
const SYM_DASH = '-'
const SYM_EQ = '='
const SYM_DQUOTE = '"'

// 第二页：常见标点（第一行 8 个；第二行是「不上档」形态，上档形态由 SHIFT_MAP 给出）
const PUNCT_ROW1 = ['!', '@', '#', '$', '%', '^', '&', '*']
const PUNCT_ROW2 = [',', ';', '[', ']', '\\', '`', '(', ')']

// Shift 锁定时符号键的**上档映射**（键面显示与发出的字符都换成它）
const SHIFT_MAP: Record<string, string> = {
  '.': '<',
  ',': '>',
  '/': '?',
  ';': ':',
  '"': "'",
  '[': '{',
  ']': '}',
  '\\': '|',
  '-': '_',
  '=': '+',
  '`': '~'
}

// 方向键：Shift 锁定时换成 Home/End/PageUp/PageDown。
// 序列与 xterm 上真实键盘一致（Home/End 是 CSI H / CSI F，PageUp/PageDown 是 CSI 5~ / 6~），
// 键面显示两字母缩写（HM/ED/PU/PD），面板窄也放得下。
type CursorName = 'left' | 'up' | 'down' | 'right'
const SHIFT_CURSOR: Record<CursorName, { label: string; seq: string }> = {
  left: { label: 'HM', seq: '\x1b[H' }, // Home
  up: { label: 'PU', seq: '\x1b[5~' }, // PageUp
  down: { label: 'PD', seq: '\x1b[6~' }, // PageDown
  right: { label: 'ED', seq: '\x1b[F' } // End
}
// 方向键的基础字形（同时作为 data-key，便于脚本按基础键定位；上档后字会变）
function cursorGlyph(name: CursorName): string {
  return { left: '←', up: '↑', down: '↓', right: '→' }[name]
}
// 方向键要发出的序列：Shift 锁定时发上档序列（长按连发同样跟着变）
function cursorSeq(name: CursorName): string {
  return props.shift ? SHIFT_CURSOR[name].seq : K[name]
}
// 方向键键面：Shift 锁定时显示缩写
function cursorLabel(name: CursorName): string {
  return props.shift ? SHIFT_CURSOR[name].label : cursorGlyph(name)
}

// 符号键发送：Shift 锁定且该键有上档映射时发上档字符，否则原样。
// Shift 是**锁定态**（不清除，见 TerminalView 里修饰键自动解除的说明），可连续上档输入。
function sendSym(ch: string) {
  send(props.shift && SHIFT_MAP[ch] ? SHIFT_MAP[ch] : ch)
}
// 该键当前是否处于「已上档」状态（用于变色）
function isUpshifted(ch: string): boolean {
  return props.shift && SHIFT_MAP[ch] !== undefined
}
// 键面文字：与发出的字符一致——Shift 锁定时显示上档字符（如 . → <）
function label(ch: string): string {
  return props.shift && SHIFT_MAP[ch] ? SHIFT_MAP[ch] : ch
}

const keyCls =
  'px-2 py-1.5 text-[11px] font-medium min-w-8 bg-black/5 dark:bg-white/10 rounded-full text-ink-soft dark:text-[#A6A6AD] select-none transition touch-manipulation'
const modOnCls = '!bg-brand !text-white'
// 上档态符号键：品牌色字 + 淡品牌底（区别于修饰键「已按下」的实心态）
const upShiftCls = '!bg-brand/15 !text-brand'
// 切页按钮：细长竖条 —— self-stretch 让它与两行标点等高，w-4 保持纤细
const navCls =
  'w-4 shrink-0 self-stretch flex items-center justify-center rounded-md text-base leading-none bg-black/5 dark:bg-white/10 text-ink-soft dark:text-[#A6A6AD] select-none transition touch-manipulation'
</script>

<template>
  <div
    v-if="mobileLayout"
    data-keypad-bar
    class="flex shrink-0 flex-col gap-1 px-2 pt-1.5 pb-2 bg-bg dark:bg-[#0B0B0F] border-t border-line dark:border-[#2A2A32] select-none"
    style="touch-action: manipulation"
    @touchstart.prevent.stop
  >
    <div class="flex items-stretch gap-1 w-full">
      <!-- 第二页：左侧竖长条按钮（回到第一页）。第一页不显示左按钮，所以不会循环。 -->
      <button
        v-if="!wide && page === 1"
        :class="navCls"
        :title="t('keypad_prev_page')"
        :aria-label="t('keypad_prev_page')"
        @click="step(-1)"
        @touchstart.prevent="step(-1)"
      >
        ‹
      </button>

      <!-- ===== 第一页：功能键 / 方向键 / 常用符号 =====
           平板档两页并排时各占一半；手机为单页故全宽 -->
      <div v-if="wide || page === 0" class="flex flex-col gap-1 flex-1 min-w-0">
        <!-- 第一行：ESC | ↑ | Tab | Ctrl | Alt | Shift | Insert（↑ 为倒 T 方向键的顶端，Insert 最右） -->
        <div class="flex items-center justify-around gap-1 flex-wrap">
          <!-- ESC：@click + @touchstart.prevent 双保险 -->
          <button
            class="rounded-full"
            :class="keyCls"
            @click="send(K.esc)"
            @touchstart.prevent="send(K.esc)"
          >
            ESC
          </button>
          <!-- ↑ 方向键：长按连发；Shift 锁定时变 PageUp（PU） -->
          <button
            class="rounded-full"
            :data-key="cursorGlyph('up')"
            :class="[keyCls, shift ? upShiftCls : '']"
            @mousedown="startRepeat('up')"
            @mouseup="stopRepeat"
            @mouseleave="stopRepeat"
            @touchstart.prevent="startRepeat('up')"
            @touchend="stopRepeat"
            @touchcancel="stopRepeat"
          >
            {{ cursorLabel('up') }}
          </button>
          <!-- Tab -->
          <button class="rounded-full" :class="keyCls" @click="send(K.tab)" @touchstart.prevent="send(K.tab)">Tab</button>
          <!-- 修饰键切换 -->
          <button
            class="rounded-full"
            :class="[keyCls, ctrl ? modOnCls : '']"
            @click="emit('toggle', 'ctrl')"
            @touchstart.prevent="emit('toggle', 'ctrl')"
          >
            Ctrl
          </button>
          <button
            class="rounded-full"
            :class="[keyCls, alt ? modOnCls : '']"
            @click="emit('toggle', 'alt')"
            @touchstart.prevent="emit('toggle', 'alt')"
          >
            Alt
          </button>
          <button
            class="rounded-full"
            :class="[keyCls, shift ? modOnCls : '']"
            @click="emit('toggle', 'shift')"
            @touchstart.prevent="emit('toggle', 'shift')"
          >
            Shift
          </button>
          <!-- Insert（显示完整，不用缩写） -->
          <button
            class="rounded-full"
            :class="keyCls"
            @click="send(K.insert)"
            @touchstart.prevent="send(K.insert)"
          >
            Insert
          </button>
        </div>

        <!-- 第二行：← | ↓ | →（倒 T 下方三键）+ 常用符号 -->
        <div class="flex items-center justify-around gap-1 flex-wrap">
          <!-- ← ↓ →：Shift 锁定时分别变 Home(HM) / PageDown(PD) / End(ED) -->
          <button class="rounded-full" :data-key="cursorGlyph('left')" :class="[keyCls, shift ? upShiftCls : '']" @mousedown="startRepeat('left')" @mouseup="stopRepeat" @mouseleave="stopRepeat" @touchstart.prevent="startRepeat('left')" @touchend="stopRepeat" @touchcancel="stopRepeat">{{ cursorLabel('left') }}</button>
          <button class="rounded-full" :data-key="cursorGlyph('down')" :class="[keyCls, shift ? upShiftCls : '']" @mousedown="startRepeat('down')" @mouseup="stopRepeat" @mouseleave="stopRepeat" @touchstart.prevent="startRepeat('down')" @touchend="stopRepeat" @touchcancel="stopRepeat">{{ cursorLabel('down') }}</button>
          <button class="rounded-full" :data-key="cursorGlyph('right')" :class="[keyCls, shift ? upShiftCls : '']" @mousedown="startRepeat('right')" @mouseup="stopRepeat" @mouseleave="stopRepeat" @touchstart.prevent="startRepeat('right')" @touchend="stopRepeat" @touchcancel="stopRepeat">{{ cursorLabel('right') }}</button>
          <!-- 常用符号（Shift 锁定时发上档字符并变色）：@click + @touchstart.prevent 双保险 -->
          <button class="rounded-full" :data-key="SYM_SLASH" :class="[keyCls, isUpshifted(SYM_SLASH) ? upShiftCls : '']" @click="sendSym(SYM_SLASH)" @touchstart.prevent="sendSym(SYM_SLASH)">{{ label(SYM_SLASH) }}</button>
          <button class="rounded-full" :data-key="SYM_DASH" :class="[keyCls, isUpshifted(SYM_DASH) ? upShiftCls : '']" @click="sendSym(SYM_DASH)" @touchstart.prevent="sendSym(SYM_DASH)">{{ label(SYM_DASH) }}</button>
          <button class="rounded-full" :data-key="SYM_EQ" :class="[keyCls, isUpshifted(SYM_EQ) ? upShiftCls : '']" @click="sendSym(SYM_EQ)" @touchstart.prevent="sendSym(SYM_EQ)">{{ label(SYM_EQ) }}</button>
          <button class="rounded-full" :data-key="SYM_DQUOTE" :class="[keyCls, isUpshifted(SYM_DQUOTE) ? upShiftCls : '']" @click="sendSym(SYM_DQUOTE)" @touchstart.prevent="sendSym(SYM_DQUOTE)">{{ label(SYM_DQUOTE) }}</button>
          <button class="rounded-full" :data-key="SYM_DOT" :class="[keyCls, isUpshifted(SYM_DOT) ? upShiftCls : '']" @click="sendSym(SYM_DOT)" @touchstart.prevent="sendSym(SYM_DOT)">{{ label(SYM_DOT) }}</button>
        </div>
      </div>

      <!-- 平板档：两页之间的分隔线 -->
      <div v-if="wide" class="w-px self-stretch bg-line dark:bg-[#2A2A32]"></div>

      <!-- ===== 第二页：常见标点（每行 8 个） ===== -->
      <div v-if="wide || page === 1" class="flex flex-col gap-1 flex-1 min-w-0">
        <div class="flex items-center justify-around gap-1 flex-wrap">
          <button
            v-for="c in PUNCT_ROW1"
            :key="c"
            :data-key="c"
            class="rounded-full"
            :class="[keyCls, isUpshifted(c) ? upShiftCls : '']"
            @click="sendSym(c)"
            @touchstart.prevent="sendSym(c)"
          >
            {{ label(c) }}
          </button>
        </div>
        <div class="flex items-center justify-around gap-1 flex-wrap">
          <button
            v-for="c in PUNCT_ROW2"
            :key="c"
            :data-key="c"
            class="rounded-full"
            :class="[keyCls, isUpshifted(c) ? upShiftCls : '']"
            @click="sendSym(c)"
            @touchstart.prevent="sendSym(c)"
          >
            {{ label(c) }}
          </button>
        </div>
      </div>

      <!-- 第一页：右侧竖长条按钮（进入第二页）。第二页不显示右按钮。 -->
      <button
        v-if="!wide && page === 0"
        :class="navCls"
        :title="t('keypad_next_page')"
        :aria-label="t('keypad_next_page')"
        @click="step(1)"
        @touchstart.prevent="step(1)"
      >
        ›
      </button>
    </div>
  </div>
</template>
