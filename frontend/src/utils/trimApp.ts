// trimApp.ts —— 宿主 SDK（@trimjs/web-app）单例 + 「就绪等待」封装。
//
// 为什么必须在**页面加载时**就完成握手，而不是等资源页挂载：
// 飞牛桌面（宿主）在挂载应用 iframe 时建立 postmate 连接，握手超时写死 60 秒
// （宿主前端包内 n1e({ ..., timeout: 60 * 1e3 })，见 /usr/trim/www/assets/index-*.js）；
// 超时后宿主会 destroy 掉自己的 message 监听 —— 此后子页再发 SYN 也没人应答。
// 而 SDK 的初始化链在「宿主无应答」时会退化成一条**永不 settle**的连接
// （内部 FallbackConnection，无 timeout），且结果按模块缓存；于是
// sdk.ready() / openFileManager() / pickUserFile() 全部永久 pending：
// 资源页的「添加」「打开」点了毫无反应（既不抛错、也不弹 toast）。
//
// 实测（/usr/trim/nginx/logs/access.log 26/Sep）：
//   22:53:08 打开应用 iframe → 22:56:07 才进资源页（179s > 60s）→ 点「打开」后
//            宿主没有任何 app 启动请求（/app/token、/websocket?type=file 全无）；
//   22:57:04 重新打开应用窗口 → 1 秒后进资源页 → /app/token + 文件管理器 websocket
//            立刻出现，「打开」恢复正常。
//
// 所以这里做三件事：
//   1) 在页面加载时（main.ts 的 primeTrimApp()）就 new TrimApp()，把握手落在宿主的
//      60 秒窗口内；
//   2) 全局只建一个实例（SDK 内部本就按模块缓存宿主连接，重复 new 只是白跑一遍初始化链）；
//   3) ready 带超时：宿主监听已经销毁时（刷新页面也救不回来，只能回桌面重开应用窗口），
//      把「静默失效」变成一次可读的报错提示，避免按钮点了没反应。

import { TrimApp } from '@trimjs/web-app'

// 等待宿主桥接就绪的上限。宿主无应答时 SDK 要先跑完 1.5s（宿主探测）+ 5s（扩展宿主探测）
// 才进入永不 settle 的兜底连接，10 秒足够覆盖这一段，也远大于正常握手的耗时（通常 <100ms）。
const READY_TIMEOUT_MS = 10_000

let instance: TrimApp | null = null

/** 取宿主 SDK 单例（首次调用即触发初始化/握手）。 */
export function trimApp(): TrimApp {
  if (!instance) instance = new TrimApp()
  return instance
}

/** 页面加载时调用：尽早与宿主握手，避免错过宿主的 60 秒握手窗口。 */
export function primeTrimApp(): void {
  trimApp()
}

/**
 * 等待宿主桥接就绪。
 * 超时（宿主的握手监听已被销毁）返回 null，由调用方提示用户回桌面重开应用窗口。
 */
export async function trimAppReady(timeoutMs = READY_TIMEOUT_MS): Promise<TrimApp | null> {
  const sdk = trimApp()
  let timer: ReturnType<typeof setTimeout> | null = null
  const timeout = new Promise<null>((resolve) => {
    timer = setTimeout(() => {
      console.warn('[trimApp] host bridge not ready within', timeoutMs, 'ms')
      resolve(null)
    }, timeoutMs)
  })
  try {
    return await Promise.race([sdk.ready().then(() => sdk), timeout])
  } finally {
    if (timer) clearTimeout(timer)
  }
}
