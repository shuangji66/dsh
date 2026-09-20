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

export interface NodeVersionInfo {
  id: string // "node24" / "node26"
  label: string // 显示名，如 "Node.js v24"
  available: boolean
}

export interface RuntimeInfo {
  configFile: string
  adminSock: string
  adminBaseURL: string
  appName: string
  fnosAvailable: boolean
  proxyPort: number
  // node 版本切换选项（node24 始终可用；node26 仅在宿主机存在时可用）
  nodeVersions?: NodeVersionInfo[]
  // 主目录相关（资源页）：
  defaultHomeSemantic: string // 默认主目录的相对/语义路径，如 /var/apps/Harness/shares/Harness
  defaultHomeDir: string // 默认主目录的实际系统路径
  homeDir: string // 当前生效的主目录（dsh 的 HOME 实际路径）
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
  nodeVersion: string // dsh 启动使用的 node 版本，"node24"/"node26"，默认 "node24"
  homeDir?: string // 当前设置的主目录实际路径（用于保存配置时保留）
  accessUrls?: string[] // 用户配置的 dsh 访问地址列表
  // 浏览器兼容模式：修正 dsh 客户端只适配 V8 的原生函数格式判断，
  // 供 Firefox/Zen/Safari 等非 V8 内核正常加载会话历史。默认关闭。
  browserCompat: boolean
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
  // 该插件是否被 cordis.patch.yml 用户补丁层停用（前端据此显示启停状态）
  disabled?: boolean
  // 该插件被启用后是否需要重启 dsh 服务才能生效（客户端插件/带原生依赖）
  needsRestart?: boolean
}

// 终端页：用户自定义快捷指令（持久化到后端文件）
export interface QuickCmd {
  id: string
  name: string
  content: string
  auto: boolean
}

// 终端活动会话摘要（后端 /api/sessions 返回，前端启动时据此恢复会话）
export interface SessionInfo {
  id: string
  created: string
  size: number
  exited: boolean
}

export interface SettingsPayload {
  config: AppConfig
  locked: boolean
  runtime: RuntimeInfo
  status: DshStatus
}

// 更新检测结果（harness 控制台 / dsh 服务 / 插件市场 各一份）
export interface UpdateStatus {
  kind: UpdateKind
  localVersion: string
  latestVersion: string
  hasUpdate: boolean
  checkedAt: string
  error?: string
  // 最新 release 的更新内容（正文，不含标题；可能为空串。市场来自 npm，恒为空）
  releaseNotes?: string
  // 下载进度（更新包下载期间由后端 SSE 推送）
  downloading?: boolean // 是否正在下载更新包
  downloadPct?: number // 下载进度百分比（0-100；total 未知时为估算值）
  downloadedBytes?: number // 已下载字节数
  totalBytes?: number // 总字节数（未知为 0）
  // 两阶段更新流程阶段：
  // ''(空闲) / downloading(下载中) / paused(已暂停，可续传) / downloaded(已下载待安装) / installing(安装中)
  phase?: string
  // 更新包已下载就绪，等待用户点击“安装”
  readyToInstall?: boolean
  // 最近一次更新是否被用户主动取消
  cancelled?: boolean
  // 下载是否被用户暂停（phase=paused）：半成品已保留，可继续断点续传
  paused?: boolean
  // 后端给错误的结构化归类：目前只有 'network'（代理与直连各 2 次均失败），
  // 前端据此显示本地化的“请检查网络或代理”提示。
  errorHint?: string
  // 仅市场：当前生效的那份 dshmarket 由谁提供（见后端 market.go）
  // server = 由 server 包提供，控制台可就地更新；profile = 由 profile 提供，
  // 应在市场面板内更新；external = 位置在 server 目录之外；missing = 未找到。
  marketScope?: MarketScope
  // 仅市场：当前生效的安装目录（诊断用）
  marketDir?: string
}

export type UpdateKind = 'harness' | 'dsh' | 'market'

// 市场安装位置的归属
export type MarketScope = 'server' | 'profile' | 'external' | 'missing'

// 市场诊断信息（GET /api/market/info）
export interface MarketInfo {
  ok: boolean
  scope: MarketScope
  dir: string
  version: string
  latest: string
  updatable: boolean
  reason: string
  error?: string
}

// 自我更新 SSE 推送与 REST 接口的载荷
export interface UpdatePayload {
  harness: UpdateStatus
  dsh: UpdateStatus
  market: UpdateStatus
}

// dsh server 备份条目
export interface ServerBackup {
  name: string
  size: number
  modified: string
  path: string
}

// dsh server 回滚状态
export interface RollbackStatus {
  running: boolean
  done: boolean
  ok: boolean
  error?: string
}

// dsh 数据备份条目（~/.dsh 备份）
export interface DshDataBackup {
  name: string
  size: number
  modified: string
  path: string
}

// dsh 数据恢复状态
export interface DshRestoreStatus {
  running: boolean
  done: boolean
  ok: boolean
  error?: string
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
  // 资源页：把某个已授权目录设为 dsh 的 HOME（可选迁移 ~/.dsh 配置），确认后重启 dsh
  dshSetHome: (path: string, migrate: boolean) =>
    request<{
      ok: boolean
      changed?: boolean
      unchanged?: boolean
      homeDir?: string
      error?: string
      status?: DshStatus
    }>('/api/dsh/set-home', {
      method: 'POST',
      body: JSON.stringify({ path, migrate })
    }),
  // 目录页：备份当前 HOME 的 ~/.dsh 到统一备份目录 dsh-data-backup-<时间戳>.tar.gz
  dshBackup: () =>
    request<{ ok: boolean; name?: string; path?: string; size?: number; error?: string }>(
      '/api/dsh/backup',
      { method: 'POST' }
    ),
  // dsh 数据备份：列表 / 删除 / 恢复 / 恢复状态（对应目录页“恢复备份”）
  listDshDataBackups: () =>
    request<{ ok: boolean; backups: DshDataBackup[] }>('/api/dsh/data-backups'),
  deleteDshDataBackup: (name: string) =>
    request<{ ok: boolean; deleted: string }>('/api/dsh/data-backups', {
      method: 'DELETE',
      body: JSON.stringify({ name })
    }),
  dshDataRestore: (name: string) =>
    request<{ ok: boolean; started: boolean }>('/api/dsh/data-restore', {
      method: 'POST',
      body: JSON.stringify({ name })
    }),
  dshDataRestoreStatus: () =>
    request<{ ok: boolean; status: DshRestoreStatus }>('/api/dsh/data-restore/status'),
  // 统一备份目录实际路径（供转换语义路径）
  backupDir: () => request<{ ok: boolean; path: string }>('/api/dsh/backup-dir'),
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
  // 通过编辑 cordis.patch.yml 启停单个插件（机制学自 dsh-market）
  togglePlugin: (name: string, enabled: boolean) =>
    request<{ ok: boolean; name: string; enabled: boolean; rows?: string[]; restart?: boolean; refresh?: boolean; error?: string }>('/api/plugins/toggle', {
      method: 'POST',
      body: JSON.stringify({ name, enabled })
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
    }),
  // 自我更新：版本检测状态 / 手动检查 / 下载更新（可取消） / 安装更新 / 取消下载 / SSE 推送
  updateStatus: () =>
    request<{ ok: boolean; harness: UpdateStatus; dsh: UpdateStatus; market: UpdateStatus }>('/api/update/status'),
  updateCheck: () =>
    request<{ ok: boolean; harness: UpdateStatus; dsh: UpdateStatus; market: UpdateStatus }>('/api/update/check', {
      method: 'POST'
    }),
  // 市场诊断：当前生效的那份 dshmarket 在哪、由谁提供、能否由控制台更新
  marketInfo: () => request<MarketInfo>('/api/market/info'),
  updateDownload: (kind: UpdateKind) =>
    request<{ ok: boolean; started: boolean; kind: UpdateKind; msg?: string }>('/api/update/download', {
      method: 'POST',
      body: JSON.stringify({ kind })
    }),
  updateInstall: (kind: UpdateKind) =>
    request<{ ok: boolean; started: boolean; kind: UpdateKind; msg?: string }>('/api/update/install', {
      method: 'POST',
      body: JSON.stringify({ kind })
    }),
  // 取消正在进行的更新下载（下载完成后的取消请求被忽略）。半成品会被删除；
  // 想保留已下载字节下次续传请用 updatePause。
  updateCancel: () =>
    request<{ ok: boolean; cancelled: boolean }>('/api/update/cancel', {
      method: 'POST'
    }),
  // 暂停下载：半成品保留，再次调 updateDownload 即从已下载字节断点续传
  updatePause: () =>
    request<{ ok: boolean; paused: boolean }>('/api/update/pause', {
      method: 'POST'
    }),
  // 删除已下载待安装的更新包，重置为待更新状态
  updateDiscard: (kind: UpdateKind) =>
    request<{ ok: boolean; discarded: boolean }>('/api/update/discard', {
      method: 'POST',
      body: JSON.stringify({ kind })
    }),
  // dsh server 回滚：备份列表 / 删除备份 / 回滚 / 回滚状态
  listBackups: () => request<{ ok: boolean; backups: ServerBackup[] }>('/api/dsh/backups'),
  deleteBackup: (name: string) =>
    request<{ ok: boolean; deleted: string }>('/api/dsh/backups', {
      method: 'DELETE',
      body: JSON.stringify({ name })
    }),
  rollback: (name: string) =>
    request<{ ok: boolean; started: boolean }>('/api/dsh/rollback', {
      method: 'POST',
      body: JSON.stringify({ name })
    }),
  rollbackStatus: () => request<{ ok: boolean; status: RollbackStatus }>('/api/dsh/rollback/status'),
  // 终端页：快捷指令列表 / 整体保存
  listQuickCmds: () => request<{ ok: boolean; path: string; commands: QuickCmd[] }>('/api/quickcmds'),
  saveQuickCmds: (commands: QuickCmd[]) =>
    request<{ ok: boolean; path: string; commands: QuickCmd[] }>('/api/quickcmds', {
      method: 'POST',
      body: JSON.stringify({ commands })
    }),
  // 终端会话：列表 / 历史 / 关闭 / 清屏（临时镜像保存与恢复）
  sessions: () => request<{ ok: boolean; sessions: SessionInfo[] }>('/api/sessions'),
  sessionHistory: (id: string) =>
    request<{ ok: boolean; id: string; size: number; content: string }>(
      '/api/session/history?id=' + encodeURIComponent(id)
    ),
  closeSession: (id: string) =>
    request<{ ok: boolean; id: string }>('/api/session?id=' + encodeURIComponent(id), { method: 'DELETE' }),
  clearSessionHistory: (id: string) =>
    request<{ ok: boolean; id: string }>('/api/session/clear?id=' + encodeURIComponent(id), { method: 'POST' })
}