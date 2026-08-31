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
    dshMemAuto: true
  })
  const runtime = ref<RuntimeInfo | null>(null)
  const status = ref<DshStatus | null>(null)
  const locked = ref(false)
  const loading = ref(false)

  const toast = useToastStore()
  const { t } = useI18n()

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

  return { config, runtime, status, locked, loading, load, save, startDsh, stopDsh, restartDsh }
})