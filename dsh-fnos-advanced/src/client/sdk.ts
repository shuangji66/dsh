/**
 * The fnOS front-end SDK handle.
 *
 * One instance per page: the SDK learns during `ready()` whether it runs inside
 * fnOS desktop / the mobile app (host bridge) or as a standalone web app, and
 * that answer decides whether the native picker is even attempted.
 */
import { TrimApp } from '@trimjs/web-app';

let sdkInstance: TrimApp | null = null;

/** Lazily created singleton. */
export function getSdk(): TrimApp {
  if (!sdkInstance) sdkInstance = new TrimApp();
  return sdkInstance;
}

/** Whether the page runs inside a fnOS host (desktop / mobile app / iframe). */
export function isFnosHost(): boolean {
  const sdk = getSdk();
  if (sdk.isWeb && sdk.isStandaloneWeb) return false;
  return window.parent !== window || sdk.isWeb === false;
}

/**
 * Whether this window is likely to have a host bridge, judged before `ready()`
 * settles. Used to decide if a popup must be opened inside the click task.
 */
export function isHostLikeWindow(): boolean {
  const sdk = getSdk();
  return window.parent !== window || sdk.isWeb === false;
}
