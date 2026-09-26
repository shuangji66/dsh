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
    // 记录保存前的代理状态（开关 + 地址）
    const oldProxyEnabled = config.value.proxyEnabled
    const oldProxyAddr = config.value.proxyAddr
    try {
      // 提交的配置快照，用于判断用户本次是否改动了代理设置
      const submitted = { ...config.value }
      const p = await api.saveSettings(submitted)
      config.value = p.config
      locked.value = p.locked
      // 部分开关（如设置页的即时保存）由调用方自行弹 toast，此处可关闭内置提示
      if (showBuiltInToast) {
        // 若 dsh 正在运行且代理相关设置发生变化，提示重启使配置生效
        const proxyChanged =
          oldProxyEnabled !== submitted.proxyEnabled ||
          oldProxyAddr !== submitted.proxyAddr
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