/**
 * One pick attempt, shared by every surface.
 *
 * Order of preference:
 *   1. the native fnOS picker (inside fnOS desktop / the mobile app);
 *   2. the fnOS web authorization page, opened from the configured proxy/LAN
 *      entry (or this origin when served by the gateway).
 *
 * The flow object carries the per-surface differences, so the authorization
 * machinery exists only once.
 */
import {
  buildAuthUrl,
  openAuthFrame,
  requestAuth,
  windowSurface,
  type AuthFrame,
  type AuthSurface,
} from './auth.ts';
import { isCancellationText, isPickerCancelled } from './cancel.ts';
import { LOG_PREFIX, POPUP_FEATURES } from './constants.ts';
import { closePopup, randomId, writePopupLoading } from './dom.ts';
import { isFnosApp } from './env.ts';
import type { PickFlow } from './flow.ts';
import { t, translateError } from './i18n.ts';
import { pickerParams } from './picker.ts';
import {
  loadConfig,
  loadSettings,
  resolveAuthTargets,
  type AuthChoice,
} from './plugin-api.ts';
import { getSdk, isHostLikeWindow } from './sdk.ts';

interface DirectPickResult {
  handled: boolean;
  paths?: string[];
  cancelled?: boolean;
  error?: string;
}

/** Drive one picker attempt from a click. */
export async function runPickFlow(button: HTMLButtonElement, flow: PickFlow): Promise<void> {
  if (button.dataset.busy === '1') return;

  markBusy(button, flow);

  // In the fnOS mobile app the native pickUserFile bridge is broken for
  // personal folders (it applies shared-folder authorization), so the
  // directory flow goes straight to the web authorization page. That page is
  // embedded in an iframe later (see startAuthFlow), which needs no popup.
  const appDirectory = isFnosApp() && flow.directory === true;

  // Top-level browsers need the popup opened inside the click task. Host
  // environments (fnOS desktop / mobile app) get the native picker first and do
  // not need a popup at all.
  let popup: Window | null = null;
  if (!appDirectory && !isHostLikeWindow()) {
    popup = window.open('', `dsh-fnos-advanced-auth-${randomId()}`, POPUP_FEATURES);
    if (popup) writePopupLoading(popup);
  }

  try {
    const direct = await pickPathsDirect(flow);
    if (direct.handled) {
      closePopup(popup);
      if (direct.paths && direct.paths.length) {
        await finishPick(flow, direct.paths);
      } else if (direct.cancelled) {
        flow.status(t('已取消选择'), false);
        flow.restore();
      } else {
        flow.status(translateError(direct.error) || t('打开选择器失败'), true);
        flow.restore();
      }
      return;
    }

    const config = await loadConfig();
    const appName = config.appName || 'Harness';
    const settings = await loadSettings();
    const target = await resolveAuthTargets(config, settings);

    if (target.mode === 'choose') {
      closePopup(popup);
      showAuthChooser(flow, appName, target.choices);
      return;
    }

    if (!appDirectory) {
      if (!popup || popup.closed) {
        popup = window.open('', `dsh-fnos-advanced-auth-${randomId()}`, POPUP_FEATURES);
      }
      if (!popup) throw new Error(t('浏览器拦截了授权窗口，请允许弹窗后重试。'));
    }

    await startAuthFlow(flow, appName, target.base, popup);
  } catch (error) {
    closePopup(popup);
    handlePickError(flow, error);
  }
}

function markBusy(button: HTMLButtonElement, flow: PickFlow): void {
  button.dataset.busy = '1';
  button.disabled = true;
  flow.setLabel(t('准备授权…'));
}

/** Apply the picked paths and report the result on the flow's surface. */
export async function finishPick(flow: PickFlow, paths: string[]): Promise<void> {
  if (flow.isDuplicate && await flow.isDuplicate(paths)) {
    flow.status(t('该目录已添加为工作区'), false);
    flow.restore();
    return;
  }

  await flow.onPaths(paths);
  flow.status(flow.doneText(paths), false);
  const label = flow.doneLabel();
  if (label) flow.button.textContent = label;
  window.setTimeout(() => flow.restore(), 1600);
}

/**
 * In fnOS desktop the official SDK can open the native picker directly through
 * the host bridge. The fnOS mobile app is different: its pickUserFile bridge
 * applies shared-folder authorization and fails for personal folders, so the
 * directory flow skips the bridge and uses the web authorization page. Other
 * bridge features (file details / open / re-authorize) are unaffected.
 */
export async function pickPathsDirect(flow: PickFlow): Promise<DirectPickResult> {
  if (isFnosApp() && flow.directory === true) return { handled: false };

  const sdk = getSdk();
  try {
    await sdk.ready();
  } catch {
    return { handled: false };
  }

  // A normal browser tab has no host bridge; don't even try.
  if (sdk.isWeb && sdk.isStandaloneWeb) return { handled: false };
  if (sdk.isWeb && window.parent === window) return { handled: false };

  const picker = sdk[flow.method];
  if (typeof picker !== 'function') return { handled: false };

  flow.setLabel(t('等待选择…'));

  let result: Awaited<ReturnType<typeof picker>> | undefined;
  try {
    result = await picker.call(sdk, pickerParams(flow));
  } catch (error) {
    console.warn(LOG_PREFIX, 'in-host picker unavailable; falling back to URL auth', error);
    flow.status(t('当前宿主未提供内嵌选择器，已改用网页授权。'), false);
    return { handled: false };
  }

  const paths = result && result.code === 0 && Array.isArray(result.data)
    ? result.data.filter((item: unknown) => typeof item === 'string' && item)
    : [];
  if (paths.length) return { handled: true, paths };

  return {
    handled: true,
    cancelled: isPickerCancelled(result),
    error: result && result.msg ? result.msg : t('打开选择器失败'),
  };
}

/** Open the authorization page and wait for its result. */
export async function startAuthFlow(
  flow: PickFlow,
  appName: string,
  base: string,
  existingPopup: Window | null,
): Promise<void> {
  flow.setLabel(t('等待授权…'));
  const state = randomId();

  // The fnOS App's WebView cannot reliably open popups, and the workspace
  // dialog lives in this page. Embed the authorization page in a full-screen
  // iframe instead; the callback posts to window.parent and we stay here.
  const useFrame = isFnosApp() && flow.directory === true;
  let frame: AuthFrame | null = null;
  let popup = existingPopup;
  let surface: AuthSurface;

  if (useFrame) {
    frame = openAuthFrame();
    if (!frame) throw new Error(t('无法打开授权页面，请重试。'));
    surface = frame.surface;
  } else {
    if (!popup || popup.closed) {
      popup = window.open('', `dsh-fnos-advanced-auth-${state}`, POPUP_FEATURES);
    }
    if (!popup) throw new Error(t('浏览器拦截了授权窗口，请允许弹窗后重试。'));
    writePopupLoading(popup);
    surface = windowSurface(popup);
  }

  try {
    const auth = await requestAuth({
      surface,
      state,
      buildUrl: () => buildAuthUrl(base, state, flow.method, { ...pickerParams(flow) }, appName),
    });
    if (!auth || auth.status !== 'success') {
      if (auth && (auth.status === 'cancelled' || isCancellationText(auth.error))) {
        // Text matching is a heuristic (older hosts report cancellation as
        // "Operation failed" instead of a status). Leave a breadcrumb so a
        // misclassified genuine failure can still be diagnosed.
        if (auth.status !== 'cancelled') {
          console.info(LOG_PREFIX, 'authorization result treated as cancellation', auth);
        }
        flow.status(t('已取消选择'), false);
        flow.restore();
        return;
      }
      throw new Error(auth && auth.error ? translateError(auth.error) : t('打开选择器失败'));
    }

    const paths = Array.isArray(auth.path)
      ? auth.path.filter((item: unknown) => typeof item === 'string' && item)
      : [];
    if (!paths.length) throw new Error(t('授权结果里没有路径'));

    await finishPick(flow, paths);
  } catch (error) {
    closePopup(popup);
    throw error;
  } finally {
    frame?.close();
  }
}

/** Report a failed pick attempt without losing the surface's state. */
export function handlePickError(flow: PickFlow, error: unknown): void {
  const message = error instanceof Error ? error.message : String(error);
  console.warn(LOG_PREFIX, message, error);
  flow.status(translateError(message), true);
  flow.restore();
}

/**
 * Ask which configured entry to authorize through. Only reachable outside the
 * gateway when both a proxy and a LAN address are configured and the setting is
 * "ask every time".
 */
export function showAuthChooser(flow: PickFlow, appName: string, choices: AuthChoice[]): void {
  const anchor = flow.anchor;
  if (!anchor) {
    flow.restore();
    return;
  }
  removeChooser(anchor);
  const host = anchor as HTMLElement;
  if (getComputedStyle(host).position === 'static') host.style.position = 'relative';
  flow.setLabel(t('选择授权入口…'));

  const panel = document.createElement('div');
  panel.className = 'dsh-fnos-advanced-chooser';
  panel.setAttribute('role', 'menu');
  panel.setAttribute('aria-label', t('选择授权入口'));

  const title = document.createElement('div');
  title.className = 'dsh-fnos-advanced-chooser-title';
  title.textContent = t('选择授权入口');
  panel.appendChild(title);

  for (const choice of choices) {
    const option = document.createElement('button');
    option.type = 'button';
    option.className = 'dsh-fnos-advanced-chooser-option';
    option.setAttribute('role', 'menuitem');

    const label = document.createElement('span');
    label.className = 'dsh-fnos-advanced-chooser-label';
    label.textContent = choice.label;

    const detail = document.createElement('span');
    detail.className = 'dsh-fnos-advanced-chooser-detail';
    detail.textContent = choice.detail;

    option.append(label, detail);
    option.addEventListener('click', (event) => {
      event.preventDefault();
      event.stopPropagation();
      removeChooser(anchor);
      startAuthFlow(flow, appName, choice.base, null).catch((error: unknown) => {
        handlePickError(flow, error);
      });
    });
    panel.appendChild(option);
  }

  const cancel = document.createElement('button');
  cancel.type = 'button';
  cancel.className = 'dsh-fnos-advanced-chooser-cancel';
  cancel.textContent = t('取消');
  cancel.addEventListener('click', (event) => {
    event.preventDefault();
    event.stopPropagation();
    removeChooser(anchor);
    flow.restore();
  });
  panel.appendChild(cancel);

  anchor.appendChild(panel);
}

function removeChooser(anchor: Element): void {
  anchor.querySelectorAll('.dsh-fnos-advanced-chooser').forEach((node) => node.remove());
}
