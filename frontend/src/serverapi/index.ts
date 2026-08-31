// API client. The frontend is served over the unix admin socket under a baseurl
// prefix fronted by nginx. The backend injects a <base href> tag into index.html
// at runtime with the real baseurl, so we resolve API paths against
// document.baseURI (which reflects that baseurl) rather than the build-time
// relative BASE_URL — the prefix is NOT known at build time.
function runtimeBase(): string {
  if (typeof document !== 'undefined' && document.baseURI) {
    const p = new URL(document.baseURI).pathname
    return p.endsWith('/') ? p.slice(0, -1) : p
  }
  return import.meta.env.BASE_URL.replace(/\/$/, '') || ''
}

// sseUrl builds an absolute URL for a Server-Sent Events endpoint under the
// runtime base path (used by the overview visitor list and the log view).
export function sseUrl(path: string): string {
  return runtimeBase() + path
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(runtimeBase() + path, {
    headers: { 'Content-Type': 'application/json' },
    ...init
  })
  const data = await res.json().catch(() => null)
  if (!res.ok) {
    throw new Error((data && (data.error || data.msg)) || `HTTP ${res.status}`)
  }
  return data as T
}

export interface RuntimeInfo {
  configFile: string
  adminSock: string
  adminBaseURL: string
  appName: string
  fnosAvailable: boolean
  proxyPort: number
}

export interface AppConfig {
  dshPort: number
  proxyEnabled: boolean
  proxyAddr: string
  authEnabled: boolean
  password?: string
  authTTLHours: number
  dshMemLimit: number
  dshMemAuto: boolean
}

export interface DshStatus {
  running: boolean
  pid: number
  startedAt: string
  dshPort: number
  proxyPort: number
  locked: boolean
  cpuPercent?: number
  memoryMB?: number
}

export interface Visitor {
  id: string
  ip: string
  lastAccess: string
  expiresAt: string
}

export interface PluginInfo {
  name: string
  version: string
  resolved?: string
}

export interface SettingsPayload {
  config: AppConfig
  locked: boolean
  runtime: RuntimeInfo
  status: DshStatus
}

// 新增一个带自定义 headers 的 request 函数
async function requestWithHeaders<T>(path: string, init?: RequestInit, headers?: Record<string, string>): Promise<T> {
  const res = await fetch(runtimeBase() + path, {
    headers: { 'Content-Type': 'application/json', ...headers },
    ...init
  })
  const data = await res.json().catch(() => null)
  if (!res.ok) {
    throw new Error((data && (data.error || data.msg)) || `HTTP ${res.status}`)
  }
  return data as T
}

// serverapi/index.ts
export const api = {
  settings: () => request<SettingsPayload>('/api/settings'),
  saveSettings: (config: AppConfig) =>
    request<{ ok: boolean; locked: boolean; config: AppConfig }>('/api/settings', {
      method: 'POST',
      body: JSON.stringify({ config })
    }),
  dshStart: () => request<DshStatus>('/api/dsh/start', { method: 'POST' }),
  dshStop: () => request<DshStatus>('/api/dsh/stop', { method: 'POST' }),
  dshRestart: () => request<DshStatus>('/api/dsh/restart', { method: 'POST' }),
  dshStatus: () => request<DshStatus>('/api/dsh/status'),
  // 读取日志文件内容
  logs: () => request<{ ok: boolean; path: string; content: string; exists: boolean }>('/api/logs'),
  // 用户授权相关（已存在，确认导出）
  // 用户授权相关：通过请求头传递 uid
  fnosUserAccess: (uid: number) =>
    requestWithHeaders<{ ok: boolean; paths: string[]; msg: string }>(
      '/api/fnos/user-access',
      undefined,
      { 'X-Trim-Userid': String(uid) }
    ),

  fnosDeleteUserAccess: (uid: number, path: string) =>
    requestWithHeaders<{ ok: boolean; msg: string }>(
      '/api/fnos/user-access',
      {
        method: 'DELETE',
        body: JSON.stringify({ path })
      },
      { 'X-Trim-Userid': String(uid) }
    ),
  // 路径转换：通过后端代理调用 fnOS 的 trim.file.convertPath
  convertPath: (paths: string[], language?: string) =>
    request<{ ok: boolean; result: Array<{ path: string; semanticPath: string }> }>(
      '/api/fnos/convert-path',
      {
        method: 'POST',
        body: JSON.stringify({ paths, language: language || navigator.language || 'zh-CN' })
      }
    ),
  fnosPlatformConfig: () =>
    request<{ ok: boolean; data: Record<string, unknown> }>('/api/fnos/platform-config'),
  // 概览页：访客列表
  visitors: () => request<{ ok: boolean; visitors: Visitor[] }>('/api/visitors'),
  // 概览页：注销访客（该访客需重新登录）
  deleteVisitor: (id: string) =>
    request<{ ok: boolean; deleted: boolean; msg: string }>('/api/visitors', {
      method: 'DELETE',
      body: JSON.stringify({ id })
    }),
  // 插件管理：列表 / 卸载 / 重置（dsh plugin --profile web）
  listPlugins: () => request<{ ok: boolean; plugins: PluginInfo[]; raw?: string }>('/api/plugins'),
  removePlugin: (name: string) =>
    request<{ ok: boolean; removed: string; msg: string }>('/api/plugins/remove', {
      method: 'POST',
      body: JSON.stringify({ name })
    }),
  resetPlugins: () =>
    request<{
      ok: boolean
      started?: boolean
      error?: string
      profileDeleted?: boolean
      profilesDir?: string
    }>('/api/plugins/reset', {
      method: 'POST'
    })
}