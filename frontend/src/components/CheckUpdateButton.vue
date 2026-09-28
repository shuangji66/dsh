<script setup lang="ts">
// CheckUpdateButton.vue —— 版本行右侧的「检查更新」图标按钮（harness / dsh / 市场三行共用一份）。
//
// 图标刻意与「dsh 服务回退」那个按钮成对：这里顺时针（Lucide rotate-cw）、回退逆时针
// （Lucide rotate-ccw），同一套线稿 —— 9 半径的圆 + 由圆弧收尾的箭头，摆在一行里才像一家人。
// 之前用的是 Feather 的 refresh-cw：圆上缺口更大、箭头是两条直边，跟回退图标并排明显不是
// 一套（反馈：「不够圆，箭头有点直」）。换图标时请连回退那个一起看（UpdateSection.vue 里
// 的 rollback 按钮）。
//
// 检查进行中图标转圈并禁用（animate-spin 顺时针，与图标自身方向一致），避免重复触发。
// 悬停色与回退按钮一致用品牌色（两个都是「图标按钮」，风格保持同一套）。
defineProps<{
  // 是否正在检查（转圈 + 禁用）
  checking: boolean
  // 无障碍标签与悬停提示（一般传 t('update_check')）
  label: string
}>()

const emit = defineEmits<{ (e: 'check'): void }>()
</script>

<template>
  <button
    type="button"
    class="flex-shrink-0 text-ink-soft dark:text-[#A6A6AD] hover:text-brand dark:hover:text-brand transition-colors disabled:opacity-50"
    :title="label"
    :aria-label="label"
    :disabled="checking"
    @click="emit('check')"
  >
    <svg
      :class="checking ? 'animate-spin' : ''"
      class="h-4 w-4"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
    ><path d="M21 12a9 9 0 1 1-9-9c2.52 0 4.93 1 6.74 2.74L21 8" /><path d="M21 3v5h-5" /></svg>
  </button>
</template>
