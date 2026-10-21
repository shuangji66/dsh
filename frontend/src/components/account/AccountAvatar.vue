<script setup lang="ts">
// 账号头像（纯展示）：卡片里的当前账号 / 账号列表行共用。
//
// 优先用平台给的 avatarUrl；**没给**（账号本身没有头像）或**拉不下来**（离线 / URL 失效）
// 都退回 DeepSeek 图标 —— 否则圆框里会是一个破图占位。未登录用灰色人形占位图。
// 图片加载失败的状态由本组件持有：地址一变（切换账号 / 重新拉状态）就复位，好让新地址
// 重新试一次。
import { computed, ref, watch } from 'vue'
import { icons } from '@/utils/icons'

const props = withDefaults(
  defineProps<{
    avatarUrl?: string
    signedIn?: boolean
    size?: 'sm' | 'md' | 'lg'
  }>(),
  { avatarUrl: '', signedIn: false, size: 'md' }
)

const broken = ref(false)
watch(
  () => props.avatarUrl,
  () => {
    broken.value = false
  }
)

const showImage = computed(() => props.signedIn && !!props.avatarUrl && !broken.value)
const fallbackIcon = computed(() => (props.signedIn ? icons.deepseek : icons.user))

// Tailwind 只认静态类名，尺寸表按 size 写死（不要拼 `w-${n}` 这种运行期类名）。
const BOX: Record<string, string> = {
  sm: 'w-8 h-8',
  md: 'w-10 h-10',
  lg: 'w-14 h-14'
}
const ICON: Record<string, string> = {
  sm: 'w-4 h-4',
  md: 'w-5 h-5',
  lg: 'w-7 h-7'
}
</script>

<template>
  <span
    class="rounded-full flex items-center justify-center overflow-hidden flex-shrink-0 transition-colors"
    :class="[
      BOX[size],
      signedIn
        ? 'bg-brand/10 text-brand ring-1 ring-brand/30'
        : 'bg-black/[0.08] text-ink-faint dark:bg-white/10 dark:text-[#8A8A92]'
    ]"
  >
    <img
      v-if="showImage"
      :src="avatarUrl"
      alt=""
      class="w-full h-full object-cover"
      @error="broken = true"
    />
    <span v-else :class="ICON[size]" v-html="fallbackIcon"></span>
  </span>
</template>
