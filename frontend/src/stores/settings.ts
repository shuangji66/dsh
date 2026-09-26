import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api, type AppConfig, type DshStatus, type RuntimeInfo, type SettingsPayload } from '@/serverapi'
import { useToastStore } from '@/stores/toast'
import { useI18n } from '@/composables/useI18n'

// 手动设置的 node 堆内存上限阈值（MB）。MEM_LIMIT_MIN_MB 与后端 config.go 的
// minDshMemLimitMB 保持一致（低于它后端也拒绝保存）；MEM_LIMIT_WARN_MB 只用于前端
// 黄字提醒，不进后端校验。
export const MEM_LIMIT_MIN_MB = 500
export const MEM_LIMIT_WARN_MB = 800

export const useSettingsStore = defineStore('settings', () => {
  const config = ref<AppConfig>({
    dshPort: 13080,
    proxyPort: 3079,
    proxyEnabled: false,
    proxyUpdate: false,
    proxyAddr: 'http://127.0.0.1:7890',
    authEnabled: true,
    password: '',
    authTTLHours: 4,
    // 0 = 未设置：真实值由后端按当前 node 自身的堆上限补齐（runtime.nodeHeapLimitMB），
    // 这里不再预置一个写死的 2048。load() 后即被后端下发的配置覆盖。
    dshMemLimit: 0,
    dshMemAuto: true,
    nodeVersion: 'node24',
    homeDir: '',
    accessUrls: [],
    browserCompat: false
  })
  const runtime = ref<RuntimeInfo | null>(null)
  const status = ref<DshStatus | null>(null)
  const locked = ref(false)
  const loading = ref(false)

  // 「上次保存生效」的代理设置快照（load()/save() 成功后更新）。
  // 为什么需要单独记一份：save() 里拿本地 config 与同一 tick 的提交快照比较时，两者取自
  // 同一个对象、必然相等，「代理设置已变更 → 请重启」的提示（saved_proxy_restart）恒不出现。
  // 只有与「已保存生效的值」比较才能判出本次保存是否真的改了代理设置。
  const appliedProxy = ref<{ enabled: boolean; addr: string } | null>(null)

  function markApplied(cfg: AppConfig) {
    appliedProxy.value = { enabled: cfg.proxyEnabled, addr: cfg.proxyAddr }
  }

  const toast = useToastStore()
  const { t } = useI18n()

  // 手动设置的 node 堆内存上限（MB）风险等级：
  //   'danger' —— 低于 MEM_LIMIT_MIN_MB：红字提示并阻止保存（save() 直接返回 false，
  //               后端 handleSaveSettings 有同阈值校验兜底）；
  //   'warn'   —— 低于 MEM_LIMIT_WARN_MB：黄字提示，**不**阻止保存；
  //   null     —— 正常值，或「自动设置」打开（此时输入框禁用，上限由系统 node 决定）。
  // 0/空值不算「低」：后端会按当前 node 自身的堆上限补齐（见 defaultDshMemLimit）。
  const memLimitLevel = computed<'danger' | 'warn' | null>(() => {
    if (config.value.dshMemAuto) return null
    const v = config.value.dshMemLimit
    if (v <= 0) return null
    if (v < MEM_LIMIT_MIN_MB) return 'danger'
    if (v < MEM_LIMIT_WARN_MB) return 'warn'
    return null
  })

  async function load() {
    loading.value = true
    try {
      const p = await api.settings()
      config.value = p.config
      runtime.value = p.runtime
      status.value = p.status
      locked.value = p.locked
      // 后端下发的就是当前生效的配置，作为「已保存」基准
      markApplied(p.config)
    } catch (e) {
      toast.show((e as Error).message, 'error')
    } finally {
      loading.value = false
    }
  }

  async function save(showBuiltInToast = true): Promise<boolean> {
    // 堆内存上限过低时整体拒绝保存：配置是整份一起提交的，顺手改别的开关（即时保存）
    // 也会把过低的数值一起写下去。后端 handleSaveSettings 有同阈值校验兜底。
    if (memLimitLevel.value === 'danger') {
      toast.show(t('settings_dsh_mem_too_low'), 'error')
      return false
    }
    loading.value = true
    // 本次要提交的配置快照（提交后 config.value 会被后端下发值覆盖，先留一份）
    const submitted = { ...config.value }
    try {
      const p = await api.saveSettings(submitted)
      config.value = p.config
      locked.value = p.locked
      // 部分开关（如设置页的即时保存）由调用方自行弹 toast，此处可关闭内置提示
      if (showBuiltInToast) {
        // 与「上次保存生效」的代理设置比较：dsh 只在启动时读取代理环境变量，
        // 保存后若代理开关/地址变了，必须提示重启才生效。
        // 还没有基准（load() 从未成功过）时不提示 —— 判不出变化时宁可不说，
        // 也不要凭空弹「代理已变更」。
        const prev = appliedProxy.value
        const proxyChanged =
          !!prev &&
          (prev.enabled !== submitted.proxyEnabled || prev.addr !== submitted.proxyAddr)
        if (status.value?.running && proxyChanged) {
          toast.show(t('saved_proxy_restart'), 'info', 5000)
        } else {
          toast.show(t('saved'), 'success')
        }
      }
      await load()
      return true
    } catch (e) {
      toast.show((e as Error).message, 'error')
      return false
    } finally {
      loading.value = false
    }
  }

  async function startDsh() {
    try {
      status.value = await api.dshStart()
      locked.value = status.value.running
      toast.show(t('dsh_started'), 'success')
      await load()
    } catch (e) {
      toast.show((e as Error).message, 'error')
    }
  }

  async function stopDsh() {
    try {
      status.value = await api.dshStop()
      locked.value = status.value.running
      toast.show(t('dsh_stopped'), 'success')
      await load()
    } catch (e) {
      toast.show((e as Error).message, 'error')
    }
  }

  async function restartDsh() {
    try {
      loading.value = true
      status.value = await api.dshRestart()
      locked.value = status.value.running
      toast.show(t('dsh_restarted'), 'success')
      await load()
    } catch (e) {
      toast.show((e as Error).message, 'error')
    } finally {
      loading.value = false
    }
  }

  return { config, runtime, status, locked, loading, memLimitLevel, load, save, startDsh, stopDsh, restartDsh }
})