// useReleaseNotes.ts —— 「点开某个 dsh 版本的更新日志」的获取与缓存。
//
// **只有 dsh 版本列表在用**（`ServerVersionsDialog` 的版本号按钮 → `ReleaseNotesDialog`）：
// 它是「点某一版看那一版」的列表，所以按版本号按需拉。插件市场的更新日志不走这里 ——
// 它和 harness 一样只取「要装的那一版」，正文随状态推送、内联显示（见 MarketDialog.vue
// 与后端 market.go 的 refreshMarketStatus）。
//
// 三条刻意的行为：
//  1. **成功才缓存**：available=false（这次没取到：断网 / 限流）与抛错都不进缓存，
//     否则用户重试也会一直看到「暂时取不到」；
//  2. **迟到的响应不覆盖当前内容**：弹窗开着时用户连点两个版本号，先发的那次可能后到，
//     这里丢掉它（对话框里显示的必须始终是最后一次点击的那个版本）；
//  3. **一次只拉一个**：正在请求同一个版本时不重复发（连点两下不会打两次接口）。
import { ref } from 'vue'
import type { ReleaseNotesPayload } from '@/serverapi'

export function useReleaseNotes(
  load: (version: string) => Promise<ReleaseNotesPayload>,
  errorTextOf: (e: unknown) => string
) {
  const version = ref('')
  const loading = ref(false)
  const available = ref(true)
  const note = ref('')
  const noteEn = ref('')
  const errorText = ref('')
  const cache = new Map<string, { note: string; noteEn: string }>()
  // 正在请求的版本号（连点同一版本不重复发）。用集合而不是单个布尔：连点两个不同版本时，
  // 先发的那次结束不能把后发那次还在转的 loading 状态清掉（否则弹窗显示「没有日志」）。
  const inFlight = new Set<string>()

  async function open(v: string) {
    if (!v) return
    version.value = v
    note.value = ''
    noteEn.value = ''
    errorText.value = ''
    available.value = true

    const cached = cache.get(v)
    if (cached) {
      note.value = cached.note
      noteEn.value = cached.noteEn
      return
    }
    if (inFlight.has(v)) return

    inFlight.add(v)
    loading.value = true
    try {
      const res = await load(v)
      if (res.available) cache.set(v, { note: res.note, noteEn: res.noteEn })
      // 用户可能已经点了别的版本：迟到的响应直接丢掉。
      if (version.value !== v) return
      available.value = res.available
      note.value = res.note
      noteEn.value = res.noteEn
    } catch (e) {
      if (version.value !== v) return
      available.value = false
      errorText.value = errorTextOf(e)
    } finally {
      inFlight.delete(v)
      // 只有「当前正在看的就是这个版本」才收起 loading。
      if (version.value === v) loading.value = false
    }
  }

  function close() {
    version.value = ''
  }

  return { version, loading, available, note, noteEn, errorText, open, close }
}
