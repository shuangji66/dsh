import { ref } from 'vue'
import { api, type QuickCmd } from '@/serverapi'
import { useToastStore } from '@/stores/toast'
import { uiErrText, useI18n } from '@/composables/useI18n'
import { randomId } from '@/utils/randomId'

// 终端快捷指令的状态与持久化（列表 / 新增 / 编辑 / 删除 / 排序 / 执行）。
//
// 从 TerminalView 抽出来：这些逻辑与 xterm/WebSocket 生命周期无关，只有「执行」需要
// 往终端写数据，所以由调用方注入三个 IO 回调；组件里只留弹窗与模板。
export interface QuickCmdsIO {
  /** 向终端发送文本；未连接时返回 false（由本 composable 统一提示）。 */
  send: (data: string) => boolean
  focusTerminal: () => void
  scrollToBottom: () => void
}

export function useQuickCmds(io: QuickCmdsIO) {
  const toast = useToastStore()
  const { t } = useI18n()

  const commands = ref<QuickCmd[]>([])
  const loading = ref(false)
  // 加载失败标志：后端返回的是整表，**没加载成功就不能改**（本地那份可能是空的/过期的，
  // 保存上去等于用一条指令覆盖掉服务端已有的全部指令），所以弹窗要据此禁用「新增」并给重试入口。
  const failed = ref(false)

  const listVisible = ref(false)
  const editVisible = ref(false)
  const editing = ref<QuickCmd | null>(null)
  const deleteVisible = ref(false)
  const deleteTarget = ref<QuickCmd | null>(null)

  // 在途请求：并发调用（打开弹窗时连点、重开弹窗）复用它，而不是直接 return。
  //
  // **不能写成 `if (loading.value) return`**：`api.request()` 没有超时/Abort，首个请求一旦
  // 挂住，loading 就永远是 true —— 关掉再打开弹窗只会撞上这个 guard，重试按钮又只在
  // failed 分支渲染，用户只能整页刷新（丢当前终端会话）。这里改成复用同一个 promise，
  // 并给一个「卡太久就允许重发」的出口：UI 上仍然是 loading，但重开会发起一次新请求。
  let pending: Promise<void> | null = null
  let pendingSince = 0
  // 请求代号：被 15 秒出口放弃的旧请求晚到时，不能覆盖新请求的结果、也不能清掉它的状态。
  let loadSeq = 0
  const LOAD_STALL_MS = 15_000

  function load(): Promise<void> {
    const now = Date.now()
    if (pending && now - pendingSince < LOAD_STALL_MS) return pending
    pendingSince = now
    const seq = ++loadSeq
    const run = (async () => {
      loading.value = true
      failed.value = false
      try {
        const res = await api.listQuickCmds()
        if (seq === loadSeq) commands.value = res.commands || []
      } catch (e) {
        if (seq !== loadSeq) return
        // 失败要**留下痕迹**：不能只在 toast 里闪一下，然后让弹窗显示「暂无快捷指令」——
        // 那会让用户以为自己的指令被删了，转头重建一份（而保存是整体覆盖，会把服务端那份盖掉）。
        failed.value = true
        toast.show(uiErrText(e, t('qc_load_failed')), 'error')
        console.warn('load quick cmds error:', e)
      } finally {
        if (seq === loadSeq) {
          loading.value = false
          pending = null
        }
      }
    })()
    pending = run
    return run
  }

  function open() {
    listVisible.value = true
    void load()
  }

  // 改动前的深拷贝快照：回滚路径必须回到「用户动手之前」的状态。
  // 旧实现把快照拍在 persist 内部，那时本地改动已经发生，回滚写回的是「已改动」状态。
  function snapshot(): QuickCmd[] {
    return commands.value.map((c) => ({ ...c }))
  }

  // 把当前列表整体写回持久化文件；失败时回滚到调用方在改动之前拍下的快照
  async function persist(rollbackTo: QuickCmd[]) {
    try {
      const res = await api.saveQuickCmds(commands.value)
      commands.value = res.commands || commands.value
    } catch (e) {
      commands.value = rollbackTo
      toast.show(t('qc_save_failed'), 'error')
      console.warn('save quick cmds error:', e)
      throw e
    }
  }

  // 将命令内容写入终端；勾选“自动执行”的命令在输入后自动回车执行，
  // 否则仅输入命令文本，由用户自行回车确认。
  function run(cmd: QuickCmd) {
    // 多行内容统一转成回车（换行即输入命令的一部分）
    const payload = cmd.content.replace(/\r?\n/g, '\r') + (cmd.auto ? '\r' : '')
    if (!io.send(payload)) {
      toast.show(t('qc_not_connected'), 'error')
      return
    }
    io.scrollToBottom()
    // 点击命令卡片后自动关闭弹窗，并把光标聚焦到终端
    listVisible.value = false
    io.focusTerminal()
  }

  function startAdd() {
    editing.value = null
    editVisible.value = true
  }

  function startEdit(cmd: QuickCmd) {
    // 直接持有列表中的引用，保存时据此更新对应卡片
    editing.value = cmd
    editVisible.value = true
  }

  function startDelete(cmd: QuickCmd) {
    deleteTarget.value = cmd
    deleteVisible.value = true
  }

  async function confirmDelete() {
    const target = deleteTarget.value
    if (!target) return
    const rollback = snapshot() // 必须在改动之前拍
    commands.value = commands.value.filter((c) => c.id !== target.id)
    deleteTarget.value = null
    deleteVisible.value = false
    try {
      await persist(rollback)
      toast.show(t('qc_deleted'), 'success')
    } catch {
      // persist 已提示失败并把列表回滚到改动前
    }
  }

  async function save(payload: { name: string; content: string; auto: boolean }) {
    const rollback = snapshot() // 改动之前拍快照（编辑是就地改，push 是新增）
    if (editing.value) {
      editing.value.name = payload.name
      editing.value.content = payload.content
      editing.value.auto = payload.auto
    } else {
      commands.value.push({
        id: randomId(),
        name: payload.name,
        content: payload.content,
        auto: payload.auto
      })
    }
    editVisible.value = false
    try {
      await persist(rollback)
      toast.show(t('qc_saved'), 'success')
    } catch {
      // persist 已提示失败并回滚（编辑场景同样还原为改动前的内容）
    }
  }

  // 上移/下移：QuickCmdsDialog 已算出新顺序，这里整体写回持久化文件
  async function reorder(ordered: QuickCmd[]) {
    const rollback = snapshot() // 改动之前拍快照
    commands.value = ordered
    try {
      await persist(rollback)
    } catch {
      // persist 已提示失败并回滚为改动前的顺序
    }
  }

  return {
    commands,
    loading,
    failed,
    listVisible,
    editVisible,
    editing,
    deleteVisible,
    deleteTarget,
    open,
    reload: load,
    run,
    startAdd,
    startEdit,
    startDelete,
    confirmDelete,
    save,
    reorder
  }
}
