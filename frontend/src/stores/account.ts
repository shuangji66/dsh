// stores/account.ts —— DeepSeek 官方账号的控制台侧状态（多账号列表 + 登录流程）。
//
// 登录/退出都在控制台后端完成（见 backend/account.go）—— 平台只接受 loopback 上的
// /oauth/callback 回调，dsh 自带的账号入口在本部署用不了。这里只负责：
//   - 拉取/刷新账号快照（当前账号资料、余额、账号库列表）；
//   - 发起登录、轮询在途授权、完成登录（用户把回调链接粘回来）；
//   - 切换 / 移除 / 退出登录，并在动作后等 dsh 认账再返回。
//
// 状态放在 Pinia 里（而不是概览页组件或单个弹窗里）：账号卡片与「添加账号」弹窗是
// 两个组件，二者必须看到**同一份**状态；轮询也要在弹窗关掉后继续跑（用户可能在
// 另一个浏览器里完成授权，回来时卡片上的头像/余额应该已经更新）。
import { computed, ref, watch } from 'vue'
import { defineStore } from 'pinia'
import { api, type AccountEntryView, type AccountLoginState, type AccountStatusPayload } from '@/serverapi'
import { uiErrText, uiText, useI18n } from '@/composables/useI18n'
import { useToastStore } from '@/stores/toast'
import { formatNonZeroWallets, formatWallets } from '@/utils/money'

export const useAccountStore = defineStore('account', () => {
  const { t, locale } = useI18n()
  const toast = useToastStore()

  const status = ref<AccountStatusPayload | null>(null)
  const login = ref<AccountLoginState | null>(null)
  // busy：发起登录 / 完成登录在途（弹窗按钮的 loading）
  const busy = ref(false)
  // rowBusy：正在切换/移除的那个账号 id（只禁用那一行，不锁整张卡片）
  const rowBusy = ref('')
  const confirmBusy = ref(false)
  const refreshing = ref(false)
  let pollTimer: number | undefined

  // 账号列表（多账号）：切换只是把另一份 token 写进 dsh 的凭据记录，列表由后端推导 active。
  const accounts = computed<AccountEntryView[]>(() => status.value?.accounts ?? [])
  const signedIn = computed(() => status.value?.signedIn === true)
  // status 还没拉到时不算「不可用」——否则首屏会闪一下错误提示。
  const unavailable = computed(() => status.value !== null && !status.value.available)
  const waiting = computed(() => login.value?.phase === 'waiting')
  const authorizeUrl = computed(() => login.value?.authorizeUrl ?? '')

  // 余额/赠送金：格式规则抄 dsh 自己的账号面板（见 utils/money.ts），否则同一个账号
  // 在 dsh 与控制台里会显示成两个数。**余额无论多少都要显示**（0 也是 ¥0.00）；
  // 赠送金为 0 的那一项不显示，全是 0（或平台没给）时格式化成空串，卡片据此整块隐藏。
  const balanceText = computed(() => formatWallets(status.value?.balance))
  const bonusText = computed(() => formatNonZeroWallets(status.value?.bonus))

  function accountLabel(a: AccountEntryView) {
    return a.name || a.contact || t('account_unknown')
  }

  // 发起方身份：平台侧只用于记录（语言/时区），版本留空由后端补控制台版本。
  function clientMeta() {
    return {
      version: '',
      locale: locale.value,
      timezoneOffsetSeconds: -new Date().getTimezoneOffset() * 60
    }
  }

  async function refresh() {
    refreshing.value = true
    try {
      status.value = await api.account()
    } catch {
      // 后端不可达（控制台重启中）时按「读不到账号状态」渲染，不把上个快照当成现状
      status.value = { available: false, signedIn: false }
    } finally {
      refreshing.value = false
    }
  }

  // 接上已有的一次在途授权（刷新页面 / 在别的浏览器里发起的）。拿不到就保持现状。
  async function syncLogin() {
    try {
      login.value = await api.accountLoginStatus()
    } catch {
      /* 后端不可达：保留当前状态，下一次再同步 */
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
  function startPoll() {
    if (pollTimer !== undefined) return
    pollTimer = window.setInterval(() => void pollLogin(), 1500)
  }

  watch(waiting, (w) => {
    if (w) startPoll()
    else stopPoll()
  })

  // 采纳一次登录状态：phase 从「非成功」变成「成功」时提示并刷新账号快照。
  // 轮询与「完成登录」共用这一条路径，因此不会重复弹 toast（先后到达时只有第一次
  // 的 prev 不是 succeeded）。
  async function applyLogin(st: AccountLoginState, announce = true) {
    const prev = login.value?.phase
    login.value = st
    if (st.phase !== 'waiting') stopPoll()
    if (announce && st.phase === 'succeeded' && prev !== 'succeeded') {
      toast.show(uiText(st.message, st.messageRef, t('account_login_done')), 'success')
      await refresh()
    }
  }

  async function pollLogin() {
    try {
      await applyLogin(await api.accountLoginStatus())
    } catch {
      /* 轮询失败等下一次（后端重启/页面刷新都不该打断用户） */
    }
  }

  // 打开「添加账号」时调用：丢掉本地残留的尝试状态（随后一律重新发起授权，
  // 旧链接可能已过期或被别的浏览器用过；后端会顺手取消上一条在途尝试）。
  function clearLogin() {
    login.value = null
    stopPoll()
  }

  // 发起授权：返回是否拿到了等待授权的链接（失败时已弹 toast，弹窗据此显示重试）。
  async function startLogin() {
    if (busy.value) return false
    busy.value = true
    try {
      await applyLogin(
        await api.accountLoginStart({
          callbackOrigin: window.location.origin,
          ...clientMeta()
        }),
        false
      )
      return login.value?.phase === 'waiting'
    } catch (e) {
      toast.show(uiErrText(e, t('account_login_failed')), 'error')
      return false
    } finally {
      busy.value = false
    }
  }

  // 完成登录：返回是否已成功（弹窗据此关闭；失败时后端给出 code，由 uiErrText 翻译）。
  async function completeLogin(url: string) {
    const link = url.trim()
    if (!link) {
      toast.show(t('account_login_need_url'), 'error')
      return false
    }
    busy.value = true
    try {
      await applyLogin(await api.accountLoginComplete({ url: link, ...clientMeta() }))
      return login.value?.phase === 'succeeded'
    } catch (e) {
      toast.show(uiErrText(e, t('account_login_failed')), 'error')
      return false
    } finally {
      busy.value = false
    }
  }

  async function cancelLogin() {
    try {
      await applyLogin(await api.accountLoginCancel(), false)
    } catch {
      /* 取消失败无所谓：下一次登录会替换掉它 */
    }
    stopPoll()
  }

  // 后端在退出登录/切换/移除时都会等 dsh 认账再返回快照（见 account.go），这里再兜一层：
  // dsh 的文件监听偶尔更慢，不能让界面停在旧状态等用户手动刷新。
  async function waitFor(expect: (s: AccountStatusPayload) => boolean) {
    for (let i = 0; i < 5; i++) {
      const cur = status.value
      if (cur && expect(cur)) return
      await new Promise((r) => window.setTimeout(r, 400))
      try {
        status.value = await api.account()
      } catch {
        return
      }
    }
  }

  async function logout() {
    confirmBusy.value = true
    try {
      status.value = await api.accountLogout()
      login.value = null
      toast.show(t('account_logout_done'), 'success')
      await waitFor((s) => !s.signedIn)
    } catch (e) {
      toast.show(uiErrText(e, t('account_logout_failed')), 'error')
    } finally {
      confirmBusy.value = false
    }
  }

  // 切换账号：把该账号的凭据变成 dsh 正在用的那份（后端写完会等 dsh 认账）。
  async function switchAccount(a: AccountEntryView) {
    if (rowBusy.value) return
    // 退出登录后点这一行等于「用这个账号登录」，提示语也要跟着变
    const wasSignedOut = !signedIn.value
    rowBusy.value = a.id
    try {
      status.value = await api.accountSwitch(a.id)
      toast.show(
        t(wasSignedOut ? 'account_switch_login_done' : 'account_switch_done', { contact: accountLabel(a) }),
        'success'
      )
      await waitFor((s) => (s.accounts ?? []).some((x) => x.id === a.id && x.active))
    } catch (e) {
      toast.show(uiErrText(e, t('account_switch_failed')), 'error')
    } finally {
      rowBusy.value = ''
    }
  }

  // 移除账号：本机凭据删除 + 请平台作废 token（退出登录只是「不再使用」，仍留着可切回）。
  async function removeAccount(target: AccountEntryView) {
    confirmBusy.value = true
    rowBusy.value = target.id
    try {
      status.value = await api.accountRemove(target.id)
      login.value = null
      toast.show(t('account_removed', { contact: accountLabel(target) }), 'success')
      await waitFor((s) => !(s.accounts ?? []).some((x) => x.id === target.id))
      await refresh() // 余额/联系方式来自当前账号资料，移除后一并刷新
    } catch (e) {
      toast.show(uiErrText(e, t('account_remove_failed')), 'error')
    } finally {
      confirmBusy.value = false
      rowBusy.value = ''
    }
  }

  // 概览页挂载时调用：拉快照 + 接上可能存在的在途授权。
  async function init() {
    await Promise.all([refresh(), syncLogin()])
  }

  return {
    status,
    login,
    busy,
    rowBusy,
    confirmBusy,
    refreshing,
    accounts,
    signedIn,
    unavailable,
    waiting,
    authorizeUrl,
    balanceText,
    bonusText,
    accountLabel,
    init,
    refresh,
    syncLogin,
    clearLogin,
    startLogin,
    completeLogin,
    cancelLogin,
    logout,
    switchAccount,
    removeAccount
  }
})
