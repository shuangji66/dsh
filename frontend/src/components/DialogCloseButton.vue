<script setup lang="ts">
// DialogCloseButton.vue —— 所有弹窗统一的右上角关闭按钮（X 图标）。
//
// 弹窗的关闭方式以前并不一致：有的用文字「×」、有的只有底部一个「关闭」按钮、有的干脆没有。
// 现在统一成「右上角 X」：位置、尺寸、配色、悬停与禁用态全部由 style.css 的 .g-dialog-close
// 提供，弹窗里不要再自己写一份（别自创尺寸或改成文字）。
//
// 用法：放进弹窗容器（.relative）内即可，绝对定位到右上角；文案用 i18n 的 dialog_close，
// 需要禁止关闭（如正在执行不可中断的操作）时传 :disabled。
//
// 与标题同一行居中：X 是 32px 见方、top-6（= 常规弹窗 p-6 的 24px 内边距，正好是标题行的
// 顶边），标题（.g-dialog-title）行高固定 2rem —— 两者同顶同高，中心自然重合，常规 p-6
// 弹窗不需要任何额外设置（绝对定位的 top 以内边距盒为基准、标题从内容盒起算，差的这一个
// padding 就由 top-6 补上，别改回 top-3）。唯一的例外是**标题栏式**弹窗（标题自带一条
// px-5 py-3 头部条、没有 p-6 内边距，如 QuickCmdsDialog）：传 class="top-3" 把 X 对到那条
// 32px 头部条上。
defineProps<{
  // 无障碍标签与悬停提示（一般传 t('dialog_close')）
  label: string
  disabled?: boolean
}>()

const emit = defineEmits<{ (e: 'close'): void }>()
</script>

<template>
  <button
    type="button"
    class="g-dialog-close"
    :title="label"
    :aria-label="label"
    :disabled="disabled"
    @click="emit('close')"
  >
    <svg
      class="w-4 h-4"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
    ><line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" /></svg>
  </button>
</template>
