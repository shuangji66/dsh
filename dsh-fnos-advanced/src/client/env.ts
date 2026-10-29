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
 * Whether this page is the console's embedded dsh (`<console base>/gui/`).
 *
 * The console hosts dsh in an iframe under its own `/gui/` reverse proxy. The
 * parent window is the console page, not the fnOS desktop/app host, so the
 * SDK's host bridge is unavailable even in the fnOS App: file details and
 * "open in fnOS" are invalid, and authorization must use the web path. The
 * direct dsh entry (`/app/<app>/dsh/`, the app icon) is not matched.
 */
export function isConsoleEmbeddedDsh(pathname?: string): boolean {
  const path = pathname ?? (typeof window !== 'undefined' ? window.location.pathname : '');
  return /\/gui(?:\/|$)/.test(path);
}

/** fnOS App WebView running the console's embedded dsh. */
export function isFnosAppEmbedded(): boolean {
  return isFnosApp() && isConsoleEmbeddedDsh();
}
