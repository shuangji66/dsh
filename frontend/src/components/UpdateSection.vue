<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api, sseUrl, type ManagedPluginKind, type UpdateKind, type ServerVersions, type UpdateStatus } from '@/serverapi'
import { useToastStore } from '@/stores/toast'
import { uiErrText, uiText, useI18n } from '@/composables/useI18n'
import { useEventStream } from '@/composables/useEventStream'
import CheckUpdateButton from '@/components/CheckUpdateButton.vue'
import ServerVersionsDialog from '@/components/ServerVersionsDialog.vue'
import ManagedPluginDialog from '@/components/ManagedPluginDialog.vue'

// 概览页「版本」卡片：三个版本（dsh 服务 / 插件市场 / 增强插件）的当前版本、检查更新
// 入口与各自的弹窗。
//
// 控制台自身的版本不在这里展示，也不支持就地自我更新：升级只走 fpk 安装包。
// 三条链路的形态：
//   - dsh 服务：本机多版本（npm install 到 `<数据目录>/server/<版本>`，可下载/删除/切换）
//     —— 弹窗见 ServerVersionsDialog.vue；
//   - 插件市场 / 增强插件：同一形态的普通 profile 插件（未安装 → 安装最新版，已安装 →
//     更新/卸载），共用 ManagedPluginDialog.vue。
//
// 本组件是这三者的**唯一数据源**：它持有 /api/update/stream 的 SSE 快照并按 kind 分发，
// 子弹窗只收 props、只发意图事件。
const toast = useToastStore()
const { t } = useI18n()

// 后端推送的更新状态（dsh 服务 / 插件市场 / 增强插件 各一份）。本地版本号由
// `/api/update/status` 统一提供。
const dshStatus = ref<UpdateStatus>({ kind: 'dsh', localVersion: '', latestVersion: '', hasUpdate: false, checkedAt: '', releaseNotes: '' })
// 受管插件（dshmarket / dsh-fnos-advanced）：普通 profile 插件，localVersion 为空即
// 「未安装」；unavailable 为真表示「这次查不到」，不能当成未安装。
const marketStatus = ref<UpdateStatus>({ kind: 'market', localVersion: '', latestVersion: '', hasUpdate: false, checkedAt: '' })
const advancedStatus = ref<UpdateStatus>({ kind: 'advanced', localVersion: '', latestVersion: '', hasUpdate: false, checkedAt: '' })
// dsh 服务版本快照（可选版本列表 + 已安装 + 选中 + 安装进度）。
const serverVersions = ref<ServerVersions | null>(null)

// 各目标是否正在“检查更新”
const checking = ref<Record<UpdateKind, boolean>>({ dsh: false, market: false, advanced: false })

// 按 kind 取对应的状态对象：三条链路的字段完全一致，只有数据来源不同。
function statusOf(kind: UpdateKind): UpdateStatus {
  if (kind === 'dsh') return dshStatus.value
  if (kind === 'advanced') return advancedStatus.value
  return marketStatus.value
}

// --- 受管插件（插件市场 / 增强插件） ---
//
// 两个插件的后端动作完全同构（安装 / 更新 / 卸载 / 收起结果），差异只有接口前缀与
// 弹窗上的名字，因此这里用 kind 参数化，避免把同一套逻辑写两遍。
const managedKinds: ManagedPluginKind[] = ['market', 'advanced']
const managedDialogVisible = ref<Record<ManagedPluginKind, boolean>>({ market: false, advanced: false })
const pendingManagedAction = ref<Record<ManagedPluginKind, 'install' | 'update' | 'remove' | null>>({
  market: null,
  advanced: null
})
// 每个插件各自记「上一条已提示过的结果」，避免 SSE 重复推同一份快照时重复弹 toast。
const announcedManagedOp: Record<ManagedPluginKind, string> = { market: '', advanced: '' }

function managedStatusOf(kind: ManagedPluginKind): UpdateStatus {
  return statusOf(kind)
}

async function runManagedAction(kind: ManagedPluginKind, action: 'install' | 'update' | 'remove') {
  pendingManagedAction.value[kind] = action
  try {
    if (action === 'install') await api.pluginInstall(kind)
    else if (action === 'update') await api.pluginUpdate(kind)
    else await api.pluginRemove(kind)
  } catch (e) {
    pendingManagedAction.value[kind] = null
    toast.show(uiErrText(e, t('update_error_unknown')), 'error')
  }
}

// managedActionLabel 把动作翻成两字动词（直接复用弹窗按钮文案，避免再维护一份）。
function managedActionLabel(action: 'install' | 'update' | 'remove'): string {
  if (action === 'update') return t('plugin_dialog_update_btn')
  if (action === 'remove') return t('plugin_dialog_remove_btn')
  return t('plugin_dialog_install_btn')
}

function managedName(kind: ManagedPluginKind): string {
  return kind === 'advanced' ? t('plugin_dialog_advanced_name') : t('plugin_dialog_market_name')
}

// 收起某个插件操作的终态（成功提示已 toast / 失败详情已看过 / 弹窗已关闭）。
async function onManagedAck(kind: ManagedPluginKind) {
  try {
    await api.pluginDone(kind)
  } catch { /* 忽略：只是清状态，失败不影响用户 */ }
}

function closeManagedDialog(kind: ManagedPluginKind) {
  managedDialogVisible.value[kind] = false
}

function managedOpSig(kind: ManagedPluginKind): string {
  const st = managedStatusOf(kind)
  return `${st.seq || 0}|${st.phase || ''}|${st.error || ''}`
}

function handleManagedResult(kind: ManagedPluginKind) {
  const st = managedStatusOf(kind)
  const sig = managedOpSig(kind)
  if (sig === announcedManagedOp[kind]) return
  if (st.phase === 'done') {
    announcedManagedOp[kind] = sig
    const action = pendingManagedAction.value[kind]
    pendingManagedAction.value[kind] = null
    toast.show(
      action
        ? t('plugin_dialog_toast_done', { action: managedActionLabel(action) })
        : t('plugin_dialog_toast_done_generic', { name: managedName(kind) }),
      'success'
    )
    // 与 dsh 版本安装同一条约定：成功只提示一次，立刻收起后端那份终态。
    api.pluginDone(kind).catch(() => { /* 忽略：只是清状态 */ })
  } else if (st.error) {
    announcedManagedOp[kind] = sig
    pendingManagedAction.value[kind] = null
    toast.show(uiText(st.error, st.errorRef), 'error')
    // 失败不清：错误详情留在弹窗里，等用户关闭弹窗时再收起。
  }
}

for (const kind of managedKinds) {
  watch(() => managedOpSig(kind), () => handleManagedResult(kind))
}

// --- dsh 服务版本弹窗 ---
const serverDialogVisible = ref(false)
const serverLoading = ref(false)

// openServerDialog 打开 dsh 版本列表：先用后端快照兜底显示，再拉一次（避免显示别人
// 刷新前的旧列表）；加载期间按钮禁用，不阻塞已装版本的切换/删除。
async function openServerDialog() {
  serverDialogVisible.value = true
  if (serverVersions.value) return
  await refreshServerVersions(false)
}

// refreshServerVersions 拉取版本列表；force 为 true 时强制联网刷新镜像源列表。
async function refreshServerVersions(force: boolean) {
  serverLoading.value = true
  try {
    const res = await api.dshVersions(force)
    serverVersions.value = res.server
    // dsh 版本与选中状态同时影响版本行与红点，这里顺带同步一次状态。
    const snap = await api.updateStatus()
    merge(snap)
  } catch (e) {
    toast.show(uiErrText(e, t('update_error_unknown')), 'error')
  } finally {
    serverLoading.value = false
  }
}

// onInstallVersion / onCancelVersion / onRemoveVersion / onSwitchVersion 是弹窗发上来的
// 意图：这里只负责调接口 + 提示，实际状态变化由后端 SSE 推回 serverVersions。
async function onInstallVersion(version: string) {
  try {
    await api.dshVersionInstall(version)
  } catch (e) {
    toast.show(uiErrText(e, t('update_error_unknown')), 'error')
  }
}

async function onCancelVersion() {
  try {
    await api.dshVersionCancel()
    toast.show(t('dsh_ver_cancelled'), 'info')
  } catch (e) {
    toast.show(uiErrText(e, t('update_error_unknown')), 'error')
  }
}

// 关闭版本弹窗：如果停在终态（成功/失败/已取消），顺手把结果收起 —— 概览页切走会卸载、
// 切回会重新挂载，状态留着就会把同一条提示再弹一次。
function closeServerDialog() {
  serverDialogVisible.value = false
  const ph = serverVersions.value?.install?.phase || ''
  const cancelled = serverVersions.value?.install?.cancelled
  if (ph === 'done' || ph === 'error' || cancelled) {
    api.dshVersionAck().catch(() => { /* 只是清状态，失败不影响用户 */ })
  }
}

async function onRemoveVersion(version: string) {
  try {
    await api.dshVersionDelete(version)
    toast.show(t('dsh_ver_deleted', { v: version }), 'success')
  } catch (e) {
    toast.show(uiErrText(e, t('update_error_unknown')), 'error')
  }
}

async function onSwitchVersion(version: string) {
  try {
    await api.dshVersionSwitch(version)
    toast.show(t('dsh_ver_switched', { v: version }), 'success')
    // 切换会停 dsh 并用新版本重启：稍等片刻再刷新，避免刷新出启动等待页。
    setTimeout(() => window.location.reload(), 3000)
  } catch (e) {
    toast.show(uiErrText(e, t('update_error_unknown')), 'error')
  }
}

// 从后端快照合并到本地响应式状态（允许只带部分 kind 的快照）
function merge(snap: { dsh?: UpdateStatus; market?: UpdateStatus; advanced?: UpdateStatus; server?: ServerVersions }) {
  if (snap.dsh) {
    dshStatus.value = { ...snap.dsh, localVersion: snap.dsh.localVersion || '' }
  }
  if (snap.market) {
    marketStatus.value = { ...snap.market, localVersion: snap.market.localVersion || '' }
  }
  if (snap.advanced) {
    advancedStatus.value = { ...snap.advanced, localVersion: snap.advanced.localVersion || '' }
  }
  // dsh 版本快照（列表 + 安装进度）也随同一条 SSE 推送更新 —— 版本弹窗里那一行的
  // 下载/包处理进度就来自这里，不需要额外开一条通道。
  if (snap.server) {
    serverVersions.value = snap.server
  }
}

// 版本号右上角红点：有更新时显示。
function hasUpdateDot(kind: UpdateKind): boolean {
  return statusOf(kind).hasUpdate
}
// 版本行的显示文本。空版本号有两种含义，必须分开：
//   - 「查不了」（unavailable，只有两个受管插件会置位：dsh 服务未就绪 / 没选中版本 /
//     `dsh plugin list` 报错）→ 显示「—」：这时我们**不知道**装没装，写「未安装」
//     就是在说假话（dsh 还没起来时最容易看到）；
//   - 明确查到了、只是没装 → 「未安装」。
function versionLabel(kind: UpdateKind): string {
  const st = statusOf(kind)
  if (st.localVersion) return st.localVersion
  return st.unavailable ? '—' : t('not_installed')
}

// 通过 SSE 监听后端推送的更新检测结果。
// 用 useEventStream 而非裸 EventSource：断线（反代掐断空闲长连接、后端重启等）
// 后会自动重连；重连成功时后端立刻补发一份初始快照，安装进度随即追平。
const updateStream = useEventStream(() => sseUrl('/api/update/stream'), {
  update: (data) => {
    const d = data as { dsh?: UpdateStatus; market?: UpdateStatus; advanced?: UpdateStatus; server?: ServerVersions }
    if (d && (d.dsh || d.market || d.advanced || d.server)) merge(d)
  }
})

// “检查更新”按钮：通知后端执行一次检测，随后 SSE 推送最新结果
async function doCheck(kind: UpdateKind) {
  checking.value[kind] = true
  try {
    // 后端同步执行检测并返回最新结果（含 dsh 版本列表快照）
    const snap = await api.updateCheck()
    merge(snap)
    const st = statusOf(kind)
    if (st?.error) {
      toast.show(uiText(st.error, st.errorRef), 'error')
    } else if (st && !st.hasUpdate) {
      toast.show(t('update_no_new'), 'success')
    }
  } catch (e) {
    toast.show(uiErrText(e, t('update_error_unknown')), 'error')
  } finally {
    checking.value[kind] = false
  }
}

// 打开对应目标的弹窗：dsh 用版本列表弹窗；插件市场与增强插件共用受管插件弹窗。
function openDialog(kind: UpdateKind) {
  if (kind === 'dsh') {
    openServerDialog()
    return
  }
  managedDialogVisible.value[kind] = true
}

// --- 安装/操作结果的一次性提示 ---
//
// 弹窗现在可以在安装过程中关闭（关掉不会中断后台任务），因此**结果必须能到达用户**：
// 完成或失败都以 toast 提示一次（成功不再常驻在弹窗里）。
//
// 只提示一次的机制有两层：①组件内按「签名」去重，SSE 重复推同一份快照不会重复弹；
// ②成功提示发出后立刻调 `dshVersionAck` 把后端那份终态收起 —— 概览页切走会卸载、切回会
// 重新挂载，状态留着就会把同一条成功提示再弹一次。失败不清（错误详情要留在弹窗里）。
//
// 签名里必须带**后端给的运行序号**（install.seq / 插件的 op seq）：只按 version+phase 去重时，
// 同一挂载内第二次相同结果会被静默吞掉（装成功 → 删掉 → 再装成功；市场装成功 → 卸载成功）。
// 这两个序号都是后端每次操作自增的，前端不能自己数（SSE 可能只收到终态那一帧）。
let announcedServerInstall = ''

const serverInstallSig = computed(() => {
  const st = serverVersions.value?.install
  return st ? `${st.seq || 0}|${st.version}|${st.phase || ''}|${st.cancelled ? 'c' : ''}` : ''
})

watch(serverInstallSig, () => {
  const st = serverVersions.value?.install
  if (!st) return
  const sig = serverInstallSig.value
  if (sig === announcedServerInstall) return
  if (st.phase === 'done') {
    announcedServerInstall = sig
    toast.show(t('dsh_ver_toast_done', { v: st.version }), 'success')
    // 成功只提示一次：立刻把后端那份终态收起，避免切回概览页又弹一次。
    api.dshVersionAck().catch(() => { /* 忽略：只是清状态 */ })
  } else if (st.phase === 'error' && st.error) {
    announcedServerInstall = sig
    toast.show(uiText(st.error, st.errorRef), 'error')
    // 失败不在这里清：错误详情要留在弹窗里给用户看，等用户关闭弹窗时再收起。
  }
})

onMounted(() => {
  // 拉取一次后端状态快照作为初始值（含 dsh 版本快照），随后交给 SSE 增量更新
  api.updateStatus().then(merge).catch(() => {})
  updateStream.start()
})
</script>
<template>
  <!-- 概览页「版本」卡片：三行版本 + 各自检查更新入口；各自的弹窗挂在卡片内（Teleport 到 body） -->
  <section class="g-card g-card-hover p-5 flex flex-col">
    <h2 class="font-display text-base font-semibold text-ink dark:text-white">{{ t('overview_versions') }}</h2>
    <p class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1 mb-3">{{ t('overview_versions_hint') }}</p>

    <div class="divide-y divide-line dark:divide-[#2A2A32]">
      <!-- dsh 服务版本 -->
      <div class="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 py-2.5">
        <span class="text-xs text-ink-soft dark:text-[#A6A6AD]">{{ t('update_dsh_ver') }}</span>
        <div class="flex items-center gap-3 min-w-0">
          <!-- 版本号：点击打开 dsh 版本列表（下载 / 删除 / 切换）。「未安装」时同样
               可点 —— 那正是用户需要去装一个版本的入口。 -->
          <button class="relative font-mono text-sm font-semibold text-ink dark:text-white underline underline-offset-4 decoration-ink-soft/50 dark:decoration-[#A6A6AD]/50" @click="openDialog('dsh')">
            {{ versionLabel('dsh') }}
            <span v-if="hasUpdateDot('dsh')" class="absolute -top-1.5 -right-2.5 h-2.5 w-2.5 rounded-full bg-[#EF4444] shadow"></span>
          </button>
          <!-- 检查更新：与 dsh 回退图标成对（见 CheckUpdateButton.vue） -->
          <CheckUpdateButton :checking="checking.dsh" :label="t('update_check')" @check="doCheck('dsh')" />
        </div>
      </div>

      <!-- 插件市场版本（dshmarket）：普通 profile 插件，未安装时可在这里装上 -->
      <div class="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 py-2.5">
        <span class="text-xs text-ink-soft dark:text-[#A6A6AD]">{{ t('update_market_ver') }}</span>
        <div class="flex items-center gap-3 min-w-0">
          <!-- 版本号：点击打开市场弹窗（未安装 → 安装最新版；已安装 → 更新 / 卸载）。
               dsh 未就绪时查不到装没装，显示「—」（见 versionLabel）—— 那不是「未安装」。 -->
          <button
            class="relative font-mono text-sm font-semibold underline underline-offset-4 decoration-ink-soft/50 dark:decoration-[#A6A6AD]/50 text-ink dark:text-white"
            @click="openDialog('market')"
          >
            {{ versionLabel('market') }}
            <span v-if="hasUpdateDot('market')" class="absolute -top-1.5 -right-2.5 h-2.5 w-2.5 rounded-full bg-[#EF4444] shadow"></span>
          </button>
          <CheckUpdateButton :checking="checking.market" :label="t('update_check')" @check="doCheck('market')" />
        </div>
      </div>

      <!-- 增强插件版本（dsh-fnos-advanced）：与插件市场同形态（普通 profile 插件），
           未安装时可在这里装上；弹窗样式与安装流程复用市场那一套。 -->
      <div class="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 py-2.5">
        <span class="text-xs text-ink-soft dark:text-[#A6A6AD]">{{ t('update_advanced_ver') }}</span>
        <div class="flex items-center gap-3 min-w-0">
          <button
            class="relative font-mono text-sm font-semibold underline underline-offset-4 decoration-ink-soft/50 dark:decoration-[#A6A6AD]/50 text-ink dark:text-white"
            @click="openDialog('advanced')"
          >
            {{ versionLabel('advanced') }}
            <span v-if="hasUpdateDot('advanced')" class="absolute -top-1.5 -right-2.5 h-2.5 w-2.5 rounded-full bg-[#EF4444] shadow"></span>
          </button>
          <CheckUpdateButton :checking="checking.advanced" :label="t('update_check')" @check="doCheck('advanced')" />
        </div>
      </div>
    </div>

    <!-- dsh 服务版本列表弹窗（下载 / 删除 / 切换，行内显示下载与包处理进度） -->
    <ServerVersionsDialog
      :visible="serverDialogVisible"
      :status="serverVersions"
      :loading="serverLoading"
      @close="closeServerDialog"
      @refresh="refreshServerVersions(true)"
      @install="onInstallVersion"
      @cancel="onCancelVersion"
      @remove="onRemoveVersion"
      @switch="onSwitchVersion"
    />

    <!-- 受管插件弹窗（插件市场 / 增强插件）：安装 / 更新 / 卸载，共用同一份实现 -->
    <ManagedPluginDialog
      v-for="kind in managedKinds"
      :key="kind"
      :visible="managedDialogVisible[kind]"
      :status="managedStatusOf(kind)"
      :plugin="kind"
      @close="closeManagedDialog(kind)"
      @install="runManagedAction(kind, 'install')"
      @update="runManagedAction(kind, 'update')"
      @remove="runManagedAction(kind, 'remove')"
      @ack="onManagedAck(kind)"
    />
  </section>
</template>
