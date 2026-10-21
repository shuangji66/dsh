<script setup lang="ts">
// AccessCard.vue —— 概览页「快捷访问」卡片。
//
// 由概览页传入两类地址：
//   - fnosEntry：固定第一行的「飞牛入口」（当前访问环境下经飞牛网关访问 dsh 的地址），
//     由概览页按请求头/浏览器地址换算后传入，不参与用户配置；
//   - accessUrls：用户在设置页配置的 dsh 访问地址。
// 桌面浏览器里两者都直接新标签页打开；飞牛 App 的「飞牛入口」需要特殊处理，见下。
import { computed, ref } from 'vue'
import { useI18n } from '@/composables/useI18n'
import { useBodyScrollLock } from '@/composables/useBodyScrollLock'
import { isFnosApp } from '@/utils/fnosApp'

const props = defineProps<{ accessUrls?: string[]; fnosEntry?: string }>()

const { t } = useI18n()

// 过滤空白项：设置页允许中途保存空行，这里不显示空按钮
const urls = computed(() => (props.accessUrls || []).map((u) => u.trim()).filter(Boolean))
const empty = computed(() => !props.fnosEntry && urls.value.length === 0)

// 飞牛 App 内「飞牛入口」用全屏 iframe 承载 dsh。
//
// 为什么不用 window.open / location.assign：App 的网关鉴权是**按入口
// （gatewayPrefix）注入**的，不是浏览器那种 host 级 Cookie。控制台入口
// （/app/Harness）的凭据不会自动带到 dsh 入口（/app/Harness/dsh）：
//   - window.open 新建的 WebView 首个导航没有 dsh 入口的凭据；
//   - 同窗口 location.assign 也一样（实测控制台页面确实被导航走了，dsh 仍收到
//     trim_http_cgi 的 200 + 13 字节 "invalid token"）。
// 但 iframe 是**控制台页面自己的子资源请求**，走的是控制台这个 WebView 的请求栈；
// 而且入口地址现在改成了控制台 baseurl 之下的 /gui/（由控制台的 app.sock 反代到
// dsh，见 backend/admin.go 的 dshUI），不再跨到独立的 dsh 入口。如果仍然被网关
// 拒绝（iframe 里出现 13 字节错误页），只显示一个全屏的重试面板；不提供
// 「在浏览器打开」，也不提供返回控制台的按钮。
// 桌面浏览器不受影响，仍新标签页打开。
const frameVisible = ref(false)
const frameUrl = ref('')
const frameError = ref(false)
const frameKey = ref(0)
useBodyScrollLock(frameVisible)

function normalizeURL(url: string): string {
  const u = url.trim()
  if (!u) return ''
  return /^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(u) ? u : 'http://' + u
}

// 打开访问地址（设置页里的自定义地址）：桌面/App 都按原行为新标签页打开。
function open(url: string) {
  const final = normalizeURL(url)
  if (final) window.open(final, '_blank', 'noopener')
}

function openFnosEntry() {
  const final = normalizeURL(props.fnosEntry || '')
  if (!final) return
  if (!isFnosApp()) {
    window.open(final, '_blank', 'noopener')
    return
  }
  frameUrl.value = final
  frameError.value = false
  frameKey.value++
  frameVisible.value = true
}

function retryFrame() {
  frameError.value = false
  frameKey.value++
}

function onFrameLoad(ev: Event) {
  const el = ev.target as HTMLIFrameElement
  let text = ''
  try {
    text = (el.contentDocument?.body?.innerText || '').trim()
  } catch {
    return // 理论上同源；读不到就不动
  }
  if (text === 'invalid token') {
    frameError.value = true
  }
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
        @click="openFnosEntry"
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

  <!-- App 内的全屏 dsh 容器：走控制台 WebView 的请求栈，避免新 WebView 的入口凭据缺失。
       按要求不保留顶栏：没有「在浏览器打开」，也没有返回控制台的按钮。 -->
  <Teleport to="body">
    <div v-if="frameVisible" class="fixed inset-0 z-[200] bg-bg dark:bg-[#0B0B0F]">
      <div v-if="frameError" class="h-full flex items-center justify-center p-6">
        <div class="g-card p-5 w-full max-w-sm text-center">
          <p class="text-sm text-ink-soft dark:text-[#A6A6AD] leading-relaxed">{{ t('access_urls_fnos_blocked') }}</p>
          <div class="flex items-center justify-center gap-2 mt-4">
            <button class="g-btn-secondary" @click="retryFrame">{{ t('access_urls_retry') }}</button>
          </div>
        </div>
      </div>
      <iframe
        v-else
        :key="frameKey"
        :src="frameUrl"
        class="w-full h-full border-0 bg-white dark:bg-[#0B0B0F]"
        @load="onFrameLoad"
      ></iframe>
    </div>
  </Teleport>
</template>
