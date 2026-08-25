<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { useSettingsStore } from '@/stores/settings'
import { useToastStore } from '@/stores/toast'
import { api, sseUrl, type Visitor } from '@/serverapi'

defineOptions({ name: 'OverviewView' })

const store = useSettingsStore()
const { status, loading } = storeToRefs(store)
const toast = useToastStore()

const visitors = ref<Visitor[]>([])
const visitorsLoading = ref(false)
const deleting = ref<string | null>(null)
let visitorES: EventSource | null = null

// 格式化最近访问时间 / 登录有效期至
function fmt(iso: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (isNaN(d.getTime())) return '—'
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

// 仅初次加载时显示“加载中…”，之后由 SSE 推送增量更新，避免文字闪烁
async function initialLoad() {
  visitorsLoading.value = true
  try {
    const p = await api.visitors()
    visitors.value = p.visitors
  } catch (e) {
    toast.show((e as Error).message, 'error')
  } finally {
    visitorsLoading.value = false
  }
}

// 通过 SSE 监听后端主动推送，替换高频轮询
function connectVisitorStream() {
  visitorES?.close()
  const es = new EventSource(sseUrl('/api/visitors/stream'))
  visitorES = es
  es.addEventListener('visitors', (ev) => {
    try {
      const data = JSON.parse((ev as MessageEvent).data)
      if (Array.isArray(data)) visitors.value = data
    } catch {
      /* ignore malformed frames */
    }
  })
  es.onerror = () => {
    // 连接断开时后端自动重连（EventSource 内置）；关闭前先释放旧连接避免堆积
    es.close()
    visitorES = null
  }
}

async function removeVisitor(id: string) {
  deleting.value = id
  try {
    const p = await api.deleteVisitor(id)
    toast.show(p.msg, p.deleted ? 'success' : 'info')
  } catch (e) {
    toast.show((e as Error).message, 'error')
  } finally {
    deleting.value = null
  }
}

onMounted(() => {
  store.load()
  initialLoad()
  connectVisitorStream()
})

onBeforeUnmount(() => {
  visitorES?.close()
  visitorES = null
})
</script>

<template>
  <div class="py-8 sm:py-12 px-4 sm:px-8 max-w-3xl mx-auto">
    <!-- 页头 -->
    <header class="mb-8">
      <p class="text-ink-faint dark:text-[#8A8A92] text-sm font-medium uppercase tracking-widest">概览</p>
    </header>

    <!-- dsh 生命周期 -->
    <section class="g-card g-card-hover p-6 mb-6">
      <div class="flex items-center justify-between mb-2">
        <h2 class="font-display text-lg font-semibold text-ink dark:text-white">DeepSeek Harness</h2>
        <span class="inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium"
          :class="status?.running ? 'bg-success/10 text-success' : 'bg-ink-faint/10 text-ink-soft dark:text-[#A6A6AD]'">
          <span class="inline-block w-2 h-2 rounded-full"
            :class="status?.running ? 'bg-success' : 'bg-ink-faint'"></span>
          {{ status?.running ? '运行中' : '已停止' }}
        </span>
      </div>
      <p class="text-sm text-ink-soft dark:text-[#A6A6AD] mb-5">管理 dsh 服务的启动、停止与重启。</p>
      <div class="flex gap-3">
        <button v-if="!status?.running" class="g-btn-primary bg-success hover:bg-success/90" :disabled="loading" @click="store.startDsh()">启动</button>
        <button v-else class="g-btn-danger" :disabled="loading" @click="store.stopDsh()">停止</button>
        <button class="g-btn-secondary" :disabled="loading" @click="store.restartDsh()">重启</button>
      </div>
    </section>

    <!-- 反代访客 -->
    <section class="g-card g-card-hover p-6 mb-6">
      <div class="flex items-center justify-between mb-2">
        <h2 class="font-display text-lg font-semibold text-ink dark:text-white">登录列表</h2>
      </div>

      <div v-if="visitorsLoading" class="text-sm text-ink-soft dark:text-[#A6A6AD] py-6 text-center">加载中…</div>

      <div v-else-if="visitors.length === 0" class="text-sm text-ink-faint dark:text-[#8A8A92] py-6 text-center">
        暂无访客记录
      </div>

      <ul v-else class="divide-y divide-line dark:divide-[#2A2A32]">
        <li v-for="v in visitors" :key="v.id" class="py-4">
          <div class="flex items-center justify-between gap-3">
            <div class="min-w-0">
              <div class="flex items-center gap-2 mb-1">
                <span class="font-mono text-sm font-medium text-ink dark:text-white truncate">{{ v.ip }}</span>
              </div>
              <div class="grid grid-cols-2 gap-x-4 gap-y-1 text-xs text-ink-soft dark:text-[#A6A6AD]">
                <span>最近访问：<span class="text-ink dark:text-[#EDEDF0]">{{ fmt(v.lastAccess) }}</span></span>
                <span>登录有效期至：<span class="text-ink dark:text-[#EDEDF0]">{{ fmt(v.expiresAt) }}</span></span>
              </div>
            </div>
            <button
              class="g-btn-secondary !h-8 !px-3 !text-xs flex-shrink-0"
              :disabled="deleting === v.id"
              @click="removeVisitor(v.id)"
            >
              {{ deleting === v.id ? '注销中…' : '注销' }}
            </button>
          </div>
        </li>
      </ul>
    </section>
  </div>
</template>