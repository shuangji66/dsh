<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from '@/composables/useI18n'
import { useWideLayout } from '@/composables/useMobileLayout'
import type { Visitor } from '@/serverapi'
import { formatDateTime } from '@/utils/format'

// 概览页「登录列表」卡片。数据来自父组件的 SSE（唯一的 visitors 来源），
// 本组件只负责排序、来源标记与桌面端的「最多 3 条」限高。
const props = defineProps<{
  visitors: Visitor[]
  loading: boolean
  /** 正在注销/删除的那一条；只禁用该行按钮。 */
  deletingId: string | null
}>()

const emit = defineEmits<{ (e: 'remove', id: string): void }>()

const { t, locale } = useI18n()

// 网关访问（飞牛入口）没有 harness 会话 cookie：列表里标记来源为「网关访问」、
// 显示飞牛用户名与来源 IP（同一个人从不同环境访问飞牛时 IP 不同，各占一条），
// 不显示登录有效期。它没有会话可注销，但可以手动删除这条记录（只清列表、不踢人，
// 该用户再次访问时会重新出现），不必干等闲置超时。
function isGateway(v: Visitor): boolean {
  return v.source === 'gateway'
}

// 列表条目「名称」：网关访问是飞牛用户名，端口访问是来源 IP（与列表里显示的保持一致）。
function visitorName(v: Visitor): string {
  return (isGateway(v) ? v.username : v.ip) || ''
}

// 按名称排序后再渲染：后端每次快照都是遍历 map（顺序随机），而 SSE 会随用户连接、
// 注销与过期清理不断重推整份列表 —— 不排序的话同一条记录会不停跳位。名称相同的
// （同一用户在多个环境/端口访问）再按 id 兜底，保证顺序稳定。
const sortedVisitors = computed(() =>
  [...props.visitors].sort((a, b) => {
    const cmp = visitorName(a).localeCompare(visitorName(b), undefined, { numeric: true, sensitivity: 'base' })
    return cmp !== 0 ? cmp : a.id.localeCompare(b.id)
  })
)

// 登录列表的限高**只在桌面端**生效：最多同时显示 3 条，超出后在卡片内滚动
// （桌面栅格里卡片高度受相邻卡片影响，不限高会把整行越撑越长）。
//
// 移动端刻意**不限制高度**：手机上没有「相邻卡片同高」的约束，页面本身就在 `main`
// 里滚动；再套一层内部滚动只会让触屏手势和底栏互相打架。列表随内容增长，由页面滚动。
//
// 高度不能写死像素：网关记录只有「来源 IP / 最近访问」两行，端口记录还多一行
// 「登录有效期」，窄屏下文本换行也会改变行高。因此按**前 3 条记录的实际渲染高度**
// 计算滚动区的 max-height（只限制列表、不限制卡片：同一栅格里相邻卡片更高时，
// 卡片仍会被拉伸到同高）。
const VISITOR_VISIBLE_COUNT = 3
const wideLayout = useWideLayout()
const visitorCardEl = ref<HTMLElement | null>(null)
const visitorListEl = ref<HTMLElement | null>(null)
const visitorListMaxHeight = ref<number | null>(null)

function measureVisitorList() {
  const el = visitorListEl.value
  // 移动端（<md）不限高：直接清掉 max-height，列表随内容自然增长。
  if (!el || !wideLayout.value) {
    visitorListMaxHeight.value = null
    return
  }
  const items = Array.from(el.children) as HTMLElement[]
  if (items.length <= VISITOR_VISIBLE_COUNT) {
    visitorListMaxHeight.value = null
    return
  }
  let height = 0
  for (let i = 0; i < VISITOR_VISIBLE_COUNT; i++) {
    height += items[i].getBoundingClientRect().height
  }
  visitorListMaxHeight.value = Math.ceil(height)
}

// 列表内容/条数变化（初次加载、SSE 推送、删除、语言切换）后重算；
// immediate 在 setup 阶段跑一次时节点还没挂载，函数自己会安全返回。
watch(
  () => sortedVisitors.value,
  async () => {
    await nextTick()
    measureVisitorList()
  },
  { immediate: true }
)

// 宽度变化会改变文本换行与行高（栅格列数、侧栏折叠、窗口缩放），需要重新量；
// 切换语言会改变标签长度与换行，而列表被 max-height 固定时卡片尺寸不再变化，
// ResizeObserver 察觉不到，因此单独监听 locale。
watch(locale, async () => {
  await nextTick()
  measureVisitorList()
})

// 跨过 md 断点（窗口缩放 / 旋转）时重新决定是否限高：进入桌面要重新量，
// 回到移动端要立刻清掉已有的 max-height。
watch(wideLayout, async () => {
  await nextTick()
  measureVisitorList()
})

let visitorResizeObserver: ResizeObserver | null = null

onMounted(() => {
  if (typeof ResizeObserver !== 'undefined' && visitorCardEl.value) {
    visitorResizeObserver = new ResizeObserver(() => measureVisitorList())
    visitorResizeObserver.observe(visitorCardEl.value)
  }
})

onBeforeUnmount(() => {
  visitorResizeObserver?.disconnect()
  visitorResizeObserver = null
})
</script>

<template>
  <!-- ④ 登录列表：与其它卡片同宽（桌面 2×2 的最后半格），不再独占一行。
       桌面端仍按「最多显示 3 条」限高并内部滚动，移动端不限制高度、随页面滚动。
       字段仍按「逐项独立一行」排列。 -->
  <section ref="visitorCardEl" class="g-card g-card-hover p-5 flex flex-col">
    <h2 class="font-display text-base font-semibold text-ink dark:text-white">{{ t('login_list') }}</h2>
    <p class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1 mb-4">{{ t('login_list_desc') }}</p>

    <div v-if="loading" class="text-sm text-ink-soft dark:text-[#A6A6AD] py-6 text-center">{{ t('loading') }}</div>

    <div v-else-if="visitors.length === 0" class="text-sm text-ink-faint dark:text-[#8A8A92] py-6 text-center">
      {{ t('no_visitors') }}
    </div>

    <ul
      v-else
      ref="visitorListEl"
      class="divide-y divide-line dark:divide-[#2A2A32] overflow-y-auto overscroll-contain pr-4 -mr-4"
      :class="wideLayout ? '[scrollbar-gutter:stable]' : ''"
      :style="visitorListMaxHeight ? { maxHeight: visitorListMaxHeight + 'px' } : undefined"
    >
      <li v-for="v in sortedVisitors" :key="v.id" class="py-4">
        <!-- 第一行：来源标记 + 身份（端口访问显示来源 IP、网关访问显示飞牛用户名） -->
        <div class="flex items-center gap-2 mb-1">
          <!-- 来源标记：端口访问（带 harness 会话 cookie，可注销）/ 飞牛网关访问
               （飞牛 OS 已认证，没有 cookie；记录可手动删除，但不影响该用户继续访问）。 -->
          <span
            class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium flex-shrink-0"
            :class="isGateway(v)
              ? 'bg-brand/10 text-brand dark:bg-brand/20 dark:text-brand'
              : 'bg-[#E8E8EC] text-ink-soft dark:bg-[#2A2A32] dark:text-[#A6A6AD]'"
          >
            {{ isGateway(v) ? t('visitor_gateway') : t('visitor_port') }}
          </span>
          <!-- 网关访问：附飞牛 OS 用户名；端口访问：紧跟来源 IP -->
          <span v-if="isGateway(v) && v.username" class="text-sm font-medium text-ink dark:text-white truncate">{{ v.username }}</span>
          <span v-if="!isGateway(v)" class="font-mono text-sm font-medium text-ink dark:text-white truncate">{{ v.ip }}</span>
        </div>
        <!-- 第二行：信息字段（逐项独立一行）+ 操作按钮。按钮与字段区同行并垂直居中，
             因此在窄屏下也跟「最近访问 / 登录有效期」处在一个水平线上，不单独占一行。
             端口访问是「注销」（吊销会话），网关访问是「删除记录」（只清列表）。 -->
        <div class="flex items-center gap-3">
          <div class="grid grid-cols-1 gap-y-1 text-xs text-ink-soft dark:text-[#A6A6AD] flex-1 min-w-0">
            <!-- 网关访问的 IP 单列展示（与端口访问一致，用于区分不同访问环境），
                 登录有效期不适用于网关访问（网关已完成认证，没有 harness 会话）。
                 窄卡片/长 IP（如 IPv6）放不下时按单行省略号截断，悬停可看完整地址。 -->
            <span v-if="isGateway(v) && v.ip" class="block truncate" :title="v.ip">{{ t('visitor_ip') }}：<span class="text-ink dark:text-[#EDEDF0] font-mono">{{ v.ip }}</span></span>
            <span>{{ t('last_access') }}：<span class="text-ink dark:text-[#EDEDF0]">{{ formatDateTime(v.lastAccess) }}</span></span>
            <span v-if="!isGateway(v)">{{ t('expires_at') }}：<span class="text-ink dark:text-[#EDEDF0]">{{ formatDateTime(v.expiresAt) }}</span></span>
          </div>
          <button
            class="g-btn-danger !h-8 !px-3 !text-xs flex-shrink-0"
            :disabled="deletingId === v.id"
            @click="emit('remove', v.id)"
          >
            {{
              deletingId === v.id
                ? isGateway(v) ? t('deleting_record') : t('logging_out')
                : isGateway(v) ? t('visitor_delete_record') : t('logout')
            }}
          </button>
        </div>
      </li>
    </ul>
  </section>
</template>
