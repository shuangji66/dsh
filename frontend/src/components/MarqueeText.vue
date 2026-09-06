<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref } from 'vue'

const props = defineProps<{ text: string }>()

const track = ref<HTMLElement | null>(null)
const overflowing = ref(false)
// 动画时长（秒）随文本长度增大，保证较长名字的滚动速度更自然
const duration = ref(8)

function measure() {
  const el = track.value
  if (!el) return
  const parent = el.parentElement
  if (!parent) return
  overflowing.value = el.scrollWidth > parent.clientWidth
  duration.value = Math.max(6, Math.min(18, Math.round(el.scrollWidth / 32)))
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
      >{{ text }}</span
    >
  </span>
</template>

<style scoped>
/* 仅当文本溢出时启用：从右侧入画，匀速向左循环滚出 */
.marquee-track {
  padding-left: 100%;
  animation-name: marquee-scroll;
  animation-timing-function: linear;
  animation-iteration-count: infinite;
  padding-right: 2rem;
}

@keyframes marquee-scroll {
  from {
    transform: translateX(0);
  }
  to {
    transform: translateX(-100%);
  }
}
</style>