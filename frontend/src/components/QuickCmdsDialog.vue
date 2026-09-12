<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from '@/composables/useI18n'
import type { QuickCmd } from '@/serverapi'

const props = defineProps<{
  visible: boolean
  commands: QuickCmd[]
  loading?: boolean
}>()

const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void
  (e: 'add'): void
  (e: 'edit', cmd: QuickCmd): void
  (e: 'delete', cmd: QuickCmd): void
  (e: 'run', cmd: QuickCmd): void
  (e: 'reorder', ordered: QuickCmd[]): void
}>()

const { t } = useI18n()

const open = ref(props.visible)
const reordering = ref(false)
watch(
  () => props.visible,
  (v) => (open.value = v)
)

function close() {
  open.value = false
  emit('update:visible', false)
}

// 上移/下移：交换相邻项后整体提交新顺序，由父组件持久化
function moveUp(idx: number) {
  if (idx <= 0 || reordering.value) return
  const arr = props.commands.slice()
  ;[arr[idx - 1], arr[idx]] = [arr[idx], arr[idx - 1]]
  reordering.value = true
  emit('reorder', arr)
  reordering.value = false
}

function moveDown(idx: number) {
  if (idx >= props.commands.length - 1 || reordering.value) return
  const arr = props.commands.slice()
  ;[arr[idx], arr[idx + 1]] = [arr[idx + 1], arr[idx]]
  reordering.value = true
  emit('reorder', arr)
  reordering.value = false
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
      <div v-if="open" class="fixed inset-0 z-50 flex items-center justify-center p-4">
        <div class="absolute inset-0 bg-black/50" @click="close"></div>
        <div
          class="relative w-full max-w-lg bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card flex flex-col"
        >
          <div class="flex items-center justify-between px-5 py-4 border-b border-line dark:border-[#2A2A32]">
            <h3 class="font-display text-lg font-semibold text-ink dark:text-white">{{ t('qc_title') }}</h3>
            <div class="flex items-center gap-2">
              <button class="g-btn-primary !h-8 px-4 text-sm" @click="emit('add')">{{ t('qc_add') }}</button>
              <button class="g-btn-ghost !h-8 !px-2 text-lg leading-none" title="close" @click="close">×</button>
            </div>
          </div>

          <!-- 命令卡片列表 -->
          <div class="flex-1 overflow-y-auto px-5 py-4 space-y-3 max-h-[50vh]">
            <div v-if="props.loading" class="text-sm text-ink-soft dark:text-[#8A8A92] text-center py-8">
              {{ t('loading') }}
            </div>
            <div v-else-if="!props.commands.length" class="text-sm text-ink-soft dark:text-[#8A8A92] text-center py-8">
              {{ t('qc_empty') }}
            </div>
            <div
              v-for="(c, idx) in props.commands"
              :key="c.id"
              class="group rounded-lg border border-[#E8E8EC] dark:border-[#2A2A32] hover:border-brand/50 dark:hover:border-brand/50 cursor-pointer transition-colors"
              :title="c.content"
              @click="emit('run', c)"
            >
              <div class="px-3.5 py-2.5">
                <div class="flex items-center gap-2 min-w-0">
                  <span class="text-sm font-semibold text-ink dark:text-white truncate">{{ c.name }}</span>
                  <span
                    v-if="c.auto"
                    class="shrink-0 inline-flex items-center px-1.5 py-0.5 rounded-full text-[10px] font-medium bg-brand/10 text-brand dark:bg-brand/20 dark:text-brand"
                  >{{ t('qc_auto_tag') }}</span>
                </div>
                <!-- 命令内容：单行省略，绝不溢出卡片 -->
                <p class="mt-1 text-xs font-mono text-ink-soft dark:text-[#A6A6AD] truncate">{{ c.content }}</p>
              </div>
              <!-- 第二行：左侧 编辑/删除，右侧 上移/下移（SVG 图标） -->
              <div class="flex items-center gap-2 px-3.5 py-2 border-t border-line dark:border-[#2A2A32]">
                <button class="g-btn-ghost !h-7 !px-2.5 text-xs" :title="t('qc_edit')" @click.stop="emit('edit', c)">
                  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                    stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="w-3.5 h-3.5">
                    <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7" />
                    <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z" />
                  </svg>
                  {{ t('qc_edit') }}
                </button>
                <button class="g-btn-ghost !h-7 !px-2.5 text-xs !text-[#EF4444] hover:!bg-[#EF4444]/10" :title="t('qc_delete')" @click.stop="emit('delete', c)">
                  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                    stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="w-3.5 h-3.5">
                    <path d="M3 6h18" />
                    <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
                  </svg>
                  {{ t('qc_delete') }}
                </button>
                <div class="flex-1"></div>
                <!-- 上移（第一项不可上移） -->
                <button
                  v-if="idx > 0"
                  class="g-btn-ghost !h-7 !px-2"
                  :title="t('qc_move_up')"
                  :disabled="reordering"
                  @click.stop="moveUp(idx)"
                >
                  <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M18 15l-6-6-6 6" />
                  </svg>
                </button>
                <!-- 下移（最后一项不可下移） -->
                <button
                  v-if="idx < props.commands.length - 1"
                  class="g-btn-ghost !h-7 !px-2"
                  :title="t('qc_move_down')"
                  :disabled="reordering"
                  @click.stop="moveDown(idx)"
                >
                  <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M6 9l6 6 6-6" />
                  </svg>
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>