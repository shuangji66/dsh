<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch, type ComponentPublicInstance } from 'vue'
import { storeToRefs } from 'pinia'
import { usePluginsStore } from '@/stores/plugins'
import { useSettingsStore } from '@/stores/settings'
import { useToastStore } from '@/stores/toast'
import { uiErrText, uiText, useI18n, type I18nKey } from '@/composables/useI18n'
import { api, type PluginInfo, type PluginUpdateEntry } from '@/serverapi'
import { countByFilter, filterPlugins, type FilterCounts, type PluginFilter } from '@/utils/pluginFilter'
import { resolveCardUpdates } from '@/utils/pluginUpdates'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import MarqueeText from '@/components/MarqueeText.vue'
import PageHeader from '@/components/PageHeader.vue'
import PluginInstallDialog from '@/components/PluginInstallDialog.vue'
import PluginDetailDialog from '@/components/PluginDetailDialog.vue'
import { icons } from '@/utils/icons'
import { copyText } from '@/utils/clipboard'
import { captureScroll, restoreScroll } from '@/utils/scroll'
import { usePluginInstall } from '@/composables/usePluginInstall'
import { usePluginUpdateChecks } from '@/composables/usePluginUpdateChecks'

const store = usePluginsStore()
const { plugins, pluginsLoading } = storeToRefs(store)
const settingsStore = useSettingsStore()
const toast = useToastStore()
const { t } = useI18n()

// --- 更新状态（手动单卡检查 + 后台定时检测）---
//
// 先声明合并视图：下面的「可更新」筛选与计数要用它。具体动作（请求 / 轮询 / 安装）见
// 文件后面的「检查更新」一节。手动检查的结果存在模块级（见 usePluginUpdateChecks），
// 切子页面回来仍然有效；后台结果由页面轮询后端缓存补齐。
//
// 与官方插件中心一致：只把「比当前版本新、非预发布、未废弃、与当前 dsh 兼容」的版本当作
// 候选（判定在 backend/plugin_update.go）。手动检查按插件、异步；后台结果来自每小时的
// 定时检测（内存缓存，页面轮询）。
// 手动检查的结果存模块级（切子页面不丢），见 usePluginUpdateChecks。
const { checks: updateChecks } = usePluginUpdateChecks()

// 后台定时检测（每小时）的结果：市场/增强插件之外、其它插件在镜像源上有没有新版本。
// 页面只轮询这份**内存缓存**（GET /api/plugins/updates，不联网、不起 `dsh plugin list`），
// 拿到后直接体现在卡片上；手动点单卡检查的结果优先于它。
const backgroundUpdates = ref<Record<string, PluginUpdateEntry>>({})

// cardUpdates 是模板用的合并视图：单卡手动检查 > 后台定时检测。
// 规则（含「手动结论必须仍对得上已装版本，否则回退后台结论」）在 utils/pluginUpdates.ts，
// 那里有单测 —— 页面之外换过版本时，旧的手动结论不能再生效，否则会给出降级箭头。
const cardUpdates = computed(() =>
  resolveCardUpdates(plugins.value, updateChecks.value, backgroundUpdates.value)
)

// --- 顶栏下的分类筛选 + 实时搜索（纯前端过滤已拉取的列表，不再跑 dsh 命令）---
//
// 口径（什么算「已停用」、搜索匹配哪个字段）在 utils/pluginFilter.ts，那里有回归测试；
// 这里只负责状态与渲染。
const pluginFilter = ref<PluginFilter>('all')
const pluginFilters: { value: PluginFilter; label: I18nKey }[] = [
  { value: 'all', label: 'plugin_filter_all' },
  { value: 'enabled', label: 'plugin_status_enabled' },
  { value: 'disabled', label: 'plugin_status_disabled' },
  { value: 'updatable', label: 'plugin_filter_updatable' }
]
const pluginQuery = ref('')

// 「可更新」= 这张卡片当前正挂着「有新版本」提示（手动检查结果优先于后台缓存）。
function isPluginUpdatable(p: PluginInfo): boolean {
  return cardUpdates.value[p.name]?.hasUpdate === true
}

// 计数只在「列表加载结束」的时刻更新：启停 / 卸载 / 重置后的重拉会先清空 store 里的
// plugins 再置 loading（那条 dsh 命令要 1~3 秒）。直接按当前列表算计数，四个按钮和
// 搜索框会在重拉窗口里整个消失再出现；加载中沿用上一次的数量就没有这个闪烁。
// 「可更新」还跟着更新状态变（手动检查 / 后台轮询），所以把 cardUpdates 也作为依赖。
const filterCounts = ref<FilterCounts>({ all: 0, enabled: 0, disabled: 0, updatable: 0 })
watch(
  [plugins, pluginsLoading, cardUpdates],
  ([list, loading, updates]) => {
    if (!loading) filterCounts.value = countByFilter(list, (p) => updates[p.name]?.hasUpdate === true)
  },
  { immediate: true }
)

// 数量为 0 的分类不渲染按钮；当前分类恰好归零（例如刚把最后一个停用插件启用）时回到
// 「全部」—— 否则会留下一个看不见的过滤条件：列表空白，且没有任何按钮是选中态。
const visibleFilters = computed(() => pluginFilters.filter((f) => filterCounts.value[f.value] > 0))
watch(filterCounts, (counts) => {
  if (counts[pluginFilter.value] === 0) pluginFilter.value = 'all'
})

// 搜索框只在插件超过 10 个时出现（10 个及以下一屏能看完，不需要它）；收起时清掉搜索词，
// 否则会留下一个看不见、却仍在过滤列表的条件。
const showSearch = computed(() => filterCounts.value.all > 10)
watch(showSearch, (show) => {
  if (!show) pluginQuery.value = ''
})

const visiblePlugins = computed(() =>
  filterPlugins(plugins.value, pluginFilter.value, pluginQuery.value, isPluginUpdatable)
)

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
      clearUpdateCheck(name)
      clearBackgroundUpdate(name)
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
    updateChecks.value = {}
    backgroundUpdates.value = {}
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
      // 命令跑完才重拉列表：滚动快照由 reloadKeepingScroll 在清缓存**之前**拍。
      // 不能在发命令前拍 —— 那条 `dsh plugin …` 要 1~3 秒，期间用户还在滚，
      // 还原时会被拽回点击时的位置。
      await reloadKeepingScroll()
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
    // restartDsh() 自己会弹错误 toast 并返回 false —— 不要在失败时也把提示条收掉，
    // 那会让「插件还没生效」看起来像已经生效。
    if (await settingsStore.restartDsh()) needsRestartPlugin.value = null
  } finally {
    restarting.value = false
  }
}

// --- 安装插件（顶栏「安装」按钮 + 弹窗）---
//
// 提交规格、轮询后台任务、进度/错误文案与结果提示都在 usePluginInstall 里（模块级单例：
// 安装期间切子页面不丢进度，重新挂载也不会起出第二条轮询）。本页只负责三件事：
// 弹窗可见性、「重启生效」提示条、装完重拉列表（保持滚动位置）。
const {
  installing: installingPlugin,
  progress: installProgress,
  error: installError,
  result: installResult,
  start: startPluginInstall,
  resume: resumeInstallIfRunning,
  pendingResult: pendingInstallResult,
  consumeResult: consumeInstallResult,
  clearError: clearInstallError
} = usePluginInstall()

const installDialogVisible = ref(false)

// 插件详情弹窗：点信息按钮才读文档（懒加载），弹窗内展示 meta.detail 或 README。
// 缓存/请求都在 usePluginDetail 里（模块级），关掉弹窗再打开同一插件不重复读盘。
const detailDialogVisible = ref(false)
const detailTarget = ref('')

function openPluginDetail(name: string) {
  detailTarget.value = name
  detailDialogVisible.value = true
}

function onInstallClick() {
  clearInstallError()
  installDialogVisible.value = true
}

// 消费一次成功结果：需要重启的挂「重启生效」提示条（与启停开关同一条），然后重拉列表。
// 失败结果已由 composable 弹过 toast 并留在弹窗里，这里不需要动作。
async function applyInstallResult() {
  const pending = pendingInstallResult()
  if (!pending) return
  consumeInstallResult(pending.seq)
  // 成功才关弹窗：失败时错误要留在弹窗里，用户改完规格可直接重试
  installDialogVisible.value = false
  // 装的就是这个包（可能是卡片上的「安装」升级）：它的「有新版本」标记已经过期
  if (pending.status.name) {
    clearUpdateCheck(pending.status.name)
    clearBackgroundUpdate(pending.status.name)
  }
  if (pending.status.restart && pending.status.name) needsRestartPlugin.value = pending.status.name
  await reloadKeepingScroll()
}

// 结果可能在页面挂载期间落定，也可能在别的页面落定（回到本页时才消费）
watch(installResult, () => void applyInstallResult(), { immediate: true })

// --- 检查更新（卡片状态右侧的图标按钮）---
//
// 与官方插件中心一致：只把「比当前版本新、非预发布、未废弃、与当前 dsh 兼容」的版本当作
// 候选（判定在 backend/plugin_update.go）。检查是按插件的、异步的：按钮转圈，结果只影响
// 这张卡片；有更新时版本号右侧显示「→ 新版本」，卸载按钮左侧出现黄色「安装」。
// 「安装」复用安装任务（dsh plugin add name@version）：先跑插件命令，再按既有流程重拉列表，
// bundle 会挂出「重启生效」提示条。
const updatingPlugin = ref<string | null>(null)

const BACKGROUND_UPDATE_POLL_MS = 5 * 60 * 1000
let backgroundTimer: number | null = null

async function loadBackgroundUpdates() {
  try {
    const r = await api.pluginUpdates()
    backgroundUpdates.value = r.updates || {}
  } catch {
    // 拿不到后台结论就当没有：不影响列表本身的加载与操作
  }
}

function isCheckingUpdate(name: string): boolean {
  return updateChecks.value[name]?.status === 'checking'
}

function clearBackgroundUpdate(name: string) {
  if (!(name in backgroundUpdates.value)) return
  const next = { ...backgroundUpdates.value }
  delete next[name]
  backgroundUpdates.value = next
}

// clearUpdateCheck 丢掉某个插件的检查结果（装/卸之后它就过期了）。
function clearUpdateCheck(name: string) {
  if (!(name in updateChecks.value)) return
  const next = { ...updateChecks.value }
  delete next[name]
  updateChecks.value = next
}

async function checkPluginUpdate(name: string) {
  if (isCheckingUpdate(name)) return
  updateChecks.value = { ...updateChecks.value, [name]: { status: 'checking' } }
  try {
    const r = await api.pluginUpdateCheck(name)
    updateChecks.value = {
      ...updateChecks.value,
      [name]: { status: 'done', latest: r.latest, hasUpdate: r.hasUpdate, current: r.current }
    }
    if (!r.hasUpdate) {
      // 后台缓存里可能还挂着上一次的「有更新」，这次手查是更新的结论 —— 清掉它，
      // 避免刷新页面后又冒出来（下一次小时级检查会重新算）。
      clearBackgroundUpdate(name)
      toast.show(t('plugin_update_none', { version: r.current || '' }), 'info', 3000)
    }
  } catch (e) {
    // 失败不留「有更新」标记：换源或控制台恢复后重查即可
    const next = { ...updateChecks.value }
    delete next[name]
    updateChecks.value = next
    toast.show(uiErrText(e, t('plugin_update_check_failed')), 'error', 5000)
  }
}

async function installUpdate(p: PluginInfo) {
  const latest = cardUpdates.value[p.name]?.latest || ''
  if (!latest || installingPlugin.value) return
  updatingPlugin.value = p.name
  try {
    // 更新标记由 applyInstallResult 在安装成功时清掉；失败时保留，用户可直接重试
    await startPluginInstall(`${p.name}@${latest}`)
  } finally {
    updatingPlugin.value = null
  }
}

onMounted(() => {
  // 命中缓存则直接使用，避免切换子页面时重复命令拉取
  store.loadPlugins()
  // 后台定时检测的结果（内存缓存）随页面打开同步一次，之后每 5 分钟拉一次：
  // 接口只读缓存，不会起 `dsh plugin list` 子进程。
  void loadBackgroundUpdates()
  backgroundTimer = window.setInterval(() => void loadBackgroundUpdates(), BACKGROUND_UPDATE_POLL_MS)
  // 后台可能还有一次安装（切换页面回来 / 刷新页面）：接上进度，并直接把弹窗打开
  void resumeInstallIfRunning().then((running) => {
    if (running) installDialogVisible.value = true
  })

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
  if (backgroundTimer !== null) {
    window.clearInterval(backgroundTimer)
    backgroundTimer = null
  }
})
</script>

<template>
  <div class="pt-3 sm:pt-4 pb-8 sm:pb-12 px-4 sm:px-8 max-w-6xl mx-auto">
    <!-- 页头（图标 + 标题 + 刷新/重置按钮）；ref 用于实测高度，给下面的 sticky 提示条定位 -->
    <PageHeader ref="headerRef" sticky class="mb-3" :title="t('nav_plugins')" :icon="icons.plugin">
      <!-- 安装插件：打开弹窗填规格；安装进行中再次点开可看进度（任务在后台跑） -->
      <button class="g-btn-secondary h-8 px-3 text-xs" @click="onInstallClick">
        {{ t('plugin_install') }}
      </button>
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

    <!-- 分类筛选 + 实时搜索（顶栏下）：
         - 每个分类按钮带该分类的数量，数量为 0 的分类连按钮一起隐藏；
         - 搜索框只在插件超过 10 个时出现，**没有搜索按钮**，输入即过滤
           （v-model → visiblePlugins）；两者都没有时整行不渲染；
         - 宽屏时搜索框贴右；宽度不够时它整块换到下一行（flex-1 + min-w-48 +
           父级 flex-wrap，见下面搜索框上的注释）。 -->
    <div v-if="visibleFilters.length > 0 || showSearch" class="mb-3 flex flex-wrap items-center gap-2">
      <div class="flex flex-wrap items-center gap-2" role="group" :aria-label="t('plugin_filter_group')">
        <button
          v-for="f in visibleFilters"
          :key="f.value"
          type="button"
          class="g-chip h-8 px-3.5 transition-colors"
          :class="pluginFilter === f.value ? 'g-chip-active' : 'hover:bg-black/10 dark:hover:bg-white/20'"
          :aria-pressed="pluginFilter === f.value"
          @click="pluginFilter = f.value"
        >
          {{ t(f.label) }}
          <!-- 计数不要再叠 opacity：选中态是白字压在 brand 底上（4.47:1，已是这种底色的
               上限），再降 70% 不透明度就掉到约 3.0:1，12px 文本低于 AA 4.5。 -->
          <span class="tabular-nums">{{ filterCounts[f.value] }}</span>
        </button>
      </div>
      <!-- 搜索框：宽屏固定 224px 贴右；窄屏用 flex-1 + min-w-48 —— 这一行放得下就
           占掉剩余宽度留在同一行，放不下才整块换到下一行（换行后 flex-1 撑满一行）。
           不用 w-full sm:w-56 那种按断点写死的做法：600px 上下明明放得下，也没必要
           把搜索框甩到第二行。 -->
      <div v-if="showSearch" class="relative min-w-48 flex-1 sm:ml-auto sm:w-56 sm:flex-none">
        <span
          class="icon-svg pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-ink-faint"
          v-html="icons.search"
        ></span>
        <input
          v-model="pluginQuery"
          type="search"
          class="g-input h-8 py-0 pl-8 pr-3 text-xs"
          :placeholder="t('plugin_search_placeholder')"
          :aria-label="t('plugin_search_placeholder')"
          autocomplete="off"
          spellcheck="false"
        />
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

      <!-- 有插件、但被分类/搜索过滤空了：与「一个插件都没有」分开说，
           否则用户会以为是列表没加载出来 -->
      <div v-else-if="visiblePlugins.length === 0" class="col-span-full py-8 text-center text-sm text-ink-faint dark:text-[#8A8A92]">
        {{ t('plugin_search_empty') }}
      </div>

      <template v-else>
        <div v-for="p in visiblePlugins" :key="p.name" class="g-card g-card-hover p-4 flex flex-col">
          <!-- 第一行：插件名 + 版本号（左）与详情按钮（右上角，无边框纯图标）。
               详情按钮只负责打开弹窗 —— 文档在弹窗打开时才读（懒加载），不预取。 -->
          <div class="flex items-start justify-between gap-2">
            <div class="min-w-0 flex-1">
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
              <!-- 版本号：刻意不可复制、不可点击；检查到更新时在右侧追加「→ 新版本号」 -->
              <div class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1.5 flex flex-wrap items-center gap-1.5">
                <span>{{ p.version }}</span>
                <template v-if="cardUpdates[p.name]?.hasUpdate">
                  <span aria-hidden="true">→</span>
                  <span class="font-medium text-warning-ink dark:text-[#FBBF24]">{{ cardUpdates[p.name]?.latest }}</span>
                </template>
              </div>
            </div>
            <button
              type="button"
              class="g-btn-ghost -mr-1 -mt-1 h-8 w-8 flex-shrink-0 p-0"
              :aria-label="t('plugin_detail_open', { name: p.name })"
              :title="t('plugin_detail_open', { name: p.name })"
              @click="openPluginDetail(p.name)"
            >
              <span class="w-4 h-4" v-html="icons.info"></span>
            </button>
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
            <!-- 检查更新：状态右侧的纯图标按钮；检查中换成转圈 -->
            <button
              type="button"
              class="g-btn-ghost h-8 w-8 flex-shrink-0 p-0"
              :aria-label="t('plugin_check_update', { name: p.name })"
              :title="t('plugin_check_update', { name: p.name })"
              :disabled="isCheckingUpdate(p.name)"
              :aria-busy="isCheckingUpdate(p.name)"
              @click="checkPluginUpdate(p.name)"
            >
              <span
                v-if="isCheckingUpdate(p.name)"
                class="w-4 h-4 rounded-full border-2 border-line border-t-brand animate-spin"
              ></span>
              <span v-else class="w-4 h-4" v-html="icons.refresh"></span>
            </button>
            <!-- 右侧动作：有新版本时黄色「安装」出现在卸载左侧 -->
            <div class="ml-auto flex flex-wrap items-center gap-2">
              <button
                v-if="cardUpdates[p.name]?.hasUpdate"
                class="g-btn-warning h-8 px-3 text-xs"
                :disabled="installingPlugin || updatingPlugin === p.name"
                :aria-label="t('plugin_update_aria', { name: p.name, version: cardUpdates[p.name]?.latest })"
                @click="installUpdate(p)"
              >
                {{ updatingPlugin === p.name ? t('plugin_updating') : t('plugin_update') }}
              </button>
              <button
                class="g-btn-danger h-8 px-3 text-xs"
                :disabled="removingPlugin === p.name || resetting"
                @click="onUninstallClick(p.name)"
              >
                {{ removingPlugin === p.name ? t('plugin_removing') : t('plugin_uninstall') }}
              </button>
            </div>
          </div>
        </div>
      </template>
    </div>

    <!-- 插件详情弹窗（只读）：打开才读文档；meta.detail 优先、README 兜底 -->
    <PluginDetailDialog v-model:visible="detailDialogVisible" :name="detailTarget" />

    <!-- 安装插件弹窗（安装是后台任务：关掉它不会中断，进度/结果由本页轮询） -->
    <PluginInstallDialog
      v-model:visible="installDialogVisible"
      :installing="installingPlugin"
      :progress="installProgress"
      :error-text="installError"
      @submit="startPluginInstall"
    />

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
        class="mb-6 rounded-lg px-3 py-2 text-xs leading-relaxed bg-[#F59E0B]/10 dark:bg-[#F59E0B]/15 border border-[#F59E0B]/40 text-warning-ink dark:text-[#FBBF24]"
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