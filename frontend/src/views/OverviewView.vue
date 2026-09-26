<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { useSettingsStore } from '@/stores/settings'
import { useToastStore } from '@/stores/toast'
import { api, sseUrl, type Visitor, type DshStatus } from '@/serverapi'
import { useI18n } from '@/composables/useI18n'
import { useEventStream } from '@/composables/useEventStream'
import { useBodyScrollLock } from '@/composables/useBodyScrollLock'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import DialogCloseButton from '@/components/DialogCloseButton.vue'
import UpdateSection from '@/components/UpdateSection.vue'
import AccessCard from '@/components/AccessCard.vue'
import PageHeader from '@/components/PageHeader.vue'
import { icons } from '@/utils/icons'

defineOptions({ name: 'OverviewView' })

const store = useSettingsStore()
const { status, loading, runtime } = storeToRefs(store)
const toast = useToastStore()
const { t } = useI18n()

// 快捷访问第一行固定的「飞牛入口」：当前访问环境下经飞牛网关访问 dsh 服务的地址。
// 优先用后端按请求头换算的结果（控制台当前访问地址剥离控制台 baseurl 得到飞牛 OS
// 访问源，再拼上 dsh 服务挂载的 baseurl）；后端推不出时退回浏览器自身地址 ——
// document.baseURI 就是控制台当前地址，剥掉它的 baseurl 路径后同样是飞牛 OS 访问源。
const fnosEntry = computed(() => {
  const fromServer = runtime.value?.fnosEntryURL || ''
  if (fromServer) return fromServer
  const mount = (runtime.value?.proxyBaseURL || '').replace(/\/+$/, '')
  if (!mount || !document.baseURI) return ''
  try {
    return new URL(document.baseURI).origin + mount
  } catch {
    return ''
  }
})

const visitors = ref<Visitor[]>([])
const visitorsLoading = ref(false)
const deleting = ref<string | null>(null)

// dsh 是否在运行：状态未知（status 尚未拉到）时按「运行中」渲染，与旧版按钮逻辑一致
// （旧版只在 running === false 时显示「启动」）。
const running = computed(() => status.value?.running !== false)

// “关于”弹窗
const aboutVisible = ref(false)

function openAbout() {
  aboutVisible.value = true
}

// 关于弹窗特性介绍：按类别分组，条目为简短词组，
// 分别对应 useI18n 里的 about_group_* / about_feature_* 键。
const aboutFeatureGroups = [
  {
    title: 'about_group_runtime',
    features: [
      'about_feature_lifecycle',
      'about_feature_monitor',
      'about_feature_update',
      'about_feature_backup',
    ],
  },
  {
    title: 'about_group_access',
    features: ['about_feature_auth', 'about_feature_proxy', 'about_feature_dirs'],
  },
  {
    title: 'about_group_tools',
    features: [
      'about_feature_plugins',
      'about_feature_terminal',
      'about_feature_logs',
      'about_feature_console',
    ],
  },
]

// 打开 GitHub 用户主页（新标签页）
function openGithubUser(user: string) {
  window.open(`https://github.com/${user}`, '_blank', 'noopener')
}

// 打开 GitHub 仓库（新标签页）
function openGithub() {
  window.open('https://github.com/shuangji66/dsh', '_blank', 'noopener')
}

// 停止/重启的二次确认弹窗：无论是否忙碌，每次点击都先弹窗确认
const lifecycleAction = ref<'stop' | 'restart' | null>(null)
const lifecycleDialogVisible = ref(false)

// 任一弹窗（停止/重启确认、"关于"）打开期间锁定页面滚动
useBodyScrollLock(() => lifecycleDialogVisible.value || aboutVisible.value)

// 打开弹窗时顺便查一次「有没有插件操作在跑」：有的话弹窗里多显示一条风险提示
// （停 dsh 会中断那次安装/卸载，并可能留下陈旧的 profile 写锁，之后插件列表与
// 安装都会白等 120 秒）。这两个按钮刻意不硬挡 —— 点它是用户的即时意图。
// 查询不阻塞弹窗：先显示，答案到了再补上提示；查不到（dsh 不可达）就不显示。
type LifecycleBusy = { busy: boolean; source?: 'market' | 'console'; detail?: string }
const lifecycleBusy = ref<LifecycleBusy | null>(null)

function openLifecycleConfirm(action: 'stop' | 'restart') {
  lifecycleAction.value = action
  lifecycleDialogVisible.value = true
  lifecycleBusy.value = null
  api
    .dshBusy()
    .then((r) => {
      // 期间用户可能已经确认/关闭了弹窗，避免把过期结果写回去
      if (lifecycleDialogVisible.value) lifecycleBusy.value = r
    })
    .catch(() => {
      lifecycleBusy.value = null
    })
}

// 忙碌提示文案：按来源区分（市场面板的安装 vs 控制台的插件命令），并带上正在处理的对象。
const lifecycleBusyText = computed(() => {
  const b = lifecycleBusy.value
  if (!b?.busy) return ''
  const detail = b.detail?.trim() || ''
  return b.source === 'console'
    ? t('lifecycle_busy_console', { detail: detail || 'dsh plugin …' })
    : t('lifecycle_busy_market', { detail: detail || t('lifecycle_busy_unknown_target') })
})

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
// 格式化 dsh 进程 PID：进程未运行时后端返回 0（或字段缺失），统一显示「—」
function fmtPid(v?: number): string {
  if (v === undefined || v === null || isNaN(v) || v <= 0) return '—'
  return String(v)
}
// 格式化进程运行时间：只取最大的两个单位（天+小时 / 小时+分 / 分+秒 / 秒），
// 小面板一行放得下，也不会每秒都在跳数字。传 undefined（未运行 / 还没拿到基准）显示「—」。
function fmtUptime(sec?: number): string {
  if (sec === undefined || sec === null || isNaN(sec) || sec < 0) return '—'
  const s = Math.floor(sec)
  if (s < 60) return t('uptime_s', { s })
  const m = Math.floor(s / 60)
  if (m < 60) return t('uptime_m_s', { m, s: s % 60 })
  const h = Math.floor(m / 60)
  if (h < 24) return t('uptime_h_m', { h, m: m % 60 })
  return t('uptime_d_h', { d: Math.floor(h / 24), h: h % 24 })
}

// --- 运行时间：后端只读一次，之后由前端自己走表 ---
//
// 状态流（/api/dsh/stream）每秒都会推 CPU / 内存，但**运行时间不必跟着轮询**：
// 拿到一次基准值（该 PID 已运行的秒数）后本机按时间差自己推进；只有「dsh 启停 / 重启 /
// 装插件自重启」这类生命周期变化才重新采纳 —— 它们在状态里表现为 running 翻转或
// （自重启时）pid 变化，因此不需要额外开一个「刷新运行时间」的接口。
//
// 用本机时钟推进而不是每拍 +1：页面被切到后台时定时器会被节流，按真实时间差算回来才不会走慢。
type UptimeBase = { pid: number; seconds: number; at: number }
const uptimeBase = ref<UptimeBase | null>(null)
const uptimeTick = ref(Date.now())
let uptimeTimer: number | null = null

// 展示值 = 基准秒数 + 本机已过去的秒数；没有基准（未运行 / 读不到）时 undefined → 「—」
const uptimeSeconds = computed(() => {
  const b = uptimeBase.value
  if (!b) return undefined
  return b.seconds + Math.max(0, Math.floor((uptimeTick.value - b.at) / 1000))
})

watch(
  () => ({
    pid: status.value?.pid ?? 0,
    running: status.value?.running === true,
    up: status.value?.uptimeSeconds,
  }),
  (cur) => {
    // 同一个 PID 仍在运行 → 已有基准，继续自己走表，不采纳后面每秒推来的值
    if (uptimeBase.value && cur.running && uptimeBase.value.pid === cur.pid) return
    // 停止 / 后端没给值（读不到该 PID 的启动时刻）→ 显示「—」
    if (!cur.running || cur.up === undefined) {
      uptimeBase.value = null
      return
    }
    // 首次拿到状态，或启停/重启（pid 变化）后的新基准
    uptimeBase.value = { pid: cur.pid, seconds: cur.up, at: Date.now() }
  },
  { immediate: true }
)

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

// 通过 SSE 监听后端主动推送，替换高频轮询。
// 用 useEventStream（断线自动重连）而不是裸 EventSource：断线后自行重连，重连
// 成功时后端立刻补发初始快照，CPU/内存与登录列表随即恢复正常刷新。
const visitorStream = useEventStream(() => sseUrl('/api/visitors/stream'), {
  visitors: (data) => {
    if (Array.isArray(data)) visitors.value = data as Visitor[]
  }
})

// 通过 SSE 每 1 秒接收 dsh 运行状态（CPU 使用率 / 内存占用），实现自动刷新
const statusStream = useEventStream(() => sseUrl('/api/dsh/stream'), {
  status: (data) => {
    status.value = data as DshStatus
  }
})

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

// 网关访问（飞牛入口）没有 harness 会话 cookie：列表里标记来源为「网关访问」、
// 显示飞牛用户名与来源 IP（同一个人从不同环境访问飞牛时 IP 不同，各占一条），
// 不显示登录有效期，也没有注销按钮。
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
  [...visitors.value].sort((a, b) => {
    const cmp = visitorName(a).localeCompare(visitorName(b), undefined, { numeric: true, sensitivity: 'base' })
    return cmp !== 0 ? cmp : a.id.localeCompare(b.id)
  })
)

onMounted(() => {
  store.load()
  initialLoad()
  visitorStream.start()
  statusStream.start()
  // 运行时间的本机秒表（见上面的 uptimeSeconds）：只在页面存活期间走，卸载时清掉
  uptimeTimer = window.setInterval(() => {
    uptimeTick.value = Date.now()
  }, 1000)
})

onBeforeUnmount(() => {
  if (uptimeTimer !== null) {
    clearInterval(uptimeTimer)
    uptimeTimer = null
  }
})
</script>

<template>
  <div class="pt-3 sm:pt-4 pb-8 sm:pb-12 px-4 sm:px-8 max-w-6xl mx-auto">
    <!-- 页头（图标 + 标题 + 右侧操作，统一卡片式标题栏） -->
    <PageHeader class="mb-3" :title="t('overview_title')" :icon="icons.overview">
      <button class="g-btn-secondary h-8 px-3 text-xs" @click="openAbout()">{{ t('about') }}</button>
    </PageHeader>

    <!-- 四卡片栅格：dsh 信息与启停 / 三个版本 / 快捷访问 / 登录列表。
         桌面固定两列（2×2，见 style.css 的 .g-card-grid-2），窄屏沿用 .g-card-grid
         的自适应列数（手机为单列）。 -->
    <div class="g-card-grid g-card-grid-2 g-fade-in">
      <!-- ① dsh 服务信息与启停 -->
      <section class="g-card g-card-hover p-5 flex flex-col">
        <div class="flex items-center justify-between gap-2">
          <h2 class="font-display text-base font-semibold text-ink dark:text-white">DeepSeek Harness</h2>
          <!-- 运行状态标记 -->
          <span
            class="g-status flex-shrink-0"
            :class="running
              ? 'bg-success/10 text-success'
              : 'bg-black/5 text-ink-soft dark:bg-white/10 dark:text-[#A6A6AD]'"
          >
            <span class="w-1.5 h-1.5 rounded-full" :class="running ? 'bg-success' : 'bg-ink-faint'"></span>
            {{ running ? t('status_running') : t('status_stopped') }}
          </span>
        </div>
        <p class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1 mb-4">{{ t('overview_dsh_desc') }}</p>

        <!-- 进程信息：CPU / 内存 一行两个等宽小面板；PID 与运行时间折到下一行
             （两列栅格放四个子项：后两个自然换行，并与上面两个等宽、左对齐）。 -->
        <div class="grid grid-cols-2 gap-3 mb-4">
          <div class="rounded-lg bg-black/[0.03] dark:bg-white/[0.05] px-3 py-2.5">
            <div class="text-[11px] text-ink-soft dark:text-[#A6A6AD] mb-0.5">{{ t('cpu_usage') }}</div>
            <div class="font-mono text-sm font-semibold" :class="cpuColor(status?.cpuPercent)">{{ fmtCpu(status?.cpuPercent) }}</div>
          </div>
          <div class="rounded-lg bg-black/[0.03] dark:bg-white/[0.05] px-3 py-2.5">
            <div class="text-[11px] text-ink-soft dark:text-[#A6A6AD] mb-0.5">{{ t('mem_usage') }}</div>
            <div class="font-mono text-sm font-semibold" :class="memColor(status?.memoryMB)">{{ fmtMem(status?.memoryMB) }}</div>
          </div>
          <div class="rounded-lg bg-black/[0.03] dark:bg-white/[0.05] px-3 py-2.5">
            <div class="text-[11px] text-ink-soft dark:text-[#A6A6AD] mb-0.5">{{ t('dsh_pid') }}</div>
            <div class="font-mono text-sm font-semibold text-ink dark:text-[#EDEDF0]">{{ fmtPid(status?.pid) }}</div>
          </div>
          <!-- 运行时间：后端只在打开/启停重启时给一次基准，之后由前端自己走表（未运行时「—」） -->
          <div class="rounded-lg bg-black/[0.03] dark:bg-white/[0.05] px-3 py-2.5">
            <div class="text-[11px] text-ink-soft dark:text-[#A6A6AD] mb-0.5">{{ t('dsh_uptime') }}</div>
            <div class="font-mono text-sm font-semibold text-ink dark:text-[#EDEDF0]">{{ fmtUptime(uptimeSeconds) }}</div>
          </div>
        </div>

        <!-- 启停操作：两列栅格，每个按钮的宽度与小面板一致（整行正好与上面的面板行同宽） -->
        <div class="grid grid-cols-2 gap-3 mt-auto">
          <button v-if="!running" class="g-btn-primary !h-9 !text-sm bg-success hover:bg-success/90" :disabled="loading" @click="store.startDsh()">{{ t('dsh_start') }}</button>
          <button v-else class="g-btn-danger !h-9 !text-sm" :disabled="loading" @click="openLifecycleConfirm('stop')">{{ t('dsh_stop') }}</button>
          <button class="g-btn-warning !h-9 !text-sm" :disabled="loading" @click="openLifecycleConfirm('restart')">{{ t('dsh_restart') }}</button>
        </div>
      </section>

      <!-- ② 三个版本：harness 控制台 / dsh 服务 / 插件市场 -->
      <UpdateSection />

      <!-- ③ 快捷访问 -->
      <AccessCard :access-urls="store.config.accessUrls || []" :fnos-entry="fnosEntry" />

      <!-- ④ 登录列表：并入上面的栅格（不再单独占一行），
           卡片宽度只有桌面栅格的一半，因此字段沿用「移动端」的显示方式（逐项独立一行）。 -->
      <section class="g-card g-card-hover p-5 flex flex-col">
        <h2 class="font-display text-base font-semibold text-ink dark:text-white">{{ t('login_list') }}</h2>
        <p class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1 mb-4">{{ t('login_list_desc') }}</p>

        <div v-if="visitorsLoading" class="text-sm text-ink-soft dark:text-[#A6A6AD] py-6 text-center">{{ t('loading') }}</div>

        <div v-else-if="visitors.length === 0" class="text-sm text-ink-faint dark:text-[#8A8A92] py-6 text-center">
          {{ t('no_visitors') }}
        </div>

        <ul v-else class="divide-y divide-line dark:divide-[#2A2A32]">
          <li v-for="v in sortedVisitors" :key="v.id" class="py-4">
            <!-- 第一行：来源标记 + 身份（端口访问显示来源 IP、网关访问显示飞牛用户名） -->
            <div class="flex items-center gap-2 mb-1">
              <!-- 来源标记：端口访问（带 harness 会话 cookie，可注销）/ 飞牛网关访问
                   （飞牛 OS 已认证，没有 cookie，不可注销）。 -->
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
            <!-- 第二行：信息字段（逐项独立一行）+ 注销按钮。按钮与字段区同行并垂直居中，
                 因此在窄屏下也跟「最近访问 / 登录有效期」处在一个水平线上，不单独占一行。 -->
            <div class="flex items-center gap-3">
              <div class="grid grid-cols-1 gap-y-1 text-xs text-ink-soft dark:text-[#A6A6AD] flex-1 min-w-0">
                <!-- 网关访问的 IP 单列展示（与端口访问一致，用于区分不同访问环境），
                     登录有效期不适用于网关访问（网关已完成认证，没有 harness 会话）。 -->
                <span v-if="isGateway(v) && v.ip">{{ t('visitor_ip') }}：<span class="text-ink dark:text-[#EDEDF0] font-mono">{{ v.ip }}</span></span>
                <span>{{ t('last_access') }}：<span class="text-ink dark:text-[#EDEDF0]">{{ fmt(v.lastAccess) }}</span></span>
                <span v-if="!isGateway(v)">{{ t('expires_at') }}：<span class="text-ink dark:text-[#EDEDF0]">{{ fmt(v.expiresAt) }}</span></span>
              </div>
              <button
                v-if="!isGateway(v)"
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
    </div>

    <!-- 停止/重启确认弹窗 -->
    <ConfirmDialog
      v-model:visible="lifecycleDialogVisible"
      :title="lifecycleAction === 'restart' ? t('confirm_restart_title') : t('confirm_stop_title')"
      :confirm-text="t('confirm_ok')"
      :cancel-text="t('confirm_cancel')"
      danger
      @confirm="onLifecycleConfirm"
    >
      <!-- 默认插槽：原确认文案 + （有插件操作在跑时）额外风险提示 -->
      <p class="text-sm text-ink-soft dark:text-[#A6A6AD] leading-relaxed whitespace-pre-line" :class="lifecycleBusyText ? 'mb-3' : 'mb-6'">
        {{ lifecycleAction === 'restart' ? t('confirm_restart_msg') : t('confirm_stop_msg') }}
      </p>
      <div
        v-if="lifecycleBusyText"
        class="mb-6 rounded-lg px-3 py-2 text-xs leading-relaxed bg-[#F59E0B]/10 dark:bg-[#F59E0B]/15 border border-[#F59E0B]/40 text-[#B45309] dark:text-[#FBBF24]"
      >
        {{ lifecycleBusyText }}
      </div>
    </ConfirmDialog>

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
        <div v-if="aboutVisible" class="fixed inset-0 z-50 flex items-center justify-center p-4 backdrop-blur-sm">
          <div class="g-modal-mask" @click="aboutVisible = false"></div>
          <!-- 比默认弹窗略宽：特性按类别双列排布，窄了会大量折行 -->
          <div class="relative w-full max-w-md bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6">
            <DialogCloseButton :label="t('dialog_close')" @close="aboutVisible = false" />
            <h3 class="g-dialog-title mb-4">{{ t('about_title') }}</h3>

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

            <!-- 特性介绍：按类别分组，条目双列排布（窄弹窗里省高度） -->
            <div class="mb-5">
              <div class="text-sm font-medium text-ink dark:text-white mb-2.5">{{ t('about_features') }}</div>
              <div v-for="g in aboutFeatureGroups" :key="g.title" class="mb-3 last:mb-0">
                <div class="text-[11px] font-medium text-ink-faint dark:text-[#8A8A92] mb-1.5">{{ t(g.title) }}</div>
                <ul class="grid grid-cols-2 gap-x-3 gap-y-1.5">
                  <li v-for="f in g.features" :key="f" class="flex items-start gap-1.5">
                    <svg class="mt-0.5 w-3 h-3 text-brand shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                    <span class="text-xs text-ink-soft dark:text-[#A6A6AD] leading-snug">{{ t(f) }}</span>
                  </li>
                </ul>
              </div>
            </div>

            <!-- 鸣谢 -->
            <div>
              <div class="text-sm font-medium text-ink dark:text-white mb-2">{{ t('about_credits') }}</div>
              <p class="text-xs text-ink-soft dark:text-[#A6A6AD] leading-relaxed">
                {{ t('about_credits_pre') }}<button class="inline text-brand hover:underline" @click="openGithubUser('yuexps')">yuexps</button>{{ t('about_credits_suf') }}
              </p>
            </div>

            <!-- 关闭入口只有右上角的 X（底部不再放「关闭」按钮） -->
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>