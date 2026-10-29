<script setup lang="ts">
// 「添加 DeepSeek 账号」弹窗：打开即申请一条新的授权链接，用户在浏览器里完成授权后
// 把回调链接粘回来。账号列表、切换/移除/退出都在账号卡片里（见 AccountCard.vue）。
//
// 每次打开都重新发起授权（不展示「先点获取链接」的中间层）：上一次的链接可能已过期、
// 也可能已经被别的浏览器用过；后端在 auth_init 时会顺手取消上一条在途尝试。
// 授权结果可能在另一个浏览器里完成，所以轮询与提示都在 store 里，弹窗只管关闭自己。
import { computed, ref, watch } from 'vue'
import { useAccountStore } from '@/stores/account'
import { useI18n, uiText } from '@/composables/useI18n'
import { useBodyScrollLock } from '@/composables/useBodyScrollLock'
import { useDialogA11y } from '@/composables/useDialogA11y'
import { useToastStore } from '@/stores/toast'
import { copyText } from '@/utils/clipboard'
import DialogCloseButton from '@/components/DialogCloseButton.vue'

const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ (e: 'update:visible', v: boolean): void }>()

const account = useAccountStore()
const { t } = useI18n()
const toast = useToastStore()

const callback = ref('')
// 本次弹窗里是否真的进入过「等待授权」。后端会保留上一次尝试的结果，旧的成功状态
// 不能当成这次的结果（否则弹窗一打开就会被下面的 watch 关掉）。
const sawWaiting = ref(false)
// 申请授权链接失败（init 失败 / 网络）：留在弹窗里给「重试」，不要静默退化成空白。
const startFailed = ref(false)

// 正在申请链接（完成登录期间 waiting 仍为真，不算这个状态）
const starting = computed(() => account.busy && !account.waiting)
const failed = computed(
  () =>
    startFailed.value ||
    account.login?.phase === 'failed' ||
    account.login?.phase === 'expired'
)
const failedText = computed(() =>
  startFailed.value
    ? t('account_login_failed')
    : uiText(account.login?.message, account.login?.messageRef, t('account_login_failed'))
)

useBodyScrollLock(() => props.visible)

const panel = ref<HTMLElement | null>(null)
useDialogA11y(() => props.visible, panel, { onClose: close })

async function begin() {
  startFailed.value = false
  if (!(await account.startLogin())) startFailed.value = true
}

// 每次打开都从零开始：清掉旧状态并立刻申请新链接。
async function onOpen() {
  callback.value = ''
  sawWaiting.value = false
  startFailed.value = false
  account.clearLogin()
  await begin()
}

watch(
  () => props.visible,
  (v) => {
    if (v) void onOpen()
  }
)

watch(
  () => account.waiting,
  (w) => {
    if (!w) return
    sawWaiting.value = true
    // 上一次申请失败的错误提示不能和新的授权链接同时出现（例如重试/重新打开时
    // 前一次请求还在路上，返回后刚好拿到链接）。
    startFailed.value = false
  }
)

// 授权在另一处完成时（store 轮询）也把弹窗收掉——不让用户对着「等待授权」干等。
// 只认本次真的等过授权的那次成功，旧的 succeeded 不算（见 sawWaiting）。
watch(
  () => account.login?.phase,
  (phase) => {
    if (phase === 'succeeded' && props.visible && sawWaiting.value) close()
  }
)

function close() {
  sawWaiting.value = false
  emit('update:visible', false)
}

async function complete() {
  if (await account.completeLogin(callback.value)) {
    callback.value = ''
    close()
  }
}

async function cancel() {
  await account.cancelLogin()
  close()
}

function copyAuthorizeLink() {
  if (!account.authorizeUrl) return
  copyText(account.authorizeUrl)
  toast.show(t('account_link_copied'), 'success')
}

function openAuthorizePage() {
  if (!account.authorizeUrl) return
  // 跳转只在用户点按钮时发生（弹窗自己不自动开标签页，避免被拦截/吓到用户）
  window.open(account.authorizeUrl, '_blank', 'noopener')
}

// 授权链接文本框：点一下就全选，方便手动复制（剪贴板按钮另有兜底实现）。
function selectAll(e: FocusEvent) {
  const el = e.target as HTMLInputElement | null
  el?.select()
}
</script>

<template>
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
        <div class="g-modal-mask" @click="close"></div>
        <!-- 比默认弹窗略宽：授权链接是长串 URL，窄了只能看到尾部；限高后内部滚动，
             保证小屏（含手机键盘弹起时）底部按钮仍可触达。 -->
        <div
          ref="panel"
          tabindex="-1"
          role="dialog"
          aria-modal="true"
          :aria-label="t('account_add_dialog_title')"
          class="relative w-full max-w-md max-h-[calc(100dvh-2rem)] overflow-y-auto bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6 outline-none"
        >
          <DialogCloseButton :label="t('dialog_close')" @close="close" />
          <h3 class="g-dialog-title mb-2">{{ t('account_add_dialog_title') }}</h3>
          <p class="text-sm text-ink-soft dark:text-[#A6A6AD] leading-relaxed">
            {{ t('account_desc') }}
          </p>

          <!-- 申请链接中：通常只闪一下；失败时改用下面的错误 + 重试 -->
          <div
            v-if="starting"
            class="mt-5 flex items-center gap-2 text-sm text-ink-soft dark:text-[#A6A6AD]"
          >
            <span class="inline-block animate-spin h-5 w-5 border-2 border-brand border-t-transparent rounded-full"></span>
            {{ t('account_login_starting') }}
          </div>

          <!-- 上一次尝试失败 / 过期 / 本次申请失败：留在原地说明原因，用户可以重试 -->
          <p v-else-if="failed" class="mt-3 text-sm text-danger-ink leading-relaxed">
            {{ failedText }}
          </p>

          <!-- 拿到授权链接：两步流程，编号 + 标题 + 操作 -->
          <div v-if="account.waiting" class="mt-5 space-y-4">
            <section class="flex gap-3">
              <span
                class="mt-0.5 w-6 h-6 rounded-full flex items-center justify-center flex-shrink-0 text-xs font-semibold bg-brand/10 text-brand"
              >1</span>
              <div class="min-w-0 flex-1">
                <div class="text-sm font-medium text-ink dark:text-white">{{ t('account_step_authorize') }}</div>
                <p class="mt-0.5 text-xs text-ink-faint dark:text-[#8A8A92] leading-relaxed">
                  {{ t('account_step_authorize_hint') }}
                </p>
                <!-- 不要用带 !important 的 !text-xs：触屏设备上它会压过 style.css 里
                     「input 字号钉在 16px」的规则，iOS 聚焦时会把整页放大。text-xs 只在
                     桌面生效，pointer: coarse 时由那条全局规则抬到 16px。 -->
                <input
                  :value="account.authorizeUrl"
                  readonly
                  autocomplete="off"
                  class="g-input w-full font-mono text-xs mt-2"
                  @focus="selectAll"
                />
                <div class="flex flex-wrap items-center gap-2 mt-2">
                  <button class="g-btn-secondary !h-9" data-autofocus @click="openAuthorizePage">
                    {{ t('account_login_open_link') }}
                  </button>
                  <button class="g-btn-secondary !h-9" @click="copyAuthorizeLink">
                    {{ t('account_login_copy_link') }}
                  </button>
                </div>
              </div>
            </section>

            <section class="flex gap-3">
              <span
                class="mt-0.5 w-6 h-6 rounded-full flex items-center justify-center flex-shrink-0 text-xs font-semibold bg-brand/10 text-brand"
              >2</span>
              <div class="min-w-0 flex-1">
                <div class="text-sm font-medium text-ink dark:text-white">{{ t('account_step_callback') }}</div>
                <p class="mt-0.5 text-xs text-ink-faint dark:text-[#8A8A92] leading-relaxed">
                  {{ t('account_step_callback_hint') }}
                </p>
                <input
                  v-model="callback"
                  :placeholder="t('account_callback_placeholder')"
                  autocomplete="off"
                  class="g-input w-full font-mono text-xs mt-2"
                  @keydown.enter="complete"
                />
                <!-- 地址打不开是正常的这一步，就在这里说（授权流程中也要看得到） -->
                <p class="mt-1.5 text-xs font-medium text-warning-ink dark:text-[#F59E0B] leading-relaxed">
                  {{ t('account_desc_callback') }}
                </p>
              </div>
            </section>

            <p class="text-xs text-ink-faint dark:text-[#8A8A92] leading-relaxed">
              {{ t('account_login_tip') }}
            </p>
          </div>

          <div class="g-dialog-actions">
            <template v-if="account.waiting">
              <button class="g-btn-secondary" :disabled="account.busy" @click="cancel">
                {{ t('account_cancel') }}
              </button>
              <button
                class="g-btn-secondary"
                :disabled="account.busy || !callback.trim()"
                @click="complete"
              >
                {{ account.busy ? t('account_login_submitting') : t('account_login_submit') }}
              </button>
            </template>
            <template v-else>
              <button class="g-btn-secondary" @click="close">{{ t('account_cancel') }}</button>
              <button v-if="!account.busy" class="g-btn-secondary" @click="begin">
                {{ t('account_retry') }}
              </button>
            </template>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
