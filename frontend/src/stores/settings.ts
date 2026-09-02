import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api, type AppConfig, type DshStatus, type RuntimeInfo, type SettingsPayload } from '@/serverapi'
import { useToastStore } from '@/stores/toast'
import { useI18n } from '@/composables/useI18n'

export const useSettingsStore = defineStore('settings', () => {
  const config = ref<AppConfig>({
    dshPort: 13080,
    proxyEnabled: false,
    proxyAddr: 'http://127.0.0.1:7890',
    authEnabled: true,
    password: '',
    authTTLHours: 2,
    dshMemLimit: 2048,
    dshMemAuto: true,
    homeDir: ''
  })
  const runtime = ref<RuntimeInfo | null>(null)
  const status = ref<DshStatus | null>(null)
  const locked = ref(false)
  const loading = ref(false)

  // dsh 版本号（来自后端 `dsh -V`）。模块级缓存：切换子页面不重复命令拉取；
  // 打开控制台/手动刷新（整页重新加载）会重建 store，缓存随之清空并重新拉取。
  const dshVersion = ref<string>('')
  let dshVersionLoaded = false

  const toast = useToastStore()
  const { t } = useI18n()

  async function loadDshVersion(force = false): Promise<string> {
    if (!force && dshVersionLoaded) return dshVersion.value
    try {
      const p = await api.dshVersion()
      dshVersion.value = (p.version || '').trim()
      dshVersionLoaded = true
    } catch {
      // 版本号仅用于展示，获取失败时静默忽略，不弹错误提示
      dshVersionLoaded = true
    }
    return dshVersion.value
  }

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

  async function save() {
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
      // 若 dsh 正在运行且代理相关设置发生变化，提示重启使配置生效
      const proxyChanged =
        oldProxyEnabled !== submitted.proxyEnabled ||
        oldProxyAddr !== submitted.proxyAddr
      if (status.value?.running && proxyChanged) {
        toast.show(t('saved_proxy_restart'), 'info', 5000)
      } else {
        toast.show(t('saved'), 'success')
      }
      await load()
    } catch (e) {
      toast.show((e as Error).message, 'error')
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

  return { config, runtime, status, locked, loading, dshVersion, load, loadDshVersion, save, startDsh, stopDsh, restartDsh }
})