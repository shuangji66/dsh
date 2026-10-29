import { onBeforeUnmount, ref, toValue, watch } from 'vue'
import type { MaybeRefOrGetter, Ref } from 'vue'

// useEventStream 管理一条「会自愈」的 SSE 连接。
//
// 为什么需要它：EventSource 自带重连，但仅限**非致命**断开；一旦显式调用
// close()，浏览器就永久放弃这条连接。此前各视图都在 onerror 里 close()，注释
// 却写着「EventSource 会内置重连」——注释与行为相反。于是反向代理按
// proxy_read_timeout（fnOS 网关 300s）掐断空闲长连接后，前端永远不会重连，
// 后续推送（例如更新包下载进度）就永久收不到，页面卡在旧状态（表现为「实际在
// 下载却一直 0%」）。这里统一改为：断开后按指数退避自动重连，重连成功后后端会
// 立刻下发一份初始快照，状态自然追平。
//
// 用法（url 支持 ref / getter，便于跟随运行期 baseurl 变化）：
//   const { start, stop, connected } = useEventStream(
//     () => sseUrl('/api/update/stream'),
//     { update: (data) => merge(data) }
//   )
//   start()
export interface UseEventStreamOptions {
  /** 重连退避上限（毫秒），默认 15s。 */
  maxDelayMs?: number
  /**
   * 页面进入后台（document.hidden）时断开，回到前台再重连。默认 true。
   *
   * 后端的 dsh 状态流是**每秒**推一次（backend/sse.go 的 handleDshStream，Status() 还要
   * 读 /proc），多标签页/手机切后台时会一直白烧 CPU 与电量。断开是安全的：重连成功后
   * 后端会立刻补发初始快照（各 SSE handler 的首帧都是完整快照），状态自然追平。
   * 终端页的 WebSocket 不走这里，不受影响（会话必须保持挂载）。
   */
  pauseWhenHidden?: boolean
}

export function useEventStream(
  url: MaybeRefOrGetter<string>,
  handlers: Record<string, (data: unknown, ev: MessageEvent) => void>,
  options: UseEventStreamOptions = {}
) {
  const maxDelayMs = options.maxDelayMs ?? 15000
  const pauseWhenHidden = options.pauseWhenHidden ?? true
  /** 当前连接是否存活（已 open 且未断开）。 */
  const connected = ref(false)

  let es: EventSource | null = null
  let retryTimer: ReturnType<typeof setTimeout> | null = null
  let delay = 1000
  let stopped = false
  // 因页面隐藏而主动断开：与 stop() 不同，可见时会自动恢复。
  let paused = false

  function clearRetry() {
    if (retryTimer) {
      clearTimeout(retryTimer)
      retryTimer = null
    }
  }

  function scheduleReconnect() {
    if (stopped || paused || retryTimer) return
    retryTimer = setTimeout(() => {
      retryTimer = null
      open()
    }, delay)
    // 指数退避，避免后端未就绪时高频重试
    delay = Math.min(delay * 2, maxDelayMs)
  }

  function open() {
    if (stopped || paused) return
    es?.close()
    const s = new EventSource(toValue(url))
    es = s
    s.onopen = () => {
      // 已被 pause / stop / 新连接替换：这条迟到的 open 不能改当前连接的状态。
      if (es !== s) return
      // 连上即重置退避；后端会在建立连接时立刻推送初始快照。
      delay = 1000
      connected.value = true
    }
    for (const [event, handler] of Object.entries(handlers)) {
      s.addEventListener(event, (ev) => {
        try {
          handler(JSON.parse((ev as MessageEvent).data), ev as MessageEvent)
        } catch {
          /* 忽略畸形帧，不影响连接 */
        }
      })
    }
    s.onerror = () => {
      // 已被 pause / stop / 新连接替换：旧连接的迟到 error 不再影响当前连接，
      // 否则会在恢复可见后多排一次重连（把刚建好的连接又替换掉）。
      if (es !== s) return
      // 连接断开（网络抖动、反代掐断空闲长连接、后端重启等）：只关掉这条
      // 已失效的连接，然后安排重连；绝不能就此永久放弃。
      connected.value = false
      s.close()
      es = null
      scheduleReconnect()
    }
  }

  /** 因页面隐藏而断开（可见时自动恢复）。 */
  function pause() {
    if (stopped || paused) return
    paused = true
    clearRetry()
    es?.close()
    es = null
    connected.value = false
  }

  /** 页面回到前台：重建连接，后端会补一份初始快照。 */
  function resume() {
    if (stopped || !paused) return
    paused = false
    delay = 1000
    open()
  }

  function onVisibilityChange() {
    if (!pauseWhenHidden) return
    if (document.hidden) pause()
    else resume()
  }

  /** 建立连接（重复调用会先释放旧连接）。 */
  function start() {
    stopped = false
    clearRetry()
    delay = 1000
    // 在后台启动就先不连：等 visibilitychange 恢复。
    if (pauseWhenHidden && typeof document !== 'undefined' && document.hidden) {
      paused = true
      return
    }
    paused = false
    open()
  }

  /** 主动停止并释放连接（不再重连）。 */
  function stop() {
    stopped = true
    paused = false
    clearRetry()
    es?.close()
    es = null
    connected.value = false
  }

  // url 变化（如运行期 baseurl 变更）时重建连接
  watch(
    () => toValue(url),
    () => {
      if (!stopped) start()
    }
  )

  if (pauseWhenHidden && typeof document !== 'undefined') {
    document.addEventListener('visibilitychange', onVisibilityChange)
  }
  onBeforeUnmount(() => {
    if (pauseWhenHidden && typeof document !== 'undefined') {
      document.removeEventListener('visibilitychange', onVisibilityChange)
    }
    stop()
  })

  return { start, stop, connected: connected as Ref<boolean> }
}
