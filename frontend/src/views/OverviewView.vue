<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { useSettingsStore } from '@/stores/settings'
import { useToastStore } from '@/stores/toast'
import { api, sseUrl, type Visitor, type DshStatus } from '@/serverapi'
import { useI18n } from '@/composables/useI18n'

defineOptions({ name: 'OverviewView' })

const store = useSettingsStore()
const { status, loading } = storeToRefs(store)
const toast = useToastStore()
const { t } = useI18n()

const visitors = ref<Visitor[]>([])
const visitorsLoading = ref(false)
const deleting = ref<string | null>(null)
let visitorES: EventSource | null = null
let statusES: EventSource | null = null

// 格式化最近访问时间 / 登录有效期至
function fmt(iso: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (isNaN(d.getTime())) return '—'
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

// 格式化 CPU 使用率（%）/ 内存占用（MB）
function fmtCpu(v?: number): string {
  if (v === undefined || v === null || isNaN(v)) return '—'
  return v.toFixed(1) + '%'
}
function fmtMem(v?: number): string {
  if (v === undefined || v === null || isNaN(v)) return '—'
  return v + ' MB'
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

// 通过 SSE 每 1 秒接收 dsh 运行状态（CPU 使用率 / 内存占用），实现自动刷新
function connectStatusStream() {
  statusES?.close()
  const es = new EventSource(sseUrl('/api/dsh/stream'))
  statusES = es
  es.addEventListener('status', (ev) => {
    try {
      const data = JSON.parse((ev as MessageEvent).data) as DshStatus
      status.value = data
    } catch {
      /* ignore malformed frames */
    }
  })
  es.onerror = () => {
    // 连接断开时后端自动重连；先释放旧连接避免堆积
    es.close()
    statusES = null
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
  connectStatusStream()
})

onBeforeUnmount(() => {
  visitorES?.close()
  visitorES = null
  statusES?.close()
  statusES = null
})
</script>

<template>
  <div class="py-8 sm:py-12 px-4 sm:px-8 max-w-3xl mx-auto">
    <!-- 页头 -->
    <header class="mb-8">
      <p class="text-ink-faint dark:text-[#8A8A92] text-sm font-medium uppercase tracking-widest">{{ t('overview_title') }}</p>
    </header>

    <!-- dsh 生命周期 -->
    <section class="g-card g-card-hover p-6 mb-6">
      <div class="flex items-center justify-between mb-2">
        <h2 class="font-display text-lg font-semibold text-ink dark:text-white">DeepSeek Harness</h2>
        <!-- dsh 进程资源使用情况（CPU / 内存） -->
        <div class="flex items-center gap-5">
          <div class="text-right">
            <div class="text-xs text-ink-soft dark:text-[#A6A6AD]">{{ t('cpu_usage') }}</div>
            <div class="text-sm font-semibold text-ink dark:text-[#EDEDF0] font-mono">{{ fmtCpu(status?.cpuPercent) }}</div>
          </div>
          <div class="text-right">
            <div class="text-xs text-ink-soft dark:text-[#A6A6AD]">{{ t('mem_usage') }}</div>
            <div class="text-sm font-semibold text-ink dark:text-[#EDEDF0] font-mono">{{ fmtMem(status?.memoryMB) }}</div>
          </div>
        </div>
      </div>
      <p class="text-sm text-ink-soft dark:text-[#A6A6AD] mb-5">{{ t('dsh_manage') }}</p>
      <div class="flex gap-3">
        <button v-if="!status?.running" class="g-btn-primary bg-success hover:bg-success/90" :disabled="loading" @click="store.startDsh()">{{ t('dsh_start') }}</button>
        <button v-else class="g-btn-danger" :disabled="loading" @click="store.stopDsh()">{{ t('dsh_stop') }}</button>
        <button class="g-btn-secondary" :disabled="loading" @click="store.restartDsh()">{{ t('dsh_restart') }}</button>
      </div>
    </section>

    <!-- 登录列表 -->
    <section class="g-card g-card-hover p-6 mb-6">
      <div class="flex items-center justify-between mb-2">
        <h2 class="font-display text-lg font-semibold text-ink dark:text-white">{{ t('login_list') }}</h2>
      </div>
      <p class="text-sm text-ink-soft dark:text-[#A6A6AD] mb-5">{{ t('login_list_desc') }}</p>

      <div v-if="visitorsLoading" class="text-sm text-ink-soft dark:text-[#A6A6AD] py-6 text-center">{{ t('loading') }}</div>

      <div v-else-if="visitors.length === 0" class="text-sm text-ink-faint dark:text-[#8A8A92] py-6 text-center">
        {{ t('no_visitors') }}
      </div>

      <ul v-else class="divide-y divide-line dark:divide-[#2A2A32]">
        <li v-for="v in visitors" :key="v.id" class="py-4">
          <div class="flex items-center justify-between gap-3">
            <div class="min-w-0">
              <div class="flex items-center gap-2 mb-1">
                <span class="font-mono text-sm font-medium text-ink dark:text-white truncate">{{ v.ip }}</span>
              </div>
              <div class="grid grid-cols-2 gap-x-4 gap-y-1 text-xs text-ink-soft dark:text-[#A6A6AD]">
                <span>{{ t('last_access') }}：<span class="text-ink dark:text-[#EDEDF0]">{{ fmt(v.lastAccess) }}</span></span>
                <span>{{ t('expires_at') }}：<span class="text-ink dark:text-[#EDEDF0]">{{ fmt(v.expiresAt) }}</span></span>
              </div>
            </div>
            <button
              class="g-btn-secondary !h-8 !px-3 !text-xs flex-shrink-0"
              :disabled="deleting === v.id"
              @click="removeVisitor(v.id)"
            >
              {{ deleting === v.id ? t('logging_out') : t('logout') }}
            </button>
          </div>
        </li>
      </ul>
    </section>
  </div>
</template>