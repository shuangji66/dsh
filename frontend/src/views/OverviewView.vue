<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { useSettingsStore } from '@/stores/settings'
import { useToastStore } from '@/stores/toast'
import { api, sseUrl, type Visitor, type DshStatus } from '@/serverapi'
import { uiErrText, uiText, useI18n } from '@/composables/useI18n'
import { useEventStream } from '@/composables/useEventStream'
import { useBodyScrollLock } from '@/composables/useBodyScrollLock'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import DialogCloseButton from '@/components/DialogCloseButton.vue'
import GithubIconLink from '@/components/GithubIconLink.vue'
import { repoSlugs, repoURL } from '@/constants/repos'
import UpdateSection from '@/components/UpdateSection.vue'
import PageHeader from '@/components/PageHeader.vue'
import AccountCard from '@/components/account/AccountCard.vue'
import { useWideLayout } from '@/composables/useMobileLayout'
import { isFnosApp } from '@/utils/fnosApp'
import { icons } from '@/utils/icons'

defineOptions({ name: 'OverviewView' })

const store = useSettingsStore()
const { status, loading, runtime } = storeToRefs(store)
const toast = useToastStore()
const { t, locale } = useI18n()

// dsh 卡片右上角「进入」的目标：当前访问环境下经飞牛网关访问 dsh 服务的地址。
// 优先后端按请求头换算的结果（控制台访问源 + 控制台入口下的 /gui/ 反代）；后端推不出
// 时退回浏览器自身地址 —— document.baseURI 就是控制台当前地址，origin 拼上
// consoleDshURL（/app/Harness/gui/）即可。
// 注意：这里不再拼独立的 /app/Harness/dsh —— 飞牛 App 的网关鉴权按入口注入，
// 控制台入口的凭据带不过去（会先看到 trim_http_cgi 的 invalid token）。
const fnosEntry = computed(() => {
  const fromServer = runtime.value?.fnosEntryURL || ''
  if (fromServer) return fromServer
  const consoleDsh = runtime.value?.consoleDshURL || ''
  if (!consoleDsh || !document.baseURI) return ''
  try {
    return new URL(document.baseURI).origin + consoleDsh
  } catch {
    return ''
  }
})

// 「进入」：桌面浏览器新标签页打开；飞牛 App 内用全屏 iframe 承载 dsh。
//
// 为什么 App 内不用 window.open / location.assign：App 的网关鉴权是**按入口
// （gatewayPrefix）注入**的，不是浏览器那种 host 级 Cookie。控制台入口
// （/app/Harness）的凭据不会自动带到 dsh 入口（/app/Harness/dsh）：
//   - window.open 新建的 WebView 首个导航没有 dsh 入口的凭据；
//   - 同窗口 location.assign 也一样（实测控制台页面确实被导航走了，dsh 仍收到
//     trim_http_cgi 的 200 + 13 字节 "invalid token"）。
// 但 iframe 是**控制台页面自己的子资源请求**，走的是控制台这个 WebView 的请求栈；
// 而且入口地址是控制台 baseurl 之下的 /gui/（由控制台的 app.sock 反代到 dsh，
// 见 backend/admin.go 的 dshUI），不跨到独立的 dsh 入口。如果仍然被网关拒绝
// （iframe 里出现 13 字节错误页），只显示一个全屏的重试面板。
const entryFrameVisible = ref(false)
const entryFrameUrl = ref('')
const entryFrameError = ref(false)
const entryFrameKey = ref(0)
// 打开覆盖层时压入一条同 URL 的历史记录，返回手势/浏览器后退会把它弹掉 ——
// 我们据此关闭覆盖层、回到控制台（而不是被 App 的返回手势直接带出控制台）。
//
// 飞牛 App / 桌面上的这一页是 portal 的 `/appview?anchor=…`（同一个文档里用 iframe 承载
// 控制台），返回键的逻辑是「**挂载时**读一次 window.history.length，>1 就 history.back()，
// 否则 navigate('/index')」（appview 的 createAppViewCloseHandler）。所以：
//   - 这里 pushState 的是控制台自己的 window（= portal 里那个 iframe）。同源 iframe 的
//     same-document 条目属于 joint session history：顶层 history.back() 会先把它弹掉，
//     popstate 派发给我们这个 window，正好用来关覆盖层；再返回一次才回应用列表。
//   - 那一次读数是闭包捕获的，之后再 pushState 也改不了它：若 App 把 `/appview` 当首个
//     历史条目打开（length=1），返回键会直接 navigate('/index')，前端无从拦截。正常从
//     应用列表点进来时 length ≥ 2，走的才是 history.back()。
// App.vue 里那条「后退不切子页面」的守卫在 URL 未变时只是 replace 同一地址，不冲突。
// dsh 内部自己的前进/后退发生在 iframe 的历史里（popstate 派发给 iframe 的 window），
// 因此不会提前关掉覆盖层。
let entryHistoryPushed = false

function closeEntryFrame() {
  entryFrameVisible.value = false
  entryFrameError.value = false
  entryFrameUrl.value = ''
  entryHistoryPushed = false
}

function onEntryPopState() {
  if (!entryFrameVisible.value) return
  closeEntryFrame()
}

function normalizeEntryURL(url: string): string {
  const u = url.trim()
  if (!u) return ''
  return /^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(u) ? u : 'http://' + u
}

function openDshEntry() {
  const final = normalizeEntryURL(fnosEntry.value)
  if (!final) return
  if (!isFnosApp()) {
    window.open(final, '_blank', 'noopener')
    return
  }
  entryFrameUrl.value = final
  entryFrameError.value = false
  entryFrameKey.value++
  if (!entryHistoryPushed) {
    // 同 URL 压栈：不改变当前页面，只是给返回手势一个「先关覆盖层」的落点。
    const prevState = typeof history.state === 'object' && history.state ? history.state : {}
    history.pushState({ ...prevState, harnessDshEntry: true }, '', location.href)
    entryHistoryPushed = true
  }
  entryFrameVisible.value = true
}

function retryEntryFrame() {
  entryFrameError.value = false
  entryFrameKey.value++
}

function onEntryFrameLoad(ev: Event) {
  const el = ev.target as HTMLIFrameElement
  let text = ''
  try {
    text = (el.contentDocument?.body?.innerText || '').trim()
  } catch {
    return // 理论上同源；读不到就不动
  }
  if (text === 'invalid token') {
    entryFrameError.value = true
  }
}

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
    // 账号与「凭据/快捷指令加密存储」都归在这里：它们解决的正是「谁拿着这台设备上的凭据」
    // （账号见 components/account/AccountCard.vue / backend/account.go，加密见 backend/secret.go 的威胁模型）。
    features: [
      'about_feature_auth',
      'about_feature_account',
      'about_feature_secrets',
      'about_feature_proxy',
      'about_feature_dirs',
    ],
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

// 停止/重启的二次确认弹窗：无论是否忙碌，每次点击都先弹窗确认
const lifecycleAction = ref<'stop' | 'restart' | null>(null)
const lifecycleDialogVisible = ref(false)

// 任一弹窗（停止/重启确认、"关于"）或 App 内的 dsh 全屏容器打开期间锁定页面滚动
useBodyScrollLock(
  () => lifecycleDialogVisible.value || aboutVisible.value || entryFrameVisible.value
)

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

// --- 运行时间：基准只取「新鲜的一拍」，之后由前端自己走表 ---
//
// 状态流（/api/dsh/stream）每秒都会推 CPU / 内存，但**运行时间不跟着这些推送换算**：
// 拿到一次基准值（该 PID 已运行的秒数）后，本机按时间差自己推进，后续每秒推来的秒数一律忽略。
// 需要重新定基准的只有三种情况：dsh 启动/停止、重启（含装插件自重启，表现为 pid 变化），
// 以及**从其他子页面切回概览后的第一拍推送** —— 见下面那段「踩过的坑」。
//
// 踩过的坑（这段逻辑存在的理由）：本页**不在** ViewSwitcher 的 KeepAlive 白名单里
// （那里只缓存 TerminalView），切到别的子页面时本组件会被**卸载** —— SSE 连接随之关闭，
// store 里的 status 冻结在「离开的那一刻」；切回来则是**重新挂载**。若直接拿 store 里那份
// 旧快照当基准，计时就会从离开时的秒数接着走，表现为「切回来像暂停过、时间对不上」。
// 因此进入页面后必须用第一拍**推送**重新定基准（SSE 连上时后端会立刻补发初始快照，
// 见 backend/sse.go 的 handleDshStream）。
//
// 用本机时钟推进而不是每拍 +1：页面被切到后台时定时器会被节流，按真实时间差算回来才不会走慢。
type UptimeBase = { pid: number; seconds: number; at: number }
const uptimeBase = ref<UptimeBase | null>(null)
const uptimeTick = ref(Date.now())
let uptimeTimer: number | null = null
// 是否还欠一次「用推送重定基准」：组件每次建立（含切回概览）都为 true。
// 注意：本视图没有进 KeepAlive 的 include，切回来一定是重新挂载，初始值就够用；
// 将来若把它加进 ViewSwitcher 的 keep-alive 名单，必须在 onActivated 里再置回 true。
let needsReanchor = true

// 采纳一份状态作为计时基准。fromPush 表示它来自 SSE 推送（新鲜数据）而不是 store 里的旧值。
function adoptStatus(s: DshStatus | null, fromPush: boolean) {
  const pid = s?.pid ?? 0
  const up = s?.uptimeSeconds
  // 未运行 / 后端读不到该 PID 的启动时刻 → 显示「—」，并等下次运行重新定基准
  if (s?.running !== true || up === undefined) {
    uptimeBase.value = null
    needsReanchor = true
    return
  }
  const samePid = uptimeBase.value !== null && uptimeBase.value.pid === pid
  // 同一 PID 的常规状态：不采纳，展示值继续由本机秒表推进。
  // 唯一例外是「刚进入页面后的第一拍推送」——store 里那份快照可能已经过期，必须用它纠正。
  if (samePid && !(fromPush && needsReanchor)) return
  uptimeBase.value = { pid, seconds: up, at: Date.now() }
  if (fromPush) needsReanchor = false
}

// 展示值 = 基准秒数 + 本机已过去的秒数；没有基准（未运行 / 读不到）时 undefined → 「—」
const uptimeSeconds = computed(() => {
  const b = uptimeBase.value
  if (!b) return undefined
  return b.seconds + Math.max(0, Math.floor((uptimeTick.value - b.at) / 1000))
})

// 本页发起的启停 / 重启会写回 store.status：这里兜住「生命周期变化」（running 翻转 / pid 变化）。
// 刚挂载时 store 里可能还是上次离开时的旧快照，先用它顶一下（避免数字闪成「—」），
// needsReanchor 仍为 true，等第一拍推送到达即被纠正。
watch(() => status.value, (cur) => adoptStatus(cur, false), { immediate: true })

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
    toast.show(uiErrText(e, t('common_op_failed')), 'error')
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
    const s = data as DshStatus
    // 先按「推送」采纳计时基准（进入页面后的第一拍就是靠它纠正过期快照的），再写入 store
    adoptStatus(s, true)
    status.value = s
  }
})

async function removeVisitor(id: string) {
  deleting.value = id
  try {
    const p = await api.deleteVisitor(id)
    toast.show(uiText(p.msg, { code: p.msgCode }, p.deleted ? t('visitor_kicked_fallback') : ''), p.deleted ? 'success' : 'info')
  } catch (e) {
    toast.show(uiErrText(e, t('common_op_failed')), 'error')
  } finally {
    deleting.value = null
  }
}

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
  [...visitors.value].sort((a, b) => {
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
  store.load()
  initialLoad()
  visitorStream.start()
  statusStream.start()
  // 运行时间的本机秒表（见上面的 uptimeSeconds）：只在页面存活期间走，卸载时清掉
  uptimeTimer = window.setInterval(() => {
    uptimeTick.value = Date.now()
  }, 1000)
  if (typeof ResizeObserver !== 'undefined' && visitorCardEl.value) {
    visitorResizeObserver = new ResizeObserver(() => measureVisitorList())
    visitorResizeObserver.observe(visitorCardEl.value)
  }
  window.addEventListener('popstate', onEntryPopState)
})

onBeforeUnmount(() => {
  if (uptimeTimer !== null) {
    clearInterval(uptimeTimer)
    uptimeTimer = null
  }
  visitorResizeObserver?.disconnect()
  visitorResizeObserver = null
  window.removeEventListener('popstate', onEntryPopState)
})
</script>

<template>
  <div class="pt-3 sm:pt-4 pb-8 sm:pb-12 px-4 sm:px-8 max-w-6xl mx-auto">
    <!-- 页头（图标 + 标题 + 右侧操作，统一卡片式标题栏） -->
    <PageHeader sticky class="mb-3" :title="t('overview_title')" :icon="icons.overview">
      <button class="g-btn-secondary h-8 px-3 text-xs" @click="openAbout()">{{ t('about') }}</button>
    </PageHeader>

    <!-- 卡片栅格：dsh 信息与启停 / 三个版本 / DeepSeek 账号 / 登录列表。
         桌面固定两列（见 style.css 的 .g-card-grid-2），四张卡片正好 2×2；
         窄屏沿用 .g-card-grid 的自适应列数（手机为单列）。 -->
    <div class="g-card-grid g-card-grid-2 g-fade-in">
      <!-- ① dsh 服务信息与启停（右上角「进入」= 当前访问环境下的飞牛入口） -->
      <section class="g-card g-card-hover p-5 flex flex-col">
        <div class="flex items-start justify-between gap-3 mb-4">
          <div class="min-w-0">
            <h2 class="font-display text-base font-semibold text-ink dark:text-white">DeepSeek Harness</h2>
            <p class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1">{{ t('overview_dsh_desc') }}</p>
          </div>
          <!-- 拿不到网关入口（非网关部署）时不显示，避免给出指向站点根的错地址 -->
          <button
            v-if="fnosEntry"
            class="g-btn-secondary !h-8 !px-3 !text-xs !border-brand flex-shrink-0"
            @click="openDshEntry"
          >{{ t('dsh_enter') }}</button>
        </div>

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

      <!-- ② 三个版本：dsh 服务 / 插件市场 / 增强插件（控制台自身只能走 fpk 升级） -->
      <UpdateSection />

      <!-- ③ DeepSeek 官方账号：头像 / 余额 / 多账号管理（登录弹窗在卡片内部） -->
      <AccountCard />

      <!-- ④ 登录列表：与其它卡片同宽（桌面 2×2 的最后半格），不再独占一行。
           桌面端仍按「最多显示 3 条」限高并内部滚动，移动端不限制高度、随页面滚动。
           字段仍按「逐项独立一行」排列。 -->
      <section ref="visitorCardEl" class="g-card g-card-hover p-5 flex flex-col">
        <h2 class="font-display text-base font-semibold text-ink dark:text-white">{{ t('login_list') }}</h2>
        <p class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1 mb-4">{{ t('login_list_desc') }}</p>

        <div v-if="visitorsLoading" class="text-sm text-ink-soft dark:text-[#A6A6AD] py-6 text-center">{{ t('loading') }}</div>

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
                <span>{{ t('last_access') }}：<span class="text-ink dark:text-[#EDEDF0]">{{ fmt(v.lastAccess) }}</span></span>
                <span v-if="!isGateway(v)">{{ t('expires_at') }}：<span class="text-ink dark:text-[#EDEDF0]">{{ fmt(v.expiresAt) }}</span></span>
              </div>
              <button
                class="g-btn-danger !h-8 !px-3 !text-xs flex-shrink-0"
                :disabled="deleting === v.id"
                @click="removeVisitor(v.id)"
              >
                {{
                  deleting === v.id
                    ? isGateway(v) ? t('deleting_record') : t('logging_out')
                    : isGateway(v) ? t('visitor_delete_record') : t('logout')
                }}
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

    <!-- App 内的全屏 dsh 容器：走控制台 WebView 的请求栈，避免新 WebView 的入口凭据缺失。
         按要求不保留顶栏：没有「在浏览器打开」，也没有返回控制台的按钮。 -->
    <Teleport to="body">
      <div v-if="entryFrameVisible" class="fixed inset-0 z-[200] bg-bg dark:bg-[#0B0B0F]">
        <div v-if="entryFrameError" class="h-full flex items-center justify-center p-6">
          <div class="g-card p-5 w-full max-w-sm text-center">
            <p class="text-sm text-ink-soft dark:text-[#A6A6AD] leading-relaxed">{{ t('dsh_enter_blocked') }}</p>
            <div class="flex items-center justify-center gap-2 mt-4">
              <button class="g-btn-secondary" @click="retryEntryFrame">{{ t('dsh_enter_retry') }}</button>
            </div>
          </div>
        </div>
        <iframe
          v-else
          :key="entryFrameKey"
          :src="entryFrameUrl"
          class="w-full h-full border-0 bg-white dark:bg-[#0B0B0F]"
          @load="onEntryFrameLoad"
        ></iframe>
      </div>
    </Teleport>

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
            <!-- 标题 + 本仓库的 GitHub 裸图标（紧靠标题；右侧留出 X 的位置） -->
            <div class="flex items-center gap-2 pr-11 mb-5">
              <h3 class="g-dialog-title !pr-0">{{ t('about_title') }}</h3>
              <GithubIconLink :href="repoURL(repoSlugs.harness)" :label="t('about_github')" />
            </div>

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