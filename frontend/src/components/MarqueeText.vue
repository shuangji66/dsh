<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'

const props = defineProps<{ text: string }>()

const root = ref<HTMLElement | null>(null)
const track = ref<HTMLElement | null>(null)
const seg = ref<HTMLElement | null>(null)
const overflowing = ref(false)
// 是否在视口附近：离屏的插件卡片不跑动画、也不保留合成层。
// 支持 IntersectionObserver 时先按「不可见」渲染，等首帧回调再决定；
// 不支持（或没有 root）时退回 true，保证动画照常运行。
const visible = ref(typeof IntersectionObserver === 'undefined')
// 动画时长（秒）随文本长度增大，保证较长名字的滚动速度更自然
const duration = ref(8)

let observer: IntersectionObserver | null = null
let measureRaf = 0

function measure() {
  const segEl = seg.value
  const el = track.value
  if (!segEl || !el) return
  const parent = el.parentElement
  if (!parent) return
  // 以单份文本宽度判断是否溢出并计算时长；副本仅用于无缝循环，不参与测量。
  const single = segEl.getBoundingClientRect().width
  overflowing.value = single > parent.clientWidth
  duration.value = Math.max(6, Math.min(18, Math.round(single / 32)))
}

// 大量插件卡片同时监听 window.resize 时，每个实例都量一次会集中在同一帧里抖动；
// 用 rAF 合并到每帧一次（各实例仍各量一次，但不会重复排队）。
function scheduleMeasure() {
  if (measureRaf) return
  measureRaf = requestAnimationFrame(() => {
    measureRaf = 0
    measure()
  })
}

onMounted(() => {
  // 首帧后测量（等字体就绪），再监听尺寸变化
  scheduleMeasure()
  window.addEventListener('resize', scheduleMeasure)
  // 离屏卡片暂停动画并移除 will-change：长列表（几十个插件）时，
  // 每个跑马灯都是一个常驻合成层，白白占 GPU 内存。
  if (typeof IntersectionObserver !== 'undefined' && root.value) {
    observer = new IntersectionObserver(
      (entries) => {
        visible.value = entries[0]?.isIntersecting ?? true
      },
      { rootMargin: '120px' }
    )
    observer.observe(root.value)
  } else {
    visible.value = true
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', scheduleMeasure)
  if (measureRaf) cancelAnimationFrame(measureRaf)
  observer?.disconnect()
  observer = null
})
</script>

<template>
  <span ref="root" class="marquee-root block overflow-hidden">
    <span
      ref="track"
      class="inline-block whitespace-nowrap"
      :class="{
        'marquee-track': overflowing,
        'will-change-transform': overflowing && visible,
        'marquee-paused': overflowing && !visible,
      }"
      :style="overflowing ? { animationDuration: duration + 's' } : undefined"
    >
      <span ref="seg" class="marquee-seg">{{ text }}</span>
      <!-- 第二份副本：用于无缝循环；仅在溢出时渲染，避免无谓的 DOM 与测量干扰 -->
      <span v-if="overflowing" class="marquee-seg" aria-hidden="true">{{ text }}</span>
    </span>
  </span>
</template>

<style scoped>
/* 仅当文本溢出时启用：文本初始左对齐，动画匀速向左循环滚出。 */
.marquee-track {
  animation-name: marquee-scroll;
  animation-timing-function: linear;
  animation-iteration-count: infinite;
}

/* 离屏（IntersectionObserver 判定在视口外）时暂停：动画与合成层都省下来。
   animation-play-state 不会重启动画，滚回视口时从原位置继续。 */
.marquee-paused {
  animation-play-state: paused;
}

/* 尊重系统的「减少动态效果」：不滚动；改为允许手动横向拖动查看完整名称
   （否则 overflow-hidden 会把名字直接截断，反而读不到）。 */
@media (prefers-reduced-motion: reduce) {
  .marquee-track {
    animation: none;
  }
  .marquee-root {
    overflow-x: auto;
  }
}

/* 每份副本自带尾部间隔（即循环中的首尾间隙）。
   两份副本总宽相等，translateX(-50%) 恰好移动一份副本的宽度，
   从而做到无缝衔接且间隔可控、不会出现额外空白。 */
.marquee-seg {
  display: inline-block;
  padding-right: 1rem;
}

@keyframes marquee-scroll {
  from {
    transform: translateX(0);
  }
  to {
    transform: translateX(-50%);
  }
}
</style>