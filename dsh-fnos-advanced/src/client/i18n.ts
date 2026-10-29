/**
 * Plugin UI translations.
 *
 * The Chinese source text doubles as the key: callers write `t('选择飞牛目录')`
 * and the English dictionary maps that exact string. `UiKey` is derived from the
 * dictionary, so a call site with a missing translation is a type error rather
 * than a Chinese string leaking into an English UI. When a runtime string (a
 * host error, for example) has no entry it falls back unchanged.
 *
 * Dynamic messages use `{name}` placeholders in both languages.
 */

const PREFERENCE_KEY = 'dsh-fnos-advanced:language';

/** English dictionary, keyed by the Chinese source string. */
export const EN = Object.freeze({
  // Common
  '取消': 'Cancel',
  '保存': 'Save',
  '保存中…': 'Saving…',
  '已保存': 'Saved',
  '读取设置…': 'Loading settings…',
  '未设置': 'Not set',
  '重试': 'Retry',

  // Picker
  '选择飞牛目录': 'Choose fnOS folder',
  '通过飞牛授权选择一个目录（会授予当前飞牛应用访问权限）': 'Choose a folder through fnOS (grants the current app access)',
  '选择工作区目录': 'Choose workspace folder',
  '确认授权': 'Authorize',
  '选择文件': 'Choose files',
  '添加引用': 'Add reference',
  '飞牛授权': 'fnOS authorization',
  '正在打开飞牛授权页面…': 'Opening the fnOS authorization page…',
  '准备授权…': 'Preparing…',
  '等待选择…': 'Waiting for selection…',
  '等待授权…': 'Waiting for authorization…',
  '选择授权入口…': 'Choosing authorization entry…',
  '已取消选择': 'Selection cancelled',
  '已取消授权': 'Authorization cancelled',
  '已填入授权目录：{path}': 'Folder authorized: {path}',
  '已填入路径': 'Path filled',
  '最近使用': 'Recent',
  '填入最近使用的目录：{path}': 'Fill the recently used folder: {path}',
  '清除最近记录': 'Clear recent folders',
  '该目录已添加为工作区': 'This folder is already a workspace',
  '打开选择器失败': 'Could not open the picker',
  '当前宿主未提供内嵌选择器，已改用网页授权。': 'The native picker is unavailable; using web authorization.',
  '浏览器拦截了授权窗口，请允许弹窗后重试。': 'The browser blocked the authorization window. Allow popups and retry.',
  '无法打开授权页面，请重试。': 'Could not open the authorization page. Try again.',
  '授权超时，请重试': 'Authorization timed out. Retry.',
  '授权窗口已关闭': 'Authorization window closed',
  '授权结果里没有路径': 'Authorization returned no path',
  '选择授权入口': 'Choose authorization entry',
  '通过反代地址': 'Via reverse proxy',
  '通过内网地址': 'Via LAN',
  '没有找到目录选择器的路径输入框': "Could not find the folder picker's path input",
  '无法确定工作区': 'Could not determine the workspace',
  '无法获取工作区目录': 'Could not read the workspace folder',
  '打开目录失败，当前宿主可能不支持该操作。': 'Could not open the folder; the host may not support it.',
  '打开目录': 'Open folder',

  // Composer
  '从飞牛选择': 'Choose from fnOS',
  '选择 NAS 上的文件，非图片作为路径引用': 'Pick files on the NAS; non-images become path references',
  'NAS 文件': 'NAS file',
  '没有找到对话的文件输入框，当前 dsh 版本可能不支持。': 'No composer file input found; this dsh version may be unsupported.',
  '当前会话暂时不能添加附件。': 'This session cannot accept attachments right now.',
  '已添加 {count} 个文件': 'Added {count} file(s)',
  '已添加 {count} 个文件（{images} 张图片会上传）': 'Added {count} file(s) ({images} image(s) will upload)',
  '{count} 张图片未能读取，已改为路径引用': '{count} image(s) could not be read; added as path references',
  '{count} 张图片读取失败（可能超过 8 MB），已改为路径引用': '{count} image(s) could not be read (possibly over 8 MB); added as path references',
  '{count} 张图片格式不支持视觉（如 HEIC），已改为路径引用': '{count} image(s) use a format vision does not support (such as HEIC); added as path references',
  '{count} 张图片未读取（设置已关闭图片读取），已改为路径引用': '{count} image(s) were not read because image reading is off; added as path references',
  '没有可添加的文件': 'No files to add',
  '宿主半侧未提供图片读取接口': 'The host does not provide the image read route',
  '读取图片失败（HTTP {status}）': 'Could not read the image (HTTP {status})',
  '已复制路径': 'Path copied',
  '复制失败，请手动复制': 'Copy failed; copy manually',

  // File actions
  '飞牛操作': 'fnOS actions',
  '在飞牛中打开': 'Open in fnOS',
  '文件详情': 'File details',
  '引用到对话': 'Quote in chat',
  '复制路径': 'Copy path',
  '在飞牛中打开失败，当前宿主可能不支持该操作。': 'Could not open in fnOS; the host may not support it.',
  '无法显示文件详情，当前宿主可能不支持该操作。': 'Could not show file details; the host may not support it.',
  '无法确定文件路径': 'Could not determine the file path',
  '没有找到对话输入框，无法引用。': 'No composer input to quote into.',

  // Workspace health
  '检查目录权限': 'Check folder access',
  '重新授权目录': 'Re-authorize folder',
  '正在检查目录权限…': 'Checking folder access…',
  '目录可读写': 'Folder is readable and writable',
  '目录不存在': 'Folder does not exist',
  '不是目录': 'Not a folder',
  '目录可读但不可写': 'Folder is readable but not writable',
  '目录不可读（{error}）': 'Folder is not readable ({error})',
  '检查失败：{error}': 'Check failed: {error}',
  '重新授权目录：{path}': 'Re-authorize folder: {path}',
  '已重新授权，请复查权限': 'Re-authorized; check access again',
  '重新授权失败：{error}': 'Re-authorization failed: {error}',
  '工作区目录权限': 'Workspace folder access',
  '检查 dsh 工作区目录的读写权限。飞牛 ACL 记录损坏或目录移动后，dsh 会读不到这些目录。': 'Check read/write access to dsh workspace folders. Broken fnOS ACL records or moved folders make dsh unable to read them.',
  '检查全部工作区': 'Check all workspaces',
  '检查中…': 'Checking…',
  '共 {count} 个': '{count} total',
  '可读写': 'Read/write',
  '不存在': 'Missing',
  '只读': 'Read-only',
  '不可读': 'No access',
  '检查失败': 'Check failed',
  '重新授权': 'Re-authorize',
  '已重新授权': 'Re-authorized',
  '没有找到工作区': 'No workspaces found',
  '没有找到工作区权限接口': 'The workspace access API is not available',
  '权限体检': 'Access check',
  '工作区体检': 'Workspace access check',

  // Diagnostics
  '诊断': 'Diagnostics',
  '检查 dsh 版本、DOM 锚点和飞牛地址连通性。': 'Check dsh versions, DOM anchors, and fnOS address reachability.',
  '版本': 'Version',
  '宿主': 'Host',
  '客户端': 'Client',
  '宿主接口': 'Host API',
  '正常': 'OK',
  '缺失（需重启 dsh）': 'Missing (restart dsh)',
  '读取失败': 'Error',
  '运行环境': 'Runtime',
  '飞牛桌面 / App': 'fnOS desktop / app',
  '飞牛网关': 'fnOS gateway',
  '普通浏览器': 'Browser',
  'DOM 锚点': 'DOM anchors',
  '对话输入框': 'Composer input',
  '文件树': 'File tree',
  '工作区菜单': 'Workspace menu',
  '目录选择器': 'Folder picker',
  '可用': 'Present',
  '不可用': 'Missing',
  '未检测': 'Not detected',
  '测试反代地址': 'Test proxy',
  '测试内网地址': 'Test LAN',
  '测试中…': 'Testing…',
  '可连接（{ms}ms）': 'Reachable ({ms}ms)',
  '无法连接': 'Unreachable',
  '连接超时': 'Timed out',
  '复制诊断信息': 'Copy diagnostics',
  '重新检测': 'Re-check',
  '诊断信息已复制': 'Diagnostics copied',
  '复制失败': 'Copy failed',
  '图片读取': 'Image read',
  '开': 'On',
  '关': 'Off',

  // Settings
  '飞牛目录授权': 'fnOS access',
  '配置非网关访问时使用的飞牛授权入口；设置保存在当前 dsh 数据目录。': 'Configure the fnOS authorization entry used outside the gateway. Settings are stored in the dsh data directory.',
  '反代飞牛地址': 'fnOS reverse-proxy address',
  '填协议 + 域名[:端口]，不要带路径；留空表示未配置。': 'Protocol + host[:port], no path; leave empty if unused.',
  '飞牛内网地址 / 端口': 'fnOS LAN address / port',
  '只填端口时使用当前访问主机的地址；默认 5666。': 'A bare port uses the current host; default 5666.',
  '非网关访问时的授权入口': 'Authorization entry outside the gateway',
  '每次询问（推荐）': 'Ask every time (recommended)',
  '优先使用反代地址': 'Prefer reverse proxy',
  '优先使用内网地址': 'Prefer LAN',
  '从飞牛选择图片时读取图片内容': 'Read image content when picking from fnOS',
  '图片需要字节才能被模型「看到」；关闭后图片也只生成路径引用。仅支持 PNG/JPEG/WebP/GIF，宿主只读取你选中的文件。': 'Images need bytes to be visible to the model. When off, images become path references. Only PNG/JPEG/WebP/GIF are read, and only for files you picked.',
  '连接与授权': 'Connection and authorization',
  '配置反代/内网授权入口、界面语言和图片读取。': 'Configure proxy/LAN authorization, interface language, and image reading.',
  '清除结果': 'Clear results',
  '展开': 'Expand',
  '收起': 'Collapse',
  '界面语言': 'Interface language',
  '跟随系统': 'Follow system',
  '中文': 'Chinese',
  'English': 'English',
  '宿主半侧版本 {host}，客户端 {client}。请重启 dsh 后再保存设置。': 'Host {host}, client {client}. Restart dsh before saving.',
  '宿主半侧未提供设置接口，通常是插件升级后没有重启 dsh。请重启 dsh 后再保存设置。': 'The host half does not expose the settings API (usually after an upgrade without restarting dsh). Restart dsh and try again.',
  '无法读取宿主设置接口，请确认 dsh 运行正常。': 'Could not read the host settings API. Check that dsh is running.',
  '保存失败（HTTP {status}）': 'Save failed (HTTP {status})',
  '当前运行在飞牛桌面/宿主内：会直接调用飞牛目录选择器，不打开网页授权。': 'Running inside fnOS desktop/app: the native picker is used; no web authorization popup.',
  '当前通过飞牛网关访问：直接使用当前同源地址授权，不会弹出入口选择。': 'Accessed through the fnOS gateway: the same origin is used for authorization; no entry chooser.',
  '当前不是通过飞牛网关访问：同时配置反代与内网地址后，点击「选择飞牛目录」会让你选择授权入口。': 'Not accessed through the fnOS gateway: with both proxy and LAN configured, choosing a folder asks which entry to use.',
  '反代 {proxy} · 内网 {lan}': 'Proxy {proxy} · LAN {lan}',

  // Host errors (surface through settings / pickers)
  '反代地址不是有效 URL': 'The proxy address is not a valid URL',
  '反代地址只支持 http/https': 'The proxy address must be http or https',
  '内网端口必须在 1-65535 之间': 'The LAN port must be between 1 and 65535',
  '内网地址格式无效': 'The LAN address is invalid',
  '内网地址只支持 http/https': 'The LAN address must be http or https',
  '图片路径无效': 'Invalid image path',
  '图片路径必须是绝对路径': 'The image path must be absolute',
  '不允许读取隐藏路径': 'Hidden paths cannot be read',
  '图片不存在': 'The image does not exist',
  '不允许读取 dsh 数据目录': 'The dsh data directory cannot be read',
  '只支持 PNG/JPEG/WebP/GIF 图片': 'Only PNG/JPEG/WebP/GIF images are supported',
  '不是普通文件': 'Not a regular file',
  '图片内容为空': 'The image is empty',
  '图片超过 8 MB': 'The image exceeds 8 MB',
  '插件设置已关闭图片读取': 'Image reading is disabled in the plugin settings',
  '设置格式无效': 'Invalid settings format',
});

/** Every translated source string; a call site outside this union is a bug. */
export type UiKey = keyof typeof EN;

/** Placeholder values substituted into `{name}` slots. */
export type UiParams = Record<string, string | number>;

export type LanguagePreference = 'system' | 'zh' | 'en';

const preferences: readonly LanguagePreference[] = ['system', 'zh', 'en'];

let preference: LanguagePreference = 'system';
let loaded = false;
/** Language reported by the fnOS host, once a resolver provided it. */
let hostLanguage: 'zh' | 'en' | null = null;
let hostLanguageResolver: (() => Promise<unknown>) | null = null;
let hostLanguageResolving: Promise<'zh' | 'en' | null> | null = null;
const languageListeners = new Set<() => void>();

function isPreference(value: unknown): value is LanguagePreference {
  return typeof value === 'string' && (preferences as readonly string[]).includes(value);
}

/**
 * Read a stored value, folding the retired `auto` / `host` split into one
 * `system` preference (fnOS language first, then the browser).
 */
function normalizePreference(value: unknown): LanguagePreference | null {
  if (isPreference(value)) return value;
  if (value === 'auto' || value === 'host') return 'system';
  return null;
}

/** Normalize an fnOS platform language (`zh-CN`, `en_US`, …). */
export function normalizeHostLanguage(value: unknown): 'zh' | 'en' | null {
  const text = typeof value === 'string' ? value : '';
  if (/^zh/i.test(text)) return 'zh';
  if (/^en/i.test(text)) return 'en';
  return null;
}

/** Install the platform-language reader (kept out of this module so tests and
 * the host-free build do not import the fnOS SDK). */
export function setHostLanguageResolver(resolver: (() => Promise<unknown>) | null): void {
  hostLanguageResolver = resolver;
}

function notifyLanguageChange(): void {
  for (const listener of [...languageListeners]) {
    try { listener(); } catch { /* a broken renderer must not stop the rest */ }
  }
}

/** Subscribe to effective-language changes; returns the unsubscribe function. */
export function onLanguageChange(listener: () => void): () => void {
  languageListeners.add(listener);
  return () => { languageListeners.delete(listener); };
}

/** Ask the host for its language (cached until `force`). */
export function resolveHostLanguage(force = false): Promise<'zh' | 'en' | null> {
  if (!force && hostLanguage) return Promise.resolve(hostLanguage);
  if (!force && hostLanguageResolving) return hostLanguageResolving;
  if (!hostLanguageResolver) return Promise.resolve(hostLanguage);

  const before = currentLanguage();
  hostLanguageResolving = (async () => {
    try {
      const config = await hostLanguageResolver?.() as { language?: unknown } | undefined;
      const next = normalizeHostLanguage(config?.language);
      if (next) hostLanguage = next;
    } catch { /* no host bridge: keep the browser fallback */ }
    hostLanguageResolving = null;
    if (currentLanguage() !== before) notifyLanguageChange();
    return hostLanguage;
  })();
  return hostLanguageResolving;
}

/** Read the stored language preference (cached after the first read). */
export function getLanguagePreference(): LanguagePreference {
  if (loaded) return preference;
  loaded = true;
  try {
    const stored = normalizePreference(window.localStorage.getItem(PREFERENCE_KEY));
    if (stored) preference = stored;
  } catch { /* private mode */ }
  return preference;
}

/** Store and apply a language preference (`system` / `zh` / `en`). */
export function setLanguagePreference(value: unknown): LanguagePreference {
  const before = currentLanguage();
  preference = normalizePreference(value) || 'system';
  loaded = true;
  try { window.localStorage.setItem(PREFERENCE_KEY, preference); } catch { /* ignore */ }
  if (preference === 'system') void resolveHostLanguage();
  if (currentLanguage() !== before) notifyLanguageChange();
  return preference;
}

/** The language currently used for rendering: explicit pick, then fnOS, then browser. */
export function currentLanguage(): 'zh' | 'en' {
  getLanguagePreference();
  if (preference === 'zh' || preference === 'en') return preference;
  if (hostLanguage) return hostLanguage;
  const html = typeof document !== 'undefined' ? document.documentElement.lang : '';
  const navigatorLanguage = typeof navigator !== 'undefined' ? navigator.language : '';
  return /^zh/i.test(html || navigatorLanguage) ? 'zh' : 'en';
}

function interpolate(text: string, params?: UiParams): string {
  if (!params) return text;
  return text.replace(/\{(\w+)\}/g, (match, key: string) => (
    Object.prototype.hasOwnProperty.call(params, key) ? String(params[key]) : match
  ));
}

/** English text for a known source string; unknown strings pass through. */
function lookup(source: string): string {
  if (currentLanguage() !== 'en') return source;
  const dictionary: Record<string, string> = EN;
  return dictionary[source] || source;
}

/**
 * Translate a Chinese source string. Dynamic messages put `{name}` placeholders
 * in both the source and the translation.
 */
export function t(source: UiKey, params?: UiParams): string {
  return interpolate(lookup(source), params);
}

/** Translate a host-side message; unknown text falls back unchanged. */
export function translateError(error: unknown): string {
  const text = error == null ? '' : String(error);
  return text ? lookup(text) : text;
}
