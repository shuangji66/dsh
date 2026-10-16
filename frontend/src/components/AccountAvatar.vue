<script setup lang="ts">
// DeepSeek 官方账号：概览页「DeepSeek Harness」卡片右上角的头像 + 管理弹窗。
//
// 登录/退出都在控制台侧完成（见 backend/account.go）—— 平台只接受 loopback 上的
// /oauth/callback 回调，dsh 自带的账号入口在本部署用不了，所以这里自己做：
//   点「登录」→ 后端 auth_init 拿授权链接 → 用户用「跳转登录」在浏览器里授权（或
//   「复制授权链接」换设备/浏览器打开）→ 授权后浏览器会停在一条**打不开的** 127.0.0.1
//   地址上（那一步跳转本身已经表示授权成功）→ 把那条地址粘进「回调链接」→ 完成登录。
//
// 头像状态：未登录是灰色圆形占位图 + 下方「未登录」；登录后换成平台头像，平台没给
// avatarUrl（或图片拉不下来）就退回 DeepSeek 图标（icons.deepseek），下方显示余额。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api, type AccountEntryView, type AccountLoginState, type AccountStatusPayload } from '@/serverapi'
import { useI18n, uiText, uiErrText } from '@/composables/useI18n'
import { useToastStore } from '@/stores/toast'
import { useBodyScrollLock } from '@/composables/useBodyScrollLock'
import { copyText } from '@/utils/clipboard'
import { formatWallets } from '@/utils/money'
import { icons } from '@/utils/icons'
import DialogCloseButton from '@/components/DialogCloseButton.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'

const { t, locale } = useI18n()
const toast = useToastStore()

const visible = ref(false)
const status = ref<AccountStatusPayload | null>(null)
const login = ref<AccountLoginState | null>(null)
const callback = ref('')
const busy = ref(false)
// rowBusy 是「正在切换/移除的那个账号 id」：只禁用那一行的按钮，不锁整张弹窗。
const rowBusy = ref('')
const confirmBusy = ref(false)
// 两个确认弹窗各自持有「可见性」与「待处理对象」——**动作不能存在被关闭路径清掉的状态里**：
// ConfirmDialog 的 onConfirm() 先 close()（发 update:visible(false) + cancel）再发 confirm，
// 上一版把动作写成 `confirmKind === 'remove' ? doRemove() : doLogout()` 并在 update:visible
// 里清空它，于是点「移除」实际执行的是「退出登录」（日志里只有 signed out，没有 removed
// account）。仓库其它弹窗一律是 `v-model:visible` + 独立 target ref，照那个写法。
const logoutVisible = ref(false)
const removeVisible = ref(false)
const removeTarget = ref<AccountEntryView | null>(null)
let pollTimer: number | undefined

useBodyScrollLock(visible)

const signedIn = computed(() => status.value?.signedIn === true)
const waiting = computed(() => login.value?.phase === 'waiting')
const authorizeUrl = computed(() => login.value?.authorizeUrl ?? '')
// 账号列表（多账号）：切换只是把另一份 token 写进 dsh 的凭据记录，列表由后端推导 active。
const accounts = computed<AccountEntryView[]>(() => status.value?.accounts ?? [])
const activeAccount = computed(() => accounts.value.find((a) => a.active))

function accountLabel(a: AccountEntryView) {
  return a.name || a.contact || t('account_unknown')
}

// 头像下方那行：未登录「未登录」；登录后优先显示余额。格式规则抄 dsh 自己的账号面板
// （截断两位 + 千分位 + 货币符号 + 子分显示，见 utils/money.ts），否则同一个账号在
// dsh 与控制台里会显示成两个数。
const balanceText = computed(() => formatWallets(status.value?.balance))
const bonusText = computed(() => formatWallets(status.value?.bonus))
const avatarLabel = computed(() => {
  if (!status.value) return ''
  if (!status.value.available) return t('account_unavailable_short')
  if (!signedIn.value) return t('account_signed_out')
  return balanceText.value || t('account_signed_in')
})

// 头像：平台给了 avatarUrl 就用图片；**没给**（平台侧账号本身没有头像）或**拉不下来**
// （离线 / URL 失效）都退回 DeepSeek 图标 —— 否则圆框里会是一个破图占位。
// avatarBroken 在地址变化（切换账号 / 重新拉状态）时复位，好让新地址重新试一次。
const avatarBroken = ref(false)
watch(() => status.value?.avatarUrl, () => { avatarBroken.value = false })
const showAvatarImage = computed(() => signedIn.value && !!status.value?.avatarUrl && !avatarBroken.value)

// 发起方身份：平台侧只用于记录（语言/时区），版本留空由后端补控制台版本。
function clientMeta() {
  return {
    version: '',
    locale: locale.value,
    timezoneOffsetSeconds: -new Date().getTimezoneOffset() * 60
  }
}

async function refresh() {
  try {
    status.value = await api.account()
  } catch {
    status.value = { available: false, signedIn: false }
  }
}

function stopPoll() {
  if (pollTimer !== undefined) {
    clearInterval(pollTimer)
    pollTimer = undefined
  }
}

// 只在「等授权」期间轮询：用户可能在**另一个浏览器/设备**里完成授权，或走 loopback
// 自动回调 —— 那种情况下我们这边只要发现 phase 变了就刷新头像与余额。
function syncPoll() {
  if (!waiting.value) {
    stopPoll()
    return
  }
  if (pollTimer !== undefined) return
  pollTimer = window.setInterval(() => void pollLogin(), 1500)
}

async function pollLogin() {
  try {
    const before = login.value?.phase
    const st = await api.accountLoginStatus()
    login.value = st
    if (st.phase !== 'waiting') {
      stopPoll()
      if (st.phase === 'succeeded' && before !== 'succeeded') {
        toast.show(uiText(st.message, st.messageRef, t('account_login_done')), 'success')
        await refresh()
      }
    }
  } catch {
    /* 轮询失败等下一次（后端重启/页面刷新都不该打断用户） */
  }
}

async function openDialog() {
  visible.value = true
  await refresh()
  try {
    login.value = await api.accountLoginStatus()
  } catch {
    /* 忽略：拿不到状态时按「未登录」渲染 */
  }
  syncPoll()
}

async function startLogin() {
  if (busy.value) return
  busy.value = true
  try {
    login.value = await api.accountLoginStart({
      callbackOrigin: window.location.origin,
      ...clientMeta()
    })
    syncPoll()
  } catch (e) {
    toast.show(uiErrText(e, t('account_login_failed')), 'error')
  } finally {
    busy.value = false
  }
}

function copyAuthorizeLink() {
  if (!authorizeUrl.value) return
  copyText(authorizeUrl.value)
  toast.show(t('account_link_copied'), 'success')
}

function openAuthorizePage() {
  if (!authorizeUrl.value) return
  window.open(authorizeUrl.value, '_blank', 'noopener')
}

async function completeLogin() {
  if (!callback.value.trim()) {
    toast.show(t('account_login_need_url'), 'error')
    return
  }
  busy.value = true
  try {
    const st = await api.accountLoginComplete({ url: callback.value.trim(), ...clientMeta() })
    login.value = st
    callback.value = ''
    toast.show(uiText(st.message, st.messageRef, t('account_login_done')), 'success')
    stopPoll()
    await refresh()
  } catch (e) {
    toast.show(uiErrText(e, t('account_login_failed')), 'error')
  } finally {
    busy.value = false
  }
}

async function cancelLogin() {
  try {
    login.value = await api.accountLoginCancel()
  } catch {
    /* 取消失败无所谓：下一次登录会替换掉它 */
  }
  stopPoll()
}

// 后端在退出登录/切换/移除时都会等 dsh 认账再返回快照（见 account.go），这里再兜一层：
// dsh 的文件监听偶尔更慢，不能让界面停在旧状态等用户手动刷新。
async function confirmSnapshot(expect: (s: AccountStatusPayload) => boolean) {
  for (let i = 0; i < 5; i++) {
    if (expect(status.value!)) return
    await new Promise((r) => window.setTimeout(r, 400))
    try {
      status.value = await api.account()
    } catch {
      return
    }
  }
}

async function doLogout() {
  confirmBusy.value = true
  try {
    status.value = await api.accountLogout()
    login.value = null
    toast.show(t('account_logout_done'), 'success')
    await confirmSnapshot((s) => !s.signedIn)
  } catch (e) {
    toast.show(uiErrText(e, t('account_logout_failed')), 'error')
  } finally {
    confirmBusy.value = false
    logoutVisible.value = false
  }
}

// 切换账号：把该账号的凭据变成 dsh 正在用的那份（后端写完会等 dsh 认账）。
async function switchAccount(a: AccountEntryView) {
  if (rowBusy.value) return
  rowBusy.value = a.id
  try {
    status.value = await api.accountSwitch(a.id)
    toast.show(t('account_switch_done', { contact: accountLabel(a) }), 'success')
    await confirmSnapshot((s) => (s.accounts ?? []).some((x) => x.id === a.id && x.active))
  } catch (e) {
    toast.show(uiErrText(e, t('account_switch_failed')), 'error')
  } finally {
    rowBusy.value = ''
  }
}

// 移除账号：本机凭据删除 + 请平台作废 token（退出登录只是「不再使用」，仍留着可切回）。
async function doRemove() {
  const target = removeTarget.value
  if (!target) return
  confirmBusy.value = true
  rowBusy.value = target.id
  try {
    status.value = await api.accountRemove(target.id)
    login.value = null
    toast.show(t('account_removed', { contact: accountLabel(target) }), 'success')
    await confirmSnapshot((s) => !(s.accounts ?? []).some((x) => x.id === target.id))
    await refresh() // 余额/联系方式来自当前账号资料，移除后一并刷新
  } catch (e) {
    toast.show(uiErrText(e, t('account_remove_failed')), 'error')
  } finally {
    confirmBusy.value = false
    rowBusy.value = ''
    removeVisible.value = false
    removeTarget.value = null
  }
}

function askRemove(a: AccountEntryView) {
  removeTarget.value = a
  removeVisible.value = true
}

function askLogout() {
  logoutVisible.value = true
}

// 授权链接文本框：点一下就全选，方便手动复制（剪贴板按钮另有兜底实现）。
function selectAll(e: FocusEvent) {
  const el = e.target as HTMLInputElement | null
  el?.select()
}

onMounted(() => {
  void refresh()
  // 刷新页面后如果还有在途的授权，接上它（后端持有尝试状态）。
  void api
    .accountLoginStatus()
    .then((st) => {
      login.value = st
      syncPoll()
    })
    .catch(() => {})
})

onBeforeUnmount(stopPoll)
</script>

<template>
  <!-- 右上角头像：未登录 = 灰色圆形占位图，头像下方一行状态/余额 -->
  <button
    type="button"
    class="flex flex-col items-center gap-1 flex-shrink-0 group"
    :title="t('account_open_dialog')"
    :aria-label="t('account_open_dialog')"
    @click="openDialog"
  >
    <span
      class="w-8 h-8 rounded-full flex items-center justify-center overflow-hidden transition-colors"
      :class="signedIn
        ? 'bg-brand/10 text-brand ring-1 ring-brand/30'
        : 'bg-black/[0.08] text-ink-faint dark:bg-white/10 dark:text-[#8A8A92] group-hover:text-ink-soft dark:group-hover:text-[#A6A6AD]'"
    >
      <img v-if="showAvatarImage" :src="status?.avatarUrl" alt="" class="w-full h-full object-cover" @error="avatarBroken = true" />
      <!-- 已登录但平台没给头像（或图片拉不下来）：用 DeepSeek 图标顶上 -->
      <span v-else-if="signedIn" class="w-4 h-4" v-html="icons.deepseek"></span>
      <span v-else class="w-4 h-4" v-html="icons.user"></span>
    </span>
    <span class="text-[10px] leading-none text-ink-faint dark:text-[#8A8A92] whitespace-nowrap">
      {{ avatarLabel }}
    </span>
  </button>

  <Teleport to="body">
    <Transition
      enter-active-class="transition duration-200 ease-out"
      enter-from-class="opacity-0"
      enter-to-class="opacity-100"
      leave-active-class="transition duration-150 ease-in"
      leave-from-class="opacity-100"
      leave-to-class="opacity-0"
    >
      <div v-if="visible" class="fixed inset-0 z-50 flex items-center justify-center p-4 backdrop-blur-sm">
        <div class="g-modal-mask" @click="visible = false"></div>
        <div class="relative w-full max-w-sm bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6">
          <DialogCloseButton :label="t('dialog_close')" @close="visible = false" />
          <div class="flex items-center gap-2 pr-11 mb-3">
            <h3 class="g-dialog-title !pr-0">{{ t('account_title') }}</h3>
          </div>

          <!-- 状态区：大头像 + 未登录 / 联系方式 + （登录后）余额与赠送金 -->
          <div class="flex items-center gap-3 mb-4">
            <span
              class="w-12 h-12 rounded-full flex items-center justify-center overflow-hidden flex-shrink-0"
              :class="signedIn ? 'bg-brand/10 text-brand ring-1 ring-brand/30' : 'bg-black/[0.08] text-ink-faint dark:bg-white/10 dark:text-[#8A8A92]'"
            >
              <img v-if="showAvatarImage" :src="status?.avatarUrl" alt="" class="w-full h-full object-cover" @error="avatarBroken = true" />
              <span v-else-if="signedIn" class="w-6 h-6" v-html="icons.deepseek"></span>
              <span v-else class="w-6 h-6" v-html="icons.user"></span>
            </span>
            <div class="min-w-0">
              <div class="text-sm font-semibold text-ink dark:text-[#EDEDF0]">
                {{ signedIn ? (status?.name || t('account_signed_in')) : t('account_signed_out') }}
              </div>
              <div v-if="signedIn && status?.contact" class="text-xs text-ink-faint dark:text-[#8A8A92] truncate">{{ status.contact }}</div>
              <div v-else-if="!status?.available" class="text-xs text-danger dark:text-[#EF4444]">
                {{ uiText(status?.message, status?.messageRef, t('account_unavailable')) }}
              </div>
            </div>
          </div>

          <!-- 账号列表（多账号）：标出使用中的那条，其余可一键切换 -->
          <div v-if="accounts.length" class="mb-4">
            <div class="text-xs text-ink-faint dark:text-[#8A8A92] mb-1.5">{{ t('account_list_title') }}</div>
            <div class="border border-[#E8E8EC] dark:border-[#2A2A32] rounded-lg divide-y divide-[#E8E8EC] dark:divide-[#2A2A32]">
              <div
                v-for="a in accounts"
                :key="a.id"
                class="flex items-center justify-between gap-2 px-3 py-2"
              >
                <div class="min-w-0">
                  <div class="text-sm text-ink dark:text-[#EDEDF0] truncate">{{ accountLabel(a) }}</div>
                  <div v-if="a.contact && a.name" class="text-xs text-ink-faint dark:text-[#8A8A92] truncate">{{ a.contact }}</div>
                </div>
                <div class="flex items-center gap-2 flex-shrink-0">
                  <span
                    v-if="a.active"
                    class="g-status bg-success/10 text-success"
                  >{{ t('account_in_use') }}</span>
                  <template v-else>
                    <button
                      class="g-btn-secondary h-8 px-3 text-xs"
                      :disabled="!!rowBusy"
                      @click="switchAccount(a)"
                    >{{ t('account_switch') }}</button>
                    <button
                      class="g-btn-danger h-8 px-3 text-xs"
                      :disabled="!!rowBusy"
                      @click="askRemove(a)"
                    >{{ t('account_remove') }}</button>
                  </template>
                </div>
              </div>
            </div>
          </div>

          <div v-if="signedIn" class="space-y-1.5 mb-4">
            <div v-if="balanceText" class="flex items-center justify-between gap-3">
              <span class="text-xs text-ink-soft dark:text-[#A6A6AD]">{{ t('account_balance') }}</span>
              <span class="font-mono text-sm text-ink dark:text-[#EDEDF0]">{{ balanceText }}</span>
            </div>
            <div v-if="bonusText" class="flex items-center justify-between gap-3">
              <span class="text-xs text-ink-soft dark:text-[#A6A6AD]">{{ t('account_bonus') }}</span>
              <span class="font-mono text-sm text-ink dark:text-[#EDEDF0]">{{ bonusText }}</span>
            </div>
          </div>

          <!-- 等授权：授权链接（复制/跳转）+ 回调链接（粘回来完成） -->
          <div v-if="waiting" class="space-y-3 mb-4">
            <div>
              <label class="block text-xs text-ink-faint dark:text-[#8A8A92] mb-1">{{ t('account_login_link_label') }}</label>
              <input
                :value="authorizeUrl"
                readonly
                autocomplete="off"
                class="g-input w-full font-mono !text-xs"
                @focus="selectAll"
              />
              <div class="flex flex-wrap items-center gap-2 mt-2">
                <button class="g-btn-secondary !h-9" @click="copyAuthorizeLink">{{ t('account_login_copy_link') }}</button>
                <button class="g-btn-secondary !h-9" @click="openAuthorizePage">{{ t('account_login_open_link') }}</button>
              </div>
            </div>
            <div>
              <label class="block text-xs text-ink-faint dark:text-[#8A8A92] mb-1">{{ t('account_callback_label') }}</label>
              <input
                v-model="callback"
                :placeholder="t('account_callback_placeholder')"
                autocomplete="off"
                class="g-input w-full font-mono !text-xs"
                @keydown.enter="completeLogin"
              />
              <!-- 地址打不开是正常的这一步，就在这里说（授权流程中也要看得到） -->
              <p class="mt-1 text-xs font-medium text-warning dark:text-[#F59E0B] leading-relaxed">
                {{ t('account_desc_callback') }}
              </p>
            </div>
            <p class="text-xs text-ink-faint dark:text-[#8A8A92] leading-relaxed">{{ t('account_login_tip') }}</p>
          </div>

          <div v-if="!signedIn && !waiting && login && (login.phase === 'failed' || login.phase === 'expired')" class="mb-4">
            <p class="text-sm text-danger dark:text-[#EF4444] leading-relaxed">
              {{ uiText(login.message, login.messageRef, t('account_login_failed')) }}
            </p>
          </div>
          <div v-if="!signedIn && !waiting" class="mb-2">
            <p class="text-xs text-ink-faint dark:text-[#8A8A92] leading-relaxed">
              {{ t('account_desc') }}
            </p>
            <!-- 回调那一步（地址打不开是正常的）单独高亮：整段说明里最容易做错的一步 -->
            <p class="mt-1 text-xs font-medium text-warning dark:text-[#F59E0B] leading-relaxed">
              {{ t('account_desc_callback') }}
            </p>
          </div>

          <div class="g-dialog-actions">
            <template v-if="waiting">
              <button class="g-btn-secondary" :disabled="busy" @click="cancelLogin">{{ t('account_cancel') }}</button>
              <button class="g-btn-secondary" :disabled="busy" @click="completeLogin">
                {{ busy ? t('account_login_submitting') : t('account_login_submit') }}
              </button>
            </template>
            <template v-else>
              <button v-if="signedIn" class="g-btn-danger" :disabled="busy" @click="askLogout">{{ t('account_logout') }}</button>
              <!-- 「登录」与「添加账号」是同一套流程（授权链接 / 复制 / 跳转 / 回调链接） -->
              <button class="g-btn-secondary" :disabled="busy" @click="startLogin">
                {{ accounts.length ? t('account_add') : t('account_login') }}
              </button>
            </template>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>

  <ConfirmDialog
    v-model:visible="logoutVisible"
    :title="t('account_logout_confirm_title')"
    :message="t('account_logout_confirm_body')"
    :confirm-text="t('confirm_ok')"
    :cancel-text="t('confirm_cancel')"
    :confirm-loading="confirmBusy"
    danger
    @confirm="doLogout()"
  />
  <ConfirmDialog
    v-model:visible="removeVisible"
    :title="t('account_remove_confirm_title')"
    :message="t('account_remove_confirm_body', { contact: removeTarget ? accountLabel(removeTarget) : '' })"
    :confirm-text="t('confirm_ok')"
    :cancel-text="t('confirm_cancel')"
    :confirm-loading="confirmBusy"
    danger
    @confirm="doRemove()"
  />
</template>
