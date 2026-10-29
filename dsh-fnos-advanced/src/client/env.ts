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
 * there. Other host-bridge features (file details, open in fnOS, re-authorize)
 * still work and must keep using `isFnosHost()`.
 */
export function isFnosApp(userAgent?: string): boolean {
  const ua = userAgent ?? (typeof navigator !== 'undefined' ? navigator.userAgent || '' : '');
  return /FNAppType\//.test(ua);
}
