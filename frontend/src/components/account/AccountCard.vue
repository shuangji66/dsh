<script setup lang="ts">
// 概览页「DeepSeek 账号」卡片：官方账号的头像、余额与多账号管理都在这里。
//
// 之前账号只是「DeepSeek Harness」卡片右上角的一个小头像（点开一个什么都有的大弹窗）；
// 独立成卡片后职责拆成三块：
//   - 本卡片：当前账号概览（头像 / 名称 / 联系方式 / 余额）+ 账号列表（切换 / 移除 / 退出）；
//   - AddAccountDialog：只负责登录流程（发起授权 → 打开链接 → 粘回回调链接）；
//   - stores/account.ts：快照、轮询与所有请求（卡片与弹窗共用同一份状态）。
import { computed, onMounted, ref } from 'vue'
import type { AccountEntryView } from '@/serverapi'
import { useAccountStore } from '@/stores/account'
import { useI18n, uiText } from '@/composables/useI18n'
import { balanceLevel, type BalanceLevel } from '@/utils/money'
import AccountAvatar from '@/components/account/AccountAvatar.vue'
import AccountListItem from '@/components/account/AccountListItem.vue'
import AddAccountDialog from '@/components/account/AddAccountDialog.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'

const account = useAccountStore()
const { t } = useI18n()

// 余额档位配色（与 OverviewView 的 CPU/内存阈值同一套色）：≥10 元绿 / 3–10 元黄 /
// <3 元（含欠款）红；读不到余额时保持正文色，不给「—」上色误导用户。
const BALANCE_COLORS: Record<BalanceLevel, string> = {
  high: 'text-success dark:text-[#10B981]',
  mid: 'text-warning dark:text-[#F59E0B]',
  low: 'text-danger dark:text-[#EF4444]',
  unknown: 'text-ink dark:text-[#EDEDF0]'
}
const balanceColor = computed(() => BALANCE_COLORS[balanceLevel(account.status?.balance)])

const addVisible = ref(false)
const logoutVisible = ref(false)
const removeVisible = ref(false)
const removeTarget = ref<AccountEntryView | null>(null)

const initialLoading = computed(() => account.status === null && !account.refreshing)
const unavailableText = computed(() =>
  uiText(account.status?.message, account.status?.messageRef, t('account_unavailable'))
)

function openAdd() {
  addVisible.value = true
}

function askRemove(a: AccountEntryView) {
  removeTarget.value = a
  removeVisible.value = true
}

// 动作不能存在「弹窗被关闭时清掉」的状态里：ConfirmDialog 先 close 再 emit confirm，
// 所以 target 用独立 ref，handler 自己收尾（与仓库其它弹窗一致）。
async function onRemoveConfirm() {
  const target = removeTarget.value
  if (!target) return
  await account.removeAccount(target)
  removeVisible.value = false
  removeTarget.value = null
}

async function onLogoutConfirm() {
  await account.logout()
  logoutVisible.value = false
}

onMounted(() => {
  void account.init()
})
</script>

<template>
  <section class="g-card g-card-hover p-5 flex flex-col">
    <div class="flex items-start justify-between gap-3 mb-4">
      <div class="min-w-0">
        <h2 class="font-display text-base font-semibold text-ink dark:text-white">{{ t('account_title') }}</h2>
        <p class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1">{{ t('account_card_desc') }}</p>
      </div>
      <!-- 唯一的登录/添加入口：未登录且库里没有账号时是「登录」，否则是「添加账号」 -->
      <button
        class="g-btn-secondary !h-8 !px-3 !text-xs flex-shrink-0"
        :disabled="account.unavailable"
        @click="openAdd"
      >{{ account.accounts.length ? t('account_add') : t('account_login') }}</button>
    </div>

    <!-- 首屏还没拿到快照：与其它卡片一致，用一行占位而不是闪一下「未登录」 -->
    <div v-if="initialLoading" class="py-6 text-center text-sm text-ink-faint dark:text-[#8A8A92]">
      {{ t('loading') }}
    </div>

    <template v-else>
      <!-- 读不到 dsh 的账号状态（未就绪 / 已停止）：说明原因并给一个重试入口。
           此时不渲染「未登录」——读不到状态不等于没登录；账号列表仍来自控制台自己的
           账号库，照常显示（切换/移除按钮禁用）。 -->
      <div v-if="account.unavailable" class="flex items-start justify-between gap-3">
        <p class="text-sm text-danger dark:text-[#EF4444] leading-relaxed">{{ unavailableText }}</p>
        <button
          class="g-btn-secondary !h-7 !px-2 !text-xs flex-shrink-0"
          :disabled="account.refreshing"
          @click="account.refresh()"
        >{{ t('account_retry') }}</button>
      </div>

      <!-- 当前账号概览：大头像 + 名称/联系方式 + 余额 -->
      <div v-if="account.signedIn && !account.unavailable" class="flex items-center gap-3">
        <AccountAvatar size="lg" signed-in :avatar-url="account.status?.avatarUrl" />
        <div class="min-w-0 flex-1">
          <div class="text-sm font-semibold text-ink dark:text-[#EDEDF0] truncate">
            {{ account.status?.name || t('account_signed_in') }}
          </div>
          <div v-if="account.status?.contact" class="text-xs text-ink-faint dark:text-[#8A8A92] truncate">
            {{ account.status.contact }}
          </div>
          <!-- 余额无条件显示（0 也显示成 ¥0.00）；平台没给余额时退到「—」——
               读不到的余额绝不伪造成 0.00（见 utils/money.ts）。金额按档位着色
               （≥10 元绿 / 3–10 元黄 / <3 元与欠款红），赠送金为 0 时不显示、显示时恒绿。 -->
          <div class="mt-1.5 flex flex-wrap gap-1.5">
            <span class="g-chip">
              {{ t('account_balance') }}
              <span class="font-mono" :class="balanceColor">{{ account.balanceText || '—' }}</span>
            </span>
            <span v-if="account.bonusText" class="g-chip">
              {{ t('account_bonus') }}
              <span class="font-mono text-success dark:text-[#10B981]">{{ account.bonusText }}</span>
            </span>
          </div>
        </div>
      </div>

      <!-- 未登录：灰色占位头像 + 说明 + 登录入口 -->
      <div v-else-if="!account.unavailable" class="flex items-center gap-3">
        <AccountAvatar size="lg" />
        <div class="min-w-0 flex-1">
          <div class="text-sm font-semibold text-ink dark:text-[#EDEDF0]">{{ t('account_signed_out') }}</div>
          <p class="mt-0.5 text-xs text-ink-faint dark:text-[#8A8A92] leading-relaxed">
            {{ t('account_signed_out_desc') }}
          </p>
        </div>
      </div>

      <!-- 账号列表：多账号的切换/移除都在这里（当前生效的那条只显示「使用中」） -->
      <div v-if="account.accounts.length" class="mt-4 pt-4 border-t border-[#E8E8EC] dark:border-[#2A2A32]">
        <div class="flex items-center justify-between gap-2 mb-1">
          <div class="text-xs text-ink-faint dark:text-[#8A8A92]">{{ t('account_list_title') }}</div>
          <button
            v-if="account.signedIn"
            class="text-xs text-ink-soft dark:text-[#A6A6AD] hover:text-danger dark:hover:text-[#EF4444] transition-colors"
            :disabled="account.confirmBusy"
            @click="logoutVisible = true"
          >{{ t('account_logout') }}</button>
        </div>
        <div class="divide-y divide-[#E8E8EC] dark:divide-[#2A2A32]">
          <AccountListItem
            v-for="a in account.accounts"
            :key="a.id"
            :account="a"
            :label="account.accountLabel(a)"
            :busy="account.rowBusy === a.id"
            :disabled="account.unavailable || !!account.rowBusy"
            :signed-in="account.signedIn"
            @switch="account.switchAccount(a)"
            @remove="askRemove(a)"
          />
        </div>
      </div>
    </template>

    <AddAccountDialog v-model:visible="addVisible" />

    <ConfirmDialog
      v-model:visible="logoutVisible"
      :title="t('account_logout_confirm_title')"
      :message="t('account_logout_confirm_body')"
      :confirm-text="t('confirm_ok')"
      :cancel-text="t('confirm_cancel')"
      :confirm-loading="account.confirmBusy"
      danger
      @confirm="onLogoutConfirm"
    />
    <ConfirmDialog
      v-model:visible="removeVisible"
      :title="t('account_remove_confirm_title')"
      :message="t('account_remove_confirm_body', { contact: removeTarget ? account.accountLabel(removeTarget) : '' })"
      :confirm-text="t('confirm_ok')"
      :cancel-text="t('confirm_cancel')"
      :confirm-loading="account.confirmBusy"
      danger
      @confirm="onRemoveConfirm"
    />
  </section>
</template>
