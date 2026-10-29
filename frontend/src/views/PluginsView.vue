<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, type ComponentPublicInstance } from 'vue'
import { storeToRefs } from 'pinia'
import { usePluginsStore } from '@/stores/plugins'
import { useSettingsStore } from '@/stores/settings'
import { useToastStore } from '@/stores/toast'
import { uiErrText, uiText, useI18n } from '@/composables/useI18n'
import { api } from '@/serverapi'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import MarqueeText from '@/components/MarqueeText.vue'
import PageHeader from '@/components/PageHeader.vue'
import { icons } from '@/utils/icons'
import { copyText } from '@/utils/clipboard'
import { captureScroll, restoreScroll } from '@/utils/scroll'

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

// 顶栏的实测高度：重启提示条要贴死在顶栏下沿（见模板的 sticky 提示条）。
// 顶栏高度不是常数 —— 断点会改顶部留白（pt-3 / sm:pt-4），换语言或更窄的屏幕还可能让
// 标题与按钮换行。写死一个像素值迟早会压住标题栏，所以直接量。
const headerRef = ref<ComponentPublicInstance | null>(null)
const headerHeight = ref(0)
let headerObserver: ResizeObserver | null = null

// 插件名点击即复制：只复制名字（版本号既不可选中也不做复制入口）。
// 复制走 utils/clipboard 的兜底实现（http 反代访问时没有 Clipboard API）。
function copyPluginName(name: string) {
  copyText(name)
  toast.show(t('plugin_name_copied'), 'success')
}

function onUninstallClick(name: string) {
  uninstallTarget.value = name
  uninstallDialogVisible.value = true
}

function onResetClick() {
  resetDialogVisible.value = true
}

// 自动刷新（启停 / 卸载 / 重置后的重拉）前后保持滚动位置：重拉前记下，列表重绘后还原，
// 用户回到刚才看的那一片。手动刷新按钮刻意不走这里 —— 那一次仍回到列表最上方。
//
// 开头那个 nextTick 不能省：卸载/重置是在确认弹窗关闭后才发起的，而弹窗的滚动锁
// （useBodyScrollLock）要到下一个 flush 才释放并把文档滚回原位，抢在它前面记录只会
// 记到被锁期间的 0。
async function reloadKeepingScroll() {
  await nextTick()
  const snapshot = captureScroll()
  store.clearPluginsCache()
  await store.loadPlugins(true)
  // 等新列表渲染完再还原：高度还没长回来时写回的滚动位置会被浏览器夹成 0
  await nextTick()
  restoreScroll(snapshot)
}

async function removePlugin(name: string) {
  removingPlugin.value = name
  try {
    const p = await api.removePlugin(name)
    if (p.ok) {
      // 卸载后清除缓存并重新拉取列表（保持滚动位置）
      await reloadKeepingScroll()
      toast.show(t('plugin_removed', { name: p.removed }), 'success')
    } else {
      toast.show(p.msg || t('plugin_remove_failed'), 'error')
    }
  } catch (e) {
    toast.show(uiErrText(e, t('common_op_failed')), 'error')
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
      toast.show(uiText(p.error, p, t('plugin_reset_failed')), 'error')
    }
  } catch (e) {
    toast.show(uiErrText(e, t('common_op_failed')), 'error')
  } finally {
    resetting.value = false
    // 重置后清除缓存并重新拉取列表（保持滚动位置）
    await reloadKeepingScroll()
  }
}

// 切换插件启停状态（按官方 dsh 的包级开关增删 dsh.profile.bundles，依赖保留）。
// 成功后再依据是否需要重启决定提示方式：
//   - 需要重启（客户端插件/带原生依赖）：不弹 toast，改在列表内联提示 + 重启生效按钮
//   - 仅需刷新：弹 toast 提示刷新 dsh 页面
async function togglePlugin(p: { name: string; disabled?: boolean }) {
  const enable = !!p.disabled
  togglingPlugin.value = p.name
  // 启停成功后 store 会重拉列表：位置在重拉前记下（此时列表还没被加载态替换）
  const snapshot = captureScroll()
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
      // 列表与（可能的）内联提示条都渲染完再还原，否则高度还差一截，位置会被夹小
      await nextTick()
      restoreScroll(snapshot)
    } else {
      toast.show(r.msg || t('plugin_toggle_failed'), 'error')
    }
  } catch (e) {
    toast.show(uiErrText(e, t('common_op_failed')), 'error')
  } finally {
    togglingPlugin.value = null
  }
}

// 点击“重启生效”：与概览页重启按钮一致，先弹二次确认再调用 settings store 的重启逻辑。
//
// 「重启生效」就是停 dsh 的入口：插件的安装/卸载在 dsh 进程内持有 profile 写锁，停 dsh 会
// 把持有者一起带走 —— 安装白做，还留下陈旧锁（之后插件列表与安装会白等 120 秒）。
// 因此弹窗打开时按需查一次「有没有插件操作在跑」（GET /api/dsh/busy），把代价说清楚；
// 但**刻意不硬挡**：点它是用户的即时意图。查询不阻塞弹窗（先显示，答案到了再补提示），
// 查不到就不显示提示。
type LifecycleBusy = { busy: boolean; source?: 'market' | 'console'; detail?: string }
const restartBusy = ref<LifecycleBusy | null>(null)
// 请求代号：用户可能关掉弹窗又马上重开，只接受「当前这次请求」的结果，避免过期结果写回
let restartBusyReq = 0

function confirmRestart() {
  restartTarget.value = needsRestartPlugin.value
  restartDialogVisible.value = true
  restartBusy.value = null
  const req = ++restartBusyReq
  api
    .dshBusy()
    .then((r) => {
      // 期间用户可能已确认/关闭弹窗：丢弃过期结果
      if (req === restartBusyReq && restartDialogVisible.value) restartBusy.value = r
    })
    .catch(() => {
      // dsh 不可达等查询失败：不显示提示，也不影响弹窗继续操作
      if (req === restartBusyReq) restartBusy.value = null
    })
}

// 忙碌提示文案：与概览页同一套 i18n 键（按来源区分市场安装 / 控制台插件命令）
const restartBusyText = computed(() => {
  const b = restartBusy.value
  if (!b?.busy) return ''
  const detail = b.detail?.trim() || ''
  return b.source === 'console'
    ? t('lifecycle_busy_console', { detail: detail || 'dsh plugin …' })
    : t('lifecycle_busy_market', { detail: detail || t('lifecycle_busy_unknown_target') })
})

async function executeRestart() {
  restarting.value = true
  restartTarget.value = null
  restartDialogVisible.value = false
  try {
    await settingsStore.restartDsh()
    // 重启完成后清掉内联提示（列表刷新后该插件应已生效）
    needsRestartPlugin.value = null
  } catch (e) {
    toast.show(uiErrText(e, t('common_op_failed')), 'error')
  } finally {
    restarting.value = false
  }
}

onMounted(() => {
  // 命中缓存则直接使用，避免切换子页面时重复命令拉取
  store.loadPlugins()

  // 量顶栏高度并跟着它变（视口宽度、语言、按钮换行都会改高度）。
  // 组件根节点就是那层 sticky 遮罩，量它的高度 = 顶栏下沿到视口顶的距离（静止与固定时相同）。
  const el = headerRef.value?.$el as HTMLElement | undefined
  if (!el) return
  headerHeight.value = el.offsetHeight
  if (typeof ResizeObserver === 'undefined') return
  headerObserver = new ResizeObserver(() => {
    headerHeight.value = el.offsetHeight
  })
  headerObserver.observe(el)
})

onBeforeUnmount(() => {
  headerObserver?.disconnect()
  headerObserver = null
})
</script>

<template>
  <div class="pt-3 sm:pt-4 pb-8 sm:pb-12 px-4 sm:px-8 max-w-6xl mx-auto">
    <!-- 页头（图标 + 标题 + 刷新/重置按钮）；ref 用于实测高度，给下面的 sticky 提示条定位 -->
    <PageHeader ref="headerRef" sticky class="mb-3" :title="t('nav_plugins')" :icon="icons.plugin">
      <button class="g-btn-secondary h-8 px-3 text-xs" :disabled="pluginsLoading" @click="store.loadPlugins(true)">
        {{ t('plugin_refresh') }}
      </button>
      <button class="g-btn-danger h-8 px-3 text-xs" :disabled="resetting || pluginsLoading" @click="onResetClick">
        {{ t('plugin_reset') }}
      </button>
    </PageHeader>

    <!-- 启用需重启的插件后的提示条（右对齐的重启生效按钮）。
         它**贴在顶栏下沿不动**、浮在列表之上：自动刷新已经会保住滚动位置，用户往下翻着
         操作时这条「重启生效」不能跟着滚出视口（否则要再滚回顶部才能点）。
         实现要点：
           1. 自己是 sticky，`top` = 实测的顶栏高度（不写死像素，理由见 script 里 headerHeight）；
           2. `-mt-3 pt-3` 与页头的 `mb-3` 抵消：静止位与固定位重合，不会先跳 12px 再粘住；
              `pt-3` 那段就是固定后标题栏与提示条之间的间隙，由这层的页面底色遮住后面滚过的卡片；
           3. 底色必须不透明（页面底色，同 PageHeader 的遮罩层）：提示条本身是半透明的琥珀色，
              只靠它自己的背景，卡片会从底下透出来；下面那条 `pb-3` 同理，给列表留出同色间隙；
           4. z-10：高于列表（`.g-fade-in` 会自建层叠上下文，卡片都在里面），低于顶栏（z-20）、
              弹窗（z-50）与 toast（z-[100]）。 -->
    <div
      v-if="needsRestartPlugin"
      class="sticky z-10 -mt-3 pt-3 pb-3 bg-bg dark:bg-[#0B0B0F]"
      :style="{ top: `${headerHeight}px` }"
    >
      <div
        class="flex items-center justify-between gap-3 px-3 py-2.5 rounded-lg border border-[#F59E0B]/40 bg-[#F59E0B]/10"
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
    </div>

    <!-- 每个插件一张卡片，按容器宽度自适应分栏（.g-card-grid）；
         加载/空态占满整行。 -->
    <div class="g-card-grid g-fade-in">
      <div v-if="pluginsLoading" class="col-span-full flex items-center justify-center py-8 text-ink-faint text-sm">
        <span class="w-4 h-4 rounded-full border-2 border-line border-t-brand animate-spin mr-2"></span>
        {{ t('plugin_loading') }}
      </div>

      <div v-else-if="plugins.length === 0" class="col-span-full py-8 text-center text-sm text-ink-faint dark:text-[#8A8A92]">
        {{ t('plugin_empty') }}
      </div>

      <template v-else>
        <div v-for="p in plugins" :key="p.name" class="g-card g-card-hover p-4 flex flex-col">
          <div class="min-w-0">
            <!-- 插件名：点击即复制（提 issue / 报错时要贴名字）。按钮化而不是可拖选 ——
                 全局默认禁止文本选择（见 style.css），这里连 select-text 也不加，点击的
                 语义就是「复制」，复制成功弹 toast。
                 过长的插件名由 MarqueeText 自动向左循环滚动显示。 -->
            <button
              type="button"
              class="block w-full text-left font-mono text-sm font-medium text-ink dark:text-white hover:text-brand dark:hover:text-brand transition-colors"
              :title="t('plugin_copy_name')"
              :aria-label="t('plugin_copy_name')"
              @click="copyPluginName(p.name)"
            >
              <MarqueeText :text="p.name" />
            </button>
            <!-- 版本号：刻意不可复制、不可点击 -->
            <div class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1.5">{{ p.version }}</div>
          </div>

          <!-- 第二栏：启停开关 + 状态标记（紧邻开关右侧，disabled 状态来自 dsh.profile.bundles）
               + 卸载按钮（ml-auto 推到最右） -->
          <div class="flex flex-wrap items-center gap-3 mt-auto pt-3 border-t border-line dark:border-[#2A2A32]">
            <!-- 只有声明了 dsh.bundle 的包才给启停开关：普通依赖不在 dsh.profile.bundles
                 里，后端会以 err_plugin_not_bundle 拒绝，给开关等于给一个点下去必失败的按钮。 -->
            <template v-if="p.bundle">
              <button
                type="button"
                class="inline-flex items-center select-none"
                :disabled="togglingPlugin === p.name || resetting"
                :aria-label="p.disabled ? t('plugin_disable') : t('plugin_enable')"
                :title="p.disabled ? t('plugin_enable') : t('plugin_disable')"
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
              <span
                class="g-status"
                :class="p.disabled
                  ? 'bg-black/5 text-ink-soft dark:bg-white/10 dark:text-[#A6A6AD]'
                  : 'bg-success/10 text-success-ink'"
              >
                <span class="w-1.5 h-1.5 rounded-full" :class="p.disabled ? 'bg-ink-faint' : 'bg-success'"></span>
                {{ p.disabled ? t('plugin_status_disabled') : t('plugin_status_enabled') }}
              </span>
            </template>
            <span v-else class="g-status bg-black/5 text-ink-soft dark:bg-white/10 dark:text-[#A6A6AD]">
              {{ t('plugin_plain_dependency') }}
            </span>
            <button
              class="g-btn-danger h-8 px-3 text-xs ml-auto"
              :disabled="removingPlugin === p.name || resetting"
              @click="onUninstallClick(p.name)"
            >
              {{ removingPlugin === p.name ? t('plugin_removing') : t('plugin_uninstall') }}
            </button>
          </div>
        </div>
      </template>
    </div>

    <!-- 重启生效二次确认弹窗（与概览页重启逻辑一致：默认插槽里放原确认文案 +
         「有插件操作在跑」时的额外风险提示，弹窗结构与样式约定见 AGENTS 第 3 节） -->
    <ConfirmDialog
      v-model:visible="restartDialogVisible"
      :title="t('confirm_plugin_restart_title')"
      :confirm-text="t('confirm_ok')"
      :cancel-text="t('confirm_cancel')"
      @confirm="executeRestart()"
    >
      <p
        class="text-sm text-ink-soft dark:text-[#A6A6AD] leading-relaxed whitespace-pre-line"
        :class="restartBusyText ? 'mb-3' : 'mb-6'"
      >
        {{ t('confirm_plugin_restart_msg', { name: restartTarget || '' }) }}
      </p>
      <div
        v-if="restartBusyText"
        class="mb-6 rounded-lg px-3 py-2 text-xs leading-relaxed bg-[#F59E0B]/10 dark:bg-[#F59E0B]/15 border border-[#F59E0B]/40 text-[#B45309] dark:text-[#FBBF24]"
      >
        {{ restartBusyText }}
      </div>
    </ConfirmDialog>

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