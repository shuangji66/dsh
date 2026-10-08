<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api, sseUrl, type UpdateKind, type ServerVersions, type UpdateStatus } from '@/serverapi'
import { useToastStore } from '@/stores/toast'
import { useI18n } from '@/composables/useI18n'
import { useEventStream } from '@/composables/useEventStream'
import { useBodyScrollLock } from '@/composables/useBodyScrollLock'
import MarkdownText from '@/components/MarkdownText.vue'
import DialogCloseButton from '@/components/DialogCloseButton.vue'
import CheckUpdateButton from '@/components/CheckUpdateButton.vue'
import GithubIconLink from '@/components/GithubIconLink.vue'
import ServerVersionsDialog from '@/components/ServerVersionsDialog.vue'
import MarketDialog from '@/components/MarketDialog.vue'

// 概览页「版本」卡片：三个版本（harness 控制台 / dsh 服务 / 插件市场）的当前版本、
// 检查更新入口与各自的弹窗。
//
// 三条链路的**形态各不相同**，因此弹窗也分成三个：
//   - harness：GitHub Release 资产（下载 → 安装两步，可暂停/取消）—— 弹窗在本组件内；
//   - dsh 服务：本机多版本（npm install 到 `<数据目录>/server/<版本>`，可下载/删除/切换）
//     —— 弹窗见 ServerVersionsDialog.vue；
//   - 插件市场：普通 profile 插件（未安装 → 安装 latest，已安装 → 更新/卸载）
//     —— 弹窗见 MarketDialog.vue。
//
// 本组件是这三者的**唯一数据源**：它持有 /api/update/stream 的 SSE 快照并按 kind 分发，
// 子弹窗只收 props、只发意图事件。
const toast = useToastStore()
const { t } = useI18n()

// 后端推送的更新状态（harness / dsh / 插件市场 各一份）。本地版本号由
// `/api/update/status` 统一提供（dsh 版本原 `/api/dsh/version` 端点已移除）。
const harnessStatus = ref<UpdateStatus>({ kind: 'harness', localVersion: '', latestVersion: '', hasUpdate: false, checkedAt: '', releaseNotes: '' })
const dshStatus = ref<UpdateStatus>({ kind: 'dsh', localVersion: '', latestVersion: '', hasUpdate: false, checkedAt: '', releaseNotes: '' })
// 市场（dshmarket）：普通 profile 插件，localVersion 为空即「未安装」。
const marketStatus = ref<UpdateStatus>({ kind: 'market', localVersion: '', latestVersion: '', hasUpdate: false, checkedAt: '' })
// dsh 服务版本快照（可选版本列表 + 已安装 + 选中 + 安装进度）。
const serverVersions = ref<ServerVersions | null>(null)

// 各目标是否正在“检查更新”
const checking = ref<Record<UpdateKind, boolean>>({ harness: false, dsh: false, market: false })

// 按 kind 取对应的状态对象：三条链路的字段完全一致，只有数据来源不同。
function statusOf(kind: UpdateKind): UpdateStatus {
  if (kind === 'harness') return harnessStatus.value
  if (kind === 'dsh') return dshStatus.value
  return marketStatus.value
}

// 就地更新某个 kind 的状态（SSE 推送与本地乐观更新共用）。
function patchStatus(kind: UpdateKind, patch: Partial<UpdateStatus>) {
  if (kind === 'harness') harnessStatus.value = { ...harnessStatus.value, ...patch }
  else if (kind === 'dsh') dshStatus.value = { ...dshStatus.value, ...patch }
  else marketStatus.value = { ...marketStatus.value, ...patch }
}

// --- dsh 服务版本弹窗 / 插件市场弹窗 ---
const serverDialogVisible = ref(false)
const serverLoading = ref(false)
const marketDialogVisible = ref(false)

// openServerDialog 打开 dsh 版本列表：先用后端快照兜底显示，再拉一次（避免显示别人
// 刷新前的旧列表）；加载期间按钮禁用，不阻塞已装版本的切换/删除。
async function openServerDialog() {
  serverDialogVisible.value = true
  if (serverVersions.value) return
  await refreshServerVersions(false)
}

// refreshServerVersions 拉取版本列表；force 为 true 时强制联网刷新镜像源列表。
async function refreshServerVersions(force: boolean) {
  serverLoading.value = true
  try {
    const res = await api.dshVersions(force)
    serverVersions.value = res.server
    // dsh 版本与选中状态同时影响版本行与红点，这里顺带同步一次状态。
    const snap = await api.updateStatus()
    merge(snap)
  } catch (e) {
    toast.show((e as Error).message || t('update_error_unknown'), 'error')
  } finally {
    serverLoading.value = false
  }
}

// onInstallVersion / onCancelVersion / onRemoveVersion / onSwitchVersion 是弹窗发上来的
// 意图：这里只负责调接口 + 提示，实际状态变化由后端 SSE 推回 serverVersions。
async function onInstallVersion(version: string) {
  try {
    await api.dshVersionInstall(version)
  } catch (e) {
    toast.show((e as Error).message || t('update_error_unknown'), 'error')
  }
}

async function onCancelVersion() {
  try {
    await api.dshVersionCancel()
    toast.show(t('dsh_ver_cancelled'), 'info')
  } catch (e) {
    toast.show((e as Error).message || t('update_error_unknown'), 'error')
  }
}

// 关闭版本弹窗：如果停在终态（成功/失败/已取消），顺手把结果收起 —— 概览页切走会卸载、
// 切回会重新挂载，状态留着就会把同一条提示再弹一次。
function closeServerDialog() {
  serverDialogVisible.value = false
  const ph = serverVersions.value?.install?.phase || ''
  const cancelled = serverVersions.value?.install?.cancelled
  if (ph === 'done' || ph === 'error' || cancelled) {
    api.dshVersionAck().catch(() => { /* 只是清状态，失败不影响用户 */ })
  }
}

async function onRemoveVersion(version: string) {
  try {
    await api.dshVersionDelete(version)
    toast.show(t('dsh_ver_deleted', { v: version }), 'success')
  } catch (e) {
    toast.show((e as Error).message || t('update_error_unknown'), 'error')
  }
}

async function onSwitchVersion(version: string) {
  try {
    await api.dshVersionSwitch(version)
    toast.show(t('dsh_ver_switched', { v: version }), 'success')
    // 切换会停 dsh 并用新版本重启：稍等片刻再刷新，避免刷新出启动等待页。
    setTimeout(() => window.location.reload(), 3000)
  } catch (e) {
    toast.show((e as Error).message || t('update_error_unknown'), 'error')
  }
}

// 市场三动作：安装（未安装 → 最新版本）/ 更新（已安装 → 最新版本）/ 卸载。
// pendingMarketAction 记下这次点的是哪个动作：结果（成功或失败）经 SSE 异步到达，
// 而弹窗可能已经被关掉，所以结果统一用 toast 通知（文案里的动词按动作区分）。
const pendingMarketAction = ref<'install' | 'update' | 'remove' | null>(null)

async function runMarketAction(action: 'install' | 'update' | 'remove') {
  pendingMarketAction.value = action
  try {
    if (action === 'install') await api.marketInstall()
    else if (action === 'update') await api.marketUpdate()
    else await api.marketRemove()
  } catch (e) {
    pendingMarketAction.value = null
    toast.show((e as Error).message || t('update_error_unknown'), 'error')
  }
}

// marketActionLabel 把动作翻成两字动词（直接复用弹窗按钮文案，避免再维护一份）。
function marketActionLabel(action: 'install' | 'update' | 'remove'): string {
  if (action === 'update') return t('market_update_btn')
  if (action === 'remove') return t('market_remove_btn')
  return t('market_install_btn')
}

// 收起市场操作的终态（成功提示已 toast / 失败详情已看过 / 弹窗已关闭）。
async function onMarketAck() {
  try {
    await api.marketDone()
  } catch { /* 忽略：只是清状态，失败不影响用户 */ }
}

function closeMarketDialog() {
  marketDialogVisible.value = false
}

// 弹窗状态
const dialogVisible = ref(false)
const dialogKind = ref<UpdateKind>('harness')
const updatingDone = ref(false) // 更新成功后短暂显示“更新成功”
const targetVersion = ref('') // 本次要安装的目标版本号（用于判定安装完成）
const installing = ref(false) // 是否正在安装（点击“安装更新”后）
// 是否处于进行中（下载中 / 安装中）：禁用右上角 X（避免关掉正在跑的更新）与重复操作
const busy = computed(() => {
  const ph = dialogStatus.value.phase
  return ph === 'downloading' || ph === 'installing' || installing.value
})
// 下载中（可暂停/取消）
const downloading = computed(() => dialogStatus.value.phase === 'downloading')
// 已暂停：半成品保留，底部按钮变为“继续下载”（后端用 Range 从断点续传）
const paused = computed(() => dialogStatus.value.phase === 'paused')
// 暂停请求是否已发出（等后端推送 phase=paused）
const pausing = ref(false)
// 已下载待安装（显示“安装更新”按钮）
const downloaded = computed(() => dialogStatus.value.phase === 'downloaded' && dialogStatus.value.readyToInstall)
// 取消更新
const cancelConfirmVisible = ref(false) // 取消二次确认弹窗
const cancelling = ref(false) // 取消请求是否已发出、等待后端中断
// 安装二次确认
const installConfirmVisible = ref(false)

let reloadTimer: ReturnType<typeof setTimeout> | null = null
// 进度兜底轮询计时器（见 startProgressPoll）：SSE 不可用时仍能显示真实进度。
let progressPollTimer: ReturnType<typeof setInterval> | null = null
// 安装前的控制台版本号与就绪轮询计时器（harness 自我更新专用，见
// startHarnessReadyPoll）。
let preInstallVersion = ''
let readyPollTimer: ReturnType<typeof setInterval> | null = null

// 从后端快照合并到本地响应式状态（允许只带部分 kind 的快照）
function merge(snap: { harness?: UpdateStatus; dsh?: UpdateStatus; market?: UpdateStatus; server?: ServerVersions }) {
  if (snap.harness) {
    harnessStatus.value = { ...snap.harness, localVersion: snap.harness.localVersion || '' }
  }
  if (snap.dsh) {
    dshStatus.value = { ...snap.dsh, localVersion: snap.dsh.localVersion || '' }
  }
  if (snap.market) {
    marketStatus.value = { ...snap.market, localVersion: snap.market.localVersion || '' }
  }
  // dsh 版本快照（列表 + 安装进度）也随同一条 SSE 推送更新 —— 版本弹窗里那一行的
  // 下载/包处理进度就来自这里，不需要额外开一条通道。
  if (snap.server) {
    serverVersions.value = snap.server
  }
}

// 弹窗所指向的目标状态
const dialogStatus = computed<UpdateStatus>(() => statusOf(dialogKind.value))

// 弹窗标题
const dialogTitle = computed(() => {
  if (dialogKind.value === 'harness') return t('update_dialog_title_harness')
  if (dialogKind.value === 'dsh') return t('update_dialog_title_dsh')
  return t('update_dialog_title_market')
})

// 三个更新目标各自的 GitHub 仓库（更新弹窗标题右侧那个裸图标外链指向的地址）：
//   - harness 控制台 = 本仓库（harness-* 与 dsh-* 两类发布资产都在这里）；
//   - dsh 服务 = 上游 deepseek-ai/deepseek-harness（server 包按上游 dsh 版本构建）；
//   - 插件市场 = dsh-market/dsh-market。
// 只作为展示用的常量，与后端更新链路用的仓库地址（update.go 的 updateRepoURL）无关。
const repoSlugs: Record<UpdateKind, string> = {
  harness: 'shuangji66/dsh',
  dsh: 'deepseek-ai/deepseek-harness',
  market: 'dsh-market/dsh-market',
}
const dialogRepoSlug = computed(() => repoSlugs[dialogKind.value])
const dialogRepoURL = computed(() => `https://github.com/${dialogRepoSlug.value}`)

// 版本号右上角红点：有更新时显示
function hasUpdateDot(kind: UpdateKind): boolean {
  return statusOf(kind).hasUpdate
}
function versionText(kind: UpdateKind): string {
  return statusOf(kind).localVersion
}
function latestText(kind: UpdateKind): string {
  return statusOf(kind).latestVersion
}

// 通过 SSE 监听后端推送的更新检测结果。
// 用 useEventStream 而非裸 EventSource：断线（反代掐断空闲长连接、后端重启等）
// 后会自动重连；重连成功时后端立刻补发一份初始快照，下载进度随即追平。此前在
// onerror 里 close() 会让浏览器永久放弃该连接，下载进度再也送不到页面。
const updateStream = useEventStream(() => sseUrl('/api/update/stream'), {
  update: (data) => {
    const d = data as { harness?: UpdateStatus; dsh?: UpdateStatus; market?: UpdateStatus; server?: ServerVersions }
    if (d && (d.harness || d.dsh || d.market || d.server)) merge(d)
  }
})

// “检查更新”按钮：通知后端执行一次检测，随后 SSE 推送最新结果
async function doCheck(kind: UpdateKind) {
  checking.value[kind] = true
  try {
    // 后端同步执行检测并返回最新结果（含 dsh 版本列表快照）
    const snap = await api.updateCheck()
    merge(snap)
    const st = statusOf(kind)
    if (st?.error) {
      toast.show(st.error, 'error')
    } else if (st && !st.hasUpdate) {
      toast.show(t('update_no_new'), 'success')
    }
  } catch (e) {
    toast.show((e as Error).message || t('update_error_unknown'), 'error')
  } finally {
    checking.value[kind] = false
  }
}

// 打开对应目标的弹窗：harness 用本组件内的更新弹窗，dsh / 市场各有自己的弹窗。
function openDialog(kind: UpdateKind) {
  if (kind === 'dsh') {
    openServerDialog()
    return
  }
  if (kind === 'market') {
    marketDialogVisible.value = true
    return
  }
  dialogKind.value = kind
  dialogVisible.value = true
}

// 第一步：下载更新包（可暂停/取消）。后端下载完成后经 SSE 推送 phase=downloaded，
// 弹窗按钮随之变为“安装更新”；暂停则推送 phase=paused，底部按钮变“继续下载”
// （继续时后端按 HTTP Range 从已下载字节续传，不重下）。
async function doDownload() {
  const kind = dialogKind.value
  if (busy.value) return
  targetVersion.value = dialogStatus.value.latestVersion
  updatingDone.value = false
  installing.value = false
  pausing.value = false
  cancelling.value = false
  // 乐观更新：点击后本地立即切到“下载中”视图（进度条 + 取消按钮），
  // 不再依赖 SSE 首帧推送——SSE 断开/丢帧时也能立刻看到进度页，
  // 下载实际已在后台执行；后续收到进度帧再实时刷新数字。
  applyLocalDownloading()
  try {
    await api.updateDownload(kind)
    // 后端异步下载，进度/完成状态经 SSE 推送。
    // 除 SSE 外再启动一个秒级轮询兜底：某些反向代理会缓冲甚至直接掐断
    // text/event-stream（此时 SSE 长时间收不到任何事件），轮询能保证进度条
    // 依然展示真实百分比，而不是一直停在 0%。
    startProgressPoll(kind)
  } catch (e) {
    // 请求阶段即失败（参数错误等）：回滚到待更新状态。
    toast.show((e as Error).message || t('update_failed'), 'error')
    applyLocalReset()
  }
}

// --- 进度兜底轮询 ---

// startProgressPoll 在下载期间以 1 秒间隔拉取一次状态快照，直接驱动进度显示。
//
// 为什么不能只靠 SSE：SSE 会经过反向代理（fnOS 网关 / 用户自建 nginx 等），
// 这类中间层可能对 text/event-stream 做缓冲，或按空闲超时掐断长连接。一旦事件
// 长时间到不了前端，进度就只能停在乐观初值 0%（历史上「实际在下载却一直显示
// 0%」的另一半原因）。轮询走的是普通 GET，不受缓冲影响，作为兜底始终有效；
// 下载结束（成功/失败/取消）即停止，不引入长期轮询开销。
function startProgressPoll(kind: UpdateKind) {
  stopProgressPoll()
  progressPollTimer = setInterval(async () => {
    try {
      const snap = await api.updateStatus()
      merge(snap)
      const st = kind === 'harness' ? snap.harness : kind === 'dsh' ? snap.dsh : snap.market
      // 下载阶段结束（已就绪 / 报错 / 取消 / 回到空闲）即停止兜底轮询。
      if (!st || st.phase !== 'downloading') stopProgressPoll()
    } catch {
      // 单次请求失败（如后端重启）忽略，下一拍继续。
    }
  }, 1000)
}

function stopProgressPoll() {
  if (progressPollTimer) {
    clearInterval(progressPollTimer)
    progressPollTimer = null
  }
}

// 本地立即进入“下载中”状态（供 doDownload 乐观更新，不等 SSE 首帧）。
function applyLocalDownloading() {
  // 续传（点击“继续下载”）时不要清零已下载字节：否则进度条会从 0 重来，
  // 而实际是从断点接着下（后端随后会用真实起点覆盖，但那一跳很刺眼）。
  const resuming = paused.value
  const patch: Partial<UpdateStatus> = {
    phase: 'downloading',
    readyToInstall: false,
    paused: false,
    downloading: true,
    error: '',
    errorHint: '',
    cancelled: false
  }
  if (!resuming) {
    patch.downloadPct = 0
    patch.downloadedBytes = 0
    patch.totalBytes = 0
  }
  patchStatus(dialogKind.value, patch)
}

// 本地把当前弹窗目标重置为“待更新、未下载”状态（下载请求失败时回滚）。
function applyLocalReset() {
  patchStatus(dialogKind.value, {
    phase: '',
    readyToInstall: false,
    paused: false,
    downloading: false,
    downloadPct: 0,
    downloadedBytes: 0,
    totalBytes: 0
  })
}

// 安装二次确认的正文（现在只有 harness 会走到这个弹窗）。
const installConfirmMsg = computed(() => t('update_install_confirm_msg'))

// 第二步：安装更新包（不可取消）。先弹二次确认，再调 /api/update/install。
function openInstallConfirm() {
  if (busy.value || !downloaded.value) return
  installConfirmVisible.value = true
}

async function doInstall() {
  const kind = dialogKind.value
  if (busy.value || !downloaded.value) return
  installConfirmVisible.value = false
  installing.value = true
  // 记录安装前的本地版本号，供 harness 就绪轮询比对（见 startHarnessReadyPoll）。
  preInstallVersion = versionText(kind)
  try {
    await api.updateInstall(kind)
    // harness 自我更新会用新二进制 exec 替换当前进程映像：推送 phase="done" 的那个
    // 进程随即消失，前端**永远**收不到成功推送（此前只能干等 60 秒兜底计时器，即
    // “等待弹窗时间太久”）。改为轮询新进程上报的版本号，进程一就绪立刻收尾。
    startHarnessReadyPoll()
  } catch (e) {
    installing.value = false
    toast.show((e as Error).message || t('update_failed'), 'error')
  }
}

// --- harness 自我更新就绪轮询 ---

// 轮询间隔与总时长上限。上限只是兜底（exec 失败、或重装同版本导致版本号不变），
// 正常情况进程一就绪（通常 1~3 秒）就会命中并立即收尾。
const HARNESS_READY_POLL_INTERVAL = 1000
const HARNESS_READY_POLL_TIMEOUT = 60000

// startHarnessReadyPoll 轮询后端，直到确认控制台已换到新进程。
//
// 为什么不能等 SSE：harness 自我更新由 syscall.Exec 用新二进制替换当前进程映像，
// 推送 phase="done" 的那个进程随即消亡，前端**永远**收不到成功推送——此前只能干等
// 60 秒兜底计时器，即“等待弹窗时间太久”。这里改为主动探测新进程，任一成立即就绪：
//   1) 版本号已不同于安装前的版本号（最可靠，正常升级必然满足）；
//   2) 出现过服务短暂不可用（exec 切换时 admin socket 会断开），且恢复后状态已不是
//      phase="installing"（新进程内存全新、phase 为空；旧进程在整个安装期间都是
//      installing）——用于覆盖“重装同一版本”这种版本号不变的场景。
// 条件 2 额外要求 phase 已非 installing，是为了防误判：偶发一次请求失败（网络抖动、
// 非重启引起）也会置位 outage 标记，若只看 outage 就会在旧进程仍安装时就判定完成。
// 两条都无法确认时兜底超时，停止轮询并提示手动刷新（不自动 reload，避免新进程
// 尚未开始监听时刷新出错误页）。
function startHarnessReadyPoll() {
  stopHarnessReadyPoll()
  const startedAt = Date.now()
  // 是否观察到服务短暂不可用（exec 切换期间 admin socket 会短暂断开）。
  let sawOutage = false
  readyPollTimer = setInterval(async () => {
    const timedOut = Date.now() - startedAt > HARNESS_READY_POLL_TIMEOUT
    try {
      const snap = await api.updateStatus()
      const local = snap.harness?.localVersion || ''
      merge(snap)
      // 版本号比对必须以“安装前版本号已知”为前提：若安装前版本号为空（初始快照
      // 请求失败），任意非空返回值都会被误判为“已换新进程”，从而在 exec 完成前
      // 就 reload。此时退化为只认 outage 信号。
      const versionChanged =
        !!preInstallVersion && !!local && local !== preInstallVersion
      const restarted = sawOutage && snap.harness?.phase !== 'installing'
      if (versionChanged || restarted) {
        stopHarnessReadyPoll()
        installing.value = false
        updatingDone.value = true
        // 短暂显示“安装成功”，随后关闭弹窗并刷新页面（此时新进程已在服务）。
        setTimeout(() => {
          dialogVisible.value = false
          window.location.reload()
        }, 1200)
        return
      }
    } catch {
      // 重启期间 admin socket 短暂不可用，属预期情况，作为就绪信号记录下来。
      sawOutage = true
    }
    if (timedOut) {
      // 无法确认新进程就绪（如 exec 失败；该错误通常会先经 SSE 推送并由
      // watchForCompletion 处理）。这里只结束“安装中”视图并提示手动刷新，
      // 由用户确认实际版本，不自动 reload（新进程可能尚未开始监听）。
      stopHarnessReadyPoll()
      installing.value = false
      toast.show(t('update_manual_refresh'), 'info')
    }
  }, HARNESS_READY_POLL_INTERVAL)
}

function stopHarnessReadyPoll() {
  if (readyPollTimer) {
    clearInterval(readyPollTimer)
    readyPollTimer = null
  }
}

// --- 更新下载进度与取消 ---

// 下载进度百分比（0-100，未知总量时为 0）
const downloadPct = computed(() => {
  return Math.max(0, Math.min(100, dialogStatus.value.downloadPct ?? 0))
})
// 是否已知更新包总大小（Content-Length）
const downloadTotalKnown = computed(() => (dialogStatus.value.totalBytes ?? 0) > 0)
// 进度条宽度：已知总量按百分比；未知总量时不设置宽度（交给 .progress-indeterminate
// 的 CSS 动画），绝不能用固定百分比占位——否则会显示成“卡在 50%”的假进度。
const progressBarStyle = computed(() =>
  downloadTotalKnown.value ? { width: downloadPct.value + '%' } : {}
)
// 未知总量时用不确定进度动画（来回滑动的窄条）表示“进行中”
const progressBarClass = computed(() => (downloadTotalKnown.value ? '' : 'progress-indeterminate'))
// 格式化字节数（下载进度文案用）
function fmtSize(bytes: number): string {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / 1024 / 1024).toFixed(1) + ' MB'
}

// 已下载 / 总量文字
const downloadSizeText = computed(() => {
  const d = dialogStatus.value
  const dl = fmtSize(d.downloadedBytes ?? 0)
  if (downloadTotalKnown.value) {
    return t('update_download_size', { downloaded: dl, total: fmtSize(d.totalBytes ?? 0) })
  }
  return t('update_download_unknown_size', { downloaded: dl })
})

// 点击“暂停” → 通知后端停止传输但保留半成品；后端推送 phase=paused 后
// 底部按钮变为“继续下载”，继续时从已下载字节续传（不重下）。
async function doPause() {
  if (!downloading.value || pausing.value) return
  pausing.value = true
  try {
    await api.updatePause()
    // 保持“下载中”视图等 SSE 推送 paused；若推送没到（反代缓冲），下面的
    // 进度兜底轮询会在 1 秒内看到 phase 变化并切换视图。
  } catch (e) {
    pausing.value = false
    toast.show((e as Error).message || t('update_failed'), 'error')
  }
}

// 点击“取消更新” → 弹出二次确认（下载中或已暂停都有效）
function openCancelConfirm() {
  if ((!downloading.value && !paused.value) || cancelling.value) return
  cancelConfirmVisible.value = true
}

// 确认取消：下载中 → 中断传输并删除半成品；已暂停 → 没有进行中的下载，
// 直接丢弃半成品并复位状态（两者对用户的语义一致：这些字节我不要了）。
async function doCancelUpdate() {
  if (cancelling.value) return
  cancelling.value = true
  try {
    if (paused.value) {
      await api.updateDiscard(dialogKind.value)
      patchStatus(dialogKind.value, {
        phase: '', paused: false, readyToInstall: false, downloading: false,
        downloadPct: 0, downloadedBytes: 0, totalBytes: 0, error: '', errorHint: '',
      })
      cancelConfirmVisible.value = false
      cancelling.value = false
      pausing.value = false
      toast.show(t('update_discarded'), 'success')
      return
    }
    await api.updateCancel()
    // 保持“下载中”状态等待 SSE 推送；不额外兜底计时器，避免与完成推送竞争。
    // 若取消请求到达时下载恰好已完成（后端无取消目标），则 phase 会变为
    // downloaded（已就绪），用户可自行选择安装。
  } catch (e) {
    cancelling.value = false
    cancelConfirmVisible.value = false
    toast.show((e as Error).message || t('update_failed'), 'error')
  }
}

// --- 安装/操作结果的一次性提示 ---
//
// 弹窗现在可以在下载/安装过程中关闭（关掉不会中断后台任务），因此**结果必须能到达用户**：
// 完成或失败都以 toast 提示一次（成功不再常驻在弹窗里）。
//
// 只提示一次的机制有两层：①组件内按「签名」去重，SSE 重复推同一份快照不会重复弹；
// ②成功提示发出后立刻调 `dshVersionAck` 把后端那份终态收起 —— 概览页切走会卸载、切回会
// 重新挂载，状态留着就会把同一条成功提示再弹一次。失败不清（错误详情要留在弹窗里）。
let announcedServerInstall = ''
let announcedMarketOp = ''

const serverInstallSig = computed(() => {
  const st = serverVersions.value?.install
  return st ? `${st.version}|${st.phase || ''}|${st.cancelled ? 'c' : ''}` : ''
})

watch(serverInstallSig, () => {
  const st = serverVersions.value?.install
  if (!st) return
  const sig = serverInstallSig.value
  if (sig === announcedServerInstall) return
  if (st.phase === 'done') {
    announcedServerInstall = sig
    toast.show(t('dsh_ver_toast_done', { v: st.version }), 'success')
    // 成功只提示一次：立刻把后端那份终态收起，避免切回概览页又弹一次。
    api.dshVersionAck().catch(() => { /* 忽略：只是清状态 */ })
  } else if (st.phase === 'error' && st.error) {
    announcedServerInstall = sig
    toast.show(st.error, 'error')
    // 失败不在这里清：错误详情要留在弹窗里给用户看，等用户关闭弹窗时再收起。
  }
})

const marketOpSig = computed(() => `${marketStatus.value.phase || ''}|${marketStatus.value.error || ''}`)

watch(marketOpSig, () => {
  const st = marketStatus.value
  const sig = marketOpSig.value
  if (sig === announcedMarketOp) return
  if (st.phase === 'done') {
    announcedMarketOp = sig
    const action = pendingMarketAction.value
    pendingMarketAction.value = null
    toast.show(
      action ? t('market_toast_done', { action: marketActionLabel(action) }) : t('market_toast_done_generic'),
      'success'
    )
    // 与 dsh 版本安装同一条约定：成功只提示一次，立刻收起后端那份终态。
    api.marketDone().catch(() => { /* 忽略：只是清状态 */ })
  } else if (st.error) {
    announcedMarketOp = sig
    pendingMarketAction.value = null
    toast.show(st.error, 'error')
    // 失败不清：错误详情留在弹窗里，等用户关闭弹窗时再收起。
  }
})

// --- 删除已下载的更新包 ---
const discardConfirmVisible = ref(false) // 删除更新包二次确认

// 任一弹窗打开期间锁定页面滚动（弹窗会叠加：更新弹窗之上还有取消/安装等二次确认；
// dsh 版本弹窗与市场弹窗也各自可能叠加确认框，但它们自己也会锁滚动）。
const anyDialogOpen = computed(
  () =>
    dialogVisible.value ||
    cancelConfirmVisible.value ||
    installConfirmVisible.value ||
    discardConfirmVisible.value
)
useBodyScrollLock(anyDialogOpen)

function openDiscardConfirm() {
  if (!downloaded.value || busy.value) return
  discardConfirmVisible.value = true
}

// 确认删除：清除后端待安装更新包并重置为待更新状态（SSE 推送 phase 清空）。
async function doDiscard() {
  const kind = dialogKind.value
  if (!downloaded.value) return
  discardConfirmVisible.value = false
  try {
    await api.updateDiscard(kind)
    toast.show(t('update_discarded'), 'success')
    // 后端已推送 phase="" / readyToInstall=false；若 SSE 暂未送达，
    // 本地也主动复位，保证按钮立刻回到“下载更新”。
    patchStatus(dialogKind.value, { phase: '', readyToInstall: false, downloading: false })
  } catch (e) {
    toast.show((e as Error).message || t('update_failed'), 'error')
  }
}

// 侦测后端推送的各阶段完成/失败状态。
// 触发时机：phase 变化 / 进度字段变化 / 安装完成后的状态推送。
function watchForCompletion(kind: UpdateKind, st: UpdateStatus) {
  if (kind !== dialogKind.value) return
  // 下载完成 → 进入 downloaded（readyToInstall=true）：清除兜底计时器，
  // 按钮自动变为“安装更新”。
  if (st.phase === 'downloaded') {
    clearTimeout(reloadTimer ?? undefined)
    stopProgressPoll()
    return
  }
  // 下载已暂停：半成品保留，视图切到“已暂停”，底部按钮变“继续下载”。
  if (st.phase === 'paused' || st.paused) {
    stopProgressPoll()
    pausing.value = false
    cancelling.value = false
    cancelConfirmVisible.value = false
    return
  }
  // 用户取消下载：退出进行中状态，可重新下载。
  if (st.cancelled || (st.error && st.error.includes('用户取消'))) {
    clearTimeout(reloadTimer ?? undefined)
    stopProgressPoll()
    stopHarnessReadyPoll()
    installing.value = false
    cancelling.value = false
    pausing.value = false
    cancelConfirmVisible.value = false
    toast.show(t('update_cancelled'), 'info')
    return
  }
  // 失败（下载失败 / 安装失败）：退出进行中状态。
  if (st.error) {
    clearTimeout(reloadTimer ?? undefined)
    stopProgressPoll()
    // 若正在等 harness 新进程就绪，失败推送说明不会再就绪，立即停止轮询，
    // 避免 60 秒后再弹一次“请手动刷新”的重复提示。
    stopHarnessReadyPoll()
    installing.value = false
    cancelling.value = false
    pausing.value = false
    cancelConfirmVisible.value = false
    installConfirmVisible.value = false
    toast.show(st.error || t('update_failed'), 'error')
    return
  }
  // 安装成功：后端完成解压并推送明确成功信号 phase==="done"（非空，JSON
  // omitempty 不会把它省略），前端即可结束弹窗——无需等待 dsh 完全启动成功。
  // 注意：只有 dsh 安装会走到这里；harness 自我更新时推送进程已被 exec 换掉，
  // 由 startHarnessReadyPoll 的轮询负责收尾。
  if (installing.value && st.phase === 'done') {
    clearTimeout(reloadTimer ?? undefined)
    stopHarnessReadyPoll()
    installing.value = false
    updatingDone.value = true
    // 短暂显示“安装成功”，随后关闭弹窗并刷新页面。
    setTimeout(() => {
      dialogVisible.value = false
      window.location.reload()
    }, 1200)
  }
}

function closeDialog() {
  if (busy.value) return
  dialogVisible.value = false
}

onMounted(() => {
  // 拉取一次后端状态快照作为初始值（含 dsh 版本快照），随后交给 SSE 增量更新
  api.updateStatus().then(merge).catch(() => {})
  updateStream.start()
})

onBeforeUnmount(() => {
  // SSE 连接由 useEventStream 自行释放（其内部注册了 onBeforeUnmount）。
  if (reloadTimer) clearTimeout(reloadTimer)
  stopProgressPoll()
  stopHarnessReadyPoll()
})

// 侦测后端推送的各阶段状态变化：下载进度、下载完成、安装完成/失败、取消。
watch(
  () => statusOf(dialogKind.value),
  (st) => {
    if (dialogVisible.value) watchForCompletion(dialogKind.value, st)
  },
  { deep: true }
)
</script>
<template>
  <!-- 概览页「版本」卡片：三行版本 + 各自检查更新入口；更新/回滚弹窗挂在卡片内（Teleport 到 body） -->
  <section class="g-card g-card-hover p-5 flex flex-col">
    <h2 class="font-display text-base font-semibold text-ink dark:text-white">{{ t('overview_versions') }}</h2>
    <p class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1 mb-3">{{ t('overview_versions_hint') }}</p>

    <div class="divide-y divide-line dark:divide-[#2A2A32]">
      <!-- harness 控制台版本 -->
      <div class="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 py-2.5">
        <span class="text-xs text-ink-soft dark:text-[#A6A6AD]">{{ t('update_harness_ver') }}</span>
        <div class="flex items-center gap-3 min-w-0">
          <!-- 版本号右对齐 + 常驻下划线：点击打开更新弹窗 -->
          <button class="relative font-mono text-sm font-semibold text-ink dark:text-white underline underline-offset-4 decoration-ink-soft/50 dark:decoration-[#A6A6AD]/50" @click="openDialog('harness')">
            {{ versionText('harness') }}
            <!-- 有更新时右上角红点 -->
            <span v-if="hasUpdateDot('harness')" class="absolute -top-1.5 -right-2.5 h-2.5 w-2.5 rounded-full bg-[#EF4444] shadow"></span>
          </button>
          <!-- 检查更新：与 dsh 回退图标成对（见 CheckUpdateButton.vue） -->
          <CheckUpdateButton :checking="checking.harness" :label="t('update_check')" @check="doCheck('harness')" />
        </div>
      </div>

      <!-- dsh 服务版本 -->
      <div class="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 py-2.5">
        <span class="text-xs text-ink-soft dark:text-[#A6A6AD]">{{ t('update_dsh_ver') }}</span>
        <div class="flex items-center gap-3 min-w-0">
          <!-- 版本号：点击打开 dsh 版本列表（下载 / 删除 / 切换）。「未安装」时同样
               可点 —— 那正是用户需要去装一个版本的入口。 -->
          <button class="relative font-mono text-sm font-semibold text-ink dark:text-white underline underline-offset-4 decoration-ink-soft/50 dark:decoration-[#A6A6AD]/50" @click="openDialog('dsh')">
            {{ versionText('dsh') || t('not_installed') }}
            <span v-if="hasUpdateDot('dsh')" class="absolute -top-1.5 -right-2.5 h-2.5 w-2.5 rounded-full bg-[#EF4444] shadow"></span>
          </button>
          <!-- 检查更新：与 dsh 回退图标成对（见 CheckUpdateButton.vue） -->
          <CheckUpdateButton :checking="checking.dsh" :label="t('update_check')" @check="doCheck('dsh')" />
        </div>
      </div>

      <!-- 插件市场版本（dshmarket）：普通 profile 插件，未安装时可在这里装上 -->
      <div class="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 py-2.5">
        <span class="text-xs text-ink-soft dark:text-[#A6A6AD]">{{ t('update_market_ver') }}</span>
        <div class="flex items-center gap-3 min-w-0">
          <!-- 版本号：点击打开市场弹窗（未安装 → 安装最新版；已安装 → 更新 / 卸载） -->
          <button
            class="relative font-mono text-sm font-semibold underline underline-offset-4 decoration-ink-soft/50 dark:decoration-[#A6A6AD]/50 text-ink dark:text-white"
            @click="openDialog('market')"
          >
            {{ versionText('market') || t('not_installed') }}
            <span v-if="hasUpdateDot('market')" class="absolute -top-1.5 -right-2.5 h-2.5 w-2.5 rounded-full bg-[#EF4444] shadow"></span>
          </button>
          <CheckUpdateButton :checking="checking.market" :label="t('update_check')" @check="doCheck('market')" />
        </div>
      </div>
    </div>

    <!-- 更新弹窗 -->
    <Teleport to="body">
      <Transition
        enter-active-class="transition duration-200 ease-out"
        enter-from-class="opacity-0"
        enter-to-class="opacity-100"
        leave-active-class="transition duration-150 ease-in"
        leave-from-class="opacity-100"
        leave-to-class="opacity-0"
      >
        <div v-if="dialogVisible" class="fixed inset-0 z-50 flex items-center justify-center p-4 backdrop-blur-sm">
          <div class="g-modal-mask" @click="closeDialog"></div>
          <div class="relative w-full max-w-sm bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6">
            <DialogCloseButton :label="t('dialog_close')" :disabled="busy" @close="closeDialog" />
            <!-- 标题 + 该目标对应的 GitHub 仓库裸图标（紧靠标题；右侧留出 X 的位置） -->
            <div class="flex items-center gap-2 pr-11 mb-1">
              <h3 class="g-dialog-title !pr-0">{{ dialogTitle }}</h3>
              <GithubIconLink :href="dialogRepoURL" :label="dialogRepoSlug" />
            </div>

            <div v-if="updatingDone" class="py-6 text-center">
              <div class="text-sm font-medium text-success dark:text-[#10B981] mb-1">{{ t('update_installed_done') }}</div>
              <div class="text-xs text-ink-soft dark:text-[#A6A6AD] mt-2">{{ t('update_manual_refresh') }}</div>
            </div>

            <!-- 下载中：进度条 + 暂停（保留已下载字节）/ 取消（放弃已下载字节） -->
            <div v-else-if="downloading" class="py-4">
              <div class="flex items-center justify-between text-xs text-ink-soft dark:text-[#A6A6AD] mb-1">
                <span>{{ t('update_downloading') }}</span>
                <span v-if="downloadTotalKnown">{{ downloadPct }}%</span>
              </div>
              <div class="h-2 rounded-full bg-black/10 dark:bg-white/10 overflow-hidden">
                <div
                  class="h-full rounded-full bg-brand transition-all duration-300"
                  :class="progressBarClass"
                  :style="progressBarStyle"
                ></div>
              </div>
              <div class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1">{{ downloadSizeText }}</div>

              <!-- 暂停（保留半成品，可继续）+ 取消（删除半成品）。 -->
              <div class="flex items-center justify-center gap-3 mt-4">
                <button
                  class="g-btn-secondary"
                  :disabled="pausing"
                  @click="doPause"
                >{{ t('update_pause_btn') }}</button>
                <button
                  class="g-btn-danger"
                  :disabled="cancelling"
                  @click="openCancelConfirm"
                >{{ t('update_cancel') }}</button>
              </div>
            </div>

            <!-- 已暂停：显示暂停位置 + 继续下载（断点续传）/ 取消 -->
            <div v-else-if="paused" class="py-4">
              <div class="flex items-center justify-between text-xs text-ink-soft dark:text-[#A6A6AD] mb-1">
                <span>{{ t('update_paused') }}</span>
                <span v-if="downloadTotalKnown">{{ downloadPct }}%</span>
              </div>
              <div class="h-2 rounded-full bg-black/10 dark:bg-white/10 overflow-hidden">
                <div class="h-full rounded-full bg-brand/50" :style="progressBarStyle"></div>
              </div>
              <div class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1">{{ downloadSizeText }}</div>
              <div class="text-xs text-ink-soft dark:text-[#A6A6AD] mt-3 leading-relaxed">{{ t('update_paused_hint') }}</div>

              <div class="flex items-center justify-center gap-3 mt-4">
                <button
                  class="g-btn-danger"
                  :disabled="cancelling"
                  @click="openCancelConfirm"
                >{{ t('update_cancel') }}</button>
              </div>
            </div>

            <!-- 已下载待安装：提示 + “删除更新包”按钮，底部为“安装更新”按钮 -->
            <div v-else-if="downloaded" class="py-4 text-center">
              <div class="text-sm text-ink dark:text-white mb-1">{{ t('update_wait_install') }}</div>
              <div class="text-xs text-ink-soft dark:text-[#A6A6AD] mt-2">{{ t('update_manual_refresh') }}</div>

              <!-- 删除更新包（清除下载，重置为待更新） -->
              <div class="text-center mt-4">
                <button
                  class="g-btn-danger"
                  @click="openDiscardConfirm"
                >{{ t('update_discard_btn') }}</button>
              </div>
            </div>

            <!-- 安装中：不可取消 -->
            <div v-else-if="installing" class="py-6 text-center">
              <div class="inline-block animate-spin h-6 w-6 border-2 border-brand border-t-transparent rounded-full mb-2"></div>
              <div class="text-sm text-ink-soft dark:text-[#A6A6AD]">{{ t('update_installing') }}</div>
              <div class="text-xs text-ink-soft dark:text-[#A6A6AD] mt-2">{{ t('update_manual_refresh') }}</div>
            </div>

            <template v-else>
              <div class="mt-3 space-y-2">
                <div class="flex items-center justify-between">
                  <span class="text-sm text-ink-soft dark:text-[#A6A6AD]">{{ t('update_local_ver') }}</span>
                  <span class="font-mono text-sm font-semibold text-ink dark:text-white">{{ versionText(dialogKind) }}</span>
                </div>
                <div class="flex items-center justify-between">
                  <span class="text-sm text-ink-soft dark:text-[#A6A6AD]">{{ t('update_latest_ver') }}</span>
                  <span class="font-mono text-sm font-semibold text-ink dark:text-white">{{ latestText(dialogKind) || '—' }}</span>
                </div>
              </div>

              <!-- 失败提示：取消显示中性提示，网络类失败额外给一条本地化指引 -->
              <div
                v-if="dialogStatus.error || dialogStatus.cancelled"
                class="mt-3 rounded-lg px-3 py-2 text-xs break-words"
                :class="dialogStatus.cancelled
                  ? 'bg-black/5 dark:bg-white/5 border border-line dark:border-[#2A2A32] text-ink-soft dark:text-[#A6A6AD]'
                  : 'bg-danger/10 dark:bg-[#EF4444]/10 border border-danger/30 dark:border-[#EF4444]/30 text-[#EF4444]'"
              >
                {{ dialogStatus.cancelled ? t('update_cancelled') : dialogStatus.error }}
                <!-- 代理与直连各 2 次都失败时，后端给 errorHint=network，这里用当前语言提示 -->
                <div v-if="dialogStatus.errorHint === 'network'" class="mt-1 font-medium">
                  {{ t('update_error_network_hint') }}
                </div>
              </div>

              <template v-else>
                <!-- 更新内容（release 正文，不含标题）：Markdown 渲染，超长可滚动，不撑破弹窗 -->
                <div v-if="dialogStatus.hasUpdate && dialogStatus.releaseNotes" class="mt-3">
                  <div class="text-xs text-ink-soft dark:text-[#A6A6AD] mb-1">{{ t('update_release_notes') }}</div>
                  <div class="rounded-lg bg-black/5 dark:bg-white/5 border border-line dark:border-[#2A2A32] px-3 py-2 text-xs text-ink dark:text-[#EDEDF0] max-h-44 overflow-y-auto leading-relaxed">
                    <MarkdownText :source="dialogStatus.releaseNotes" />
                  </div>
                </div>

                <!-- 无更新提示 -->
                <div v-else-if="!dialogStatus.hasUpdate" class="mt-3 text-sm text-ink-soft dark:text-[#A6A6AD]">
                  {{ t('update_no_update') }}
                </div>
              </template>
            </template>

            <!-- 底部操作行：只放「本次可执行的动作」（安装/下载/继续/重新下载）。
                 关闭不再需要底部按钮 —— 右上角 X 统一负责；没有可执行动作时整行隐藏，
                 避免留一条空白。 -->
            <div v-if="downloaded || (dialogStatus.hasUpdate && !busy && !updatingDone)" class="g-dialog-actions">
              <!-- 已下载待安装 → 安装更新 -->
              <button
                v-if="downloaded"
                class="g-btn-secondary"
                @click="openInstallConfirm"
              >{{ t('update_install_btn') }}</button>
              <!-- 空闲且有待更新 → 下载更新 / 继续下载（暂停后断点续传）/ 重新下载（取消后） -->
              <button
                v-else-if="dialogStatus.hasUpdate && !busy && !updatingDone"
                class="g-btn-secondary"
                @click="doDownload"
              >{{ paused ? t('update_resume_btn') : dialogStatus.cancelled ? t('update_redownload') : t('update_download_btn') }}</button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

    <!-- 取消更新二次确认弹窗 -->
    <Teleport to="body">
      <Transition
        enter-active-class="transition duration-200 ease-out"
        enter-from-class="opacity-0"
        enter-to-class="opacity-100"
        leave-active-class="transition duration-150 ease-in"
        leave-from-class="opacity-100"
        leave-to-class="opacity-0"
      >
        <div v-if="cancelConfirmVisible" class="fixed inset-0 z-[60] flex items-center justify-center p-4 backdrop-blur-sm">
          <div class="g-modal-mask" @click="cancelConfirmVisible = false"></div>
          <div class="relative w-full max-w-sm bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6">
            <DialogCloseButton :label="t('dialog_close')" :disabled="cancelling" @close="cancelConfirmVisible = false" />
            <h3 class="g-dialog-title mb-3">{{ t('update_cancel_confirm_title') }}</h3>
            <p class="text-sm text-ink-soft dark:text-[#A6A6AD] leading-relaxed mb-6">{{ t('update_cancel_confirm_msg') }}</p>
            <div class="g-dialog-actions">
              <button class="g-btn-secondary" :disabled="cancelling" @click="cancelConfirmVisible = false">{{ t('confirm_cancel') }}</button>
              <button class="g-btn-danger" :disabled="cancelling" @click="doCancelUpdate">{{ t('update_cancel_confirm_ok') }}</button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

    <!-- 安装更新二次确认弹窗 -->
    <Teleport to="body">
      <Transition
        enter-active-class="transition duration-200 ease-out"
        enter-from-class="opacity-0"
        enter-to-class="opacity-100"
        leave-active-class="transition duration-150 ease-in"
        leave-from-class="opacity-100"
        leave-to-class="opacity-0"
      >
        <div v-if="installConfirmVisible" class="fixed inset-0 z-[60] flex items-center justify-center p-4 backdrop-blur-sm">
          <div class="g-modal-mask" @click="installConfirmVisible = false"></div>
          <div class="relative w-full max-w-sm bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6">
            <DialogCloseButton :label="t('dialog_close')" @close="installConfirmVisible = false" />
            <h3 class="g-dialog-title mb-3">{{ t('update_install_confirm_title') }}</h3>
            <p class="text-sm text-ink-soft dark:text-[#A6A6AD] leading-relaxed mb-6">{{ installConfirmMsg }}</p>
            <div class="g-dialog-actions">
              <button class="g-btn-secondary" @click="installConfirmVisible = false">{{ t('confirm_cancel') }}</button>
              <button class="g-btn-secondary" @click="doInstall">{{ t('update_install_confirm_ok') }}</button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

    <!-- 删除更新包二次确认弹窗 -->
    <Teleport to="body">
      <Transition
        enter-active-class="transition duration-200 ease-out"
        enter-from-class="opacity-0"
        enter-to-class="opacity-100"
        leave-active-class="transition duration-150 ease-in"
        leave-from-class="opacity-100"
        leave-to-class="opacity-0"
      >
        <div v-if="discardConfirmVisible" class="fixed inset-0 z-[60] flex items-center justify-center p-4 backdrop-blur-sm">
          <div class="g-modal-mask" @click="discardConfirmVisible = false"></div>
          <div class="relative w-full max-w-sm bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6">
            <DialogCloseButton :label="t('dialog_close')" @close="discardConfirmVisible = false" />
            <h3 class="g-dialog-title mb-3">{{ t('update_discard_confirm_title') }}</h3>
            <p class="text-sm text-ink-soft dark:text-[#A6A6AD] leading-relaxed mb-6">{{ t('update_discard_confirm_msg') }}</p>
            <div class="g-dialog-actions">
              <button class="g-btn-secondary" @click="discardConfirmVisible = false">{{ t('confirm_cancel') }}</button>
              <button class="g-btn-danger" @click="doDiscard">{{ t('update_discard_confirm_ok') }}</button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

    <!-- dsh 服务版本列表弹窗（下载 / 删除 / 切换，行内显示下载与包处理进度） -->
    <ServerVersionsDialog
      :visible="serverDialogVisible"
      :status="serverVersions"
      :loading="serverLoading"
      @close="closeServerDialog"
      @refresh="refreshServerVersions(true)"
      @install="onInstallVersion"
      @cancel="onCancelVersion"
      @remove="onRemoveVersion"
      @switch="onSwitchVersion"
    />

    <!-- 插件市场弹窗（安装 / 更新 / 卸载） -->
    <MarketDialog
      :visible="marketDialogVisible"
      :status="marketStatus"
      @close="closeMarketDialog"
      @install="runMarketAction('install')"
      @update="runMarketAction('update')"
      @remove="runMarketAction('remove')"
      @ack="onMarketAck"
    />
  </section>
</template>
