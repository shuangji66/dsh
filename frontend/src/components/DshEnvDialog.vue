<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n, uiErrText } from '@/composables/useI18n'
import { useBodyScrollLock } from '@/composables/useBodyScrollLock'
import { useDialogA11y } from '@/composables/useDialogA11y'
import DialogCloseButton from '@/components/DialogCloseButton.vue'
import { useToastStore } from '@/stores/toast'
import { api, type DshEnvVar } from '@/serverapi'

const props = defineProps<{ visible: boolean }>()

const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void
  (e: 'saved'): void
}>()

const { t } = useI18n()
const toast = useToastStore()

// 变量名/值长度上限与后端 dsh_env.go 的 maxDshEnvNameLen / maxDshEnvValueLen 一致。
// 变量名只允许 ASCII 标识符，字符数 == 字节数；变量值可以是任意文本，后端按**字节**算，
// 所以这里也按字节算（否则 5000 个汉字前端放行、点保存后端 400）。
const NAME_MAX = 128
const VALUE_MAX = 8 << 10
const NAME_RE = /^[A-Za-z_][A-Za-z0-9_]*$/
const encoder = new TextEncoder()
function valueBytes(s: string): number {
  return encoder.encode(s).length
}

const open = ref(props.visible)
// 注意：不能带 immediate —— 回调会在下面那些 ref 初始化之前执行（load 访问它们会 TDZ）。
watch(
  () => props.visible,
  (v) => {
    open.value = v
    if (v) void load()
  }
)

useBodyScrollLock(() => open.value)

const panel = ref<HTMLElement | null>(null)
useDialogA11y(() => open.value, panel, { onClose: close })

// 行内 key：变量本身没有 id（整表提交），用稳定 key 避免删除中间行时输入框错位。
type Row = DshEnvVar & { key: number }
let nextKey = 1
function makeKey() {
  return nextKey++
}

const rows = ref<Row[]>([])
const reserved = ref<string[]>([])
const reservedPrefixes = ref<string[]>([])
const maxVars = ref(64)
const loading = ref(false)
const failed = ref(false)
const errorText = ref('')
const saving = ref(false)
const formError = ref('')
const rowErrors = ref<Record<number, string>>({})

const ready = computed(() => !loading.value && !failed.value)
const canAdd = computed(() => ready.value && rows.value.length < maxVars.value)

function close() {
  open.value = false
  emit('update:visible', false)
}

async function load() {
  loading.value = true
  failed.value = false
  errorText.value = ''
  formError.value = ''
  rowErrors.value = {}
  try {
    const p = await api.dshEnv()
    rows.value = p.variables.map((v) => ({ key: makeKey(), name: v.name, value: v.value }))
    reserved.value = p.reserved
    reservedPrefixes.value = p.reservedPrefixes
    maxVars.value = p.max
  } catch (e) {
    failed.value = true
    errorText.value = uiErrText(e, t('dsh_env_load_failed'))
  } finally {
    loading.value = false
  }
}

function addRow() {
  if (!canAdd.value) return
  rows.value.push({ key: makeKey(), name: '', value: '' })
  formError.value = ''
}

function removeRow(key: number) {
  const i = rows.value.findIndex((r) => r.key === key)
  if (i < 0) return
  rows.value.splice(i, 1)
  // 行索引会整体前移，清掉全部行内错误最省事，也不会把错误挂到别的行上。
  rowErrors.value = {}
  formError.value = ''
}

function isReserved(name: string): boolean {
  const upper = name.toUpperCase()
  if (reserved.value.some((n) => n.toUpperCase() === upper)) return true
  return reservedPrefixes.value.some((p) => upper.startsWith(p.toUpperCase()))
}

// 前端即时校验与后端同一口径；后端仍是最终权威（错误码会 toast 出来）。
function validate(): boolean {
  rowErrors.value = {}
  formError.value = ''
  if (rows.value.length > maxVars.value) {
    formError.value = t('dsh_env_too_many', { max: maxVars.value })
    return false
  }
  const seen = new Set<string>()
  let ok = true
  for (const row of rows.value) {
    const name = row.name.trim()
    if (!name) {
      rowErrors.value[row.key] = t('dsh_env_name_required')
      ok = false
      continue
    }
    if (name.length > NAME_MAX || !NAME_RE.test(name)) {
      rowErrors.value[row.key] = t('dsh_env_name_invalid')
      ok = false
      continue
    }
    if (isReserved(name)) {
      rowErrors.value[row.key] = t('dsh_env_name_reserved', { name })
      ok = false
      continue
    }
    if (seen.has(name)) {
      rowErrors.value[row.key] = t('dsh_env_duplicate', { name })
      ok = false
      continue
    }
    seen.add(name)
    if (valueBytes(row.value) > VALUE_MAX) {
      rowErrors.value[row.key] = t('dsh_env_value_too_long', { max: VALUE_MAX })
      ok = false
      continue
    }
    if (row.value.includes('\0')) {
      rowErrors.value[row.key] = t('dsh_env_value_invalid')
      ok = false
    }
  }
  return ok
}

// 组件若以 visible=true 挂载（父级状态恢复等），补一次加载。
if (open.value) void load()

async function save() {
  if (!ready.value || saving.value || !validate()) return
  saving.value = true
  try {
    const p = await api.saveDshEnv(
      rows.value.map((r) => ({ name: r.name.trim(), value: r.value }))
    )
    rows.value = p.variables.map((v) => ({ key: makeKey(), name: v.name, value: v.value }))
    emit('saved')
    close()
  } catch (e) {
    toast.show(uiErrText(e, t('dsh_env_save_failed')), 'error', 6000)
  } finally {
    saving.value = false
  }
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
      <div v-if="open" class="fixed inset-0 z-50 flex items-center justify-center p-4 backdrop-blur-sm">
        <div class="g-modal-mask" @click="close"></div>
        <div
          ref="panel"
          tabindex="-1"
          role="dialog"
          aria-modal="true"
          :aria-label="t('dsh_env_title')"
          class="relative w-full max-w-2xl max-h-[90dvh] bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card p-6 outline-none flex flex-col"
        >
          <DialogCloseButton :label="t('dialog_close')" @close="close" />

          <!-- 标题行：右侧一个 32px 方形图标按钮（新增），pr-12 给绝对的 X 让位 -->
          <div class="flex items-start justify-between gap-3 pr-12">
            <h3 class="g-dialog-title !pr-0">{{ t('dsh_env_title') }}</h3>
            <div class="flex items-center gap-2 shrink-0 pt-0.5">
              <button
                class="g-btn-secondary !h-8 !w-8 !p-0"
                :title="t('dsh_env_add')"
                :aria-label="t('dsh_env_add')"
                :disabled="!canAdd"
                @click="addRow"
              >
                <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19" /><line x1="5" y1="12" x2="19" y2="12" /></svg>
              </button>
            </div>
          </div>

          <p class="text-sm text-ink-soft dark:text-[#A6A6AD] leading-relaxed mt-2">{{ t('dsh_env_desc') }}</p>
          <p class="text-xs text-ink-faint dark:text-[#8A8A92] mt-1">{{ t('dsh_env_reserved_hint') }}</p>

          <div class="mt-4 flex-1 min-h-0 overflow-y-auto pr-1">
            <div v-if="loading" class="text-sm text-ink-soft dark:text-[#8A8A92] text-center py-8">
              {{ t('dsh_env_loading') }}
            </div>

            <!-- 加载失败：本地列表不可信（后端是整表覆盖），不渲染编辑区，避免把服务端那份盖掉 -->
            <div v-else-if="failed" class="text-center py-8">
              <p class="text-sm text-ink-soft dark:text-[#A6A6AD] leading-relaxed">{{ t('dsh_env_load_failed') }}</p>
              <p class="text-xs text-danger-ink mt-2 break-words select-text">{{ errorText }}</p>
              <button class="g-btn-secondary mt-3" @click="load">{{ t('dsh_env_retry') }}</button>
            </div>

            <template v-else>
              <div v-if="!rows.length" class="text-center py-8">
                <p class="text-sm text-ink-soft dark:text-[#8A8A92]">{{ t('dsh_env_empty') }}</p>
                <button class="g-btn-secondary mt-3" @click="addRow">{{ t('dsh_env_add') }}</button>
              </div>

              <div
                v-for="row in rows"
                :key="row.key"
                class="rounded-lg border border-[#E8E8EC] dark:border-[#2A2A32] p-3 mb-2"
              >
                <!-- 宽度够时名称/值同一行；窄屏（<sm）分行显示 -->
                <div class="flex flex-col sm:flex-row sm:items-start gap-2">
                  <div class="flex-1 min-w-0">
                    <label class="block text-xs text-ink-faint dark:text-[#8A8A92] mb-1">{{ t('dsh_env_name') }}</label>
                    <input
                      v-model="row.name"
                      type="text"
                      class="g-input font-mono text-sm"
                      :placeholder="t('dsh_env_name')"
                      :aria-label="t('dsh_env_name')"
                      :maxlength="NAME_MAX"
                      autocomplete="off"
                      spellcheck="false"
                    />
                  </div>
                  <div class="flex-[1.4] min-w-0">
                    <label class="block text-xs text-ink-faint dark:text-[#8A8A92] mb-1">{{ t('dsh_env_value') }}</label>
                    <!-- 刻意不写 maxlength：它按 UTF-16 码元截断，和后端的字节上限对不上，
                         而且被静默截短的往往是 token —— 用户不会发现少了几个字符。
                         超长由 validate() 按字节判断，给行内错误。 -->
                    <input
                      v-model="row.value"
                      type="text"
                      class="g-input font-mono text-sm"
                      :placeholder="t('dsh_env_value')"
                      :aria-label="t('dsh_env_value')"
                      autocomplete="off"
                      spellcheck="false"
                    />
                  </div>
                  <button
                    class="g-btn-ghost self-end sm:self-center !h-8 !px-2 !text-danger-ink hover:!bg-danger/10 shrink-0"
                    :title="t('dsh_env_delete')"
                    :aria-label="t('dsh_env_delete')"
                    @click="removeRow(row.key)"
                  >
                    <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                      <path d="M3 6h18" />
                      <path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
                      <path d="M10 11v6M14 11v6" />
                      <path d="M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2" />
                    </svg>
                  </button>
                </div>
                <p v-if="rowErrors[row.key]" class="text-xs text-danger-ink mt-1.5">{{ rowErrors[row.key] }}</p>
              </div>

              <p v-if="formError" class="text-xs text-danger-ink mt-2">{{ formError }}</p>
            </template>
          </div>

          <div class="g-dialog-actions">
            <button class="g-btn-secondary" @click="close">{{ t('dsh_env_cancel') }}</button>
            <button class="g-btn-secondary" :disabled="!ready || saving" @click="save">
              {{ saving ? t('dsh_env_saving') : t('dsh_env_save') }}
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
