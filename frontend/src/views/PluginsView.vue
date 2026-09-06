<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { usePluginsStore } from '@/stores/plugins'
import { useSettingsStore } from '@/stores/settings'
import { useToastStore } from '@/stores/toast'
import { useI18n } from '@/composables/useI18n'
import { api } from '@/serverapi'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import MarqueeText from '@/components/MarqueeText.vue'

const store = usePluginsStore()
const { plugins, pluginsLoading } = storeToRefs(store)
const settingsStore = useSettingsStore()
const toast = useToastStore()
const { t } = useI18n()

// 卸载/重置插件的确认弹窗状态
const uninstallDialogVisible = ref(false)
const uninstallTarget = ref<string | null>(null)
const resetDialogVisible = ref(false)
const removingPlugin = ref<string | null>(null)
const resetting = ref(false)
// 正在切换启停状态的插件名
const togglingPlugin = ref<string | null>(null)
// 需重启 dsh 才能生效的插件（列表内联提示条 + 重启生效按钮）
const needsRestartPlugin = ref<string | null>(null)
const restarting = ref(false)
const restartDialogVisible = ref(false)
const restartTarget = ref<string | null>(null)

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

// 切换插件启停状态（通过编辑 cordis.patch.yml，机制学自 dsh-market）。
// 后端已用插件在补丁层的正确行 id 写入，并同步 state.json 的 disabled 数组
// （包名）。成功后再依据是否需要重启决定提示方式：
//   - 需要重启（客户端插件/带原生依赖）：不弹 toast，改在列表内联提示 + 重启生效按钮
//   - 仅需刷新：弹 toast 提示刷新 dsh 页面
async function togglePlugin(p: { name: string; disabled?: boolean }) {
  const enable = !!p.disabled
  togglingPlugin.value = p.name
  try {
    const r = await store.togglePlugin(p.name, enable)
    if (r.ok) {
      if (r.restart) {
        // 需要重启：收起之前的内联提示，显示本插件的重启生效条
        needsRestartPlugin.value = p.name
      } else {
        toast.show(
          t('plugin_toggle_refresh', {
            action: t(enable ? 'plugin_enable' : 'plugin_disable'),
            name: p.name
          }),
          'info',
          4000
        )
      }
    } else {
      toast.show(r.msg || t('plugin_toggle_failed'), 'error')
    }
  } catch (e) {
    toast.show((e as Error).message, 'error')
  } finally {
    togglingPlugin.value = null
  }
}

// 点击“重启生效”：与概览页重启按钮一致，先弹二次确认再调用 settings store 的重启逻辑。
function confirmRestart() {
  restartTarget.value = needsRestartPlugin.value
  restartDialogVisible.value = true
}

async function executeRestart() {
  restarting.value = true
  const name = restartTarget.value
  restartTarget.value = null
  restartDialogVisible.value = false
  try {
    await settingsStore.restartDsh()
    // 重启完成后清掉内联提示（列表刷新后该插件应已生效）
    needsRestartPlugin.value = null
  } catch (e) {
    toast.show((e as Error).message, 'error')
  } finally {
    restarting.value = false
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

      <!-- 启用需重启的插件后的内联提示条（右对齐的重启生效按钮） -->
      <div
        v-if="needsRestartPlugin"
        class="flex items-center justify-between gap-3 mb-4 px-3 py-2.5 rounded-lg border border-[#F59E0B]/40 bg-[#F59E0B]/10"
      >
        <span class="text-xs text-ink-soft dark:text-[#A6A6AD]">
          {{ t('plugin_restart_prompt', { name: needsRestartPlugin }) }}
        </span>
        <button
          class="g-btn-warning h-8 px-3 text-xs flex-shrink-0"
          :disabled="restarting || resetting"
          @click="confirmRestart"
        >
          {{ restarting ? t('plugin_restarting') : t('plugin_restart_apply') }}
        </button>
      </div>

      <div v-if="pluginsLoading" class="flex items-center justify-center py-8 text-ink-faint text-sm">
        <span class="w-4 h-4 rounded-full border-2 border-line border-t-brand animate-spin mr-2"></span>
        {{ t('plugin_loading') }}
      </div>

      <div v-else-if="plugins.length === 0" class="py-8 text-center text-sm text-ink-faint dark:text-[#8A8A92]">
        {{ t('plugin_empty') }}
      </div>

      <ul v-else class="divide-y divide-line dark:divide-[#2A2A32]">
        <li v-for="p in plugins" :key="p.name" class="py-3 flex items-center justify-between gap-3">
          <div class="min-w-0 flex-1">
            <!-- 过长的插件名自动向左循环滚动显示 -->
            <div class="font-mono text-sm font-medium text-ink dark:text-white">
              <MarqueeText :text="p.name" />
            </div>
            <div class="text-xs text-ink-faint dark:text-[#8A8A92]">{{ p.version }}</div>
          </div>
          <div class="flex items-center gap-3 flex-shrink-0">
            <!-- 启停开关（disabled 状态来自 cordis.patch.yml），仅以开关状态表示 -->
            <button
              type="button"
              class="inline-flex items-center select-none"
              :disabled="togglingPlugin === p.name || resetting"
              :aria-label="p.disabled ? t('plugin_disable') : t('plugin_enable')"
              :title="p.disabled ? t('plugin_disable') : t('plugin_enable')"
              @click="togglePlugin(p)"
            >
              <span
                class="inline-block w-9 h-5 rounded-full p-0.5 transition-colors duration-200"
                :class="p.disabled ? 'bg-[#3A3A44]' : 'bg-brand'"
              >
                <span
                  class="block w-4 h-4 rounded-full bg-white shadow transition-transform duration-200"
                  :class="p.disabled ? 'translate-x-0' : 'translate-x-4'"
                ></span>
              </span>
            </button>
            <button
              class="g-btn-danger h-8 px-3 text-xs"
              :disabled="removingPlugin === p.name || resetting"
              @click="onUninstallClick(p.name)"
            >
              {{ removingPlugin === p.name ? t('plugin_removing') : t('plugin_uninstall') }}
            </button>
          </div>
        </li>
      </ul>
    </section>

    <!-- 重启生效二次确认弹窗（与概览页重启逻辑一致） -->
    <ConfirmDialog
      v-model:visible="restartDialogVisible"
      :title="t('confirm_plugin_restart_title')"
      :message="t('confirm_plugin_restart_msg', { name: restartTarget || '' })"
      :confirm-text="t('confirm_ok')"
      :cancel-text="t('confirm_cancel')"
      @confirm="executeRestart()"
    />

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