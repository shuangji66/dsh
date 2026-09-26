<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { useSettingsStore } from '@/stores/settings'
import { useToastStore } from '@/stores/toast'
import { useI18n, setLocale } from '@/composables/useI18n'
import { useTheme } from '@/composables/useTheme'
import { useConsolePrefs, type DefaultPage } from '@/composables/useConsolePrefs'
import PageHeader from '@/components/PageHeader.vue'
import { icons } from '@/utils/icons'

const store = useSettingsStore()
const { config, runtime, locked, loading, memLimitLevel } = storeToRefs(store)
const toast = useToastStore()
const { t } = useI18n()
const { themeMode, setTheme } = useTheme()
const { locale } = useI18n()
const { defaultPage, setDefaultPage } = useConsolePrefs()

// node 版本切换选项：来自后端 runtime.nodeVersions。
// node24 始终可用；node26 仅当宿主机存在对应 node 二进制时可用。
const nodeVersions = computed(() => runtime.value?.nodeVersions ?? [])

// node26 是否可用（未安装时提示可安装 v26 进行切换，但可能存在未知问题）
const node26Usable = computed(() =>
  nodeVersions.value.some((v) => v.id === 'node26' && v.available)
)

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
  if (pwd.length < 8) return t('pwd_too_short')
  let hasLetter = false
  let hasDigit = false
  let hasPunct = false
  for (const ch of pwd) {
    if (ch >= '0' && ch <= '9') { hasDigit = true; continue }
    if ((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')) { hasLetter = true; continue }
    if (SAFE_PUNCT.has(ch)) { hasPunct = true; continue }
    if (ch.charCodeAt(0) < 128) return t('pwd_danger_punct', { ch })
  }
  if (!hasLetter) return t('pwd_missing_letter')
  if (!hasDigit) return t('pwd_missing_digit')
  if (!hasPunct) return t('pwd_missing_punct')
  return ''
}

// 保存前校验：仅当开启登录鉴权且填写了密码时，才要求满足强度要求（≥8位，含字母、数字、标点）；
// 关闭鉴权时不校验，与后端 handleSaveSettings 保持一致
function submit() {
  const pwd = config.value.password
  if (config.value.authEnabled && pwd) {
    const err = validatePassword(pwd)
    if (err) {
      toast.show(err, 'error')
      return
    }
  }
  // 反代端口校验（后端同样校验，这里先拦一道给出可读提示）：
  // 必须落在 1-65535，且不能与 dsh 端口相同 —— 两者会争抢同一个 TCP 端口。
  const pp = config.value.proxyPort
  if (!Number.isInteger(pp) || pp < 1 || pp > 65535) {
    toast.show(t('settings_proxy_port_invalid'), 'error')
    return
  }
  if (pp === config.value.dshPort) {
    toast.show(t('settings_proxy_port_same'), 'error')
    return
  }
  store.save()
}

onMounted(() => store.load())

// --- dsh 访问地址（DynamicList 风格） ---
const newAccessUrl = ref('')

// 确保 config.accessUrls 为数组，并返回该数组（便于调用处获得非 undefined 的窄化类型）
function ensureAccessUrls(): string[] {
  if (!Array.isArray(config.value.accessUrls)) config.value.accessUrls = []
  return config.value.accessUrls
}

// 添加一个访问地址并自动保存（每添加一个就刷新概览页数据）
function addAccessUrl() {
  const url = newAccessUrl.value.trim()
  if (!url) return
  ensureAccessUrls().push(url)
  newAccessUrl.value = ''
  store.save()
}

// 移除一个访问地址
function removeAccessUrl(idx: number) {
  ensureAccessUrls().splice(idx, 1)
  store.save()
}

// --- 开关即时保存 ---
// 以下即时保存都要看 store.save() 的返回值：返回 false 表示这次保存被拒绝
// （如堆内存上限过低）或后端报错，此时不能再弹「已保存」的成功提示。

// 「代理dsh」开关：即时保存，提示需重启 dsh 生效（代理是 dsh 启动时下发的环境变量）
async function onProxyToggle() {
  if (!(await store.save(false))) return
  toast.show(t('saved_proxy_restart'), 'info', 5000)
}

// 「代理更新」开关：即时保存。它只影响 harness 自身的更新探测与下载，不需要重启 dsh。
async function onProxyUpdateToggle() {
  if (!(await store.save(false))) return
  toast.show(
    config.value.proxyUpdate ? t('settings_proxy_update_on') : t('settings_proxy_update_off'),
    'success',
    5000
  )
}

// 堆内存「自动设置」开关：即时保存，提示需重启 dsh 生效。
// 注意：保存被拒时（手动值 < MEM_LIMIT_MIN_MB）**不把开关回滚**，与鉴权开关的处理
// 相反 —— 回滚成「自动设置」会让上面的输入框重新禁用，用户就没法把值改大了。
async function onMemAutoToggle() {
  if (!(await store.save(false))) return
  toast.show(t('saved_mem_restart'), 'info', 5000)
}

// 堆内存上限输入框的展示值：
//  - 「自动设置」打开时，输入框（禁用）展示当前 node 自身的堆上限 —— 后端用
//    `v8.getHeapStatistics().heap_size_limit` 折算出的 MB 数（runtime.nodeHeapLimitMB），
//    这才是自动设置实际生效的上限；不再展示持久化的手动值；
//  - 关闭时展示并编辑持久化的手动值。
// 写入只发生在关闭状态下（禁用时不会触发 setter），因此展示值不会覆盖手动值。
// 探测不到（后端返回 0）时显示空，避免让用户以为上限是 0。
const memLimit = computed<number | string>({
  get: () =>
    config.value.dshMemAuto
      ? (runtime.value?.nodeHeapLimitMB || '')
      : config.value.dshMemLimit,
  set: (v) => {
    config.value.dshMemLimit = Number(v) || 0
  }
})

// 浏览器兼容模式：即时保存。反代按该开关决定是否修正引擎兼容判断，
// 该修正发生在页面加载阶段，因此提示用户刷新页面即可生效（无需重启 dsh）。
async function onBrowserCompatToggle() {
  if (!(await store.save(false))) return
  toast.show(t('settings_browser_compat_saved'), 'success', 5000)
}

// node 版本切换：即时保存，切换后需重启 dsh 服务生效
async function onNodeVersionChange() {
  if (!(await store.save(false))) return
  toast.show(t('saved_node_version_restart'), 'info', 5000)
}

// 鉴权登录开关：即时保存。开启前先做前端密码强度校验（i18n 提示，失败回滚开关）；
// 关闭鉴权时不校验密码，允许直接关闭。
async function onAuthToggle() {
  const pwd = config.value.password
  if (config.value.authEnabled && pwd) {
    const err = validatePassword(pwd)
    if (err) {
      config.value.authEnabled = false // 回滚开关，避免 UI 与后端状态不一致
      toast.show(err, 'error')
      return
    }
  }
  if (!(await store.save(false))) return
  toast.show(t('settings_toggle_saved'), 'success')
}
</script>

<template>
  <div class="pt-3 sm:pt-4 pb-8 sm:pb-12 px-4 sm:px-8 max-w-6xl mx-auto">
    <!-- 页头（图标 + 标题 + 保存按钮；标题栏按钮统一样式：小一号字号 + 细边框 + 不填充底色） -->
    <PageHeader class="mb-3" :title="t('settings_title')" :icon="icons.settings">
      <button class="g-btn-secondary h-8 px-3 text-xs" :disabled="loading" @click="submit()">{{ t('settings_save') }}</button>
    </PageHeader>

    <!-- 六个卡片自适应分栏：代理 / 端口与兼容 / node 版本与内存 / 登录鉴权 / 快捷访问 / 控制台设置。
         列数由容器宽度自动决定（见 style.css 的 .g-card-grid），窄屏自动降为单列。 -->
    <div class="g-card-grid g-fade-in">
      <!-- ① 代理：「代理dsh」与「代理更新」两个开关 + 共用的代理地址（任一开启时显示） -->
      <section class="g-card g-card-hover p-5 flex flex-col">
        <h2 class="font-display text-base font-semibold text-ink dark:text-white pb-3 mb-4 border-b border-line dark:border-[#2A2A32]">
          {{ t('settings_card_proxy') }}
        </h2>

        <label class="flex items-center justify-between gap-3 cursor-pointer select-none">
          <span class="text-sm font-medium text-ink dark:text-[#EDEDF0]">{{ t('settings_enable_proxy') }}</span>
          <span class="relative inline-flex items-center flex-shrink-0">
            <input type="checkbox" v-model="config.proxyEnabled" class="sr-only peer" @change="onProxyToggle">
            <span class="w-11 h-6 bg-[#E8E8EC] dark:bg-[#2A2A32] rounded-full peer peer-checked:bg-brand transition-colors"></span>
            <span class="absolute left-0.5 top-0.5 w-5 h-5 bg-white rounded-full transition-transform peer-checked:translate-x-5"></span>
          </span>
        </label>
        <p class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1.5">{{ t('settings_enable_proxy_hint') }}</p>

        <!-- 代理更新：只控制 harness 与 dsh 服务更新是否先从代理走（探测不通回退直连），
             与上面的「代理dsh」相互独立；插件市场下载始终直连。 -->
        <label class="mt-4 flex items-center justify-between gap-3 cursor-pointer select-none">
          <span class="text-sm font-medium text-ink dark:text-[#EDEDF0]">{{ t('settings_proxy_update') }}</span>
          <span class="relative inline-flex items-center flex-shrink-0">
            <input type="checkbox" v-model="config.proxyUpdate" class="sr-only peer" @change="onProxyUpdateToggle">
            <span class="w-11 h-6 bg-[#E8E8EC] dark:bg-[#2A2A32] rounded-full peer peer-checked:bg-brand transition-colors"></span>
            <span class="absolute left-0.5 top-0.5 w-5 h-5 bg-white rounded-full transition-transform peer-checked:translate-x-5"></span>
          </span>
        </label>
        <p class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1.5">{{ t('settings_proxy_update_hint') }}</p>

        <!-- 代理地址：两个开关共用，任一开启即显示 -->
        <div v-if="config.proxyEnabled || config.proxyUpdate" class="mt-4">
          <label class="block text-sm text-ink-soft dark:text-[#A6A6AD] mb-1.5">{{ t('settings_proxy_addr') }}</label>
          <input v-model="config.proxyAddr" :placeholder="t('settings_proxy_addr')" class="g-input" autocomplete="off" />
          <p class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1.5">{{ t('settings_proxy_hint') }}</p>
        </div>
      </section>

      <!-- ② 端口与兼容：dsh 端口 / 反代监听端口 / 浏览器兼容模式 -->
      <section class="g-card g-card-hover p-5 flex flex-col">
        <h2 class="font-display text-base font-semibold text-ink dark:text-white pb-3 mb-4 border-b border-line dark:border-[#2A2A32]">
          {{ t('settings_card_port') }}
        </h2>

        <div class="flex flex-col gap-4">
          <div>
            <label class="block text-sm text-ink-soft dark:text-[#A6A6AD] mb-1.5">{{ t('settings_dsh_port') }}</label>
            <input
              v-model.number="config.dshPort"
              type="number"
              min="1"
              max="65535"
              autocomplete="off"
              :disabled="locked"
              class="g-input disabled:cursor-not-allowed"
            />
            <p class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1.5">{{ t('settings_dsh_port_hint') }}</p>
          </div>

          <!-- 反向代理监听端口（外部端口访问）：由 harness 自己监听，保存后即时重绑生效 -->
          <div>
            <label class="block text-sm text-ink-soft dark:text-[#A6A6AD] mb-1.5">{{ t('settings_proxy_port') }}</label>
            <input
              v-model.number="config.proxyPort"
              type="number"
              min="1"
              max="65535"
              autocomplete="off"
              class="g-input"
            />
            <p class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1.5">{{ t('settings_proxy_port_hint') }}</p>
          </div>

          <!-- 浏览器兼容模式（开关）：修复 Firefox/Safari 会话历史无法加载 + iPhone 上模型/推理等级菜单点选项没反应 -->
          <div>
            <div class="flex items-center justify-between gap-3">
              <span class="text-sm font-medium text-ink dark:text-[#EDEDF0]">{{ t('settings_browser_compat') }}</span>
              <label class="relative inline-flex items-center cursor-pointer flex-shrink-0">
                <input type="checkbox" v-model="config.browserCompat" class="sr-only peer" @change="onBrowserCompatToggle">
                <span class="w-11 h-6 bg-[#E8E8EC] dark:bg-[#2A2A32] rounded-full peer peer-checked:bg-brand transition-colors"></span>
                <span class="absolute left-0.5 top-0.5 w-5 h-5 bg-white rounded-full transition-transform peer-checked:translate-x-5"></span>
              </label>
            </div>
            <p class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1.5">
              {{ t('settings_browser_compat_hint') }}
            </p>
          </div>
        </div>
      </section>

      <!-- ③ node 版本与内存：node 版本切换 + node 堆内存上限（含自动设置） -->
      <section class="g-card g-card-hover p-5 flex flex-col">
        <h2 class="font-display text-base font-semibold text-ink dark:text-white pb-3 mb-4 border-b border-line dark:border-[#2A2A32]">
          {{ t('settings_card_node') }}
        </h2>

        <div class="flex flex-col gap-4">
          <!-- node 版本切换 -->
          <div>
            <label class="block text-sm text-ink-soft dark:text-[#A6A6AD] mb-1.5">{{ t('settings_node_version') }}</label>
            <select v-model="config.nodeVersion" class="g-input" @change="onNodeVersionChange">
              <option v-for="v in nodeVersions" :key="v.id" :value="v.id">{{ v.label }}</option>
            </select>
            <p class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1.5">
              <template v-if="node26Usable">{{ t('settings_node_version_hint') }}</template>
              <template v-else>{{ t('settings_node_version_no26_hint') }}</template>
            </p>
          </div>

          <!-- node 堆内存上限（MB，单位已在标签里说明，输入框内不再重复） -->
          <div>
            <label class="block text-sm text-ink-soft dark:text-[#A6A6AD] mb-1.5">
              {{ t('settings_dsh_mem_limit') }} <span class="text-ink-faint">{{ t('settings_dsh_mem_mb') }}</span>
            </label>
            <input
              v-model.number="memLimit"
              type="number"
              min="1"
              max="65536"
              autocomplete="off"
              :disabled="config.dshMemAuto"
              class="g-input disabled:cursor-not-allowed"
            />
            <!-- 输入框下的说明/告警（两者互斥）：
                 - 手动设置且过低：「<800」黄字提醒但不阻止保存，「<500」红字并阻止保存
                   （阻止逻辑在 store.save()，后端有同阈值校验兜底）；
                 - 其余情况：自动设置时说明当前上限由系统 node 决定，手动时给一般提示。 -->
            <p v-if="memLimitLevel === 'danger'" class="text-xs text-danger mt-1.5">
              {{ t('settings_dsh_mem_too_low') }}
            </p>
            <p v-else-if="memLimitLevel === 'warn'" class="text-xs text-warning mt-1.5">
              {{ t('settings_dsh_mem_warn') }}
            </p>
            <p v-else class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1.5">
              {{ config.dshMemAuto ? t('settings_dsh_mem_auto_hint') : t('settings_dsh_mem_hint') }}
            </p>
            <!-- 自动设置开关：关闭时传 NODE_OPTIONS，由系统 node 自动分配内存 -->
            <div class="flex items-center justify-between gap-3 mt-3">
              <span class="text-sm text-ink dark:text-[#EDEDF0]">{{ t('settings_dsh_mem_auto') }}</span>
              <label class="relative inline-flex items-center cursor-pointer flex-shrink-0">
                <input type="checkbox" v-model="config.dshMemAuto" class="sr-only peer" @change="onMemAutoToggle">
                <span class="w-11 h-6 bg-[#E8E8EC] dark:bg-[#2A2A32] rounded-full peer peer-checked:bg-brand transition-colors"></span>
                <span class="absolute left-0.5 top-0.5 w-5 h-5 bg-white rounded-full transition-transform peer-checked:translate-x-5"></span>
              </label>
            </div>
          </div>
        </div>
      </section>

      <!-- ④ 登录鉴权：开关 + 访问密码 + 登录有效期 -->
      <section class="g-card g-card-hover p-5 flex flex-col">
        <h2 class="font-display text-base font-semibold text-ink dark:text-white pb-3 mb-4 border-b border-line dark:border-[#2A2A32]">
          {{ t('settings_card_auth') }}
        </h2>

        <label class="flex items-center justify-between gap-3 cursor-pointer select-none">
          <span class="text-sm font-medium text-ink dark:text-[#EDEDF0]">{{ t('settings_enable_auth') }}</span>
          <span class="relative inline-flex items-center flex-shrink-0">
            <input type="checkbox" v-model="config.authEnabled" class="sr-only peer" @change="onAuthToggle">
            <span class="w-11 h-6 bg-[#E8E8EC] dark:bg-[#2A2A32] rounded-full peer peer-checked:bg-brand transition-colors"></span>
            <span class="absolute left-0.5 top-0.5 w-5 h-5 bg-white rounded-full transition-transform peer-checked:translate-x-5"></span>
          </span>
        </label>
        <!-- 开关说明：网关访问本就跳过鉴权；端口访问在局域网内可达，建议保持开启。 -->
        <p class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1.5 leading-relaxed">{{ t('settings_auth_gateway_hint') }}</p>

        <div class="mt-4">
          <label class="block text-sm text-ink-soft dark:text-[#A6A6AD] mb-1.5">
            {{ t('settings_password') }} <span class="text-ink-faint">{{ t('settings_password_hint') }}</span>
          </label>
          <div class="relative">
            <input
              :type="showPassword ? 'text' : 'password'"
              v-model="config.password"
              :placeholder="t('settings_password_placeholder')"
              autocomplete="new-password"
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

        <!-- 登录有效期（小时，单位已在标签里说明，输入框内不再重复） -->
        <div class="mt-4">
          <label class="block text-sm text-ink-soft dark:text-[#A6A6AD] mb-1.5">
            {{ t('settings_auth_ttl') }} <span class="text-ink-faint">{{ t('settings_auth_ttl_hour') }}</span>
          </label>
          <input
            v-model.number="config.authTTLHours"
            type="number"
            min="1"
            max="720"
            step="1"
            autocomplete="off"
            class="g-input"
          />
          <p class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1.5">
            {{ t('settings_auth_ttl_hint') }}
          </p>
        </div>
      </section>

      <!-- ⑤ 快捷访问：概览页快捷访问地址（DynamicList 风格） -->
      <section class="g-card g-card-hover p-5 flex flex-col">
        <h2 class="font-display text-base font-semibold text-ink dark:text-white pb-3 mb-4 border-b border-line dark:border-[#2A2A32]">
          {{ t('access_urls_hint') }}
        </h2>

        <div class="flex gap-2 mb-3">
          <input
            v-model="newAccessUrl"
            :placeholder="t('access_urls_placeholder')"
            autocomplete="off"
            class="g-input flex-1"
            @keydown.enter="addAccessUrl"
          />
          <!-- 添加按钮与标题栏按钮同一风格：不填充底色 + 细边框（高度与输入框对齐） -->
          <button class="g-btn-secondary !h-10 shrink-0" @click="addAccessUrl">{{ t('access_urls_add') }}</button>
        </div>
        <div v-if="config.accessUrls && config.accessUrls.length" class="border border-[#E8E8EC] dark:border-[#2A2A32] rounded-lg divide-y divide-[#E8E8EC] dark:divide-[#2A2A32]">
          <div v-for="(url, i) in config.accessUrls" :key="i" class="flex items-center justify-between gap-2 px-3 py-2">
            <span class="g-access-url text-ink dark:text-[#EDEDF0] truncate">{{ url }}</span>
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
      </section>

      <!-- ⑥ 控制台设置：主题 / 语言 / 默认页面（仅存浏览器 localStorage） -->
      <section class="g-card g-card-hover p-5 flex flex-col">
        <h2 class="font-display text-base font-semibold text-ink dark:text-white pb-3 mb-4 border-b border-line dark:border-[#2A2A32]">
          {{ t('console_title') }}
        </h2>

        <div class="flex flex-col gap-4">
          <div>
            <label class="block text-sm text-ink-soft dark:text-[#A6A6AD] mb-1.5">{{ t('console_theme') }}</label>
            <select v-model="themeMode" @change="onThemeChange" class="g-input">
              <option value="light">{{ t('theme_light') }}</option>
              <option value="dark">{{ t('theme_dark') }}</option>
              <option value="system">{{ t('theme_system') }}</option>
            </select>
          </div>
          <div>
            <label class="block text-sm text-ink-soft dark:text-[#A6A6AD] mb-1.5">{{ t('console_language') }}</label>
            <select v-model="locale" @change="onLanguageChange" class="g-input">
              <option value="zh">{{ t('lang_zh') }}</option>
              <option value="en">{{ t('lang_en') }}</option>
            </select>
          </div>
          <div>
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
  </div>
</template>
