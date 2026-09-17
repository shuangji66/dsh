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
}

export function useEventStream(
  url: MaybeRefOrGetter<string>,
  handlers: Record<string, (data: unknown, ev: MessageEvent) => void>,
  options: UseEventStreamOptions = {}
) {
  const maxDelayMs = options.maxDelayMs ?? 15000
  /** 当前连接是否存活（已 open 且未断开）。 */
  const connected = ref(false)

  let es: EventSource | null = null
  let retryTimer: ReturnType<typeof setTimeout> | null = null
  let delay = 1000
  let stopped = false

  function clearRetry() {
    if (retryTimer) {
      clearTimeout(retryTimer)
      retryTimer = null
    }
  }

  function scheduleReconnect() {
    if (stopped || retryTimer) return
    retryTimer = setTimeout(() => {
      retryTimer = null
      open()
    }, delay)
    // 指数退避，避免后端未就绪时高频重试
    delay = Math.min(delay * 2, maxDelayMs)
  }

  function open() {
    if (stopped) return
    es?.close()
    const s = new EventSource(toValue(url))
    es = s
    s.onopen = () => {
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
      // 连接断开（网络抖动、反代掐断空闲长连接、后端重启等）：只关掉这条
      // 已失效的连接，然后安排重连；绝不能就此永久放弃。
      connected.value = false
      s.close()
      if (es === s) es = null
      scheduleReconnect()
    }
  }

  /** 建立连接（重复调用会先释放旧连接）。 */
  function start() {
    stopped = false
    clearRetry()
    delay = 1000
    open()
  }

  /** 主动停止并释放连接（不再重连）。 */
  function stop() {
    stopped = true
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

  onBeforeUnmount(stop)

  return { start, stop, connected: connected as Ref<boolean> }
}
