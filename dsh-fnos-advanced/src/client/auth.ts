/**
 * Web authorization: build the fnOS authorization URL and wait for its result.
 *
 * The result arrives through whichever of the three channels survives:
 * `postMessage` from the callback page, the `storage` event, or polling the
 * host's `auth-result` route. The popup closing is the cancellation signal.
 */
import type { AppAuthResult } from '@trimjs/web-app';
import { AUTH_TIMEOUT_MS, MSG_TYPE, POPUP_FEATURES, STORAGE_KEY } from './constants.ts';
import { randomId, writePopupLoading } from './dom.ts';
import { t } from './i18n.ts';
import { pluginURL } from './plugin-api.ts';
import { getSdk } from './sdk.ts';

export type AuthResult = AppAuthResult;

/**
 * Closing the popup is a cancellation the SDK never reports: it emits no status
 * at all, so the plugin synthesizes one (the status string is deliberately the
 * long form, distinct from the SDK's `cancel`).
 */
export interface AuthCancelled {
  status: 'cancelled';
  error: string;
}

/** What a finished authorization attempt resolves to. */
export type AuthOutcome = AuthResult | AuthCancelled;

export interface AuthPayload {
  type?: string;
  state?: string;
  result?: AuthResult;
  search?: string;
}

interface StoredResult extends AuthPayload {
  ts?: number;
}

/**
 * Build the authorization URL, pointing the SDK at `base` for this one call.
 *
 * `getAppAuthBaseUrl` is the SDK's own seam for a page that is not served by
 * the gateway; it is restored immediately so a later call is unaffected.
 */
export async function buildAuthUrl(
  base: string,
  state: string,
  method: 'pickUserFile' | 'authorizeUserFile',
  params: Record<string, unknown>,
  appName: string,
): Promise<string> {
  const sdk = getSdk();
  const previousBase = sdk.getAppAuthBaseUrl;

  sdk.getAppAuthBaseUrl = async () => base;
  try {
    const payload: Record<string, unknown> = {
      appName,
      ...params,
      redirectUri: pluginURL('fnos-advanced/auth-callback.html'),
      state,
    };
    // The SDK serializes the picker fields it knows; `title` / `okText` /
    // `creatable` travel as extra query parameters on purpose.
    const build = sdk.buildAppAuthUrl as unknown as (
      methodName: string,
      value: Record<string, unknown>,
    ) => Promise<string>;
    const url = await build.call(sdk, method, payload);

    // The SDK only serializes directory/accept/sidebarGroup/title/okText.
    // Pass the newer folder-creation hint through when the host supports it.
    const target = new URL(url);
    if (payload.creatable) target.searchParams.set('creatable', 'true');
    return target.toString();
  } finally {
    sdk.getAppAuthBaseUrl = previousBase;
  }
}

/** Open the (blank) authorization popup inside a click task. */
export function openAuthPopup(): Window | null {
  const popup = window.open('', `dsh-fnos-advanced-auth-${randomId()}`, POPUP_FEATURES);
  if (popup) writePopupLoading(popup);
  return popup;
}

/** Read the callback page's storage copy. */
export function readStoredResult(): StoredResult | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    return JSON.parse(raw) as StoredResult;
  } catch {
    return null;
  }
}

/** Accept a stored result when it belongs to the flow we are waiting on. */
export function acceptStoredResult(
  expectedState: string,
  accept: (payload: AuthPayload) => void,
): void {
  const stored = readStoredResult();
  if (!stored) return;
  if (expectedState && stored.state && stored.state !== expectedState) return;
  accept(stored);
}

/** Prefer the SDK's parser on the raw query; fall back to the pre-parsed copy. */
export function parseAuthPayload(payload: AuthPayload | null | undefined): AuthResult | null {
  if (!payload) return null;
  if (typeof payload.search === 'string' && payload.search) {
    try {
      const parsed = getSdk().parseAppAuthCallback(payload.search);
      if (parsed && (parsed.status || parsed.error || (parsed.path && parsed.path.length))) {
        return parsed;
      }
    } catch { /* fall through to the pre-parsed result */ }
  }
  return payload.result || null;
}

export interface AuthRequest {
  popup: Window;
  state: string;
  /** Build the authorization URL; called once the listeners are installed. */
  buildUrl: () => Promise<string>;
}

/**
 * Wait for the callback of one authorization attempt.
 *
 * Resolves with `{ status: 'cancelled' }` when the popup is closed, rejects on
 * timeout or when the URL cannot be built.
 */
export function requestAuth({ popup, state, buildUrl }: AuthRequest): Promise<AuthOutcome> {
  return new Promise<AuthOutcome>((resolve, reject) => {
    let settled = false;
    let closeTimer = 0;
    let pollTimer = 0;
    let resultPollTimer = 0;
    let resultPolling = false;
    let timeoutTimer = 0;

    const lookupStored = () => acceptStoredResult(state, accept);

    const cleanup = () => {
      window.removeEventListener('message', onMessage);
      window.removeEventListener('storage', onStorage);
      window.removeEventListener('focus', lookupStored);
      document.removeEventListener('visibilitychange', onVisibility);
      window.clearInterval(pollTimer);
      window.clearInterval(resultPollTimer);
      window.clearTimeout(closeTimer);
      window.clearTimeout(timeoutTimer);
      try { localStorage.removeItem(STORAGE_KEY); } catch { /* ignore */ }
    };

    const finish = (result: AuthOutcome) => {
      if (settled) return;
      settled = true;
      cleanup();
      resolve(result);
    };

    const fail = (error: Error) => {
      if (settled) return;
      settled = true;
      cleanup();
      reject(error);
    };

    function accept(payload: AuthPayload | null) {
      if (!payload || settled) return;
      const payloadState = payload.state || (payload.result && payload.result.state) || '';
      if (state && payloadState && payloadState !== state) return;
      const result = parseAuthPayload(payload);
      if (result) finish(result);
    }

    function onMessage(event: MessageEvent) {
      if (event.origin !== window.location.origin) return;
      const data = event.data as AuthPayload | null;
      if (!data || data.type !== MSG_TYPE) return;
      accept(data);
    }

    function onStorage(event: StorageEvent) {
      if (event.key !== STORAGE_KEY || !event.newValue) return;
      try { accept(JSON.parse(event.newValue) as AuthPayload); } catch { /* ignore */ }
    }

    function onVisibility() {
      if (document.visibilityState === 'visible') lookupStored();
    }

    async function readServerResult() {
      try {
        const response = await fetch(
          `${pluginURL('fnos-advanced/auth-result')}?state=${encodeURIComponent(state)}`,
          { credentials: 'same-origin', cache: 'no-store', headers: { Accept: 'application/json' } },
        );
        if (!response.ok) return;
        const data = await response.json() as { ok?: boolean; state?: string; search?: string };
        if (data && data.ok && typeof data.search === 'string') {
          accept({ state: data.state || state, search: data.search });
        }
      } catch { /* host route may not be available yet */ }
    }

    async function pollServerResult() {
      if (settled || resultPolling) return;
      resultPolling = true;
      try {
        await readServerResult();
      } finally {
        resultPolling = false;
      }
    }

    window.addEventListener('message', onMessage);
    window.addEventListener('storage', onStorage);
    window.addEventListener('focus', lookupStored);
    document.addEventListener('visibilitychange', onVisibility);

    // The storage event is the reliable path when a cross-origin redirect
    // severs window.opener; this also covers the same-tab focus case.
    acceptStoredResult(state, accept);
    resultPollTimer = window.setInterval(pollServerResult, 900);

    pollTimer = window.setInterval(() => {
      if (!popup.closed) return;
      window.clearInterval(pollTimer);
      // Give the callback page a moment to publish its result before treating
      // the closed popup as a cancellation.
      closeTimer = window.setTimeout(async () => {
        const stored = readStoredResult();
        if (stored) {
          accept(stored);
          return;
        }
        await readServerResult();
        if (!settled) finish({ status: 'cancelled', error: t('授权窗口已关闭') });
      }, 450);
    }, 500);

    timeoutTimer = window.setTimeout(() => {
      fail(new Error(t('授权超时，请重试')));
    }, AUTH_TIMEOUT_MS);

    void (async () => {
      try {
        const url = await buildUrl();
        if (settled) return;
        popup.location.replace(url);
      } catch (error) {
        fail(error instanceof Error ? error : new Error(String(error)));
      }
    })();
  });
}
