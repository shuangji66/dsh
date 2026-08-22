<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { useSettingsStore } from '@/stores/settings'
import { useToastStore } from '@/stores/toast'

const store = useSettingsStore()
const { config, status, locked, loading } = storeToRefs(store)
const toast = useToastStore()

// 访问密码明文/密文切换
const showPassword = ref(false)

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
  if (config.password) {
    const err = validatePassword(config.password)
    if (err) {
      toast.show(err, 'error')
      return
    }
  }
  store.save()
}

onMounted(() => store.load())
</script>

<template>
  <div class="py-8 sm:py-12 px-4 sm:px-8 max-w-3xl mx-auto">
    <!-- 页头 -->
    <header class="mb-8">
      <p class="text-ink-faint dark:text-[#8A8A92] text-sm font-medium uppercase tracking-widest">DeepSeek Harness · 控制台</p>
    </header>

    <!-- dsh 生命周期 -->
    <section class="g-card g-card-hover p-6 mb-6">
      <div class="flex items-center justify-between mb-2">
        <h2 class="font-display text-lg font-semibold text-ink dark:text-white">DeepSeek Harness</h2>
        <span class="inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium"
          :class="status?.running ? 'bg-success/10 text-success' : 'bg-ink-faint/10 text-ink-soft dark:text-[#A6A6AD]'">
          <span class="inline-block w-2 h-2 rounded-full"
            :class="status?.running ? 'bg-success' : 'bg-ink-faint'"></span>
          {{ status?.running ? '运行中' : '已停止' }}
        </span>
      </div>
      <p class="text-sm text-ink-soft dark:text-[#A6A6AD] mb-5">管理 dsh 服务的启动、停止与重启。</p>
      <div class="flex gap-3">
        <button v-if="!status?.running" class="g-btn-primary bg-success hover:bg-success/90" :disabled="loading" @click="store.startDsh()">启动</button>
        <button v-else class="g-btn-danger" :disabled="loading" @click="store.stopDsh()">停止</button>
        <button class="g-btn-secondary" :disabled="loading" @click="store.restartDsh()">重启</button>
      </div>
    </section>

    <!-- 反向代理与鉴权 -->
    <section class="g-card g-card-hover p-6 mb-6">
      <h2 class="font-display text-lg font-semibold text-ink dark:text-white mb-2">设置</h2>
      <p class="text-sm text-ink-soft dark:text-[#A6A6AD] mb-6">配置代理、端口与登录鉴权。</p>

      <div class="divide-y divide-line dark:divide-[#2A2A32]">
        <!-- 启用外部代理（开关） -->
        <label class="flex items-center justify-between py-4 cursor-pointer">
          <span class="text-sm font-medium text-ink dark:text-[#EDEDF0]">启用代理</span>
          <div class="relative inline-flex items-center">
            <input type="checkbox" v-model="config.proxyEnabled" class="sr-only peer">
            <div class="w-11 h-6 bg-[#E8E8EC] dark:bg-[#2A2A32] rounded-full peer peer-checked:bg-brand transition-colors"></div>
            <div class="absolute left-0.5 top-0.5 w-5 h-5 bg-white rounded-full transition-transform peer-checked:translate-x-5"></div>
          </div>
        </label>

        <div v-if="config.proxyEnabled" class="py-4">
          <label class="block text-sm text-ink-soft dark:text-[#A6A6AD] mb-1.5">代理地址</label>
          <input v-model="config.proxyAddr" placeholder="http:// 或 socks5:// 代理地址" class="g-input" />
          <p v-if="!config.proxyAddr" class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1.5">
            可填写 http 或 socks5 代理，例如 http://127.0.0.1:7890 或 socks5://127.0.0.1:1080
          </p>
        </div>

        <div class="py-4">
          <label class="block text-sm text-ink-soft dark:text-[#A6A6AD] mb-1.5">dsh 端口</label>
          <input
            v-model.number="config.dshPort"
            type="number"
            min="1"
            max="65535"
            :disabled="locked"
            class="g-input disabled:cursor-not-allowed"
          />
        </div>

        <!-- 启用登录鉴权（开关） -->
        <label class="flex items-center justify-between py-4 cursor-pointer">
          <span class="text-sm font-medium text-ink dark:text-[#EDEDF0]">启用登录鉴权</span>
          <div class="relative inline-flex items-center">
            <input type="checkbox" v-model="config.authEnabled" class="sr-only peer">
            <div class="w-11 h-6 bg-[#E8E8EC] dark:bg-[#2A2A32] rounded-full peer peer-checked:bg-brand transition-colors"></div>
            <div class="absolute left-0.5 top-0.5 w-5 h-5 bg-white rounded-full transition-transform peer-checked:translate-x-5"></div>
          </div>
        </label>

        <div class="py-4">
          <label class="block text-sm text-ink-soft dark:text-[#A6A6AD] mb-1.5">
            访问密码 <span class="text-ink-faint">(≥8位，含字母/数字/标点)</span>
          </label>
          <div class="relative">
            <input
              :type="showPassword ? 'text' : 'password'"
              v-model="config.password"
              placeholder="留空则鉴权不生效"
              class="g-input pr-10"
            />
            <button
              type="button"
              @click="showPassword = !showPassword"
              class="absolute right-1.5 top-1/2 -translate-y-1/2 w-7 h-7 flex items-center justify-center rounded-md text-ink-soft dark:text-[#A6A6AD] hover:text-ink dark:hover:text-white hover:bg-black/5 dark:hover:bg-white/5 transition-colors"
              :title="showPassword ? '隐藏密码' : '显示密码'"
            >
              <svg v-if="showPassword" xmlns="http://www.w3.org/2000/svg" class="w-4.5 h-4.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94"/><path d="M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19"/><line x1="1" y1="1" x2="23" y2="23"/></svg>
              <svg v-else xmlns="http://www.w3.org/2000/svg" class="w-4.5 h-4.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
            </button>
          </div>
        </div>
      </div>

      <button class="g-btn-primary mt-6" :disabled="loading" @click="submit()">保存配置</button>
    </section>
  </div>
</template>