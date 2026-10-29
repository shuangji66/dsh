// fnosAuth.ts —— 飞牛「独立 Web」目录授权（/app-auth/pick-user-file）。
//
// 背景：飞牛 App 内置 WebView 里，官方 SDK（@trimjs/web-app）的 pickUserFile 走
// Flutter 宿主桥接。iOS App 1.37.x 实测该桥接会按「应用共享授权」处理，个人目录
// 授权报应用鉴权异常（errcode 1003103），与桌面浏览器/独立 Web 的表现不同。
//
// 平台另有一条与桌面浏览器完全相同的纯网页授权通路：
//
//   GET <fnOS 网关>/app-auth/pick-user-file
//        ?appName=<应用名>&redirectUri=<回跳地址>&state=<随机串>&directory=true
//
// 用户在授权页选完目录后，页面用 location.replace 把结果作为 query 参数
// （status / method / appName / state / path）带回 redirectUri。
//
// App 的 WebView 没有可靠的多窗口支持，因此这里走「同窗口跳转」：redirectUri 指向
// 控制台自己的目录页，授权完成后整页回到控制台，由 DirectoriesView 解析结果。
// 这样不依赖 window.opener / window.open，也不依赖 callback.html。

export interface FnosAuthReturn {
  status: 'success' | 'cancel' | 'error'
  error?: string
  method?: string
  appName?: string
  state?: string
  path: string[]
}

/** sessionStorage 里待校验的 state（同一次 WebView 内跨整页跳转仍然有效）。 */
const AUTH_STATE_KEY = 'harness:fnos-app-auth-state'

/** 授权页回传、需要从控制台 URL 上清理掉的参数。 */
const AUTH_RETURN_PARAMS = ['status', 'method', 'appName', 'state', 'path', 'error'] as const

/**
 * 飞牛授权页所在的 origin。
 *
 * - 经飞牛网关访问（App / 桌面 iframe 的 `/app/…` 入口）：授权页就在同一个 origin；
 * - 独立 Web / 反代端口访问（如 `http://<host>:3079`）：授权页在网关的 5666 端口；
 * - HTTPS 的 443 端口通常也是网关本身（与独立 Web 的 `:5666` 约定一致）。
 *
 * 这里只做「从当前访问地址推断」，不引入额外设置项：App 场景一定走第一条。
 */
export function fnosAuthBase(): string {
  const { origin, pathname, protocol, hostname, port } = window.location

  // 网关前缀（/app/<AppName>/…）：授权页与控制台同源。
  if (/^\/app(?:\/|$)/.test(pathname)) return origin

  // HTTPS 标准端口：网关一般也在同一 origin 的根上。
  if (protocol === 'https:' && (port === '' || port === '443')) return origin

  // 其余情况（局域网反代端口 / 开发地址）：飞牛网关默认在 5666。
  return `${protocol}//${hostname}:5666`
}

/** 控制台目录页的绝对地址：授权完成后回到这里。 */
export function fnosAuthReturnURL(): string {
  // document.baseURI 由后端注入的 <base href> 决定（运行时 baseurl），不能写死前缀。
  const url = new URL(document.baseURI)
  url.searchParams.set('view', 'directory')
  return url.toString()
}

export interface BuildFnosAuthURLOptions {
  appName: string
  redirectUri: string
  state: string
  directory?: boolean
}

/** 拼装平台网页授权地址（字段与官方 SDK 的 buildAppAuthUrl 保持一致）。 */
export function buildFnosAuthURL(opts: BuildFnosAuthURLOptions): string {
  const url = new URL('/app-auth/pick-user-file', fnosAuthBase())
  url.searchParams.set('appName', opts.appName)
  url.searchParams.set('redirectUri', opts.redirectUri)
  url.searchParams.set('state', opts.state)
  if (opts.directory) url.searchParams.set('directory', 'true')
  // 与桌面/独立 Web 的目录选择器保持同一组侧边栏入口。
  url.searchParams.set('sidebarGroup', 'myFiles,external,favorites,team')
  return url.toString()
}

/** 记住本次授权的 state（同窗口跳转后回来校验）。 */
export function rememberFnosAuthState(state: string): void {
  try {
    sessionStorage.setItem(AUTH_STATE_KEY, state)
  } catch {
    /* 隐私模式 / 存储被禁用：state 只用于防串扰，失败不阻塞授权 */
  }
}

/** 取出并清掉待校验的 state。 */
export function takeFnosAuthState(): string {
  try {
    const state = sessionStorage.getItem(AUTH_STATE_KEY) || ''
    sessionStorage.removeItem(AUTH_STATE_KEY)
    return state
  } catch {
    return ''
  }
}

/** 解析授权页带回的参数；不是授权回传（没有 status）时返回 null。 */
export function readFnosAuthReturn(search = window.location.search): FnosAuthReturn | null {
  const q = new URLSearchParams(search)
  const rawStatus = q.get('status')
  if (!rawStatus) return null

  let paths: string[] = []
  const rawPath = q.get('path')
  if (rawPath) {
    try {
      const parsed = JSON.parse(rawPath)
      if (Array.isArray(parsed)) {
        paths = parsed.filter((item): item is string => typeof item === 'string' && item !== '')
      }
    } catch {
      /* path 解析失败按「没有路径」处理，结果仍按 status 展示 */
    }
  }

  return {
    status: rawStatus === 'success' || rawStatus === 'cancel' ? rawStatus : 'error',
    error: q.get('error') || undefined,
    method: q.get('method') || undefined,
    appName: q.get('appName') || undefined,
    state: q.get('state') || undefined,
    path: paths,
  }
}

/** 把授权回传参数从地址栏清掉（保留其它 query，如 view=directory）。 */
export function stripFnosAuthReturn(): void {
  const url = new URL(window.location.href)
  let changed = false
  for (const key of AUTH_RETURN_PARAMS) {
    if (url.searchParams.has(key)) {
      url.searchParams.delete(key)
      changed = true
    }
  }
  if (!changed) return
  try {
    // 保留 Vue Router 写入的 history.state，避免影响其内部状态。
    window.history.replaceState(window.history.state, '', url.toString())
  } catch {
    /* 地址栏清理失败不影响授权结果本身 */
  }
}
