// usePluginInstall —— 插件页「安装插件」的前端侧状态（模块级单例）。
//
// 为什么状态放在模块级而不是 PluginsView 里：安装是**后端异步任务**（pnpm 可能要跑
// 几十秒到几分钟，见 backend/plugin_install.go），用户完全可能在安装期间切到别的子页面
// —— 那时 PluginsView 会被卸载。状态与轮询跟着模块走，切回来仍是同一份进度，也不会
// 因为「重新挂载 + 再次查询状态」而起出第二条轮询（两条链会把同一次安装的结果应用两遍）。
//
// 分工：
//   - 这里：提交规格、轮询、进度/错误文案、结果提示（toast）与「待视图消费的结果」；
//   - 视图：弹窗可见性、消费结果里的 `restart`（弹「重启生效」提示条）并重拉列表。
import { ref } from 'vue'
import { api, type PluginInstallStatus } from '@/serverapi'
import { useToastStore } from '@/stores/toast'
import { uiErrText, uiText, useI18n } from '@/composables/useI18n'

// 同一时刻只允许一个安装任务（后端同样是这个约束）。
const installing = ref(false)
// 进度行（后端 message 本地化后的文本）
const progress = ref('')
// 失败文案（留在弹窗里；用户改完规格可直接重试）
const error = ref('')
// 已落定、还没被视图消费的成功结果。seq 单调递增，视图消费一次即记下序号，
// 避免「切换页面后重新挂载」把同一次安装的列表刷新/提示条再做一遍。
const result = ref<{ seq: number; status: PluginInstallStatus } | null>(null)

let pollSeq = 0
let resultSeq = 0
let consumedSeq = 0

const sleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms))

// waitInstallDone 轮询后台任务直到落定；返回 null 表示这一轮已被更新的一轮取代。
// 连续 5 次请求失败才判失败 —— 控制台重启/短暂断网不该把一次还在跑的安装判死。
async function waitInstallDone(seq: number): Promise<PluginInstallStatus | null> {
  let failures = 0
  const { t } = useI18n()
  for (;;) {
    await sleep(1200)
    if (seq !== pollSeq) return null
    try {
      const st = await api.pluginInstallStatus()
      failures = 0
      if (seq !== pollSeq) return null
      if (st.message || st.messageRef) {
        progress.value = uiText(st.message, st.messageRef, t('plugin_install_running'))
      }
      if (!st.running) return st
    } catch (e) {
      if (++failures >= 5) throw e
    }
  }
}

// handleResult 把落定状态转成提示与「待消费结果」：失败只弹错误（弹窗里也会显示），
// 成功弹一条结果提示，并把 restart 留给视图决定「重启生效」提示条。
function handleResult(st: PluginInstallStatus) {
  const { t } = useI18n()
  const toast = useToastStore()
  if (st.phase === 'failed' || st.error) {
    const msg = uiText(st.error, st.errorRef, t('plugin_install_failed'))
    error.value = msg
    toast.show(msg, 'error', 6000)
    return
  }
  const name = st.name || ''
  if (st.restart && name) {
    // 「重启生效」提示条由视图设置（新装的 bundle 要让 dsh 重新装配）
    toast.show(t('plugin_install_done', { name }), 'success')
  } else if (st.restart) {
    toast.show(t('plugin_install_done_restart'), 'success')
  } else if (name) {
    // 普通依赖（不是 bundle）：装完即可，不需要重启也不需要刷新页面
    toast.show(t('plugin_install_done', { name }), 'success')
  } else {
    toast.show(t('plugin_install_done_generic'), 'success')
  }
  result.value = { seq: ++resultSeq, status: st }
}

async function track(seq: number): Promise<PluginInstallStatus | null> {
  const { t } = useI18n()
  const toast = useToastStore()
  try {
    const st = await waitInstallDone(seq)
    if (st) handleResult(st)
    return st
  } catch (e) {
    const msg = uiErrText(e, t('plugin_install_failed'))
    error.value = msg
    toast.show(msg, 'error', 6000)
    return null
  } finally {
    if (seq === pollSeq) installing.value = false
  }
}

// start 提交规格并开始跟踪。校验失败/忙拒绝（400/409）留在弹窗里显示原因，输入不丢。
// 返回落定状态（失败/被取代时为 null）—— 卡片上的「安装（升级）」按钮据此决定是否清掉
// 「有新版本」的标记。
async function start(spec: string): Promise<PluginInstallStatus | null> {
  const seq = ++pollSeq
  const { t } = useI18n()
  const toast = useToastStore()
  error.value = ''
  progress.value = t('plugin_install_running')
  installing.value = true
  try {
    await api.installPlugin(spec)
  } catch (e) {
    if (seq === pollSeq) installing.value = false
    error.value = uiErrText(e, t('plugin_install_failed'))
    toast.show(error.value, 'error', 6000)
    return null
  }
  return await track(seq)
}

// resume 在页面打开/重新挂载时接上一次还在跑的后台安装；返回是否真的在跑
// （视图据此决定要不要直接打开进度弹窗）。
async function resume(): Promise<boolean> {
  const { t } = useI18n()
  try {
    const st = await api.pluginInstallStatus()
    if (!st.running) return false
    const seq = ++pollSeq
    installing.value = true
    progress.value = uiText(st.message, st.messageRef, t('plugin_install_running'))
    void track(seq)
    return true
  } catch {
    // 查不到状态（旧后端 / 控制台正在重启）：当作没有任务，不打扰用户
    return false
  }
}

// pendingResult 返回还没被消费的成功结果（视图在挂载时与结果变化时都查一次）。
function pendingResult(): { seq: number; status: PluginInstallStatus } | null {
  const r = result.value
  return r && r.seq > consumedSeq ? r : null
}

function consumeResult(seq: number) {
  if (seq > consumedSeq) consumedSeq = seq
}

function clearError() {
  error.value = ''
}

export function usePluginInstall() {
  return {
    installing,
    progress,
    error,
    result,
    start,
    resume,
    pendingResult,
    consumeResult,
    clearError
  }
}
