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
  dshBin: string
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
}

export interface DshStatus {
  running: boolean
  pid: number
  startedAt: string
  dshPort: number
  proxyPort: number
  locked: boolean
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
  fnosPlatformConfig: () =>
    request<{ ok: boolean; data: Record<string, unknown> }>('/api/fnos/platform-config')
}