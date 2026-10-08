import { computed, ref } from 'vue'
import { api, type DshBackupStatus } from '@/serverapi'
import { useToastStore } from '@/stores/toast'
import { useI18n } from '@/composables/useI18n'

// useDshBackup —— dsh 数据备份的观察端。
//
// 备份在**服务端** goroutine 里跑（见 backend/update.go 的 BackupDshData），前端弹窗
// 只是观察者。因此状态放在**模块级共享**（与 useKeypadPage 同一模式），不能放在弹窗
// 组件里：
//   - 关掉弹窗（组件卸载）只是停止渲染，备份照跑；
//   - 重新打开弹窗时调用 syncForDialog()：先拉一次后端快照再续上轮询，进度直接追平；
//   - 页面刷新/切走再回来（onMounted 的 sync()）同样能重新接上 —— 后端状态是权威来源。
//
// 轮询只在「有一次备份正在跑」期间进行，状态落定即停，不留长期计时器。

// 轮询间隔：备份是本地打包（无网络往返），亚秒级刷新对进度条足够，也不至于把请求打满。
const POLL_INTERVAL_MS = 800
// 连续失败上限：后端重启/离线时别把计时器留成永久轮询。
const MAX_POLL_FAILURES = 10

const { t } = useI18n()

// toast 是 Pinia store，只能在 pinia 安装之后实例化 —— 而本模块在应用启动早期就被
// import（视图的静态依赖），因此延迟到第一次真正要提示时再取。
let toastStore: ReturnType<typeof useToastStore> | null = null
function toast(): ReturnType<typeof useToastStore> {
  return (toastStore ??= useToastStore())
}

const status = ref<DshBackupStatus | null>(null)
const starting = ref(false)
const cancelling = ref(false)
// lastError 是「启动请求失败」的原因（与 status.error 不同：那是打包过程中的失败）。
const lastError = ref('')

const running = computed(() => status.value?.running === true)

let timer: ReturnType<typeof setInterval> | null = null
let polling = false
let failures = 0
// 已经给过提示的那次运行（后端快照的 seq），见 adopt。
let announcedRun = 0

function stopPoll() {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
}

function startPoll() {
  if (timer) return
  failures = 0
  void poll()
  timer = setInterval(() => void poll(), POLL_INTERVAL_MS)
}

async function poll() {
  if (polling) return // 上一次还没回来（后端卡住时别堆积请求）
  polling = true
  try {
    const res = await api.dshBackupStatus()
    failures = 0
    adopt(res.status, { announce: true })
  } catch {
    // 单次失败忽略、下一拍继续；连续失败到上限就放弃（弹窗仍显示最后一次进度）。
    if (++failures >= MAX_POLL_FAILURES) stopPoll()
  } finally {
    polling = false
  }
}

// adopt 把一份后端快照合并进本地状态，并在「一次备份刚刚结束」时给出唯一一次提示。
//
// 只有真正观察到 running → 结束 的转换才提示：启动时的 sync() 可能拿到上一轮遗留的
// done 快照，那不该弹 toast（刚打开页面就被告知「备份成功」很奇怪）。announce 专供
// 「用户刚点了备份/取消」这条路径 —— 那次的结果无论多快都必须告诉他。
//
// announcedRun 记下已经提示过的那一次运行（后端快照的 seq）：取消请求与轮询可能先后
// 把同一份 done 快照送回来，没有它就同一个结果弹两次 toast。
function adopt(st: DshBackupStatus, opts?: { announce?: boolean }) {
  const wasRunning = running.value
  status.value = st
  if (st.running) {
    startPoll()
    return
  }
  stopPoll()
  cancelling.value = false
  if (!wasRunning && !opts?.announce) return
  if (announcedRun === st.seq) return
  announcedRun = st.seq
  if (st.cancelled) {
    toast().show(t('backup_cancelled'), 'info')
    return
  }
  if (st.ok) {
    toast().show(t('directory_backup_success', { name: st.name || '' }), 'success')
    return
  }
  toast().show(st.error || t('directory_backup_failed'), 'error')
}

// sync 拉一次后端快照：running 时顺带开始轮询，落定则停止。
async function sync() {
  try {
    const res = await api.dshBackupStatus()
    adopt(res.status)
  } catch {
    /* 后端不可达：保持现状，弹窗里仍可点「开始备份」 */
  }
}

// syncForDialog 供「打开备份弹窗」调用：先追平进度；若没有备份在跑，则清掉上一次的
// 结果，让弹窗回到确认视图（否则重开弹窗会一直显示上一轮的结论）。
async function syncForDialog() {
  await sync()
  if (!running.value) {
    status.value = null
    lastError.value = ''
  }
}

// start 启动一次备份；后端立刻返回，随后由轮询推进进度。
// 返回 false 表示启动失败（原因在 lastError，错误提示已弹出）。
async function start(): Promise<boolean> {
  if (starting.value || running.value) return false
  starting.value = true
  lastError.value = ''
  try {
    const res = await api.dshBackup()
    if (res.status) {
      adopt(res.status, { announce: true })
    } else {
      await sync() // 后端没带快照（理论上不会）：退回拉一次状态
    }
    return true
  } catch (e) {
    // 常见原因是「已有备份任务正在进行」（另一个标签页/设备发起的）：同步一次状态，
    // 若确实有备份在跑就顺手接上，而不是把它当成失败。
    await sync()
    if (running.value) return true
    lastError.value = (e as Error).message || t('directory_backup_failed')
    return false
  } finally {
    starting.value = false
  }
}

// cancel 请求取消当前备份：后端中止打包并删除不完整的备份文件。
async function cancel() {
  if (!running.value || cancelling.value) return
  cancelling.value = true
  try {
    const res = await api.dshBackupCancel()
    adopt(res.status, { announce: true })
  } catch (e) {
    cancelling.value = false
    toast().show((e as Error).message || t('backup_cancel_failed'), 'error')
  }
}

// clearResult 关闭弹窗时清掉结果态（下次打开回到确认视图；备份本身不受影响）。
function clearResult() {
  if (!running.value) status.value = null
  lastError.value = ''
}

export function useDshBackup() {
  return {
    // state
    status,
    running,
    starting,
    cancelling,
    lastError,
    // actions
    start,
    cancel,
    sync,
    syncForDialog,
    clearResult,
  }
}
