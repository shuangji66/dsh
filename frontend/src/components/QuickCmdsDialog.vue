<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from '@/composables/useI18n'
import { useBodyScrollLock } from '@/composables/useBodyScrollLock'
import DialogCloseButton from '@/components/DialogCloseButton.vue'
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

// 弹窗打开期间锁定页面滚动，避免在弹窗背后继续滚动/拖动页面
useBodyScrollLock(() => open.value)

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
      <div v-if="open" class="fixed inset-0 z-50 flex items-center justify-center p-4 backdrop-blur-sm">
        <div class="g-modal-mask" @click="close"></div>
        <div
          class="relative w-full max-w-lg h-[90dvh] max-h-full bg-white dark:bg-[#16161B] border border-[#E8E8EC] dark:border-[#2A2A32] rounded-xl shadow-card flex flex-col"
        >
          <!-- 右上角 X：所有弹窗统一（见 DialogCloseButton.vue） -->
          <DialogCloseButton :label="t('dialog_close')" @close="close" />
          <!-- 顶部：标题 + 新增。
               新增与右上角的 X 同样是「32px 方形的边框图标按钮」（同尺寸、同边框、不填色，
               只是图标不同），右侧留出 pr-14 给绝对定位的 X，避免两个按钮叠在一起。 -->
          <div class="flex items-center justify-between px-5 py-3 pr-14 border-b border-line dark:border-[#2A2A32] shrink-0">
            <h3 class="font-display text-base font-semibold text-ink dark:text-white">{{ t('qc_title') }}</h3>
            <button class="g-btn-secondary !h-8 !w-8 !p-0" :title="t('qc_add_title')" :aria-label="t('qc_add_title')" @click="emit('add')">
              <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19" /><line x1="5" y1="12" x2="19" y2="12" /></svg>
            </button>
          </div>

          <!-- 命令卡片列表：弹窗整体高度固定为终端页的 90%，列表占满剩余空间并滚动 -->
          <div class="flex-1 min-h-0 overflow-y-auto px-5 py-4 space-y-3">
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
                <!-- 第一行：命令名称 + 自动执行标签，右侧同一行右对齐四个图标按钮（编辑/删除/上移/下移）；
                     紧凑化前「编辑/删除/上移/下移」独占第二行，卡片高度白白多一行 -->
                <div class="flex items-center gap-2 min-w-0">
                  <span class="min-w-0 text-sm font-semibold text-ink dark:text-white truncate">{{ c.name }}</span>
                  <span
                    v-if="c.auto"
                    class="shrink-0 inline-flex items-center px-1.5 py-0.5 rounded-full text-[10px] font-medium bg-brand/10 text-brand dark:bg-brand/20 dark:text-brand"
                  >{{ t('qc_auto_tag') }}</span>
                  <div class="flex-1"></div>
                  <div class="flex items-center gap-1 shrink-0">
                    <!-- 编辑（铅笔） -->
                    <button class="g-btn-ghost !h-7 !px-2" @click.stop="emit('edit', c)">
                      <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <path d="M17 3a2.828 2.828 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5L17 3z" />
                      </svg>
                    </button>
                    <!-- 删除（垃圾桶，红色） -->
                    <button
                      class="g-btn-ghost !h-7 !px-2 !text-danger hover:!bg-danger/10"
                      @click.stop="emit('delete', c)"
                    >
                      <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <path d="M3 6h18" />
                        <path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
                        <path d="M10 11v6M14 11v6" />
                        <path d="M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2" />
                      </svg>
                    </button>
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
                <!-- 命令内容：单行省略，绝不溢出卡片 -->
                <p class="mt-1 text-xs font-mono text-ink-soft dark:text-[#A6A6AD] truncate">{{ c.content }}</p>
              </div>
            </div>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
