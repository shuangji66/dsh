<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { useSettingsStore } from '@/stores/settings'
import { useToastStore } from '@/stores/toast'
import { api, sseUrl, type Visitor, type DshStatus } from '@/serverapi'
import { useI18n } from '@/composables/useI18n'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import UpdateSection from '@/components/UpdateSection.vue'

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

// “关于”弹窗
const aboutVisible = ref(false)

function openAbout() {
  aboutVisible.value = true
}

// 打开 GitHub 仓库（新标签页）
function openGithub() {
  window.open('https://github.com/shuangji66/dsh', '_blank', 'noopener')
}

// 停止/重启的二次确认弹窗：无论是否忙碌，每次点击都先弹窗确认
const lifecycleAction = ref<'stop' | 'restart' | null>(null)
const lifecycleDialogVisible = ref(false)

function openLifecycleConfirm(action: 'stop' | 'restart') {
  lifecycleAction.value = action
  lifecycleDialogVisible.value = true
}

async function executeLifecycle(action: 'stop' | 'restart') {
  if (action === 'stop') await store.stopDsh()
  else await store.restartDsh()
}

function onLifecycleConfirm() {
  const action = lifecycleAction.value
  lifecycleAction.value = null
  if (action) executeLifecycle(action)
}

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

// CPU 占用阈值颜色：0-20% 绿 / 20-50% 橙 / 50% 以上红；未知用黑色（—）
function cpuColor(v?: number): string {
  if (v === undefined || v === null || isNaN(v)) return 'text-ink dark:text-[#EDEDF0]'
  if (v < 20) return 'text-success dark:text-[#10B981]'
  if (v < 50) return 'text-warning dark:text-[#F59E0B]'
  return 'text-danger dark:text-[#EF4444]'
}
// 内存阈值颜色：<500MB 绿 / 500-1000MB 橙 / 1000MB 以上红；未知用黑色（—）
function memColor(v?: number): string {
  if (v === undefined || v === null || isNaN(v)) return 'text-ink dark:text-[#EDEDF0]'
  if (v < 500) return 'text-success dark:text-[#10B981]'
  if (v < 1000) return 'text-warning dark:text-[#F59E0B]'
  return 'text-danger dark:text-[#EF4444]'
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
    <header class="flex items-center justify-between mb-8">
      <p class="text-ink-faint dark:text-[#8A8A92] text-sm font-medium uppercase tracking-widest">{{ t('overview_title') }}</p>
      <!-- 关于按钮（右上角） -->
      <button class="g-btn-secondary !h-9 !px-4 !text-sm flex-shrink-0" @click="openAbout()">{{ t('about') }}</button>
    </header>

    <!-- dsh 生命周期 -->
    <section class="g-card g-card-hover p-6 mb-6">
      <div class="flex items-center justify-between gap-3 mb-2">
        <h2 class="font-display text-lg font-semibold text-ink dark:text-white">DeepSeek Harness</h2>
      </div>
      <div class="flex items-center justify-between gap-3 flex-wrap mt-4">
        <div class="flex gap-3">
          <button v-if="status?.running === false" class="g-btn-primary bg-success hover:bg-success/90" :disabled="loading" @click="store.startDsh()">{{ t('dsh_start') }}</button>
          <button v-else class="g-btn-danger" :disabled="loading" @click="openLifecycleConfirm('stop')">{{ t('dsh_stop') }}</button>
          <button class="g-btn-warning" :disabled="loading" @click="openLifecycleConfirm('restart')">{{ t('dsh_restart') }}</button>
        </div>
        <!-- dsh 进程资源使用情况（CPU / 内存），与操作按钮同行 -->
        <div class="flex items-center gap-5">
          <div class="text-right">
            <div class="text-xs text-ink-soft dark:text-[#A6A6AD]">{{ t('cpu_usage') }}</div>
            <div class="text-sm font-semibold font-mono" :class="cpuColor(status?.cpuPercent)">{{ fmtCpu(status?.cpuPercent) }}</div>
          </div>
          <div class="text-right">
            <div class="text-xs text-ink-soft dark:text-[#A6A6AD]">{{ t('mem_usage') }}</div>
            <div class="text-sm font-semibold font-mono" :class="memColor(status?.memoryMB)">{{ fmtMem(status?.memoryMB) }}</div>
          </div>
        </div>
      </div>

      <!-- 自我更新：harness 控制台版本 + dsh 服务版本 -->
      <UpdateSection :access-urls="store.config.accessUrls || []" />
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
              class="g-btn-danger !h-8 !px-3 !text-xs flex-shrink-0"
              :disabled="deleting === v.id"
              @click="removeVisitor(v.id)"
            >
              {{ deleting === v.id ? t('logging_out') : t('logout') }}
            </button>
          </div>
        </li>
      </ul>
    </section>

    <!-- 停止/重启确认弹窗 -->
    <ConfirmDialog
      v-model:visible="lifecycleDialogVisible"
      :title="lifecycleAction === 'restart' ? t('confirm_restart_title') : t('confirm_stop_title')"
      :message="lifecycleAction === 'restart' ? t('confirm_restart_msg') : t('confirm_stop_msg')"
      :confirm-text="t('confirm_ok')"
      :cancel-text="t('confirm_cancel')"
      danger
      @confirm="onLifecycleConfirm"
    />

    <!-- 关于弹窗 -->
    <Teleport to="body">
      <Transition
        enter-active-class="transition duration-200 ease-out"
        enter-from-class="opacity-0"
        enter-to-class="opacity-100"
        leave-active-class="transition duration-150 ease-in"
        leave-from-class="opacity-100"
        leave-to-class="opacity-0"
      >
        <div v-if="aboutVisible" class="fixed inset-0 z-50 flex items-center justify-center p-4">
          <div class="absolute inset-0 bg-black/50" @click="aboutVisible = false"></div>
          <div class="relative w-full max-w-sm bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6">
            <h3 class="font-display text-lg font-semibold text-ink dark:text-white mb-4">DeepSeek Harness</h3>

            <!-- Github 仓库按钮 -->
            <button
              class="w-full flex items-center justify-center gap-2 g-btn-secondary mb-5"
              @click="openGithub"
            >
              <svg class="h-4 w-4" viewBox="0 0 24 24" fill="currentColor">
                <path d="M12 .5C5.7.5.5 5.7.5 12c0 5.1 3.3 9.4 7.9 10.9.6.1.8-.2.8-.5v-1.7c-3.2.7-3.9-1.4-3.9-1.4-.5-1.3-1.3-1.7-1.3-1.7-1-.7.1-.7.1-.7 1.2.1 1.8 1.2 1.8 1.2 1 1.8 2.7 1.3 3.4 1 .1-.8.4-1.3.7-1.6-2.6-.3-5.3-1.3-5.3-5.7 0-1.3.5-2.3 1.2-3.1-.1-.3-.5-1.5.1-3.1 0 0 1-.3 3.2 1.2a11 11 0 0 1 5.8 0C17.2 4.7 18.2 5 18.2 5c.6 1.6.2 2.8.1 3.1.8.8 1.2 1.8 1.2 3.1 0 4.4-2.7 5.4-5.3 5.7.4.4.8 1.1.8 2.2v3.2c0 .3.2.6.8.5 4.6-1.5 7.9-5.8 7.9-10.9C23.5 5.7 18.3.5 12 .5z"/>
              </svg>
              {{ t('about_github') }}
            </button>

            <!-- 特性描述（预留，内容暂无） -->
            <div class="mb-4">
              <div class="text-sm font-medium text-ink dark:text-white mb-1">{{ t('about_features') }}</div>
              <div class="text-xs text-ink-faint dark:text-[#8A8A92]">{{ t('about_empty') }}</div>
            </div>

            <!-- 鸣谢（预留，内容暂无） -->
            <div>
              <div class="text-sm font-medium text-ink dark:text-white mb-1">{{ t('about_credits') }}</div>
              <div class="text-xs text-ink-faint dark:text-[#8A8A92]">{{ t('about_empty') }}</div>
            </div>

            <!-- 底部关闭按钮 -->
            <div class="flex justify-end mt-6">
              <button class="g-btn-primary" @click="aboutVisible = false">{{ t('about_close') }}</button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>