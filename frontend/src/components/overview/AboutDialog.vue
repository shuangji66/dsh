<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from '@/composables/useI18n'
import { useBodyScrollLock } from '@/composables/useBodyScrollLock'
import { useDialogA11y } from '@/composables/useDialogA11y'
import DialogCloseButton from '@/components/DialogCloseButton.vue'
import GithubIconLink from '@/components/GithubIconLink.vue'
import { repoSlugs, repoURL } from '@/constants/repos'

// 概览页「关于」弹窗：只读信息面板，没有任何可执行动作。
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ (e: 'update:visible', v: boolean): void }>()

const { t } = useI18n()

useBodyScrollLock(() => props.visible)

const panel = ref<HTMLElement | null>(null)
useDialogA11y(() => props.visible, panel, { onClose: close })

// 特性介绍按类别分组，条目为简短词组，对应 useI18n 里的 about_group_* / about_feature_* 键。
const aboutFeatureGroups = [
  {
    title: 'about_group_runtime',
    features: [
      'about_feature_lifecycle',
      'about_feature_monitor',
      'about_feature_update',
      'about_feature_backup',
    ],
  },
  {
    title: 'about_group_access',
    // 账号与「凭据/快捷指令加密存储」都归在这里：它们解决的正是「谁拿着这台设备上的凭据」
    // （账号见 components/account/AccountCard.vue / backend/account.go，加密见 backend/secret.go 的威胁模型）。
    features: [
      'about_feature_auth',
      'about_feature_account',
      'about_feature_secrets',
      'about_feature_proxy',
      'about_feature_dirs',
    ],
  },
  {
    title: 'about_group_tools',
    features: [
      'about_feature_plugins',
      // 终端功能可在编译期整体剔除（TERMINAL=0）：介绍里也不再列它。
      ...(__TERMINAL_ENABLED__ ? (['about_feature_terminal'] as const) : []),
      'about_feature_logs',
      'about_feature_console',
    ],
  },
] as const

function openGithubUser(user: string) {
  window.open(`https://github.com/${user}`, '_blank', 'noopener')
}

function close() {
  emit('update:visible', false)
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
      <div v-if="props.visible" class="fixed inset-0 z-50 flex items-center justify-center p-4 backdrop-blur-sm">
        <div class="g-modal-mask" @click="close"></div>
        <!-- 比默认弹窗略宽：特性按类别双列排布，窄了会大量折行 -->
        <div
          ref="panel"
          tabindex="-1"
          role="dialog"
          aria-modal="true"
          :aria-label="t('about_title')"
          class="relative w-full max-w-md bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6 outline-none"
        >
          <DialogCloseButton :label="t('dialog_close')" @close="close" />
          <!-- 标题 + 本仓库的 GitHub 裸图标（紧靠标题；右侧留出 X 的位置） -->
          <div class="flex items-center gap-2 pr-11 mb-5">
            <h3 class="g-dialog-title !pr-0">{{ t('about_title') }}</h3>
            <GithubIconLink :href="repoURL(repoSlugs.harness)" :label="t('about_github')" />
          </div>

          <!-- 特性介绍：按类别分组，条目双列排布（窄弹窗里省高度） -->
          <div class="mb-5">
            <div class="text-sm font-medium text-ink dark:text-white mb-2.5">{{ t('about_features') }}</div>
            <div v-for="g in aboutFeatureGroups" :key="g.title" class="mb-3 last:mb-0">
              <div class="text-xs font-medium text-ink-faint dark:text-[#8A8A92] mb-1.5">{{ t(g.title) }}</div>
              <ul class="grid grid-cols-2 gap-x-3 gap-y-1.5">
                <li v-for="f in g.features" :key="f" class="flex items-start gap-1.5">
                  <svg class="mt-0.5 w-3 h-3 text-brand shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                  <span class="text-xs text-ink-soft dark:text-[#A6A6AD] leading-snug">{{ t(f) }}</span>
                </li>
              </ul>
            </div>
          </div>

          <!-- 鸣谢 -->
          <div>
            <div class="text-sm font-medium text-ink dark:text-white mb-2">{{ t('about_credits') }}</div>
            <p class="text-xs text-ink-soft dark:text-[#A6A6AD] leading-relaxed">
              {{ t('about_credits_pre') }}<button class="inline text-brand hover:underline" @click="openGithubUser('yuexps')">yuexps</button>{{ t('about_credits_suf') }}
            </p>
          </div>

          <!-- 关闭入口只有右上角的 X（底部不再放「关闭」按钮） -->
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
