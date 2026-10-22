/**
 * The plugin's same-origin API, as seen by the browser half.
 *
 * Every call targets `/fnos-advanced/*` resolved against `document.baseURI`, so
 * the plugin keeps working under the console's runtime base URL (`<base href>`)
 * and under the dsh sub-path mount — never hardcode an origin or a prefix.
 */
import { DEFAULT_SETTINGS, isAuthMode, normalizeBase, type PluginSettings } from '../shared/settings.ts';
import { AUTH_BASE_KEY } from './constants.ts';
import { t, translateError } from './i18n.ts';
import { fileNameForPath } from './files.ts';

export interface PluginConfig {
  ok?: boolean;
  appName?: string;
  authBase?: string;
  version?: string;
}

export interface PluginFeatures {
  filePicker?: boolean;
  imageRead?: boolean;
  workspaceHealth?: boolean;
}

export interface PluginHealth {
  ok?: boolean;
  appName?: string;
  version?: string;
  features?: PluginFeatures;
}

export interface WorkspaceHealthRow {
  id: string;
  path: string;
  title: string;
  exists: boolean;
  directory: boolean;
  readable: boolean;
  writable: boolean;
  error: string;
}

export interface WorkspaceSummary {
  id: string;
  path: string;
  title: string;
}

/** How the host settings endpoint answered last time it was read. */
export type SettingsHostState = 'unknown' | 'ok' | 'missing' | 'error';

export interface AuthChoice {
  id: 'proxy' | 'lan';
  label: string;
  detail: string;
  base: string;
}

/**
 * Where the web authorization page should be loaded from:
 * - `gateway`: the page itself is served by the fnOS gateway (same origin);
 * - `single`: exactly one usable entry — no chooser;
 * - `choose`: both entries are configured and the user picks one.
 */
export type AuthTarget =
  | { mode: 'gateway'; base: string; via: 'gateway' }
  | { mode: 'single'; base: string; via: 'proxy' | 'lan' | 'auto' }
  | { mode: 'choose'; choices: AuthChoice[] };

/** Resolve a same-origin plugin path against the document base URL. */
export function pluginURL(relative: string): string {
  const base = document.baseURI.endsWith('/') ? document.baseURI : `${document.baseURI}/`;
  return new URL(relative, base).href;
}

let configPromise: Promise<PluginConfig> | null = null;

/** Host config (app name / version); cached for the life of the page. */
export function loadConfig(): Promise<PluginConfig> {
  if (!configPromise) {
    configPromise = (async () => {
      try {
        const response = await fetch(pluginURL('fnos-advanced/config'), {
          credentials: 'same-origin',
          cache: 'no-store',
          headers: { Accept: 'application/json' },
        });
        if (response.ok) {
          const data = await response.json() as PluginConfig;
          if (data && typeof data === 'object') return data;
        }
      } catch { /* the plugin may be loaded from a static bundle */ }
      return { appName: 'Harness', authBase: '' };
    })();
  }
  return configPromise;
}

let settingsCache: PluginSettings | null = null;
let settingsCachedAt = 0;
let settingsPromise: Promise<PluginSettings> | null = null;
let hostState: SettingsHostState = 'unknown';

/** Result of the last settings read; the settings page reports it. */
export function getSettingsHostState(): SettingsHostState {
  return hostState;
}

/** Normalize a settings payload from the host into the client shape. */
export function normalizeClientSettings(input: unknown): PluginSettings {
  const record: Record<string, unknown> = typeof input === 'object' && input !== null
    ? input as Record<string, unknown>
    : {};
  return {
    proxyBase: normalizeBase(typeof record.proxyBase === 'string' ? record.proxyBase : ''),
    lanTarget: typeof record.lanTarget === 'string' && record.lanTarget.trim()
      ? record.lanTarget.trim()
      : DEFAULT_SETTINGS.lanTarget,
    authMode: isAuthMode(record.authMode) ? record.authMode : DEFAULT_SETTINGS.authMode,
    imageRead: record.imageRead !== false,
  };
}

/**
 * Read the settings (5s cache). `force` bypasses both the cache and an in-flight
 * request; the settings page uses it on mount so a just-saved value is not
 * shadowed by a cached read.
 */
export async function loadSettings(force = false): Promise<PluginSettings> {
  if (!force && settingsCache && Date.now() - settingsCachedAt < 5000) return settingsCache;
  if (!force && settingsPromise) return settingsPromise;

  const request = (async (): Promise<PluginSettings> => {
    try {
      const response = await fetch(pluginURL('fnos-advanced/settings'), {
        credentials: 'same-origin',
        cache: 'no-store',
        headers: { Accept: 'application/json' },
      });
      if (response.ok) {
        const data = await response.json() as { ok?: boolean; settings?: unknown };
        if (data && data.ok && data.settings) {
          settingsCache = normalizeClientSettings(data.settings);
          settingsCachedAt = Date.now();
          hostState = 'ok';
          return settingsCache;
        }
        hostState = 'error';
      } else if (response.status === 404 || response.status === 405) {
        hostState = 'missing';
      } else {
        hostState = 'error';
      }
    } catch {
      hostState = 'error';
    }

    return settingsCache || { ...DEFAULT_SETTINGS };
  })();

  settingsPromise = request;
  try {
    return await request;
  } finally {
    if (settingsPromise === request) settingsPromise = null;
  }
}

/**
 * Persist settings. A missing endpoint means the plugin was upgraded without
 * restarting dsh — say so instead of a bare 404.
 */
export async function saveSettings(input: PluginSettings): Promise<PluginSettings> {
  const proxyBase = validateProxyBase(input.proxyBase);
  const response = await fetch(pluginURL('fnos-advanced/settings'), {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify({
      proxyBase,
      lanTarget: String(input.lanTarget || '').trim() || DEFAULT_SETTINGS.lanTarget,
      authMode: input.authMode,
      imageRead: input.imageRead !== false,
    }),
  });
  if (response.status === 404 || response.status === 405) {
    throw new Error(t('宿主半侧未提供设置接口，通常是插件升级后没有重启 dsh。请重启 dsh 后再保存设置。'));
  }
  const data = await response.json().catch(() => ({})) as { ok?: boolean; error?: string; settings?: unknown };
  if (!response.ok || !data.ok) {
    throw new Error(translateError(data.error) || t('保存失败（HTTP {status}）', { status: response.status }));
  }
  const saved = normalizeClientSettings(data.settings);
  settingsCache = saved;
  settingsCachedAt = Date.now();
  return saved;
}

/** Diagnostics feature flags, or `null` when the host does not answer. */
export async function fetchPluginHealth(): Promise<PluginHealth | null> {
  try {
    const response = await fetch(pluginURL('fnos-advanced/health'), {
      credentials: 'same-origin',
      cache: 'no-store',
    });
    if (!response.ok) return null;
    return await response.json() as PluginHealth;
  } catch {
    return null;
  }
}

/** Workspace id → path, fed by the live `workspaces` service. */
let workspacePaths = new Map<string, string>();

/** Replace the known workspace map (live registry snapshot). */
export function setWorkspacePaths(entries: Iterable<readonly [string, string]>): void {
  workspacePaths = new Map(entries);
}

/** Unique workspace paths, from the live service or the host registry. */
export async function knownWorkspacePaths(): Promise<string[]> {
  if (workspacePaths.size === 0) {
    try {
      const response = await fetch(pluginURL('fnos-advanced/workspaces'), {
        credentials: 'same-origin',
        cache: 'no-store',
        headers: { Accept: 'application/json' },
      });
      if (response.ok) {
        const data = await response.json() as { workspaces?: WorkspaceSummary[] };
        const items = data && Array.isArray(data.workspaces) ? data.workspaces : [];
        for (const workspace of items) {
          if (workspace && workspace.id && typeof workspace.path === 'string') {
            workspacePaths.set(String(workspace.id), workspace.path);
          }
        }
      }
    } catch { /* old host or transient failure: no disabled paths */ }
  }
  return [...new Set([...workspacePaths.values()].filter(Boolean))];
}

/** Resolve one workspace id to its path, hitting the host when unknown. */
export async function resolveWorkspacePath(id: string): Promise<string> {
  const cached = workspacePaths.get(id);
  if (cached) return cached;

  try {
    const response = await fetch(
      `${pluginURL('fnos-advanced/workspace-path')}?id=${encodeURIComponent(id)}`,
      { credentials: 'same-origin', cache: 'no-store', headers: { Accept: 'application/json' } },
    );
    if (response.ok) {
      const data = await response.json() as { ok?: boolean; path?: unknown };
      if (data && data.ok && typeof data.path === 'string' && data.path) {
        workspacePaths.set(id, data.path);
        return data.path;
      }
    }
  } catch { /* older host half or transient network error */ }
  return '';
}

/** Metadata-only access check for the given ids (empty = all workspaces). */
export async function checkWorkspaceHealth(ids: string[]): Promise<WorkspaceHealthRow[]> {
  const response = await fetch(pluginURL('fnos-advanced/workspace-health'), {
    method: 'POST',
    credentials: 'same-origin',
    cache: 'no-store',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify({ ids: Array.isArray(ids) ? ids : [] }),
  });
  if (response.status === 404 || response.status === 405) {
    throw new Error(t('没有找到工作区权限接口'));
  }
  const data = await response.json().catch(() => ({})) as {
    ok?: boolean;
    error?: string;
    results?: WorkspaceHealthRow[];
  };
  if (!response.ok || !data.ok) {
    throw new Error(translateError(data.error) || t('检查失败：{error}', { error: `HTTP ${response.status}` }));
  }
  return Array.isArray(data.results) ? data.results : [];
}

/** Human sentence for one health row. */
export function workspaceHealthText(result: WorkspaceHealthRow | null | undefined): string {
  if (!result || !result.exists) return t('目录不存在');
  if (result.directory === false) return t('不是目录');
  if (!result.readable) return t('目录不可读（{error}）', { error: result.error || 'EACCES' });
  if (!result.writable) return t('目录可读但不可写');
  return t('目录可读写');
}

/** Fetch one picked image through the host's one-time token route. */
export async function readImageFile(path: string, mime: string): Promise<File> {
  const tokenResponse = await fetch(pluginURL('fnos-advanced/file-token'), {
    method: 'POST',
    credentials: 'same-origin',
    cache: 'no-store',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify({ path }),
  });
  if (tokenResponse.status === 404 || tokenResponse.status === 405) {
    throw new Error(t('宿主半侧未提供图片读取接口'));
  }
  const info = await tokenResponse.json().catch(() => ({})) as {
    ok?: boolean;
    token?: string;
    mime?: string;
    name?: string;
    error?: string;
  };
  if (!tokenResponse.ok || !info.ok || !info.token) {
    throw new Error(translateError(info.error) || t('读取图片失败（HTTP {status}）', { status: tokenResponse.status }));
  }

  const response = await fetch(
    pluginURL(`fnos-advanced/file-bytes?token=${encodeURIComponent(info.token)}`),
    { credentials: 'same-origin', cache: 'no-store' },
  );
  if (!response.ok) throw new Error(t('读取图片失败（HTTP {status}）', { status: response.status }));
  const blob = await response.blob();
  return new File([blob], info.name || fileNameForPath(path), {
    type: info.mime || mime || blob.type || 'application/octet-stream',
  });
}

/** Whether the fnOS gateway serves its authorization pages from this origin. */
export async function isGatewayAccess(appName: string): Promise<boolean> {
  if (/^\/app(?:\/|$)/.test(window.location.pathname)) return true;
  return authRouteExists(appName);
}

/**
 * Probe the fnOS authorization route. `redirect: 'manual'` distinguishes "the
 * gateway redirected to login" (reachable) from a plain 404/502 from some other
 * server on the same host.
 */
export async function authRouteExists(appName: string): Promise<boolean> {
  try {
    const response = await fetch(
      `${window.location.origin}/app-auth/pick-user-file?appName=${encodeURIComponent(appName || 'Harness')}&directory=true`,
      {
        method: 'GET',
        credentials: 'include',
        cache: 'no-store',
        redirect: 'manual',
      },
    );
    if (response.type === 'opaqueredirect') return true;
    return response.status !== 404 && response.status !== 502 && response.status !== 400;
  } catch {
    return false;
  }
}

/** LAN origin derived from the settings (`5666` or a full origin). */
export function lanBaseFromSettings(settings: PluginSettings): string {
  const target = String(settings.lanTarget || '').trim() || DEFAULT_SETTINGS.lanTarget;

  if (/^\d+$/.test(target)) {
    const port = Number(target);
    if (!Number.isInteger(port) || port < 1 || port > 65535) return '';
    return `http://${window.location.hostname}:${port}`;
  }

  try {
    const url = new URL(/^[a-z][a-z0-9+.-]*:\/\//i.test(target) ? target : `http://${target}`);
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return '';
    return normalizeBase(url.origin);
  } catch {
    return '';
  }
}

/** Decide which fnOS entry the web authorization should use. */
export async function resolveAuthTargets(config: PluginConfig, settings: PluginSettings): Promise<AuthTarget> {
  const appName = config.appName || 'Harness';

  if (await isGatewayAccess(appName)) {
    return { mode: 'gateway', base: window.location.origin, via: 'gateway' };
  }

  const proxy = normalizeBase(settings.proxyBase || '');
  const lan = lanBaseFromSettings(settings);
  const choices: AuthChoice[] = [];

  if (proxy) {
    choices.push({ id: 'proxy', label: t('通过反代地址'), detail: proxy, base: proxy });
  }
  if (lan && !choices.some((choice) => choice.base === lan)) {
    choices.push({ id: 'lan', label: t('通过内网地址'), detail: lan, base: lan });
  }

  if (settings.authMode === 'proxy' && proxy) {
    return { mode: 'single', base: proxy, via: 'proxy' };
  }
  if (settings.authMode === 'lan' && lan) {
    return { mode: 'single', base: lan, via: 'lan' };
  }
  if (choices.length > 1) return { mode: 'choose', choices };
  const [only] = choices;
  if (only) return { mode: 'single', base: only.base, via: only.id };

  return { mode: 'single', base: resolveAuthBase(), via: 'auto' };
}

/** A browser-side fnOS base override stored by the user, if any. */
export function readStoredAuthBase(): string {
  try { return localStorage.getItem(AUTH_BASE_KEY) || ''; } catch { return ''; }
}

/**
 * Best-effort fnOS origin when nothing is configured:
 * gateway path → same origin, HTTPS on 443 → same origin, otherwise the usual
 * `:5666` gateway on the current host.
 */
export function resolveAuthBase(): string {
  const stored = readStoredAuthBase();
  if (stored) return normalizeBase(stored);

  const { origin, pathname, protocol, hostname, port } = window.location;

  // Served through the fnOS gateway (e.g. /app/Harness/dsh): the auth pages
  // live on the same origin.
  if (/^\/app(?:\/|$)/.test(pathname)) return origin;

  // A gateway on the standard HTTPS port is normally reached at the root of
  // its own origin as well.
  if (protocol === 'https:' && (port === '' || port === '443')) return origin;

  // Served through the Harness reverse proxy (e.g. :3079): the fnOS gateway is
  // normally on the same host at 5666. An override is available through
  // DSH_FNOS_AUTH_BASE (host) or localStorage (browser).
  if (protocol === 'http:' || protocol === 'https:') {
    return `${protocol}//${hostname}:5666`;
  }
  return origin;
}

/** Validate the proxy field before saving; returns the normalized origin. */
export function validateProxyBase(value: unknown): string {
  const text = String(value || '').trim();
  if (!text) return '';
  const url = new URL(text);
  if (url.protocol !== 'http:' && url.protocol !== 'https:') {
    throw new Error(t('反代地址只支持 http/https'));
  }
  return url.origin;
}

/** Last successfully read settings, for the plugin-center summary row. */
export function getCachedSettings(): PluginSettings {
  return settingsCache ?? { ...DEFAULT_SETTINGS };
}
