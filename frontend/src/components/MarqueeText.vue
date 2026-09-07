<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'

const props = defineProps<{ text: string }>()

const track = ref<HTMLElement | null>(null)
const seg = ref<HTMLElement | null>(null)
const overflowing = ref(false)
// 动画时长（秒）随文本长度增大，保证较长名字的滚动速度更自然
const duration = ref(8)

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

onMounted(() => {
  // 首帧后测量（等字体就绪），再监听尺寸变化
  requestAnimationFrame(measure)
  window.addEventListener('resize', measure)
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', measure)
})
</script>

<template>
  <span class="block overflow-hidden">
    <span
      ref="track"
      class="inline-block whitespace-nowrap will-change-transform"
      :class="overflowing ? 'marquee-track' : ''"
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