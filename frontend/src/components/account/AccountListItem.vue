<script setup lang="ts">
// 账号列表的一行：头像 + 名称/联系方式 + 「使用中」标记 / 切换 / 移除。
// 只负责展示与抛事件，状态与请求都在 stores/account.ts（卡片是唯一的使用方）。
import type { AccountEntryView } from '@/serverapi'
import { useI18n } from '@/composables/useI18n'
import AccountAvatar from '@/components/account/AccountAvatar.vue'

defineProps<{
  account: AccountEntryView
  label: string
  // 这一行正在切换/移除
  busy?: boolean
  // 整体不可操作（dsh 未就绪 / 另一行正在处理）
  disabled?: boolean
  // 当前是否已有生效账号：已退出登录时，这一行的「切换」实际就是「登录」
  signedIn?: boolean
}>()

const emit = defineEmits<{
  (e: 'switch'): void
  (e: 'remove'): void
}>()

const { t } = useI18n()
</script>

<template>
  <div class="flex items-center gap-3 py-2.5">
    <!-- 库里的每条都是已登录过的账号：头像与品牌色兜底图标都按「已登录」渲染 -->
    <AccountAvatar size="sm" signed-in :avatar-url="account.avatarUrl" />
    <div class="min-w-0 flex-1">
      <div class="text-sm text-ink dark:text-[#EDEDF0] truncate">{{ label }}</div>
      <!-- 名称与联系方式都有时才补一行：只有一个时它就是上面那行，别重复显示 -->
      <div
        v-if="account.name && account.contact"
        class="text-xs text-ink-faint dark:text-[#8A8A92] truncate"
      >
        {{ account.contact }}
      </div>
    </div>
    <div class="flex items-center gap-2 flex-shrink-0">
      <span v-if="account.active" class="g-status bg-success/10 text-success">
        {{ t('account_in_use') }}
      </span>
      <template v-else>
        <button
          class="g-btn-secondary !h-8 !px-3 !text-xs"
          :disabled="busy || disabled"
          @click="emit('switch')"
        >{{ signedIn ? t('account_switch') : t('account_login') }}</button>
        <button
          class="g-btn-danger !h-8 !px-3 !text-xs"
          :disabled="busy || disabled"
          @click="emit('remove')"
        >{{ t('account_remove') }}</button>
      </template>
    </div>
  </div>
</template>
