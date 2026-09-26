<script setup lang="ts">
// AccessCard.vue —— 概览页「快捷访问」卡片。
//
// 由概览页传入两类地址：
//   - fnosEntry：固定第一行的「飞牛入口」（当前访问环境下经飞牛网关访问 dsh 的地址），
//     由概览页按请求头/浏览器地址换算后传入，不参与用户配置；
//   - accessUrls：用户在设置页配置的 dsh 访问地址。
// 两者都只是「点击即新标签页打开」，因此这里不需要任何后端请求与状态，保持纯展示。
import { computed } from 'vue'
import { useI18n } from '@/composables/useI18n'

const props = defineProps<{ accessUrls?: string[]; fnosEntry?: string }>()

const { t } = useI18n()

// 过滤空白项：设置页允许中途保存空行，这里不显示空按钮
const urls = computed(() => (props.accessUrls || []).map((u) => u.trim()).filter(Boolean))
const empty = computed(() => !props.fnosEntry && urls.value.length === 0)

// 打开访问地址（新标签页）。裸地址（没有 scheme）补 http://，否则 window.open 会当相对路径。
function open(url: string) {
  const u = url.trim()
  if (!u) return
  const final = /^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(u) ? u : 'http://' + u
  window.open(final, '_blank', 'noopener')
}
</script>

<template>
  <section class="g-card g-card-hover p-5 flex flex-col">
    <h2 class="font-display text-base font-semibold text-ink dark:text-white">{{ t('access_urls_overview_title') }}</h2>
    <p class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1 mb-4">{{ t('access_urls_hint') }}</p>

    <p v-if="empty" class="text-sm text-ink-faint dark:text-[#8A8A92] py-4 text-center">{{ t('access_urls_empty') }}</p>

    <div v-else class="flex flex-col gap-2">
      <!-- 固定项：只显示「飞牛入口」名称，具体地址放 title 里悬停查看 -->
      <button
        v-if="fnosEntry"
        class="group w-full flex items-center justify-between gap-2 px-3 py-2 rounded-lg bg-brand/[0.06] dark:bg-brand/[0.12]
          border border-brand/30 text-xs font-medium text-brand
          hover:bg-brand/10 dark:hover:bg-brand/20 transition-colors duration-150"
        :title="fnosEntry"
        @click="open(fnosEntry)"
      >
        <span class="truncate">{{ t('access_urls_fnos_entry') }}</span>
        <svg class="w-3.5 h-3.5 shrink-0 transition-transform duration-200 group-hover:translate-x-0.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M7 17 17 7"/><path d="M9 7h8v8"/></svg>
      </button>

      <button
        v-for="(url, i) in urls"
        :key="i"
        class="group w-full flex items-center justify-between gap-2 px-3 py-2 rounded-lg bg-transparent
          border border-ink/15 dark:border-white/25 text-xs font-mono text-ink dark:text-white
          hover:bg-black/5 dark:hover:bg-white/10 hover:border-ink/30 dark:hover:border-white/40 transition-colors duration-150"
        :title="url"
        @click="open(url)"
      >
        <span class="truncate">{{ url }}</span>
        <svg class="w-3.5 h-3.5 shrink-0 opacity-50 transition-all duration-200 group-hover:opacity-100 group-hover:translate-x-0.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M7 17 17 7"/><path d="M9 7h8v8"/></svg>
      </button>
    </div>
  </section>
</template>
