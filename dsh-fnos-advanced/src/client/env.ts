/**
 * Environment probes that need neither the fnOS SDK nor a DOM.
 *
 * Kept separate from sdk.ts so pure unit tests can import them without pulling
 * in `@trimjs/web-app` (whose browser bundle touches `window` at import time).
 */

/**
 * Whether the current WebView is the fnOS mobile app.
 *
 * The app UA carries `FNAppType/iOS` (or `.../Android`); desktop browsers and
 * the fnOS desktop iframe do not. The app's native `pickUserFile` bridge is
 * broken for personal folders (it applies shared-folder authorization and
 * fails with E_APP_AUTH), so the directory picker must use the web auth page
 * there.
 *
 * This only answers "is this the app WebView". Whether the **host bridge** is
 * usable also depends on the entry: the icon entry is a top-level dsh page and
 * can use native file details/open/re-authorize; the console entry embeds dsh
 * in an iframe, where the bridge handshake never reaches the fnOS host (see
 * {@link isConsoleEmbeddedDsh}).
 */
export function isFnosApp(userAgent?: string): boolean {
  const ua = userAgent ?? (typeof navigator !== 'undefined' ? navigator.userAgent || '' : '');
  return /FNAppType\//.test(ua);
}

/**
 * 从 dsh 页面自身的路径反推控制台 base：`/app/Harness/dsh/...` → `/app/Harness`。
 * 认不出 `<base>/dsh` 形态时返回空串（此时无法判断，调用方按“非内嵌”处理）。
 */
function consoleBaseOf(pathname: string): string {
  const match = /^(.*?)\/dsh(?:\/|$)/.exec(pathname);
  return match ? match[1] : '';
}

/**
 * 控制台内嵌 dsh 的纯判定：dsh 页面挂在 `<consoleBase>/dsh/...` 下，且它的父页面
 * 就是控制台 SPA（父路径落在同一个 consoleBase 之下）。
 *
 * 控制台与 App 图标入口现在共用同一个 `/app/<app>/dsh/` 路径（同一个网关入口），
 * 路径本身不再能区分两者；真正不同的是**挂载层级**：控制台把 dsh 嵌在控制台页面的
 * iframe 里（父页面是控制台），图标入口是 fnOS 外壳直接加载的页面（父页面是外壳，
 * 或者干脆是顶层窗口）。`parentPath` 为 null 表示父页面跨源、读不到 —— 按“内嵌”
 * 处理是安全方向：误判只会少用宿主桥接、多一次网页授权；反过来会在没有桥接的地方
 * 调用原生选择器。
 */
export function isConsoleEmbeddedPath(pathname: string, parentPath: string | null): boolean {
  const base = consoleBaseOf(pathname);
  if (!base) return false;
  if (parentPath === null) return true;
  return parentPath === base || parentPath.startsWith(base + '/');
}

/**
 * Whether this page is the console's embedded dsh.
 *
 * 与 {@link isConsoleEmbeddedPath} 同一判据，数据从当前窗口取：顶层窗口（图标入口
 * 直接加载，或浏览器新标签页）不是内嵌；嵌套时看父页面路径是不是控制台 SPA。
 * 控制台页面与 dsh 同源，`window.parent.location.pathname` 可读。
 */
export function isConsoleEmbeddedDsh(): boolean {
  if (typeof window === 'undefined') return false;
  if (window.parent === window) return false;
  let parentPath: string | null;
  try {
    parentPath = window.parent.location.pathname;
  } catch {
    parentPath = null;
  }
  return isConsoleEmbeddedPath(window.location.pathname, parentPath);
}

/** fnOS App WebView running the console's embedded dsh. */
export function isFnosAppEmbedded(): boolean {
  return isFnosApp() && isConsoleEmbeddedDsh();
}
