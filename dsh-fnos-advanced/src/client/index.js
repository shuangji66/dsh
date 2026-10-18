/**
 * dsh-fnos-advanced — browser half.
 *
 * The workspace directory picker is a React dialog owned by dsh. Rather than
 * importing version-specific slots or UI packages, this plugin watches the DOM
 * for the picker dialog and adds one button to its footer:
 *
 *   [选择飞牛目录] [New folder] ...
 *
 * Clicking the button asks fnOS for a user directory authorization through the
 * official `@trimjs/web-app` SDK. The authorization page is opened in a popup;
 * its callback page (served by the host half) returns the authorized path via
 * postMessage, localStorage and a same-origin host result route. The path is
 * then written into the picker's path
 * input and Enter is dispatched, which navigates the picker to that directory.
 *
 * No `@deepseek-ai/*` package is imported. The picker itself only relies on
 * `window.__ModuleLoader__`, accessible dialog roles/labels and button texts,
 * and `ctx.effect`. The optional plugin-center settings page uses the
 * `plugins.bundle.config` slot and the shared `react` platform module.
 */

import * as React from 'react';
import { TrimApp } from '@trimjs/web-app';
import { isCancellationText, isPickerCancelled } from './cancel.js';

const LOG_PREFIX = '[dsh-fnos-advanced]';
const VERSION = '0.2.5';

const MSG_TYPE = 'dsh-fnos-advanced:auth';
const STORAGE_KEY = 'dsh-fnos-advanced:auth-result';
const AUTH_BASE_KEY = 'dsh-fnos-advanced:auth-base';
const SETTINGS_SLOT_KEY = 'dsh-fnos-advanced';
const DEFAULT_SETTINGS = Object.freeze({ proxyBase: '', lanTarget: '5666', authMode: 'ask' });

const BUTTON_ATTR = 'data-dsh-fnos-advanced';
const STATUS_CLASS = 'dsh-fnos-advanced-status';
const STYLE_ID = 'dsh-fnos-advanced-style';

const WORKSPACE_TITLE_RE = /workspace|工作区/i;
const PATH_LABEL_RE = /(?:^|\b)(?:edit\s*path|path)(?:\b|$)|路径|编辑路径|修改路径/i;
const OPEN_BUTTON_RE = /^(?:open|打开|确定|选择|select|choose)/i;
const CANCEL_BUTTON_RE = /^(?:cancel|取消)/i;

const POPUP_FEATURES = 'popup=yes,width=760,height=680,resizable=yes,scrollbars=yes';
const AUTH_TIMEOUT_MS = 5 * 60 * 1000;
const FOLDER_OPEN_SVG = '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" stroke-width="1"><path d="M1.5 3.5C1.5 2.95 1.95 2.5 2.5 2.5H5.4C5.65 2.5 5.88 2.59 6.07 2.75L7.7 4.2C7.88 4.36 8.12 4.45 8.36 4.45H13.5C14.05 4.45 14.5 4.9 14.5 5.45V12.5C14.5 13.05 14.05 13.5 13.5 13.5H2.5C1.95 13.5 1.5 13.05 1.5 12.5V3.5Z" stroke="currentColor"/></svg>';

let installedDispose = null;
let sdkInstance = null;
let configPromise = null;
let settingsCache = null;
let settingsCachedAt = 0;
let settingsPromise = null;
let settingsHostState = 'unknown';
let workspacePaths = new Map();
let lastWorkspaceMenu = null;

/**
 * Cordis client plugin entry.
 *
 * @param {any} ctx
 */
export function apply(ctx) {
  if (typeof window === 'undefined' || typeof document === 'undefined') return;
  const dispose = install();

  if (ctx && typeof ctx.effect === 'function') {
    ctx.effect(() => dispose, 'dsh-fnos-advanced: workspace directory picker');
  }

  registerSettingsSlot(ctx);
  registerWorkspacePaths(ctx);
}

/** @returns {() => void} */
function install() {
  if (installedDispose) return installedDispose;

  injectStyle();
  const cleanups = [];

  let scanTimer = 0;
  const scheduleScan = () => {
    if (scanTimer) return;
    scanTimer = window.setTimeout(() => {
      scanTimer = 0;
      scan();
    }, 80);
  };

  const observer = new MutationObserver(scheduleScan);
  observer.observe(document.documentElement, { childList: true, subtree: true });
  cleanups.push(() => observer.disconnect());

  const onClick = (event) => {
    const target = event.target instanceof Element
      ? event.target.closest(`[${BUTTON_ATTR}]`)
      : null;
    if (!target) return;
    event.preventDefault();
    event.stopPropagation();
    void handlePick(target);
  };
  document.addEventListener('click', onClick, true);
  cleanups.push(() => document.removeEventListener('click', onClick, true));

  const onWorkspaceRowClick = (event) => {
    const row = event.target instanceof Element
      ? event.target.closest('[data-row-key^="workspace:"]')
      : null;
    if (!row) return;
    const key = row.getAttribute('data-row-key') || '';
    const id = key.slice('workspace:'.length);
    if (id) {
      lastWorkspaceMenu = { id, at: Date.now() };
      scheduleScan();
    }
  };
  document.addEventListener('click', onWorkspaceRowClick, true);
  cleanups.push(() => document.removeEventListener('click', onWorkspaceRowClick, true));

  // The SDK learns whether this page is a standalone web app or a host
  // (fnOS desktop / mobile app) during init. Re-scan once it settles.
  getSdk().ready().then(scheduleScan).catch(() => {});

  const dispose = () => {
    if (scanTimer) {
      window.clearTimeout(scanTimer);
      scanTimer = 0;
    }
    for (const cleanup of cleanups.splice(0)) {
      try { cleanup(); } catch { /* ignore */ }
    }
    for (const node of document.querySelectorAll(`[${BUTTON_ATTR}], .${STATUS_CLASS}, [data-dsh-fnos-open-dir]`)) {
      node.remove();
    }
    const style = document.getElementById(STYLE_ID);
    if (style) style.remove();
    installedDispose = null;
  };

  installedDispose = dispose;
  scan();
  return dispose;
}

function scan() {
  for (const dialog of document.querySelectorAll('[role="dialog"]')) {
    if (!isWorkspacePicker(dialog)) {
      dialog.querySelectorAll(`[${BUTTON_ATTR}], .${STATUS_CLASS}`).forEach((node) => node.remove());
      continue;
    }
    if (dialog.querySelector(`[${BUTTON_ATTR}]`)) continue;
    injectButton(dialog);
  }
  scanWorkspaceMenus();
}

function registerWorkspacePaths(ctx) {
  if (!ctx || typeof ctx.inject !== 'function') return;

  ctx.inject(['workspaces'], (scope) => {
    const workspaces = scope.workspaces;
    const list = workspaces && workspaces.list;
    if (!list || typeof list.getSnapshot !== 'function') return;

    const update = () => {
      try {
        const snapshot = list.getSnapshot();
        const items = Array.isArray(snapshot?.items) ? snapshot.items : [];
        workspacePaths = new Map(
          items
            .filter((item) => item && item.workspaceId && typeof item.path === 'string')
            .map((item) => [String(item.workspaceId), item.path]),
        );
      } catch { /* ignore a transient snapshot */ }
    };

    update();
    if (typeof list.subscribe === 'function' && typeof scope.effect === 'function') {
      try {
        scope.effect(() => list.subscribe(update), 'dsh-fnos-advanced: workspace paths');
      } catch { /* the host route remains as a fallback */ }
    }
  });
}

function isFnosHost() {
  const sdk = getSdk();
  if (sdk.isWeb && sdk.isStandaloneWeb) return false;
  return window.parent !== window || sdk.isWeb === false;
}

function scanWorkspaceMenus() {
  if (!isFnosHost()) return;
  if (!lastWorkspaceMenu || Date.now() - lastWorkspaceMenu.at > 5 * 60 * 1000) return;

  for (const menu of document.querySelectorAll('[role="menu"]')) {
    if (!isVisible(menu)) continue;
    if (menu.querySelector('[data-dsh-fnos-open-dir]')) continue;
    if (!isWorkspaceActionsMenu(menu)) continue;
    injectOpenDirItem(menu);
  }
}

function isWorkspaceActionsMenu(menu) {
  const text = (menu.textContent || '').replace(/\s+/g, ' ');
  return /Delete workspace|删除工作区|删除工作空间|删除工作區/i.test(text);
}

function injectOpenDirItem(menu) {
  const viewport = menu.querySelector('[role="presentation"]') || menu;
  const templateButton = menu.querySelector('button[role="menuitem"]');
  const template = templateButton && templateButton.closest('div');
  if (!template) return;

  const item = template.cloneNode(true);
  item.setAttribute('data-dsh-fnos-open-dir', '1');
  const button = item.querySelector('button[role="menuitem"]');
  if (!button) return;

  const spans = Array.from(button.querySelectorAll('span'));
  const label = spans.find((span) => /Rename|重命名|Delete|删除/i.test(span.textContent || ''))
    || spans[spans.length - 1];
  if (label) label.textContent = '打开目录';

  const icon = spans.find((span) => span !== label);
  if (icon) icon.innerHTML = FOLDER_OPEN_SVG;

  button.addEventListener('click', (event) => {
    event.preventDefault();
    event.stopPropagation();
    const context = lastWorkspaceMenu;
    closeMenu(menu);
    if (!context) {
      showToast('无法确定工作区');
      return;
    }
    void openWorkspaceDirectory(context.id);
  });

  viewport.insertBefore(item, viewport.firstChild);
}

function closeMenu(menu) {
  try {
    const options = { key: 'Escape', code: 'Escape', keyCode: 27, which: 27, bubbles: true };
    document.dispatchEvent(new KeyboardEvent('keydown', options));
    window.dispatchEvent(new KeyboardEvent('keydown', options));
  } catch { /* ignore */ }
  window.setTimeout(() => {
    if (menu.isConnected) menu.remove();
  }, 0);
}

async function resolveWorkspacePath(id) {
  const cached = workspacePaths.get(id);
  if (cached) return cached;

  try {
    const response = await fetch(
      `${pluginURL('fnos-advanced/workspace-path')}?id=${encodeURIComponent(id)}`,
      { credentials: 'same-origin', cache: 'no-store', headers: { Accept: 'application/json' } },
    );
    if (response.ok) {
      const data = await response.json();
      if (data && data.ok && typeof data.path === 'string' && data.path) {
        workspacePaths.set(id, data.path);
        return data.path;
      }
    }
  } catch { /* older host half or transient network error */ }
  return '';
}

async function openWorkspaceDirectory(id) {
  const path = await resolveWorkspacePath(id);
  if (!path) {
    showToast('无法获取工作区目录');
    return;
  }

  const sdk = getSdk();
  try { await sdk.ready(); } catch { /* fall through to the bridge call */ }
  try {
    await sdk.openFileManager(path);
  } catch (error) {
    console.warn(LOG_PREFIX, 'openFileManager failed', error);
    showToast('打开目录失败，当前宿主可能不支持该操作。');
  }
}

function showToast(text) {
  let toast = document.querySelector('.dsh-fnos-advanced-toast');
  if (!toast) {
    toast = document.createElement('div');
    toast.className = 'dsh-fnos-advanced-toast';
    toast.setAttribute('role', 'status');
    document.body.appendChild(toast);
  }
  toast.textContent = text;
  toast.style.display = 'block';
  if (toast.__dshFnosTimer) window.clearTimeout(toast.__dshFnosTimer);
  toast.__dshFnosTimer = window.setTimeout(() => {
    toast.style.display = 'none';
  }, 5000);
}

function isWorkspacePicker(dialog) {
  if (!isVisible(dialog)) return false;

  const title = [
    dialog.getAttribute('aria-label') || '',
    ...Array.from(dialog.querySelectorAll('h1, h2, h3')).map((node) => node.textContent || ''),
  ].join(' ');
  if (!WORKSPACE_TITLE_RE.test(title)) return false;

  // A workspace rename dialog also has an input and a footer. Requiring a
  // path-labelled input (or the picker's edit-path affordance) keeps us out
  // of those dialogs.
  if (!findPathInput(dialog) && !findEditButton(dialog)) return false;
  return Boolean(findFooter(dialog));
}

function injectButton(dialog) {
  const footer = findFooter(dialog);
  if (!footer) return;

  const button = document.createElement('button');
  button.type = 'button';
  button.setAttribute(BUTTON_ATTR, '1');
  button.className = 'dsh-fnos-advanced-button';
  button.textContent = '选择飞牛目录';
  button.title = '通过飞牛授权选择一个目录（会授予当前飞牛应用访问权限）';

  // Put it before the leading footer action (New folder / 新建文件夹), so the
  // button does not sit between Cancel and Open.
  footer.insertBefore(button, footer.firstChild);
}

function findPathInput(dialog) {
  for (const input of dialog.querySelectorAll('input')) {
    if (input.type && input.type !== 'text') continue;
    const label = [
      input.getAttribute('aria-label') || '',
      input.getAttribute('placeholder') || '',
      input.title || '',
    ].join(' ');
    if (PATH_LABEL_RE.test(label)) return input;
    if (typeof input.value === 'string' && input.value.startsWith('/')) return input;
  }
  return null;
}

function findEditButton(dialog) {
  for (const button of dialog.querySelectorAll('button')) {
    const label = [
      button.getAttribute('aria-label') || '',
      button.title || '',
      button.textContent || '',
    ].join(' ');
    if (PATH_LABEL_RE.test(label)) return button;
  }

  // Language-independent structural fallback: the picker's crumb trail is a
  // [role=navigation] element, and the edit-path button is its icon-only
  // sibling inside the crumb bar.
  const trail = dialog.querySelector('[role="navigation"]');
  const bar = trail && trail.parentElement;
  if (bar) {
    for (const button of bar.querySelectorAll('button')) {
      if (trail.contains(button)) continue;
      if (!(button.textContent || '').trim()) return button;
    }
  }
  return null;
}

function findFooter(dialog) {
  const buttons = Array.from(dialog.querySelectorAll('button'));
  const cancel = buttons.find((button) => CANCEL_BUTTON_RE.test(buttonText(button)));
  const open = buttons.find((button) => OPEN_BUTTON_RE.test(buttonText(button)));

  if (cancel && open && cancel.parentElement === open.parentElement) {
    return cancel.parentElement;
  }
  if (open && open.parentElement) return open.parentElement;
  if (cancel && cancel.parentElement) return cancel.parentElement;

  // Unknown locale: the footer is the parent of the last button in DOM order.
  const last = buttons[buttons.length - 1];
  if (last && last.parentElement && last.parentElement !== dialog) return last.parentElement;
  return null;
}

function buttonText(button) {
  return (button.textContent || '').replace(/\s+/g, ' ').trim();
}

async function ensurePathInput(dialog) {
  let input = findPathInput(dialog);
  if (input) return input;

  const edit = findEditButton(dialog);
  if (!edit) return null;

  edit.click();
  for (let i = 0; i < 24; i += 1) {
    await sleep(50);
    input = findPathInput(dialog);
    if (input) {
      input.focus();
      return input;
    }
  }
  return null;
}

async function handlePick(button) {
  if (button.dataset.busy === '1') return;
  const dialog = button.closest('[role="dialog"]');
  if (!dialog) return;

  const originalText = button.textContent || '选择飞牛目录';
  button.dataset.busy = '1';
  button.disabled = true;
  button.textContent = '准备授权…';

  // Top-level browsers need the popup opened inside the click task. Host
  // environments (fnOS desktop / mobile app) get the native picker first and
  // do not need a popup at all.
  const initialSdk = getSdk();
  const hostLike = window.parent !== window || initialSdk.isWeb === false;
  let popup = null;
  if (!hostLike) {
    popup = window.open('', `dsh-fnos-advanced-auth-${randomId()}`, POPUP_FEATURES);
    if (popup) writePopupLoading(popup);
  }

  try {
    const direct = await tryDirectPick(button, dialog, originalText);
    if (direct) {
      if (popup) {
        try { popup.close(); } catch { /* ignore */ }
      }
      return;
    }

    const config = await loadConfig();
    const appName = config.appName || 'Harness';
    const settings = await loadSettings();
    const target = await resolveAuthTargets(config, settings);

    if (target.mode === 'choose') {
      if (popup) {
        try { popup.close(); } catch { /* ignore */ }
      }
      showAuthChooser(dialog, button, originalText, appName, target.choices);
      return;
    }

    if (!popup || popup.closed) {
      popup = window.open('', `dsh-fnos-advanced-auth-${randomId()}`, POPUP_FEATURES);
    }
    if (!popup) throw new Error('浏览器拦截了授权窗口，请允许弹窗后重试。');

    await startAuthFlow(button, dialog, originalText, appName, target.base, popup);
  } catch (error) {
    if (popup) {
      try { popup.close(); } catch { /* ignore */ }
    }
    handlePickError(button, dialog, originalText, error);
  }
}

/**
 * In fnOS desktop / the mobile app, the official SDK can open the native
 * directory picker directly through the host bridge. Only fall back to URL
 * authorization when that bridge is not available.
 *
 * @returns {Promise<boolean>} true when the direct attempt was handled
 *   (success, or the user cancelled); false when the caller should fall back.
 */
async function tryDirectPick(button, dialog, originalText) {
  const sdk = getSdk();
  try {
    await sdk.ready();
  } catch {
    return false;
  }

  // A normal browser tab has no host bridge; don't even try.
  if (sdk.isWeb && sdk.isStandaloneWeb) return false;
  if (sdk.isWeb && window.parent === window) return false;

  button.textContent = '等待选择目录…';

  let result;
  try {
    result = await sdk.pickUserFile({
      directory: true,
      title: '选择工作区目录',
      okText: '确认授权',
      sidebarGroup: ['myFiles', 'otherShare', 'external', 'remote', 'favorites', 'team'],
    });
  } catch (error) {
    console.warn(LOG_PREFIX, 'in-host picker unavailable; falling back to URL auth', error);
    showStatus(dialog, '当前宿主未提供内嵌目录选择器，已改用网页授权。', false);
    return false;
  }

  const path = result && result.code === 0 && Array.isArray(result.data) ? result.data[0] : '';
  if (!path) {
    if (isPickerCancelled(result)) {
      showStatus(dialog, '已取消选择目录', false);
    } else {
      showStatus(dialog, result && result.msg ? result.msg : '打开目录选择器失败', true);
    }
    resetButton(button, originalText);
    return true;
  }

  const input = await ensurePathInput(dialog);
  if (!input) throw new Error('没有找到目录选择器的路径输入框');

  fillAndEnter(input, path);
  showStatus(dialog, `已填入授权目录：${path}`, false);
  button.textContent = '已填入路径';
  window.setTimeout(() => resetButton(button, originalText), 1600);
  return true;
}

async function startAuthFlow(button, dialog, originalText, appName, base, existingPopup) {
  button.textContent = '等待授权…';
  const state = randomId();

  let popup = existingPopup;
  if (!popup || popup.closed) {
    popup = window.open('', `dsh-fnos-advanced-auth-${state}`, POPUP_FEATURES);
  }
  if (!popup) throw new Error('浏览器拦截了授权窗口，请允许弹窗后重试。');
  writePopupLoading(popup);

  try {
    const input = await ensurePathInput(dialog);
    if (!input) throw new Error('没有找到目录选择器的路径输入框');

    const auth = await requestAuth(popup, state, appName, base);
    if (!auth || auth.status !== 'success') {
      if (auth && (auth.status === 'cancelled' || isCancellationText(auth.error))) {
        // Text matching is a heuristic (older hosts report cancellation as
        // "Operation failed" instead of a status). Leave a breadcrumb so a
        // misclassified genuine failure can still be diagnosed.
        if (auth.status !== 'cancelled') {
          console.info(LOG_PREFIX, 'authorization result treated as cancellation', auth);
        }
        showStatus(dialog, '已取消选择目录', false);
        resetButton(button, originalText);
        return;
      }
      throw new Error(auth && auth.error ? auth.error : '打开目录选择器失败');
    }

    const path = Array.isArray(auth.path) ? auth.path[0] : '';
    if (!path) throw new Error('授权结果里没有目录路径');

    fillAndEnter(input, path);
    showStatus(dialog, `已填入授权目录：${path}`, false);
    button.textContent = '已填入路径';
    window.setTimeout(() => resetButton(button, originalText), 1600);
  } catch (error) {
    try { popup.close(); } catch { /* ignore */ }
    throw error;
  }
}

function handlePickError(button, dialog, originalText, error) {
  const message = error && error.message ? error.message : String(error);
  console.warn(LOG_PREFIX, message, error);
  showStatus(dialog, message, true);
  resetButton(button, originalText);
}

function resetButton(button, text) {
  button.textContent = text;
  button.disabled = false;
  delete button.dataset.busy;
}

function writePopupLoading(popup) {
  try {
    popup.document.title = '飞牛目录授权';
    popup.document.body.innerHTML = '<p style="font:14px/1.6 -apple-system,BlinkMacSystemFont,\'PingFang SC\',sans-serif;padding:24px;color:#555">正在打开飞牛授权页面…</p>';
  } catch { /* about:blank may already be navigating */ }
}

/**
 * Ask fnOS for a directory authorization through the web authorization page.
 *
 * @param {Window} popup
 * @param {string} state
 * @param {string} appName
 * @param {string} base
 * @returns {Promise<any>}
 */
function requestAuth(popup, state, appName, base) {
  return new Promise((resolve, reject) => {
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

    const finish = (result) => {
      if (settled) return;
      settled = true;
      cleanup();
      resolve(result);
    };

    const fail = (error) => {
      if (settled) return;
      settled = true;
      cleanup();
      reject(error);
    };

    const accept = (payload) => {
      if (!payload || settled) return;
      const payloadState = payload.state || (payload.result && payload.result.state) || '';
      if (state && payloadState && payloadState !== state) return;
      const result = parseAuthPayload(payload);
      if (!result) return;
      finish(result);
    };

    function onMessage(event) {
      if (event.origin !== window.location.origin) return;
      const data = event.data;
      if (!data || data.type !== MSG_TYPE) return;
      accept(data);
    }

    function onStorage(event) {
      if (event.key !== STORAGE_KEY || !event.newValue) return;
      try { accept(JSON.parse(event.newValue)); } catch { /* ignore */ }
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
        const data = await response.json();
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
        if (!settled) finish({ status: 'cancelled', error: '授权窗口已关闭' });
      }, 450);
    }, 500);

    timeoutTimer = window.setTimeout(() => {
      fail(new Error('授权超时，请重试'));
    }, AUTH_TIMEOUT_MS);

    (async () => {
      try {
        const sdk = getSdk();
        const previousBase = sdk.getAppAuthBaseUrl;

        sdk.getAppAuthBaseUrl = async () => base;
        let url;
        try {
          url = await sdk.buildAppAuthUrl('pickUserFile', {
            appName,
            directory: true,
            sidebarGroup: ['myFiles', 'otherShare', 'external', 'remote', 'favorites', 'team'],
            title: '选择工作区目录',
            okText: '确认授权',
            redirectUri: pluginURL('fnos-advanced/auth-callback.html'),
            state,
          });
        } finally {
          sdk.getAppAuthBaseUrl = previousBase;
        }

        if (settled) return;
        popup.location.replace(url);
      } catch (error) {
        fail(error instanceof Error ? error : new Error(String(error)));
      }
    })();
  });
}

function showAuthChooser(dialog, button, originalText, appName, choices) {
  removeChooser(dialog);
  if (getComputedStyle(dialog).position === 'static') dialog.style.position = 'relative';
  button.textContent = '选择授权入口…';

  const panel = document.createElement('div');
  panel.className = 'dsh-fnos-advanced-chooser';
  panel.setAttribute('role', 'menu');
  panel.setAttribute('aria-label', '选择授权入口');

  const title = document.createElement('div');
  title.className = 'dsh-fnos-advanced-chooser-title';
  title.textContent = '选择授权入口';
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
      removeChooser(dialog);
      startAuthFlow(button, dialog, originalText, appName, choice.base, null).catch((error) => {
        handlePickError(button, dialog, originalText, error);
      });
    });
    panel.appendChild(option);
  }

  const cancel = document.createElement('button');
  cancel.type = 'button';
  cancel.className = 'dsh-fnos-advanced-chooser-cancel';
  cancel.textContent = '取消';
  cancel.addEventListener('click', (event) => {
    event.preventDefault();
    event.stopPropagation();
    removeChooser(dialog);
    resetButton(button, originalText);
  });
  panel.appendChild(cancel);

  dialog.appendChild(panel);
}

function removeChooser(dialog) {
  dialog.querySelectorAll('.dsh-fnos-advanced-chooser').forEach((node) => node.remove());
}

function parseAuthPayload(payload) {
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

function getSdk() {
  if (!sdkInstance) sdkInstance = new TrimApp();
  return sdkInstance;
}

function acceptStoredResult(expectedState, accept) {
  const stored = readStoredResult();
  if (!stored) return;
  if (expectedState && stored.state && stored.state !== expectedState) return;
  if (typeof accept === 'function') accept(stored);
}

function readStoredResult() {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    return JSON.parse(raw);
  } catch {
    return null;
  }
}

function loadConfig() {
  if (configPromise) return configPromise;
  configPromise = (async () => {
    try {
      const response = await fetch(pluginURL('fnos-advanced/config'), {
        credentials: 'same-origin',
        cache: 'no-store',
        headers: { Accept: 'application/json' },
      });
      if (response.ok) {
        const data = await response.json();
        if (data && typeof data === 'object') return data;
      }
    } catch { /* the plugin may be loaded from a static bundle */ }
    return { appName: 'Harness', authBase: '' };
  })();
  return configPromise;
}

function pluginURL(relative) {
  const base = document.baseURI.endsWith('/') ? document.baseURI : `${document.baseURI}/`;
  return new URL(relative, base).href;
}

async function resolveAuthTargets(config, settings) {
  const appName = config.appName || 'Harness';

  if (await isGatewayAccess(appName)) {
    return { mode: 'gateway', base: window.location.origin, via: 'gateway' };
  }

  const proxy = normalizeBase(settings.proxyBase || '');
  const lan = lanBaseFromSettings(settings);
  const choices = [];

  if (proxy) {
    choices.push({ id: 'proxy', label: '通过反代地址', detail: proxy, base: proxy });
  }
  if (lan && !choices.some((choice) => choice.base === lan)) {
    choices.push({ id: 'lan', label: '通过内网地址', detail: lan, base: lan });
  }

  if (settings.authMode === 'proxy' && proxy) {
    return { mode: 'single', base: proxy, via: 'proxy' };
  }
  if (settings.authMode === 'lan' && lan) {
    return { mode: 'single', base: lan, via: 'lan' };
  }
  if (choices.length > 1) return { mode: 'choose', choices };
  if (choices.length === 1) return { mode: 'single', base: choices[0].base, via: choices[0].id };

  return { mode: 'single', base: resolveAuthBase(), via: 'auto' };
}

async function isGatewayAccess(appName) {
  if (/^\/app(?:\/|$)/.test(window.location.pathname)) return true;
  return authRouteExists(appName);
}

function lanBaseFromSettings(settings) {
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

async function loadSettings(force = false) {
  if (!force && settingsCache && Date.now() - settingsCachedAt < 5000) return settingsCache;
  if (!force && settingsPromise) return settingsPromise;

  const request = (async () => {
    try {
      const response = await fetch(pluginURL('fnos-advanced/settings'), {
        credentials: 'same-origin',
        cache: 'no-store',
        headers: { Accept: 'application/json' },
      });
      if (response.ok) {
        const data = await response.json();
        if (data && data.ok && data.settings) {
          settingsCache = normalizeClientSettings(data.settings);
          settingsCachedAt = Date.now();
          settingsHostState = 'ok';
          return settingsCache;
        }
        settingsHostState = 'error';
      } else if (response.status === 404 || response.status === 405) {
        settingsHostState = 'missing';
      } else {
        settingsHostState = 'error';
      }
    } catch {
      settingsHostState = 'error';
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

function normalizeClientSettings(settings) {
  return {
    proxyBase: normalizeBase(typeof settings?.proxyBase === 'string' ? settings.proxyBase : ''),
    lanTarget: typeof settings?.lanTarget === 'string' && settings.lanTarget.trim()
      ? settings.lanTarget.trim()
      : DEFAULT_SETTINGS.lanTarget,
    authMode: ['ask', 'proxy', 'lan'].includes(settings?.authMode)
      ? settings.authMode
      : DEFAULT_SETTINGS.authMode,
  };
}

async function authRouteExists(appName) {
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

function resolveAuthBase() {
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

function readStoredAuthBase() {
  try { return localStorage.getItem(AUTH_BASE_KEY) || ''; } catch { return ''; }
}

function normalizeBase(value) {
  return String(value || '').replace(/\/+$/, '');
}

function fillAndEnter(input, path) {
  input.focus();

  const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')?.set;
  if (setter) setter.call(input, path);
  else input.value = path;

  input.dispatchEvent(new Event('input', { bubbles: true }));
  input.dispatchEvent(new Event('change', { bubbles: true }));

  // Let React commit the controlled value before the Enter handler reads it.
  window.setTimeout(() => {
    input.focus();
    const options = {
      key: 'Enter',
      code: 'Enter',
      keyCode: 13,
      which: 13,
      charCode: 13,
      bubbles: true,
      cancelable: true,
    };
    input.dispatchEvent(new KeyboardEvent('keydown', options));
    input.dispatchEvent(new KeyboardEvent('keypress', options));
    input.dispatchEvent(new KeyboardEvent('keyup', options));
  }, 0);
}

function showStatus(dialog, text, isError) {
  if (getComputedStyle(dialog).position === 'static') dialog.style.position = 'relative';

  let status = dialog.querySelector(`.${STATUS_CLASS}`);
  if (!status) {
    status = document.createElement('div');
    status.className = STATUS_CLASS;
    status.setAttribute('role', 'status');
    dialog.appendChild(status);
  }
  status.textContent = text;
  status.dataset.error = isError ? '1' : '0';
  status.style.display = 'block';

  if (status.__dshFnosTimer) window.clearTimeout(status.__dshFnosTimer);
  status.__dshFnosTimer = window.setTimeout(() => {
    status.style.display = 'none';
  }, isError ? 10000 : 6000);
}

function registerSettingsSlot(ctx) {
  if (!ctx || typeof ctx.inject !== 'function') return;

  ctx.inject(['slots'], (scope) => {
    const slots = scope.slots;
    if (!slots || typeof slots.inject !== 'function' || typeof slots.register !== 'function') return;

    // Bundle-level config renders directly on the package page, above the
    // component rows. The row-level slot would bury the same form one level
    // deeper (the user asked for it on this page instead).
    const install = () => slots.inject('plugins.bundle.config', () =>
      slots.register({ name: 'plugins.bundle.config', key: SETTINGS_SLOT_KEY }, SettingsSlot));

    if (typeof scope.effect === 'function') {
      scope.effect(install, 'dsh-fnos-advanced: settings page');
    } else {
      install();
    }
  });
}

function SettingsSlot(props) {
  if (props && props.view === 'summary') return settingsSummary();
  return React.createElement(SettingsPage, props || {});
}

function settingsSummary() {
  const settings = settingsCache || DEFAULT_SETTINGS;
  const proxy = settings.proxyBase || '未设置';
  return `反代 ${proxy} · 内网 ${settings.lanTarget}`;
}

function SettingsPage() {
  const [loading, setLoading] = React.useState(true);
  const [saving, setSaving] = React.useState(false);
  const [settings, setSettings] = React.useState({ ...DEFAULT_SETTINGS });
  const [message, setMessage] = React.useState('');
  const [error, setError] = React.useState('');
  const [embedded, setEmbedded] = React.useState(false);
  const [gatewayAccess, setGatewayAccess] = React.useState(/^\/app(?:\/|$)/.test(window.location.pathname));
  const [hostState, setHostState] = React.useState('unknown');
  const [hostVersion, setHostVersion] = React.useState('');

  React.useEffect(() => {
    let alive = true;
    loadSettings(true).then((loaded) => {
      if (!alive) return;
      setSettings(loaded);
      setHostState(settingsHostState);
      setLoading(false);
    }).catch(() => {
      if (alive) setLoading(false);
    });

    loadConfig().then((config) => {
      if (alive) setHostVersion(config.version || '');
    }).catch(() => { /* keep empty */ });

    const sdk = getSdk();
    sdk.ready().then(() => {
      if (alive) setEmbedded(!sdk.isStandaloneWeb);
    }).catch(() => { /* no host bridge */ });

    if (!/^\/app(?:\/|$)/.test(window.location.pathname)) {
      loadConfig()
        .then((config) => authRouteExists(config.appName || 'Harness'))
        .then((ok) => { if (alive) setGatewayAccess(ok); })
        .catch(() => { /* keep path-based result */ });
    }

    return () => { alive = false; };
  }, []);

  const update = (key) => (event) => {
    const value = event.target.value;
    setSettings((current) => ({ ...current, [key]: value }));
    setMessage('');
    setError('');
  };

  const save = async () => {
    setSaving(true);
    setMessage('');
    setError('');
    try {
      const proxyBase = validateProxyBase(settings.proxyBase);
      const response = await fetch(pluginURL('fnos-advanced/settings'), {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
        body: JSON.stringify({
          proxyBase,
          lanTarget: String(settings.lanTarget || '').trim() || DEFAULT_SETTINGS.lanTarget,
          authMode: settings.authMode,
        }),
      });
      if (response.status === 404 || response.status === 405) {
        throw new Error('宿主半侧未提供设置接口，通常是插件升级后没有重启 dsh。请重启 dsh 后再保存。');
      }
      const data = await response.json().catch(() => ({}));
      if (!response.ok || !data.ok) throw new Error(data.error || `保存失败（HTTP ${response.status}）`);
      const saved = normalizeClientSettings(data.settings);
      settingsCache = saved;
      settingsCachedAt = Date.now();
      setSettings(saved);
      setMessage('已保存');
    } catch (err) {
      setError(err && err.message ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const hostOutdated = Boolean(hostVersion && hostVersion !== VERSION);
  const hostReady = hostState === 'ok' && !hostOutdated;
  const hostWarning = hostReady
    ? ''
    : hostOutdated
      ? `宿主半侧版本 ${hostVersion}，客户端 ${VERSION}。请重启 dsh 后再保存设置。`
      : hostState === 'missing'
        ? '宿主半侧未提供设置接口，通常是插件升级后没有重启 dsh。请重启 dsh 后再保存设置。'
        : hostState === 'error'
          ? '无法读取宿主设置接口，请确认 dsh 运行正常。'
          : '';
  const disabled = loading || saving || !hostReady;
  const inputStyle = {
    boxSizing: 'border-box',
    width: '100%',
    height: 36,
    padding: '0 10px',
    borderRadius: 8,
    border: '1px solid var(--dsw-alias-border-l2, rgba(127,127,127,.32))',
    background: 'transparent',
    color: 'var(--dsw-alias-label-primary, inherit)',
    font: 'inherit',
    fontSize: 14,
    outline: 'none',
  };
  const labelStyle = {
    display: 'flex',
    flexDirection: 'column',
    gap: 6,
    fontSize: 13,
    color: 'var(--dsw-alias-label-secondary, #666)',
  };
  const hintStyle = {
    margin: 0,
    fontSize: 12,
    lineHeight: '18px',
    color: 'var(--dsw-alias-label-tertiary, #888)',
  };
  const buttonStyle = {
    boxSizing: 'border-box',
    height: 36,
    padding: '0 16px',
    borderRadius: 8,
    border: '1px solid var(--dsw-alias-border-l2, rgba(127,127,127,.35))',
    background: 'transparent',
    color: 'var(--dsw-alias-label-primary, inherit)',
    font: 'inherit',
    fontSize: 14,
    fontWeight: 500,
    cursor: disabled ? 'default' : 'pointer',
    opacity: disabled ? 0.6 : 1,
  };

  const accessNote = embedded
    ? '当前运行在飞牛桌面/宿主内：会直接调用飞牛目录选择器，不打开网页授权。'
    : gatewayAccess
      ? '当前通过飞牛网关访问：直接使用当前同源地址授权，不会弹出入口选择。'
      : '当前不是通过飞牛网关访问：同时配置反代与内网地址后，点击「选择飞牛目录」会让你选择授权入口。';

  return React.createElement('div', {
    style: {
      display: 'flex',
      flexDirection: 'column',
      gap: 14,
      padding: '4px 0 10px',
      maxWidth: 640,
      color: 'var(--dsw-alias-label-primary, inherit)',
      fontSize: 14,
    },
  }, [
    React.createElement('div', { key: 'heading', style: { display: 'flex', flexDirection: 'column', gap: 4 } }, [
      React.createElement('div', { key: 'title', style: { fontSize: 15, fontWeight: 600 } }, '飞牛目录授权'),
      React.createElement('p', { key: 'desc', style: hintStyle }, '配置非网关访问时使用的飞牛授权入口；设置保存在当前 dsh 数据目录。'),
    ]),

    React.createElement('label', { key: 'proxy', style: labelStyle }, [
      React.createElement('span', { key: 'label' }, '反代飞牛地址'),
      React.createElement('input', {
        key: 'input',
        type: 'text',
        value: settings.proxyBase,
        onChange: update('proxyBase'),
        placeholder: 'https://fnos.example.com',
        autoComplete: 'off',
        spellCheck: false,
        disabled,
        style: inputStyle,
      }),
      React.createElement('span', { key: 'hint', style: hintStyle }, '填协议 + 域名[:端口]，不要带路径；留空表示未配置。'),
    ]),

    React.createElement('label', { key: 'lan', style: labelStyle }, [
      React.createElement('span', { key: 'label' }, '飞牛内网地址 / 端口'),
      React.createElement('input', {
        key: 'input',
        type: 'text',
        value: settings.lanTarget,
        onChange: update('lanTarget'),
        placeholder: '5666 或 http://192.168.1.10:5666',
        autoComplete: 'off',
        spellCheck: false,
        disabled,
        style: inputStyle,
      }),
      React.createElement('span', { key: 'hint', style: hintStyle }, '只填端口时使用当前访问主机的地址；默认 5666。'),
    ]),

    React.createElement('label', { key: 'mode', style: labelStyle }, [
      React.createElement('span', { key: 'label' }, '非网关访问时的授权入口'),
      React.createElement('select', {
        key: 'select',
        value: settings.authMode,
        onChange: update('authMode'),
        disabled,
        style: inputStyle,
      }, [
        React.createElement('option', { key: 'ask', value: 'ask' }, '每次询问（推荐）'),
        React.createElement('option', { key: 'proxy', value: 'proxy' }, '优先使用反代地址'),
        React.createElement('option', { key: 'lan', value: 'lan' }, '优先使用内网地址'),
      ]),
    ]),

    React.createElement('p', { key: 'access', style: hintStyle }, accessNote),
    hostWarning ? React.createElement('p', {
      key: 'host-warning',
      style: { ...hintStyle, color: 'var(--dsw-alias-state-error-primary, #d93025)' },
    }, hostWarning) : null,

    React.createElement('div', { key: 'actions', style: { display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' } }, [
      React.createElement('button', {
        key: 'save',
        type: 'button',
        onClick: save,
        disabled,
        style: buttonStyle,
      }, saving ? '保存中…' : '保存'),
      loading ? React.createElement('span', { key: 'loading', style: hintStyle }, '读取设置…') : null,
      message ? React.createElement('span', { key: 'ok', style: { fontSize: 12, color: 'var(--dsw-alias-state-success-primary, #188038)' } }, message) : null,
      error ? React.createElement('span', { key: 'err', style: { fontSize: 12, color: 'var(--dsw-alias-state-error-primary, #d93025)' } }, error) : null,
    ]),
  ]);
}

function validateProxyBase(value) {
  const text = String(value || '').trim();
  if (!text) return '';
  const url = new URL(text);
  if (url.protocol !== 'http:' && url.protocol !== 'https:') {
    throw new Error('反代地址只支持 http/https');
  }
  return url.origin;
}

function injectStyle() {
  if (document.getElementById(STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = STYLE_ID;
  style.textContent = `
    .dsh-fnos-advanced-button {
      box-sizing: border-box;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      height: 36px;
      padding: 0 14px;
      border: 1px solid var(--dsw-alias-border-l2, rgba(127, 127, 127, .35));
      border-radius: var(--dsw-radius-md, 8px);
      background: transparent;
      color: var(--dsw-alias-label-primary, inherit);
      font: inherit;
      font-size: 14px;
      font-weight: 500;
      line-height: 1;
      white-space: nowrap;
      cursor: pointer;
      transition: background-color .15s ease, border-color .15s ease;
    }
    .dsh-fnos-advanced-button:hover:not(:disabled) {
      background: var(--dsw-alias-interactive-bg-hover, rgba(127, 127, 127, .1));
      border-color: var(--dsw-alias-border-l1, rgba(127, 127, 127, .5));
    }
    .dsh-fnos-advanced-button:focus-visible {
      outline: var(--dsw-focus-ring-width, 2px) solid var(--dsw-alias-state-business-primary, #4f7cff);
      outline-offset: 1px;
    }
    .dsh-fnos-advanced-button:disabled {
      opacity: .62;
      cursor: default;
    }
    .dsh-fnos-advanced-status {
      position: absolute;
      left: 24px;
      right: 24px;
      bottom: 70px;
      z-index: 10;
      display: none;
      box-sizing: border-box;
      padding: 8px 12px;
      border-radius: 8px;
      background: var(--dsw-alias-bg-layer-2, rgba(127, 127, 127, .12));
      color: var(--dsw-alias-label-secondary, inherit);
      font-size: 12px;
      line-height: 18px;
      overflow-wrap: anywhere;
      pointer-events: none;
    }
    .dsh-fnos-advanced-status[data-error="1"] {
      color: var(--dsw-alias-state-error-primary, #d93025);
    }
    .dsh-fnos-advanced-chooser {
      position: absolute;
      left: 24px;
      right: 24px;
      bottom: 70px;
      z-index: 30;
      display: flex;
      flex-direction: column;
      gap: 8px;
      box-sizing: border-box;
      padding: 12px;
      border: 1px solid var(--dsw-alias-border-l2, rgba(127, 127, 127, .32));
      border-radius: 12px;
      background: var(--dsw-alias-bg-layer-1, #fff);
      color: var(--dsw-alias-label-primary, inherit);
      box-shadow: 0 12px 40px rgba(0, 0, 0, .18);
    }
    .dsh-fnos-advanced-chooser-title {
      font-size: 13px;
      font-weight: 600;
      line-height: 18px;
    }
    .dsh-fnos-advanced-chooser-option,
    .dsh-fnos-advanced-chooser-cancel {
      box-sizing: border-box;
      display: flex;
      flex-direction: column;
      align-items: flex-start;
      gap: 2px;
      width: 100%;
      padding: 8px 10px;
      border: 1px solid var(--dsw-alias-border-l2, rgba(127, 127, 127, .32));
      border-radius: 8px;
      background: transparent;
      color: inherit;
      font: inherit;
      text-align: left;
      cursor: pointer;
    }
    .dsh-fnos-advanced-chooser-option:hover,
    .dsh-fnos-advanced-chooser-cancel:hover {
      background: var(--dsw-alias-interactive-bg-hover, rgba(127, 127, 127, .1));
    }
    .dsh-fnos-advanced-chooser-label {
      font-size: 13px;
      font-weight: 500;
      line-height: 18px;
    }
    .dsh-fnos-advanced-chooser-detail {
      max-width: 100%;
      overflow-wrap: anywhere;
      color: var(--dsw-alias-label-tertiary, #888);
      font-size: 11px;
      line-height: 16px;
    }
    .dsh-fnos-advanced-chooser-cancel {
      align-items: center;
      color: var(--dsw-alias-label-secondary, #666);
      font-size: 12px;
    }
    .dsh-fnos-advanced-toast {
      position: fixed;
      left: 50%;
      bottom: 32px;
      z-index: 10000;
      display: none;
      box-sizing: border-box;
      max-width: min(480px, calc(100vw - 32px));
      padding: 10px 14px;
      border-radius: 10px;
      background: rgba(24, 24, 28, .92);
      color: #fff;
      font-size: 13px;
      line-height: 18px;
      overflow-wrap: anywhere;
      box-shadow: 0 10px 30px rgba(0, 0, 0, .24);
      transform: translateX(-50%);
      pointer-events: none;
    }
  `;
  (document.head || document.documentElement).appendChild(style);
}

function randomId() {
  try {
    if (window.crypto && typeof window.crypto.randomUUID === 'function') {
      return window.crypto.randomUUID();
    }
  } catch { /* ignore */ }
  return `fnos-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

function sleep(ms) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

function isVisible(element) {
  return Boolean(element && (element.offsetWidth || element.offsetHeight || element.getClientRects().length));
}

// Test/debug handle. The host-side workspace guard is the real installer; this
// exists so a standalone build can be exercised without the module loader.
if (typeof window !== 'undefined') {
  // Debug surface for the browser console (real-device diagnosis). Not a public
  // API; the pure cancellation helpers are unit-tested in test/cancel.test.mjs.
  window.__dshFnosAdvanced = {
    version: VERSION,
    install,
    handlePick,
    isPickerCancelled,
    isCancellationText,
    pickUserFile: (options) => getSdk().pickUserFile(options),
    openFileManager: (path) => getSdk().openFileManager(path),
    loadSettings,
    resolveAuthTargets,
    resolveAuthBase,
    findPathInput,
    findEditButton,
    findFooter,
    isWorkspacePicker,
    fillAndEnter,
  };
}
