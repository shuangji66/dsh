<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { useSettingsStore } from '@/stores/settings'
import { useToastStore } from '@/stores/toast'
import { useI18n, setLocale } from '@/composables/useI18n'
import { useTheme } from '@/composables/useTheme'
import { useConsolePrefs, type DefaultPage } from '@/composables/useConsolePrefs'

const store = useSettingsStore()
const { config, locked, loading } = storeToRefs(store)
const toast = useToastStore()
const { t } = useI18n()
const { themeMode, setTheme } = useTheme()
const { locale } = useI18n()
const { defaultPage, setDefaultPage } = useConsolePrefs()

// 访问密码明文/密文切换
const showPassword = ref(false)

// 控制台设置变更：实时生效，仅保存在浏览器 localStorage，不持久化到后端
function onThemeChange() {
  setTheme(themeMode.value)
}
function onLanguageChange() {
  setLocale(locale.value)
}
function onDefaultPageChange() {
  setDefaultPage(defaultPage.value as DefaultPage)
}

// 安全标点：与后端 auth.go 的 safePunctStr 保持一致
const SAFE_PUNCT = new Set('.,-_:/@%^=+~')

// 前端密码校验：至少 8 位，且必须同时含字母、数字、标点；
// 危险标点（ASCII 中不属于安全集合的字符）会拒绝，非 ASCII 字符允许。
function validatePassword(pwd: string): string {
  if (pwd.length < 8) return '密码长度不足 8 位'
  let hasLetter = false
  let hasDigit = false
  let hasPunct = false
  for (const ch of pwd) {
    if (ch >= '0' && ch <= '9') { hasDigit = true; continue }
    if ((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')) { hasLetter = true; continue }
    if (SAFE_PUNCT.has(ch)) { hasPunct = true; continue }
    if (ch.charCodeAt(0) < 128) return `含危险标点 "${ch}"（仅允许 . , - _ : / @ % ^ = + ~）`
  }
  if (!hasLetter) return '密码缺少字母'
  if (!hasDigit) return '密码缺少数字'
  if (!hasPunct) return '密码缺少标点（仅允许 . , - _ : / @ % ^ = + ~）'
  return ''
}

// 保存前校验：只要填写了密码，就必须满足强度要求（≥8位，含字母、数字、标点）
function submit() {
  const pwd = config.value.password
  if (pwd) {
    const err = validatePassword(pwd)
    if (err) {
      toast.show(err, 'error')
      return
    }
  }
  store.save()
}

onMounted(() => store.load())

// --- dsh 访问地址（DynamicList 风格） ---
const newAccessUrl = ref('')

// 确保 config.accessUrls 为数组
function ensureAccessUrls() {
  if (!Array.isArray(config.value.accessUrls)) config.value.accessUrls = []
}

// 添加一个访问地址并自动保存（每添加一个就刷新概览页数据）
function addAccessUrl() {
  const url = newAccessUrl.value.trim()
  if (!url) return
  ensureAccessUrls()
  config.value.accessUrls.push(url)
  newAccessUrl.value = ''
  store.save()
}

// 移除一个访问地址
function removeAccessUrl(idx: number) {
  ensureAccessUrls()
  config.value.accessUrls.splice(idx, 1)
  store.save()
}

// --- 开关即时保存 ---

// 代理开关：即时保存，提示需重启 dsh 生效
function onProxyToggle() {
  store.save(false)
  toast.show(t('saved_proxy_restart'), 'info', 5000)
}

// 栈内存自动设置开关：即时保存，提示需重启 dsh 生效
function onMemAutoToggle() {
  store.save(false)
  toast.show(t('saved_mem_restart'), 'info', 5000)
}

// 鉴权登录开关：即时保存，仅提示切换成功
function onAuthToggle() {
  store.save(false)
  toast.show(t('settings_toggle_saved'), 'success')
}
</script>

<template>
  <div class="py-8 sm:py-12 px-4 sm:px-8 max-w-3xl mx-auto">
    <!-- 页头 -->
    <header class="flex items-center justify-between mb-8 flex-wrap gap-3">
      <p class="text-ink-faint dark:text-[#8A8A92] text-sm font-medium uppercase tracking-widest">{{ t('settings_title') }}</p>
      <button class="g-btn-primary" :disabled="loading" @click="submit()">{{ t('settings_save') }}</button>
    </header>

    <!-- 反向代理与鉴权 -->
    <section class="g-card g-card-hover p-6 mb-6">
      <div class="divide-y divide-line dark:divide-[#2A2A32]">
        <!-- 启用外部代理（开关）+ 代理地址（同组，分隔线移除） -->
        <div>
          <label class="flex items-center justify-between py-4 cursor-pointer">
            <span class="text-sm font-medium text-ink dark:text-[#EDEDF0]">{{ t('settings_enable_proxy') }}</span>
            <div class="relative inline-flex items-center">
              <input type="checkbox" v-model="config.proxyEnabled" class="sr-only peer" @change="onProxyToggle">
              <div class="w-11 h-6 bg-[#E8E8EC] dark:bg-[#2A2A32] rounded-full peer peer-checked:bg-brand transition-colors"></div>
              <div class="absolute left-0.5 top-0.5 w-5 h-5 bg-white rounded-full transition-transform peer-checked:translate-x-5"></div>
            </div>
          </label>

          <div v-if="config.proxyEnabled" class="py-4">
            <label class="block text-sm text-ink-soft dark:text-[#A6A6AD] mb-1.5">{{ t('settings_proxy_addr') }}</label>
            <input v-model="config.proxyAddr" :placeholder="t('settings_proxy_addr')" class="g-input" />
            <p v-if="!config.proxyAddr" class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1.5">
              {{ t('settings_proxy_hint') }}
            </p>
          </div>
        </div>

        <div class="py-4">
          <label class="block text-sm text-ink-soft dark:text-[#A6A6AD] mb-1.5">{{ t('settings_dsh_port') }}</label>
          <input
            v-model.number="config.dshPort"
            type="number"
            min="1"
            max="65535"
            :disabled="locked"
            class="g-input disabled:cursor-not-allowed"
          />
          <p class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1.5">{{ t('settings_dsh_port_hint') }}</p>
        </div>

        <!-- node栈内存限制（MB） -->
        <div class="py-4">
          <label class="block text-sm text-ink-soft dark:text-[#A6A6AD] mb-1.5">
            {{ t('settings_dsh_mem_limit') }} <span class="text-ink-faint">{{ t('settings_dsh_mem_mb') }}</span>
          </label>
          <div class="flex items-center gap-2">
            <input
              v-model.number="config.dshMemLimit"
              type="number"
              min="1"
              max="65536"
              :disabled="config.dshMemAuto"
              class="g-input max-w-40 disabled:cursor-not-allowed"
            />
            <span class="text-sm text-ink-soft dark:text-[#A6A6AD]">MB</span>
          </div>
          <!-- 自动设置开关：关闭时传 NODE_OPTIONS，由系统 node 自动分配内存 -->
          <div class="flex items-center justify-between gap-3 mt-3">
            <span class="text-sm text-ink dark:text-[#EDEDF0]">{{ t('settings_dsh_mem_auto') }}</span>
            <label class="relative inline-flex items-center cursor-pointer">
              <input type="checkbox" v-model="config.dshMemAuto" class="sr-only peer" @change="onMemAutoToggle">
              <div class="w-11 h-6 bg-[#E8E8EC] dark:bg-[#2A2A32] rounded-full peer peer-checked:bg-brand transition-colors"></div>
              <div class="absolute left-0.5 top-0.5 w-5 h-5 bg-white rounded-full transition-transform peer-checked:translate-x-5"></div>
            </label>
          </div>
          <p class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1.5">
            {{ config.dshMemAuto ? t('settings_dsh_mem_auto_hint') : t('settings_dsh_mem_hint') }}
          </p>
        </div>

        <!-- 启用登录鉴权（开关）+ 密码 + 登录有效期（同组，分隔线移除） -->
        <div>
          <label class="flex items-center justify-between py-4 cursor-pointer">
            <span class="text-sm font-medium text-ink dark:text-[#EDEDF0]">{{ t('settings_enable_auth') }}</span>
            <div class="relative inline-flex items-center">
              <input type="checkbox" v-model="config.authEnabled" class="sr-only peer" @change="onAuthToggle">
              <div class="w-11 h-6 bg-[#E8E8EC] dark:bg-[#2A2A32] rounded-full peer peer-checked:bg-brand transition-colors"></div>
              <div class="absolute left-0.5 top-0.5 w-5 h-5 bg-white rounded-full transition-transform peer-checked:translate-x-5"></div>
            </div>
          </label>

          <div class="py-4">
            <label class="block text-sm text-ink-soft dark:text-[#A6A6AD] mb-1.5">
              {{ t('settings_password') }} <span class="text-ink-faint">{{ t('settings_password_hint') }}</span>
            </label>
            <div class="relative">
              <input
                :type="showPassword ? 'text' : 'password'"
                v-model="config.password"
                :placeholder="t('settings_password_placeholder')"
                class="g-input pr-10"
              />
              <button
                type="button"
                @click="showPassword = !showPassword"
                class="absolute right-1.5 top-1/2 -translate-y-1/2 w-7 h-7 flex items-center justify-center rounded-md text-ink-soft dark:text-[#A6A6AD] hover:text-ink dark:hover:text-white hover:bg-black/5 dark:hover:bg-white/5 transition-colors"
                :title="showPassword ? t('settings_hide_password') : t('settings_show_password')"
              >
                <svg v-if="showPassword" xmlns="http://www.w3.org/2000/svg" class="w-4.5 h-4.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94"/><path d="M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19"/><line x1="1" y1="1" x2="23" y2="23"/></svg>
                <svg v-else xmlns="http://www.w3.org/2000/svg" class="w-4.5 h-4.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
              </button>
            </div>
          </div>

          <!-- 登录有效期（小时） -->
          <div class="py-4">
            <label class="block text-sm text-ink-soft dark:text-[#A6A6AD] mb-1.5">
              {{ t('settings_auth_ttl') }} <span class="text-ink-faint">{{ t('settings_auth_ttl_hour') }}</span>
            </label>
            <div class="flex items-center gap-2">
              <input
                v-model.number="config.authTTLHours"
                type="number"
                min="1"
                max="720"
                step="1"
                class="g-input max-w-40"
              />
              <span class="text-sm text-ink-soft dark:text-[#A6A6AD]">h</span>
            </div>
            <p class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1.5">
              {{ t('settings_auth_ttl_hint') }}
            </p>
          </div>
        </div>

        <!-- dsh 快捷访问地址（DynamicList 风格） -->
        <div class="py-4">
          <label class="block text-sm font-medium text-ink dark:text-[#EDEDF0] mb-3">{{ t('access_urls_hint') }}</label>
          <div class="flex gap-2 mb-3">
            <input
              v-model="newAccessUrl"
              :placeholder="t('access_urls_placeholder')"
              class="g-input flex-1"
              @keydown.enter="addAccessUrl"
            />
            <button class="g-btn-primary !h-10 shrink-0" @click="addAccessUrl">{{ t('access_urls_add') }}</button>
          </div>
          <div v-if="config.accessUrls && config.accessUrls.length" class="border border-[#E8E8EC] dark:border-[#2A2A32] rounded-lg divide-y divide-[#E8E8EC] dark:divide-[#2A2A32]">
            <div v-for="(url, i) in config.accessUrls" :key="i" class="flex items-center justify-between gap-2 px-3 py-2">
              <span class="text-sm text-ink dark:text-[#EDEDF0] font-mono truncate">{{ url }}</span>
              <button
                type="button"
                class="flex-shrink-0 text-ink-soft dark:text-[#A6A6AD] hover:text-danger dark:hover:text-[#EF4444] transition-colors"
                :title="t('rollback_delete')"
                @click="removeAccessUrl(i)"
              >
                <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
              </button>
            </div>
          </div>
          <p v-else class="text-xs text-ink-faint dark:text-[#8A8A92]">{{ t('access_urls_empty') }}</p>
        </div>
      </div>
    </section>

    <!-- 控制台设置 -->
    <section class="g-card g-card-hover p-6 mb-6">
      <h2 class="font-display text-lg font-semibold text-ink dark:text-white mb-6">{{ t('console_title') }}</h2>

      <div class="divide-y divide-line dark:divide-[#2A2A32]">
        <div class="py-4">
          <label class="block text-sm text-ink-soft dark:text-[#A6A6AD] mb-1.5">{{ t('console_theme') }}</label>
          <select v-model="themeMode" @change="onThemeChange" class="g-input">
            <option value="light">{{ t('theme_light') }}</option>
            <option value="dark">{{ t('theme_dark') }}</option>
            <option value="system">{{ t('theme_system') }}</option>
          </select>
        </div>
        <div class="py-4">
          <label class="block text-sm text-ink-soft dark:text-[#A6A6AD] mb-1.5">{{ t('console_language') }}</label>
          <select v-model="locale" @change="onLanguageChange" class="g-input">
            <option value="zh">{{ t('lang_zh') }}</option>
            <option value="en">{{ t('lang_en') }}</option>
          </select>
        </div>
        <div class="py-4">
          <label class="block text-sm text-ink-soft dark:text-[#A6A6AD] mb-1.5">{{ t('console_default_page') }}</label>
          <select v-model="defaultPage" @change="onDefaultPageChange" class="g-input">
            <option value="overview">{{ t('default_overview') }}</option>
            <option value="last">{{ t('default_last') }}</option>
            <option value="settings">{{ t('default_settings') }}</option>
            <option value="directory">{{ t('default_directory') }}</option>
            <option value="plugins">{{ t('default_plugins') }}</option>
            <option value="terminal">{{ t('default_terminal') }}</option>
            <option value="logs">{{ t('default_logs') }}</option>
          </select>
        </div>
      </div>
    </section>
  </div>
</template>