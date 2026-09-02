<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { usePluginsStore } from '@/stores/plugins'
import { useToastStore } from '@/stores/toast'
import { useI18n } from '@/composables/useI18n'
import { api } from '@/serverapi'
import ConfirmDialog from '@/components/ConfirmDialog.vue'

const store = usePluginsStore()
const { plugins, pluginsLoading } = storeToRefs(store)
const toast = useToastStore()
const { t } = useI18n()

// 卸载/重置插件的确认弹窗状态
const uninstallDialogVisible = ref(false)
const uninstallTarget = ref<string | null>(null)
const resetDialogVisible = ref(false)
const removingPlugin = ref<string | null>(null)
const resetting = ref(false)

function onUninstallClick(name: string) {
  uninstallTarget.value = name
  uninstallDialogVisible.value = true
}

function onResetClick() {
  resetDialogVisible.value = true
}

async function removePlugin(name: string) {
  removingPlugin.value = name
  try {
    const p = await api.removePlugin(name)
    if (p.ok) {
      // 卸载后清除缓存并重新拉取列表
      store.clearPluginsCache()
      await store.loadPlugins(true)
      toast.show(t('plugin_removed', { name: p.removed }), 'success')
    } else {
      toast.show(p.msg || t('plugin_remove_failed'), 'error')
    }
  } catch (e) {
    toast.show((e as Error).message, 'error')
  } finally {
    removingPlugin.value = null
  }
}

async function resetPlugins() {
  resetting.value = true
  try {
    const p = await api.resetPlugins()
    if (p.ok) {
      // 重置请求已返回，后台正在重启 dsh 并触发 node-pty 自动 patch，提示用户
      toast.show(t('plugin_reset_started'), 'success')
    } else {
      toast.show(p.error || t('plugin_reset_failed'), 'error')
    }
  } catch (e) {
    toast.show((e as Error).message, 'error')
  } finally {
    resetting.value = false
    // 重置后清除缓存并重新拉取列表
    store.clearPluginsCache()
    await store.loadPlugins(true)
  }
}

onMounted(() => {
  // 命中缓存则直接使用，避免切换子页面时重复命令拉取
  store.loadPlugins()
})
</script>

<template>
  <div class="py-8 sm:py-12 px-4 sm:px-8 max-w-3xl mx-auto">
    <header class="flex items-center justify-between mb-8">
      <div>
        <p class="text-ink-faint dark:text-[#8A8A92] text-sm font-medium uppercase tracking-widest">{{ t('nav_plugins') }}</p>
      </div>
      <div class="flex items-center gap-2">
        <button class="g-btn-secondary h-8 px-3 text-xs" :disabled="pluginsLoading" @click="store.loadPlugins(true)">
          {{ t('plugin_refresh') }}
        </button>
        <button class="g-btn-danger h-8 px-3 text-xs" :disabled="resetting || pluginsLoading" @click="onResetClick">
          {{ t('plugin_reset') }}
        </button>
      </div>
    </header>

    <section class="g-card g-card-hover p-6">
      <p class="text-sm text-ink-soft dark:text-[#A6A6AD] mb-5">{{ t('plugin_desc') }}</p>

      <div v-if="pluginsLoading" class="flex items-center justify-center py-8 text-ink-faint text-sm">
        <span class="w-4 h-4 rounded-full border-2 border-line border-t-brand animate-spin mr-2"></span>
        {{ t('plugin_loading') }}
      </div>

      <div v-else-if="plugins.length === 0" class="py-8 text-center text-sm text-ink-faint dark:text-[#8A8A92]">
        {{ t('plugin_empty') }}
      </div>

      <ul v-else class="divide-y divide-line dark:divide-[#2A2A32]">
        <li v-for="p in plugins" :key="p.name" class="py-3 flex items-center justify-between gap-3">
          <div class="min-w-0">
            <div class="font-mono text-sm font-medium text-ink dark:text-white truncate">{{ p.name }}</div>
            <div class="text-xs text-ink-faint dark:text-[#8A8A92]">{{ p.version }}</div>
          </div>
          <button
            class="g-btn-danger h-8 px-3 text-xs flex-shrink-0"
            :disabled="removingPlugin === p.name || resetting"
            @click="onUninstallClick(p.name)"
          >
            {{ removingPlugin === p.name ? t('plugin_removing') : t('plugin_uninstall') }}
          </button>
        </li>
      </ul>
    </section>

    <!-- 卸载插件确认弹窗 -->
    <ConfirmDialog
      v-model:visible="uninstallDialogVisible"
      :title="t('confirm_plugin_uninstall_title')"
      :message="t('confirm_plugin_uninstall_msg', { name: uninstallTarget || '' })"
      :confirm-text="t('confirm_ok')"
      :cancel-text="t('confirm_cancel')"
      danger
      @confirm="uninstallTarget && removePlugin(uninstallTarget)"
    />

    <!-- 重置全部插件确认弹窗 -->
    <ConfirmDialog
      v-model:visible="resetDialogVisible"
      :title="t('confirm_plugin_reset_title')"
      :message="t('confirm_plugin_reset_msg')"
      :confirm-text="t('confirm_ok')"
      :cancel-text="t('confirm_cancel')"
      danger
      @confirm="resetPlugins()"
    />
  </div>
</template>