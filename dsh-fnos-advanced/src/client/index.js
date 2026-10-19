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
import { fileNameForPath, imageMimeForPath } from './files.js';
import { looksLikeComposerCommandMenu } from './menu.js';
import { fileRowActions } from './file-actions.js';
import { pickerParams, isDuplicateWorkspacePath } from './picker.js';
import { t, translateError, getLanguagePreference, setLanguagePreference, currentLanguage } from './i18n.js';

const LOG_PREFIX = '[dsh-fnos-advanced]';
const VERSION = '0.3.1';

const MSG_TYPE = 'dsh-fnos-advanced:auth';
const STORAGE_KEY = 'dsh-fnos-advanced:auth-result';
const AUTH_BASE_KEY = 'dsh-fnos-advanced:auth-base';
const SETTINGS_SLOT_KEY = 'dsh-fnos-advanced';
const DEFAULT_SETTINGS = Object.freeze({ proxyBase: '', lanTarget: '5666', authMode: 'ask', imageRead: true });

const BUTTON_ATTR = 'data-dsh-fnos-advanced';
const PICK_ITEM_ATTR = 'data-dsh-fnos-pick-file';
const FILE_ACTION_ATTR = 'data-dsh-fnos-file-action';
const STATUS_CLASS = 'dsh-fnos-advanced-status';
const STYLE_ID = 'dsh-fnos-advanced-style';

const WORKSPACE_TITLE_RE = /workspace|工作区/i;
const PATH_LABEL_RE = /(?:^|\b)(?:edit\s*path|path)(?:\b|$)|路径|编辑路径|修改路径/i;
const OPEN_BUTTON_RE = /^(?:open|打开|确定|选择|select|choose)/i;
const CANCEL_BUTTON_RE = /^(?:cancel|取消)/i;

const POPUP_FEATURES = 'popup=yes,width=760,height=680,resizable=yes,scrollbars=yes';
const AUTH_TIMEOUT_MS = 5 * 60 * 1000;
const FOLDER_OPEN_SVG = '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" stroke-width="1"><path d="M1.5 3.5C1.5 2.95 1.95 2.5 2.5 2.5H5.4C5.65 2.5 5.88 2.59 6.07 2.75L7.7 4.2C7.88 4.36 8.12 4.45 8.36 4.45H13.5C14.05 4.45 14.5 4.9 14.5 5.45V12.5C14.5 13.05 14.05 13.5 13.5 13.5H2.5C1.95 13.5 1.5 13.05 1.5 12.5V3.5Z" stroke="currentColor"/></svg>';
const PICK_FILE_SVG = '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path d="M9.5 1.5H4.5C3.95 1.5 3.5 1.95 3.5 2.5V13.5C3.5 14.05 3.95 14.5 4.5 14.5H11.5C12.05 14.5 12.5 14.05 12.5 13.5V4.5L9.5 1.5Z" stroke="currentColor" stroke-linejoin="round"/><path d="M9.5 1.5V4.5H12.5" stroke="currentColor" stroke-linejoin="round"/><path d="M6 8.5H10M8 6.5V10.5" stroke="currentColor" stroke-linecap="round"/></svg>';
const FILE_ACTION_SVG = '<svg width="14" height="14" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><circle cx="3.5" cy="8" r="1.15" fill="currentColor"/><circle cx="8" cy="8" r="1.15" fill="currentColor"/><circle cx="12.5" cy="8" r="1.15" fill="currentColor"/></svg>';

let installedDispose = null;
let sdkInstance = null;
let configPromise = null;
let settingsCache = null;
let settingsCachedAt = 0;
let settingsPromise = null;
let settingsHostState = 'unknown';
let workspacePaths = new Map();
let lastWorkspaceMenu = null;
/** Paths registered for File objects handed to dsh's composer input. */
const hostPathByFile = new WeakMap();
/** The store dsh's captured bridge actually reads (survives client HMR). */
let hostPathStore = hostPathByFile;

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
  // dsh captures the host-path bridge once when the composer slot is injected
  // (`const bridge = hostPathBridge()`), so install ours at plugin load time,
  // long before a file is picked. Waiting until the click would be too late.
  ensureHostPathBridge();
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
    closeFileMenu();
    for (const node of document.querySelectorAll(`[${BUTTON_ATTR}], [${PICK_ITEM_ATTR}], .${STATUS_CLASS}, [data-dsh-fnos-open-dir]`)) {
      node.remove();
    }
    for (const row of document.querySelectorAll('.dsh-fnos-file-row')) {
      row.classList.remove('dsh-fnos-file-row');
      if (row.dataset.dshFnosPosition === '1') {
        row.style.position = '';
        delete row.dataset.dshFnosPosition;
      }
    }
    for (const node of document.querySelectorAll(`[${FILE_ACTION_ATTR}]`)) node.remove();
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
  scanCommandMenu();
  scanFileTree();
  scanWorkspaceMenus();
}

/**
 * Add "从飞牛选择" to dsh's composer command menu (the "+" menu: 文件 / 目标 /
 * 计划 / …) instead of adding a standalone toolbar button. A separate button
 * would compete with dsh's own tool row layout on narrow screens; the command
 * menu already owns a stable list surface.
 *
 * The menu is the input-trigger popup: `[data-trigger-menu]` +
 * `role="listbox"` + `button[role="option"]`. Identify it by the English
 * command aliases (file / goal / plan), which are language-independent.
 */
function scanCommandMenu() {
  for (const menu of document.querySelectorAll('[data-trigger-menu]')) {
    if (!isVisible(menu)) continue;
    if (menu.querySelector(`[${PICK_ITEM_ATTR}]`)) continue;
    if (!isComposerCommandMenu(menu)) continue;
    injectPickItem(menu);
  }
}

function isComposerCommandMenu(menu) {
  const options = Array.from(menu.querySelectorAll('button[role="option"]'));
  return looksLikeComposerCommandMenu(options.map((option) => option.textContent || ''));
}

function injectPickItem(menu) {
  const options = Array.from(menu.querySelectorAll('button[role="option"]'));
  const fileOption = options.find((option) => /\bfile\b/i.test(option.textContent || ''));
  if (!fileOption || !fileOption.parentElement) return;

  const item = fileOption.cloneNode(true);
  item.removeAttribute('id');
  item.removeAttribute('aria-selected');
  item.removeAttribute('aria-label');
  item.setAttribute(PICK_ITEM_ATTR, '1');
  // The source row may be the highlighted one; keep only one active row.
  if (item.className) {
    item.className = item.className.split(/\s+/).filter((name) => name && !/active/i.test(name)).join(' ');
  }

  // The row is: [icon] [name] [alias?] [description?]. English menus render
  // the same word for name and alias ("File"/"file"), so the alias row may be
  // absent; locate the icon by its SVG and walk siblings instead of matching
  // text. No CSS-module class names are used.
  const spans = Array.from(item.querySelectorAll('span'));
  const icon = spans.find((span) => span.querySelector('svg')) || spans[0];
  const name = icon ? icon.nextElementSibling : spans[1];
  let alias = name ? name.nextElementSibling : null;
  if (alias && (alias.querySelector('svg') || !(alias.textContent || '').trim())) alias = null;
  let description = alias ? alias.nextElementSibling : name ? name.nextElementSibling : null;

  // The "file" row has no description; borrow the description element (and its
  // styling) from a sibling command row instead of inventing a class name.
  if (!description) {
    const template = options
      .map((option) => {
        const optionAlias = Array.from(option.querySelectorAll('span'))
          .find((span) => (span.textContent || '').trim().toLowerCase() === 'goal');
        return optionAlias ? optionAlias.nextElementSibling : null;
      })
      .find((node) => node && node.tagName === 'SPAN');
    if (template) {
      description = template.cloneNode(false);
      item.appendChild(description);
    }
  }

  if (icon) icon.innerHTML = PICK_FILE_SVG;
  if (name) name.textContent = t('从飞牛选择');
  if (alias) alias.textContent = 'fnos';
  if (description) description.textContent = t('选择 NAS 上的文件，非图片作为路径引用');

  item.addEventListener('mousedown', (event) => {
    event.preventDefault();
    event.stopPropagation();
  });
  item.addEventListener('click', (event) => {
    event.preventDefault();
    event.stopPropagation();
    dismissTriggerMenu();
    void handlePickFiles(item);
  });

  fileOption.after(item);
}

function dismissTriggerMenu() {
  // The trigger menu dismisses itself on a pointerdown outside its surface.
  try {
    const event = typeof PointerEvent === 'function'
      ? new PointerEvent('pointerdown', { bubbles: true, cancelable: true })
      : new MouseEvent('mousedown', { bubbles: true, cancelable: true });
    (document.body || document.documentElement).dispatchEvent(event);
  } catch { /* React will also close it on the next outside click */ }
}

/** The visible composer's hidden file input (the menu is portaled outside it). */
function findActiveComposerInput() {
  for (const region of document.querySelectorAll('[data-conversation-region="composer"]')) {
    if (!isVisible(region)) continue;
    const input = findComposerFileInput(region);
    if (input) return { input, region };
  }
  return null;
}

function findComposerFileInput(region) {
  return Array.from(region.querySelectorAll('input[type="file"]'))
    .find((input) => input.multiple && !input.disabled) || null;
}

/** Row action menu state (only one open at a time). */
let fileMenu = null;
let fileMenuCleanup = null;

/**
 * Add a small "fnOS actions" control to every file-tree row.
 *
 * Rows expose `data-files-entry` (`file` / `directory` / `other`) and an
 * absolute `data-files-path`; both are deliberate automation anchors in
 * 0.2.0-rc.2 and 0.2.1-rc.2. The control sits outside the row <button> so a
 * click never opens/toggles the file itself.
 */
function scanFileTree() {
  for (const row of document.querySelectorAll('[data-files-entry][data-files-path]')) {
    const entry = row.getAttribute('data-files-entry') || '';
    const action = row.querySelector(`:scope > [${FILE_ACTION_ATTR}]`);
    if (entry !== 'file' && entry !== 'directory') {
      if (action) action.remove();
      row.classList.remove('dsh-fnos-file-row');
      if (row.dataset.dshFnosPosition === '1') {
        row.style.position = '';
        delete row.dataset.dshFnosPosition;
      }
      continue;
    }
    if (action) {
      const rowButton = row.querySelector(':scope > button');
      const height = rowButton ? rowButton.offsetHeight : 0;
      if (height && action.style.height !== `${height}px`) action.style.height = `${height}px`;
      continue;
    }
    injectFileAction(row);
  }
}

function injectFileAction(row) {
  const rowButton = row.querySelector(':scope > button');
  if (!rowButton) return;

  row.classList.add('dsh-fnos-file-row');
  if (getComputedStyle(row).position === 'static') {
    row.style.position = 'relative';
    row.dataset.dshFnosPosition = '1';
  }

  const action = document.createElement('button');
  action.type = 'button';
  action.setAttribute(FILE_ACTION_ATTR, '1');
  action.className = 'dsh-fnos-file-action';
  action.innerHTML = FILE_ACTION_SVG;
  action.title = t('飞牛操作');
  action.setAttribute('aria-label', t('飞牛操作'));
  action.style.height = `${rowButton.offsetHeight || 28}px`;

  action.addEventListener('mousedown', (event) => {
    event.preventDefault();
    event.stopPropagation();
  });
  action.addEventListener('click', (event) => {
    event.preventDefault();
    event.stopPropagation();
    if (fileMenu && fileMenu.__dshFnosRow === row) {
      closeFileMenu();
      return;
    }
    openFileMenu(action, row);
  });

  row.appendChild(action);
}

const FILE_ACTION_TEXT = Object.freeze({
  open: '在飞牛中打开',
  quote: '引用到对话',
  copy: '复制路径',
});

function openFileMenu(anchor, row) {
  closeFileMenu();
  const path = row.getAttribute('data-files-path') || '';
  const entry = row.getAttribute('data-files-entry') || '';
  const actions = fileRowActions(entry, isFnosHost());
  if (!path || !actions.length) return;

  const menu = document.createElement('div');
  menu.className = 'dsh-fnos-file-menu';
  menu.setAttribute('role', 'menu');
  menu.__dshFnosRow = row;

  const title = document.createElement('div');
  title.className = 'dsh-fnos-file-menu-title';
  title.textContent = path.split('/').filter(Boolean).pop() || path;
  title.title = path;
  menu.append(title);

  for (const action of actions) {
    const item = document.createElement('button');
    item.type = 'button';
    item.className = 'dsh-fnos-file-menu-item';
    item.setAttribute('role', 'menuitem');

    const text = document.createElement('span');
    text.className = 'dsh-fnos-file-menu-label';
    text.textContent = t(FILE_ACTION_TEXT[action]);
    item.append(text);

    item.addEventListener('click', (event) => {
      event.preventDefault();
      event.stopPropagation();
      closeFileMenu();
      void runFileAction(action, path, entry);
    });
    menu.appendChild(item);
  }

  document.body.appendChild(menu);
  positionFileMenu(menu, anchor);
  fileMenu = menu;

  const onPointerDown = (event) => {
    if (!(event.target instanceof Node)) return;
    if (menu.contains(event.target) || anchor.contains(event.target)) return;
    closeFileMenu();
  };
  const onKeyDown = (event) => {
    if (event.key === 'Escape') closeFileMenu();
  };
  document.addEventListener('pointerdown', onPointerDown, true);
  document.addEventListener('keydown', onKeyDown, true);
  window.addEventListener('scroll', closeFileMenu, true);
  window.addEventListener('resize', closeFileMenu);

  fileMenuCleanup = () => {
    document.removeEventListener('pointerdown', onPointerDown, true);
    document.removeEventListener('keydown', onKeyDown, true);
    window.removeEventListener('scroll', closeFileMenu, true);
    window.removeEventListener('resize', closeFileMenu);
  };
}

function positionFileMenu(menu, anchor) {
  const margin = 8;
  const rect = anchor.getBoundingClientRect();
  const width = menu.offsetWidth || 200;
  const height = menu.offsetHeight || 100;
  menu.style.left = `${Math.min(Math.max(margin, rect.right - width), window.innerWidth - width - margin)}px`;
  let top = rect.bottom + 6;
  if (top + height > window.innerHeight - margin) top = Math.max(margin, rect.top - height - 6);
  menu.style.top = `${top}px`;
}

function closeFileMenu() {
  if (fileMenuCleanup) {
    try { fileMenuCleanup(); } catch { /* ignore */ }
    fileMenuCleanup = null;
  }
  if (fileMenu) {
    fileMenu.remove();
    fileMenu = null;
  }
}

async function runFileAction(action, path, entry) {
  const isDirectory = entry === 'directory';
  try {
    if (action === 'open') {
      await openPathInFnos(path, isDirectory);
    } else if (action === 'quote') {
      await quotePathToComposer(path);
    } else if (action === 'copy') {
      await copyPathToClipboard(path);
    }
  } catch (error) {
    const message = error && error.message ? error.message : String(error);
    console.warn(LOG_PREFIX, 'file-tree action failed', action, message, error);
    showToast(action === 'open' ? t('在飞牛中打开失败，当前宿主可能不支持该操作。') : translateError(message));
  }
}

async function openPathInFnos(path, isDirectory) {
  const sdk = getSdk();
  try { await sdk.ready(); } catch { /* fall through to the host call */ }

  if (isDirectory || typeof sdk.openFile !== 'function') {
    await sdk.openFileManager(path);
    return;
  }
  try {
    const opened = await sdk.openFile(path);
    if (opened === false) throw new Error('openFile is not supported by this host');
  } catch (error) {
    console.warn(LOG_PREFIX, 'openFile failed; falling back to the file manager', error);
    await sdk.openFileManager(path);
  }
}

async function quotePathToComposer(path) {
  const active = findActiveComposerInput();
  if (!active) {
    showToast(t('没有找到对话输入框，无法引用。'));
    return;
  }
  await addPathsToComposer([path], active.input);
}

async function copyPathToClipboard(path) {
  const ok = await copyText(path);
  showToast(ok ? t('已复制路径') : t('复制失败，请手动复制'));
}

async function copyText(text) {
  try {
    if (navigator.clipboard && typeof navigator.clipboard.writeText === 'function') {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch { /* insecure context or permission denied: use the fallback */ }

  try {
    const area = document.createElement('textarea');
    area.value = text;
    area.setAttribute('readonly', '');
    area.style.position = 'fixed';
    area.style.top = '-1000px';
    area.style.opacity = '0';
    document.body.appendChild(area);
    area.select();
    const ok = typeof document.execCommand === 'function' && document.execCommand('copy');
    area.remove();
    return Boolean(ok);
  } catch {
    return false;
  }
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
  if (!lastWorkspaceMenu || Date.now() - lastWorkspaceMenu.at > 5 * 60 * 1000) return;

  for (const menu of document.querySelectorAll('[role="menu"]')) {
    if (!isVisible(menu)) continue;
    if (menu.querySelector('[data-dsh-fnos-check-dir]')) continue;
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

  const make = (attr, label, icon, onClick) => {
    const item = template.cloneNode(true);
    item.setAttribute(attr, '1');
    const button = item.querySelector('button[role="menuitem"]');
    if (!button) return null;

    const spans = Array.from(button.querySelectorAll('span'));
    const labelNode = spans.find((span) => /Rename|重命名|Delete|删除/i.test(span.textContent || ''))
      || spans[spans.length - 1];
    if (labelNode) labelNode.textContent = label;

    const iconNode = spans.find((span) => span !== labelNode);
    if (iconNode) iconNode.innerHTML = icon;

    button.addEventListener('click', (event) => {
      event.preventDefault();
      event.stopPropagation();
      const context = lastWorkspaceMenu;
      closeMenu(menu);
      if (!context) {
        showToast(t('无法确定工作区'));
        return;
      }
      onClick(context.id);
    });
    return item;
  };

  const open = isFnosHost()
    ? make('data-dsh-fnos-open-dir', t('打开目录'), FOLDER_OPEN_SVG, (id) => {
      void openWorkspaceDirectory(id);
    })
    : null;
  const check = make('data-dsh-fnos-check-dir', t('检查目录权限'), FOLDER_OPEN_SVG, (id) => {
    void checkWorkspaceFromMenu(id);
  });
  const reauth = make('data-dsh-fnos-reauth-dir', t('重新授权目录'), FOLDER_OPEN_SVG, (id) => {
    let popup = null;
    if (!isFnosHost()) {
      popup = window.open('', `dsh-fnos-advanced-auth-${randomId()}`, POPUP_FEATURES);
      if (popup) writePopupLoading(popup);
    }
    void reauthWorkspaceFromMenu(id, popup);
  });

  // Keep the existing order at the top: open, check, re-authorize.
  if (reauth) viewport.insertBefore(reauth, viewport.firstChild);
  if (check) viewport.insertBefore(check, viewport.firstChild);
  if (open) viewport.insertBefore(open, viewport.firstChild);
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

/** Unique workspace paths, from the live service or the host registry. */
async function knownWorkspacePaths() {
  if (workspacePaths.size === 0) {
    try {
      const response = await fetch(pluginURL('fnos-advanced/workspaces'), {
        credentials: 'same-origin',
        cache: 'no-store',
        headers: { Accept: 'application/json' },
      });
      if (response.ok) {
        const data = await response.json();
        for (const workspace of data && Array.isArray(data.workspaces) ? data.workspaces : []) {
          if (workspace && workspace.id && typeof workspace.path === 'string') {
            workspacePaths.set(String(workspace.id), workspace.path);
          }
        }
      }
    } catch { /* old host or transient failure: no disabled paths */ }
  }
  return [...new Set([...workspacePaths.values()].filter(Boolean))];
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
    showToast(t('无法获取工作区目录'));
    return;
  }

  const sdk = getSdk();
  try { await sdk.ready(); } catch { /* fall through to the bridge call */ }
  try {
    await sdk.openFileManager(path);
  } catch (error) {
    console.warn(LOG_PREFIX, 'openFileManager failed', error);
    showToast(t('打开目录失败，当前宿主可能不支持该操作。'));
  }
}

async function checkWorkspaceHealth(ids) {
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
  const data = await response.json().catch(() => ({}));
  if (!response.ok || !data.ok) {
    throw new Error(translateError(data.error) || t('检查失败：{error}', { error: `HTTP ${response.status}` }));
  }
  return Array.isArray(data.results) ? data.results : [];
}

function workspaceHealthText(result) {
  if (!result || !result.exists) return t('目录不存在');
  if (result.directory === false) return t('不是目录');
  if (!result.readable) return t('目录不可读（{error}）', { error: result.error || 'EACCES' });
  if (!result.writable) return t('目录可读但不可写');
  return t('目录可读写');
}

async function checkWorkspaceFromMenu(id) {
  showToast(t('正在检查目录权限…'));
  try {
    const [result] = await checkWorkspaceHealth([id]);
    showToast(workspaceHealthText(result));
  } catch (error) {
    showToast(t('检查失败：{error}', { error: translateError(error && error.message) }));
  }
}

async function reauthWorkspaceFromMenu(id, popup) {
  const path = await resolveWorkspacePath(id);
  if (!path) {
    closePopup(popup);
    showToast(t('无法获取工作区目录'));
    return;
  }
  try {
    await authorizeWorkspacePath(path, popup);
    const [result] = await checkWorkspaceHealth([id]);
    showToast(`${t('已重新授权')} · ${workspaceHealthText(result)}`);
  } catch (error) {
    closePopup(popup);
    showToast(t('重新授权失败：{error}', { error: translateError(error && error.message) }));
  }
}

/**
 * Re-authorize one known path. In a fnOS host the SDK opens the native
 * authorization sheet; in a browser the existing app-auth popup is used. The
 * caller must open a blank popup inside its click handler for the browser path.
 */
async function authorizeWorkspacePath(path, popup) {
  if (isFnosHost()) {
    const sdk = getSdk();
    try { await sdk.ready(); } catch { /* fall through to the host call */ }
    const result = await sdk.authorizeUserFile(path);
    if (result && result.code === 0) return true;
    throw new Error(result && result.msg ? translateError(result.msg) : t('重新授权失败：{error}', { error: 'EACCES' }));
  }

  const config = await loadConfig();
  const settings = await loadSettings();
  const target = await resolveAuthTargets(config, settings);
  let base = target.base;
  if (target.mode === 'choose') {
    const preferred = target.choices.find((choice) => choice.id === settings.authMode) || target.choices[0];
    base = preferred.base;
  }

  let windowRef = popup;
  if (!windowRef || windowRef.closed) {
    windowRef = window.open('', `dsh-fnos-advanced-auth-${randomId()}`, POPUP_FEATURES);
  }
  if (!windowRef) throw new Error(t('浏览器拦截了授权窗口，请允许弹窗后重试。'));
  writePopupLoading(windowRef);

  try {
    const auth = await requestAuth(windowRef, randomId(), config.appName || 'Harness', base, 'authorizeUserFile', { path });
    if (auth && auth.status === 'success') return true;
    if (auth && (auth.status === 'cancelled' || isCancellationText(auth.error))) {
      throw new Error(t('已取消授权'));
    }
    throw new Error(auth && auth.error ? translateError(auth.error) : t('打开选择器失败'));
  } catch (error) {
    closePopup(windowRef);
    throw error;
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

  const make = (pick, label, title) => {
    const button = document.createElement('button');
    button.type = 'button';
    button.setAttribute(BUTTON_ATTR, '1');
    button.dataset.pick = pick;
    button.className = 'dsh-fnos-advanced-button';
    button.textContent = label;
    button.title = title;
    return button;
  };

  const primary = make('directory', t('选择飞牛目录'), t('通过飞牛授权选择一个目录（会授予当前飞牛应用访问权限）'));
  const shared = make('shared', t('共享'), t('选择飞牛共享目录'));

  // Keep both before the leading footer action (New folder / 新建文件夹), so
  // neither sits between Cancel and Open.
  footer.insertBefore(shared, footer.firstChild);
  footer.insertBefore(primary, footer.firstChild);
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
  const dialog = button.closest('[role="dialog"]');
  if (!dialog) return;
  const shared = button.dataset.pick === 'shared';
  await runPickFlow(button, shared ? sharedFlow(dialog, button) : directoryFlow(dialog, button));
}

async function handlePickFiles(button) {
  const active = findActiveComposerInput();
  if (!active) {
    showToast(t('没有找到对话的文件输入框，当前 dsh 版本可能不支持。'));
    return;
  }
  const { input, region } = active;
  if (input.disabled) {
    showToast(t('当前会话暂时不能添加附件。'));
    return;
  }
  const editor = region.querySelector('[data-composer-input]');
  if (editor && editor.getAttribute('contenteditable') === 'false') {
    showToast(t('当前会话暂时不能添加附件。'));
    return;
  }
  await runPickFlow(button, composerFlow(input, button));
}

/**
 * Drive one picker attempt: native host picker first, then the fnOS web
 * authorization page. The flow object carries the per-surface differences
 * (directory picker vs conversation composer) so the authorization machinery
 * exists only once.
 *
 * @param {HTMLButtonElement} button
 * @param {any} flow
 */
async function runPickFlow(button, flow) {
  if (button.dataset.busy === '1') return;

  markBusy(button, flow);

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
    const direct = await pickPathsDirect(flow, button);
    if (direct.handled) {
      closePopup(popup);
      if (direct.paths && direct.paths.length) {
        await finishPick(button, flow, direct.paths);
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

    if (!popup || popup.closed) {
      popup = window.open('', `dsh-fnos-advanced-auth-${randomId()}`, POPUP_FEATURES);
    }
    if (!popup) throw new Error(t('浏览器拦截了授权窗口，请允许弹窗后重试。'));

    await startAuthFlow(button, appName, target.base, popup, flow);
  } catch (error) {
    closePopup(popup);
    handlePickError(button, flow, error);
  }
}

function directoryFlow(dialog, button) {
  const initialText = button.textContent || t('选择飞牛目录');
  return {
    button,
    method: 'pickUserFile',
    directory: true,
    creatable: true,
    title: t('选择工作区目录'),
    okText: t('确认授权'),
    anchor: dialog,
    status: (text, isError) => showStatus(dialog, text, isError),
    isDuplicate: async (paths) => isDuplicateWorkspacePath(paths[0], await knownWorkspacePaths()),
    onPaths: async (paths) => {
      const input = await ensurePathInput(dialog);
      if (!input) throw new Error(t('没有找到目录选择器的路径输入框'));
      fillAndEnter(input, paths[0]);
    },
    doneText: (paths) => t('已填入授权目录：{path}', { path: paths[0] }),
    setLabel: (text) => { button.textContent = text; },
    doneLabel: () => t('已填入路径'),
    restore: () => {
      button.textContent = initialText;
      button.disabled = false;
      delete button.dataset.busy;
    },
  };
}

function composerFlow(input, button) {
  return {
    button,
    method: 'pickUserFile',
    directory: false,
    title: t('选择文件'),
    okText: t('添加引用'),
    anchor: input.closest('[data-composer-card]') || input.parentElement,
    status: (text) => showToast(text),
    onPaths: (paths) => addPathsToComposer(paths, input),
    doneText: (paths) => t('已添加 {count} 个文件', { count: paths.length }),
    setLabel: () => { /* icon-only button: progress goes to the toast */ },
    doneLabel: () => '',
    restore: () => {
      button.disabled = false;
      delete button.dataset.busy;
    },
  };
}

function sharedFlow(dialog, button) {
  const initialText = button.textContent || t('共享');
  return {
    button,
    method: 'pickSharedFile',
    title: t('选择共享目录'),
    okText: t('确认授权'),
    anchor: dialog,
    status: (text, isError) => showStatus(dialog, text, isError),
    isDuplicate: async (paths) => isDuplicateWorkspacePath(paths[0], await knownWorkspacePaths()),
    onPaths: async (paths) => {
      const input = await ensurePathInput(dialog);
      if (!input) throw new Error(t('没有找到目录选择器的路径输入框'));
      fillAndEnter(input, paths[0]);
    },
    doneText: (paths) => t('已填入授权目录：{path}', { path: paths[0] }),
    setLabel: (text) => { button.textContent = text; },
    doneLabel: () => t('已填入路径'),
    restore: () => {
      button.textContent = initialText;
      button.disabled = false;
      delete button.dataset.busy;
    },
  };
}

function markBusy(button, flow) {
  button.dataset.busy = '1';
  button.disabled = true;
  flow.setLabel(t('准备授权…'));
}

async function finishPick(button, flow, paths) {
  if (typeof flow.isDuplicate === 'function' && await flow.isDuplicate(paths)) {
    flow.status(t('该目录已添加为工作区'), false);
    flow.restore();
    return;
  }
  await flow.onPaths(paths);
  flow.status(flow.doneText(paths), false);
  const label = flow.doneLabel();
  if (label) button.textContent = label;
  window.setTimeout(() => flow.restore(), 1600);
}

/**
 * In fnOS desktop / the mobile app, the official SDK can open the native
 * picker directly through the host bridge. Only fall back to URL
 * authorization when that bridge is not available.
 *
 * @returns {Promise<{handled: boolean, paths?: string[], cancelled?: boolean, error?: string}>}
 */
async function pickPathsDirect(flow, button) {
  const sdk = getSdk();
  try {
    await sdk.ready();
  } catch {
    return { handled: false };
  }

  // A normal browser tab has no host bridge; don't even try.
  if (sdk.isWeb && sdk.isStandaloneWeb) return { handled: false };
  if (sdk.isWeb && window.parent === window) return { handled: false };

  const method = flow.method || 'pickUserFile';
  const picker = sdk[method];
  if (typeof picker !== 'function') return { handled: false };

  button.dataset.direct = '1';
  flow.setLabel(t('等待选择…'));

  const params = pickerParams(flow);

  let result;
  try {
    result = await picker.call(sdk, params);
  } catch (error) {
    console.warn(LOG_PREFIX, 'in-host picker unavailable; falling back to URL auth', error);
    flow.status(t('当前宿主未提供内嵌选择器，已改用网页授权。'), false);
    return { handled: false };
  } finally {
    delete button.dataset.direct;
  }

  const paths = result && result.code === 0 && Array.isArray(result.data)
    ? result.data.filter((item) => typeof item === 'string' && item)
    : [];
  if (paths.length) return { handled: true, paths };

  return {
    handled: true,
    cancelled: isPickerCancelled(result),
    error: result && result.msg ? result.msg : t('打开选择器失败'),
  };
}

async function startAuthFlow(button, appName, base, existingPopup, flow) {
  flow.setLabel(t('等待授权…'));
  const state = randomId();

  let popup = existingPopup;
  if (!popup || popup.closed) {
    popup = window.open('', `dsh-fnos-advanced-auth-${state}`, POPUP_FEATURES);
  }
  if (!popup) throw new Error(t('浏览器拦截了授权窗口，请允许弹窗后重试。'));
  writePopupLoading(popup);

  try {
    const auth = await requestAuth(
      popup,
      state,
      appName,
      base,
      flow.method || 'pickUserFile',
      pickerParams(flow),
    );
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
      ? auth.path.filter((item) => typeof item === 'string' && item)
      : [];
    if (!paths.length) throw new Error(t('授权结果里没有路径'));

    await finishPick(button, flow, paths);
  } catch (error) {
    closePopup(popup);
    throw error;
  }
}

function handlePickError(button, flow, error) {
  const message = error && error.message ? error.message : String(error);
  console.warn(LOG_PREFIX, message, error);
  flow.status(translateError(message), true);
  flow.restore();
}

function closePopup(popup) {
  if (!popup) return;
  try { popup.close(); } catch { /* ignore */ }
}

/**
 * Turn picked fnOS paths into dsh composer attachments.
 *
 * - non-images: an empty `File` plus a `__DSH_HOST_PATHS__` mapping, so dsh
 *   inserts a zero-copy `@path` reference instead of uploading bytes;
 * - images: read through the host's one-time token route (vision needs bytes)
 *   and fall back to a path reference when the host cannot serve them.
 */
async function addPathsToComposer(paths, input) {
  const settings = await loadSettings();
  ensureHostPathBridge();

  const files = [];
  let images = 0;
  let unreadable = 0;

  for (const raw of paths) {
    const path = String(raw);
    const mime = imageMimeForPath(path);
    if (mime && settings.imageRead !== false) {
      try {
        const file = await readImageFile(path, mime);
        hostPathStore.set(file, path);
        files.push(file);
        images += 1;
        continue;
      } catch (error) {
        console.warn(LOG_PREFIX, 'image bytes unavailable; adding a path reference instead', error);
        unreadable += 1;
      }
    }
    const file = new File([], fileNameForPath(path), { type: 'application/octet-stream' });
    hostPathStore.set(file, path);
    files.push(file);
  }

  if (!files.length) throw new Error(t('没有可添加的文件'));
  dispatchComposerFiles(input, files);

  if (unreadable > 0) {
    showToast(t('{count} 张图片未能读取，已改为路径引用', { count: unreadable }));
  } else if (images > 0) {
    showToast(t('已添加 {count} 个文件（{images} 张图片会上传）', { count: files.length, images }));
  }
}

async function readImageFile(path, mime) {
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
  const info = await tokenResponse.json().catch(() => ({}));
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

/**
 * Make dsh treat mapped files as real host paths. The Desktop application
 * installs its own bridge; preserve it as the fallback so plugin files and
 * Desktop-dropped files keep working together.
 */
function ensureHostPathBridge() {
  const existing = globalThis.__DSH_HOST_PATHS__;
  if (existing && existing.__dshFnosAdvanced) {
    hostPathStore = existing.__dshFnosFiles || hostPathStore;
    return existing;
  }

  // Patch a bridge that another shell (dsh Desktop) installed *in place*:
  // dsh captures that exact object when the composer mounts, so replacing it
  // with a wrapper would leave the captured reference without our files.
  if (existing && typeof existing === 'object') {
    const original = typeof existing.pathFor === 'function' ? existing.pathFor : null;
    const store = existing.__dshFnosFiles || new WeakMap();
    try {
      existing.pathFor = function pathFor(file) {
        const own = store.get(file);
        if (own) return own;
        try {
          return original ? original.call(existing, file) : '';
        } catch {
          return '';
        }
      };
      existing.__dshFnosFiles = store;
      existing.__dshFnosAdvanced = true;
      hostPathStore = store;
      return existing;
    } catch { /* frozen object: fall back to a wrapper */ }
  }

  const store = hostPathByFile;
  const bridge = {
    __dshFnosAdvanced: true,
    __dshFnosFiles: store,
    pathFor(file) {
      const own = store.get(file);
      if (own) return own;
      try {
        return existing && typeof existing.pathFor === 'function' ? existing.pathFor(file) : '';
      } catch {
        return '';
      }
    },
  };

  try {
    globalThis.__DSH_HOST_PATHS__ = bridge;
  } catch { /* ignore a frozen global */ }
  hostPathStore = store;
  return bridge;
}

function dispatchComposerFiles(input, files) {
  const transfer = new DataTransfer();
  for (const file of files) transfer.items.add(file);

  input.files = transfer.files;
  input.dispatchEvent(new Event('change', { bubbles: true }));
}

function writePopupLoading(popup) {
  try {
    popup.document.title = t('飞牛授权');
    popup.document.body.innerHTML = `<p style="font:14px/1.6 -apple-system,BlinkMacSystemFont,'PingFang SC',sans-serif;padding:24px;color:#555">${t('正在打开飞牛授权页面…')}</p>`;
  } catch { /* about:blank may already be navigating */ }
}

/**
 * Ask fnOS for a directory authorization through the web authorization page.
 *
 * @param {Window} popup
 * @param {string} state
 * @param {string} appName
 * @param {string} base
 * @param {{directory: boolean, title: string, okText: string}} pick
 * @returns {Promise<any>}
 */
function requestAuth(popup, state, appName, base, method, params) {
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
        if (!settled) finish({ status: 'cancelled', error: t('授权窗口已关闭') });
      }, 450);
    }, 500);

    timeoutTimer = window.setTimeout(() => {
      fail(new Error(t('授权超时，请重试')));
    }, AUTH_TIMEOUT_MS);

    (async () => {
      try {
        const sdk = getSdk();
        const previousBase = sdk.getAppAuthBaseUrl;

        sdk.getAppAuthBaseUrl = async () => base;
        let url;
        try {
          url = await sdk.buildAppAuthUrl(method, {
            appName,
            ...params,
            redirectUri: pluginURL('fnos-advanced/auth-callback.html'),
            state,
          });
        } finally {
          sdk.getAppAuthBaseUrl = previousBase;
        }

        // The SDK only serializes directory/accept/sidebarGroup/title/okText.
        // Pass the newer folder-creation hint through when the host supports it.
        const targetUrl = new URL(url);
        if (params.creatable) targetUrl.searchParams.set('creatable', 'true');

        if (settled) return;
        popup.location.replace(targetUrl.toString());
      } catch (error) {
        fail(error instanceof Error ? error : new Error(String(error)));
      }
    })();
  });
}

function showAuthChooser(flow, appName, choices) {
  const anchor = flow.anchor;
  if (!anchor) {
    flow.restore();
    return;
  }
  removeChooser(anchor);
  if (getComputedStyle(anchor).position === 'static') anchor.style.position = 'relative';
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
      startAuthFlow(flow.button, appName, choice.base, null, flow).catch((error) => {
        handlePickError(flow.button, flow, error);
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

function removeChooser(anchor) {
  anchor.querySelectorAll('.dsh-fnos-advanced-chooser').forEach((node) => node.remove());
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
    imageRead: settings?.imageRead !== false,
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
  const proxy = settings.proxyBase || t('未设置');
  return t('反代 {proxy} · 内网 {lan}', { proxy, lan: settings.lanTarget });
}

function CollapsibleSection({ title, description, defaultOpen, children }) {
  const [open, setOpen] = React.useState(defaultOpen !== false);
  const hintStyle = {
    margin: 0,
    fontSize: 12,
    lineHeight: '18px',
    color: 'var(--dsw-alias-label-tertiary, #888)',
  };

  return React.createElement('section', {
    style: {
      display: 'flex',
      flexDirection: 'column',
      gap: 8,
      paddingTop: 10,
      borderTop: '1px solid var(--dsw-alias-border-l3, rgba(127,127,127,.22))',
    },
  }, [
    React.createElement('button', {
      key: 'toggle',
      type: 'button',
      'aria-expanded': open,
      'aria-label': `${title} · ${open ? t('收起') : t('展开')}`,
      onClick: () => setOpen((value) => !value),
      style: {
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        gap: 12,
        width: '100%',
        padding: '2px 0',
        border: 'none',
        background: 'transparent',
        color: 'inherit',
        font: 'inherit',
        textAlign: 'left',
        cursor: 'pointer',
      },
    }, [
      React.createElement('span', { key: 'title', style: { fontSize: 14, fontWeight: 600 } }, title),
      React.createElement('span', {
        key: 'chevron',
        'aria-hidden': true,
        style: {
          color: 'var(--dsw-alias-label-tertiary, #888)',
          fontSize: 14,
          transform: open ? 'rotate(90deg)' : 'none',
          transition: 'transform .12s ease',
        },
      }, '›'),
    ]),
    description ? React.createElement('p', { key: 'desc', style: { ...hintStyle, display: open ? 'block' : 'none' } }, description) : null,
    React.createElement('div', {
      key: 'body',
      style: { display: open ? 'block' : 'none' },
    }, children),
  ]);
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
  const [language, setLanguage] = React.useState(getLanguagePreference());

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
          imageRead: settings.imageRead !== false,
        }),
      });
      if (response.status === 404 || response.status === 405) {
        throw new Error(t('宿主半侧未提供设置接口，通常是插件升级后没有重启 dsh。请重启 dsh 后再保存设置。'));
      }
      const data = await response.json().catch(() => ({}));
      if (!response.ok || !data.ok) throw new Error(translateError(data.error) || t('保存失败（HTTP {status}）', { status: response.status }));
      const saved = normalizeClientSettings(data.settings);
      settingsCache = saved;
      settingsCachedAt = Date.now();
      setSettings(saved);
      setMessage(t('已保存'));
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
      ? t('宿主半侧版本 {host}，客户端 {client}。请重启 dsh 后再保存设置。', { host: hostVersion, client: VERSION })
      : hostState === 'missing'
        ? t('宿主半侧未提供设置接口，通常是插件升级后没有重启 dsh。请重启 dsh 后再保存设置。')
        : hostState === 'error'
          ? t('无法读取宿主设置接口，请确认 dsh 运行正常。')
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
    ? t('当前运行在飞牛桌面/宿主内：会直接调用飞牛目录选择器，不打开网页授权。')
    : gatewayAccess
      ? t('当前通过飞牛网关访问：直接使用当前同源地址授权，不会弹出入口选择。')
      : t('当前不是通过飞牛网关访问：同时配置反代与内网地址后，点击「选择飞牛目录」会让你选择授权入口。');

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
      React.createElement('div', { key: 'title', style: { fontSize: 15, fontWeight: 600 } }, t('飞牛目录授权')),
      React.createElement('p', { key: 'desc', style: hintStyle }, t('配置非网关访问时使用的飞牛授权入口；设置保存在当前 dsh 数据目录。')),
    ]),

    React.createElement(CollapsibleSection, {
      key: 'auth',
      title: t('连接与授权'),
      description: t('配置反代/内网授权入口、界面语言和图片读取。'),
      defaultOpen: true,
    }, [
      React.createElement('label', { key: 'proxy', style: labelStyle }, [
        React.createElement('span', { key: 'label' }, t('反代飞牛地址')),
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
        React.createElement('span', { key: 'hint', style: hintStyle }, t('填协议 + 域名[:端口]，不要带路径；留空表示未配置。')),
      ]),

      React.createElement('label', { key: 'lan', style: labelStyle }, [
        React.createElement('span', { key: 'label' }, t('飞牛内网地址 / 端口')),
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
        React.createElement('span', { key: 'hint', style: hintStyle }, t('只填端口时使用当前访问主机的地址；默认 5666。')),
      ]),

      React.createElement('label', { key: 'mode', style: labelStyle }, [
        React.createElement('span', { key: 'label' }, t('非网关访问时的授权入口')),
        React.createElement('select', {
          key: 'select',
          value: settings.authMode,
          onChange: update('authMode'),
          disabled,
          style: inputStyle,
        }, [
          React.createElement('option', { key: 'ask', value: 'ask' }, t('每次询问（推荐）')),
          React.createElement('option', { key: 'proxy', value: 'proxy' }, t('优先使用反代地址')),
          React.createElement('option', { key: 'lan', value: 'lan' }, t('优先使用内网地址')),
        ]),
      ]),

      React.createElement('label', { key: 'language', style: labelStyle }, [
        React.createElement('span', { key: 'label' }, t('界面语言')),
        React.createElement('select', {
          key: 'select',
          value: language,
          onChange: (event) => {
            const next = setLanguagePreference(event.target.value);
            setLanguage(next);
            setMessage('');
            setError('');
          },
          disabled,
          style: inputStyle,
        }, [
          React.createElement('option', { key: 'auto', value: 'auto' }, t('跟随浏览器')),
          React.createElement('option', { key: 'zh', value: 'zh' }, t('中文')),
          React.createElement('option', { key: 'en', value: 'en' }, 'English'),
        ]),
      ]),

      React.createElement('label', { key: 'image-read', style: { ...labelStyle, flexDirection: 'row', alignItems: 'flex-start', gap: 8 } }, [
        React.createElement('input', {
          key: 'checkbox',
          type: 'checkbox',
          checked: settings.imageRead !== false,
          onChange: (event) => {
            const checked = event.target.checked;
            setSettings((current) => ({ ...current, imageRead: checked }));
            setMessage('');
            setError('');
          },
          disabled,
          style: { marginTop: 3 },
        }),
        React.createElement('span', { key: 'text', style: { display: 'flex', flexDirection: 'column', gap: 4 } }, [
          React.createElement('span', { key: 'label' }, t('从飞牛选择图片时读取图片内容')),
          React.createElement('span', { key: 'hint', style: hintStyle }, t('图片需要字节才能被模型「看到」；关闭后图片也只生成路径引用。仅支持 PNG/JPEG/WebP/GIF，宿主只读取你选中的文件。')),
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
        }, saving ? t('保存中…') : t('保存')),
        loading ? React.createElement('span', { key: 'loading', style: hintStyle }, t('读取设置…')) : null,
        message ? React.createElement('span', { key: 'ok', style: { fontSize: 12, color: 'var(--dsw-alias-state-success-primary, #188038)' } }, message) : null,
        error ? React.createElement('span', { key: 'err', style: { fontSize: 12, color: 'var(--dsw-alias-state-error-primary, #d93025)' } }, error) : null,
      ]),
    ]),

    React.createElement(CollapsibleSection, {
      key: 'health',
      title: t('工作区目录权限'),
      description: t('检查 dsh 工作区目录的读写权限。飞牛 ACL 记录损坏或目录移动后，dsh 会读不到这些目录。'),
      defaultOpen: true,
    }, React.createElement(WorkspaceHealthSection, { disabled })),

    React.createElement(CollapsibleSection, {
      key: 'diagnostics',
      title: t('诊断'),
      description: t('检查 dsh 版本、DOM 锚点和飞牛地址连通性。'),
      defaultOpen: false,
    }, React.createElement(DiagnosticsSection, {
      settings,
      hostVersion,
      hostState,
      embedded,
      gatewayAccess,
    })),
  ]);
}

function DiagnosticsSection({ settings, hostVersion, hostState, embedded, gatewayAccess }) {
  const [proxyStatus, setProxyStatus] = React.useState(null);
  const [lanStatus, setLanStatus] = React.useState(null);
  const [copyMessage, setCopyMessage] = React.useState(null);
  const [health, setHealth] = React.useState(null);
  const [tick, setTick] = React.useState(0);
  const [appName, setAppName] = React.useState('Harness');

  const loadHealth = React.useCallback(() => {
    fetch(pluginURL('fnos-advanced/health'), { credentials: 'same-origin', cache: 'no-store' })
      .then((response) => (response.ok ? response.json() : null))
      .then((data) => setHealth(data))
      .catch(() => setHealth(null));
  }, []);

  React.useEffect(() => {
    let alive = true;
    loadConfig().then((config) => {
      if (alive && config.appName) setAppName(config.appName);
    }).catch(() => { /* keep default */ });
    loadHealth();
    return () => { alive = false; };
  }, [loadHealth]);

  const hintStyle = {
    margin: 0,
    fontSize: 12,
    lineHeight: '18px',
    color: 'var(--dsw-alias-label-tertiary, #888)',
  };
  const buttonStyle = {
    boxSizing: 'border-box',
    height: 32,
    padding: '0 12px',
    borderRadius: 8,
    border: '1px solid var(--dsw-alias-border-l2, rgba(127,127,127,.35))',
    background: 'transparent',
    color: 'var(--dsw-alias-label-primary, inherit)',
    font: 'inherit',
    fontSize: 13,
    cursor: 'pointer',
  };
  const statusStyle = (status) => ({
    fontSize: 12,
    color: status && status.ok === true
      ? 'var(--dsw-alias-state-success-primary, #188038)'
      : status && status.ok === false
        ? 'var(--dsw-alias-state-error-primary, #d93025)'
        : 'var(--dsw-alias-label-tertiary, #888)',
  });
  const statusText = (status) => {
    if (!status) return '';
    if (status.reason === 'notSet') return t('未设置');
    if (status.reason === 'timeout') return t('连接超时');
    if (status.reason === 'unreachable') return t('无法连接');
    return t('可连接（{ms}ms）', { ms: status.ms });
  };

  // Recompute anchors on every render (the sidebar/tab may have opened since).
  const composerRegion = document.querySelector('[data-conversation-region="composer"]');
  const composerAnchor = composerRegion
    ? Boolean(composerRegion.querySelector('input[type="file"][multiple]'))
    : null;
  const treeRoot = document.querySelector('[data-files-root]');
  const fileTreeAnchor = treeRoot ? Boolean(treeRoot.querySelector('[data-files-entry][data-files-path]')) : null;
  const anchorText = (value) => (
    value === true ? t('可用') : value === false ? t('不可用') : t('未检测')
  );

  const testAddress = async (kind) => {
    const setStatus = kind === 'proxy' ? setProxyStatus : setLanStatus;
    const base = kind === 'proxy'
      ? normalizeBase(settings.proxyBase || '')
      : lanBaseFromSettings(settings);
    if (!base) {
      setStatus({ reason: 'notSet' });
      return;
    }
    setStatus({ pending: true });
    const controller = typeof AbortController === 'function' ? new AbortController() : null;
    const timer = controller ? window.setTimeout(() => controller.abort(), 4000) : 0;
    const started = Date.now();
    try {
      await fetch(
        `${base}/app-auth/pick-user-file?appName=${encodeURIComponent(appName)}&directory=true`,
        {
          mode: 'no-cors',
          cache: 'no-store',
          credentials: 'omit',
          signal: controller ? controller.signal : undefined,
        },
      );
      setStatus({ ok: true, ms: Date.now() - started });
    } catch (error) {
      setStatus({ ok: false, reason: error && error.name === 'AbortError' ? 'timeout' : 'unreachable' });
    } finally {
      if (timer) window.clearTimeout(timer);
    }
  };

  const runtime = embedded ? t('飞牛桌面 / App') : gatewayAccess ? t('飞牛网关') : t('普通浏览器');
  const hostStateText = hostState === 'ok'
    ? t('正常')
    : hostState === 'missing'
      ? t('缺失（需重启 dsh）')
      : hostState === 'error' ? t('读取失败') : t('未检测');

  const copyDiagnostics = async () => {
    const lines = [
      `dsh-fnos-advanced ${VERSION}`,
      `${t('客户端')}: ${VERSION}`,
      `${t('宿主')}: ${hostVersion || '?'} (${hostStateText})`,
      `${t('运行环境')}: ${runtime}`,
      `${t('界面语言')}: ${currentLanguage()}`,
      `${t('对话输入框')}: ${anchorText(composerAnchor)}`,
      `${t('文件树')}: ${anchorText(fileTreeAnchor)}`,
      `${t('图片读取')}: ${health && health.features ? (health.features.imageRead ? t('开') : t('关')) : t('未检测')}`,
      `${t('工作区体检')}: ${health && health.features ? (health.features.workspaceHealth ? t('可用') : t('不可用')) : t('未检测')}`,
      `${t('反代飞牛地址')}: ${settings.proxyBase || t('未设置')}`,
      `${t('飞牛内网地址 / 端口')}: ${settings.lanTarget}`,
      `${t('测试反代地址')}: ${statusText(proxyStatus) || t('未检测')}`,
      `${t('测试内网地址')}: ${statusText(lanStatus) || t('未检测')}`,
    ];
    const ok = await copyText(lines.join('\n'));
    setCopyMessage(Boolean(ok));
  };

  return React.createElement('div', {
    key: `diagnostics-${tick}`,
    style: { display: 'flex', flexDirection: 'column', gap: 10, paddingTop: 4 },
  }, [
    React.createElement('div', { key: 'versions', style: { display: 'grid', gridTemplateColumns: 'auto 1fr', gap: '4px 12px', fontSize: 12 } }, [
      React.createElement('span', { key: 'h1', style: hintStyle }, t('宿主')), React.createElement('span', { key: 'v1' }, `${hostVersion || '?'} · ${hostStateText}`),
      React.createElement('span', { key: 'h2', style: hintStyle }, t('客户端')), React.createElement('span', { key: 'v2' }, VERSION),
      React.createElement('span', { key: 'h3', style: hintStyle }, t('运行环境')), React.createElement('span', { key: 'v3' }, runtime),
      React.createElement('span', { key: 'h4', style: hintStyle }, t('图片读取')), React.createElement('span', { key: 'v4' }, health && health.features ? (health.features.imageRead ? t('开') : t('关')) : t('未检测')),
      React.createElement('span', { key: 'h5', style: hintStyle }, t('对话输入框')), React.createElement('span', { key: 'v5' }, anchorText(composerAnchor)),
      React.createElement('span', { key: 'h6', style: hintStyle }, t('文件树')), React.createElement('span', { key: 'v6' }, anchorText(fileTreeAnchor)),
      React.createElement('span', { key: 'h7', style: hintStyle }, t('工作区体检')), React.createElement('span', { key: 'v7' }, health && health.features ? (health.features.workspaceHealth ? t('可用') : t('不可用')) : t('未检测')),
    ]),
    React.createElement('div', { key: 'addresses', style: { display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' } }, [
      React.createElement('button', { key: 'proxy', type: 'button', onClick: () => testAddress('proxy'), style: buttonStyle }, t('测试反代地址')),
      proxyStatus ? React.createElement('span', { key: 'proxy-status', style: statusStyle(proxyStatus) }, proxyStatus.pending ? t('测试中…') : statusText(proxyStatus)) : null,
      React.createElement('button', { key: 'lan', type: 'button', onClick: () => testAddress('lan'), style: buttonStyle }, t('测试内网地址')),
      lanStatus ? React.createElement('span', { key: 'lan-status', style: statusStyle(lanStatus) }, lanStatus.pending ? t('测试中…') : statusText(lanStatus)) : null,
    ]),
    React.createElement('div', { key: 'actions', style: { display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' } }, [
      React.createElement('button', { key: 'copy', type: 'button', onClick: copyDiagnostics, style: buttonStyle }, t('复制诊断信息')),
      React.createElement('button', {
        key: 'refresh',
        type: 'button',
        onClick: () => {
          setProxyStatus(null);
          setLanStatus(null);
          setCopyMessage(null);
          loadHealth();
          setTick((value) => value + 1);
        },
        style: buttonStyle,
      }, t('重新检测')),
      copyMessage !== null ? React.createElement('span', { key: 'copy-msg', style: hintStyle }, copyMessage ? t('诊断信息已复制') : t('复制失败')) : null,
    ]),
  ]);
}

function WorkspaceHealthSection({ disabled }) {
  const [results, setResults] = React.useState([]);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState('');

  const check = async () => {
    setLoading(true);
    setError('');
    try {
      setResults(await checkWorkspaceHealth([]));
    } catch (err) {
      setError(translateError(err && err.message));
    } finally {
      setLoading(false);
    }
  };

  const reauth = async (result) => {
    let popup = null;
    if (!isFnosHost()) {
      popup = window.open('', `dsh-fnos-advanced-auth-${randomId()}`, POPUP_FEATURES);
      if (popup) writePopupLoading(popup);
    }
    try {
      await authorizeWorkspacePath(result.path, popup);
      const [next] = await checkWorkspaceHealth([result.id]);
      if (next) {
        setResults((current) => current.map((item) => (item.id === next.id ? next : item)));
      }
      showToast(t('已重新授权'));
    } catch (err) {
      closePopup(popup);
      showToast(t('重新授权失败：{error}', { error: translateError(err && err.message) }));
    }
  };

  const buttonStyle = {
    boxSizing: 'border-box',
    height: 32,
    padding: '0 12px',
    borderRadius: 8,
    border: '1px solid var(--dsw-alias-border-l2, rgba(127,127,127,.35))',
    background: 'transparent',
    color: 'var(--dsw-alias-label-primary, inherit)',
    font: 'inherit',
    fontSize: 13,
    cursor: disabled || loading ? 'default' : 'pointer',
    opacity: disabled || loading ? 0.6 : 1,
  };
  const hintStyle = {
    margin: 0,
    fontSize: 12,
    lineHeight: '18px',
    color: 'var(--dsw-alias-label-tertiary, #888)',
  };
  const rowStyle = {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    padding: '8px 0',
    borderTop: '1px solid var(--dsw-alias-border-l3, rgba(127,127,127,.2))',
  };

  return React.createElement('div', {
    style: { display: 'flex', flexDirection: 'column', gap: 10, paddingTop: 4 },
  }, [
    React.createElement('div', { key: 'actions', style: { display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' } }, [
      React.createElement('button', {
        key: 'check',
        type: 'button',
        onClick: check,
        disabled: disabled || loading,
        style: buttonStyle,
      }, loading ? t('检查中…') : t('检查全部工作区')),
      results.length ? React.createElement('span', { key: 'count', style: hintStyle }, t('共 {count} 个', { count: results.length })) : null,
      error ? React.createElement('span', { key: 'error', style: { ...hintStyle, color: 'var(--dsw-alias-state-error-primary, #d93025)' } }, error) : null,
      (results.length || error) ? React.createElement('button', {
        key: 'clear',
        type: 'button',
        onClick: () => {
          setResults([]);
          setError('');
        },
        disabled: loading,
        style: buttonStyle,
      }, t('清除结果')) : null,
    ]),
    results.length ? React.createElement('div', { key: 'list' }, results.map((result) => {
      const healthy = result.exists && result.readable && result.writable;
      const status = workspaceHealthText(result);
      return React.createElement('div', { key: result.id, style: rowStyle }, [
        React.createElement('div', { key: 'info', style: { minWidth: 0, flex: 1, display: 'flex', flexDirection: 'column', gap: 2 } }, [
          React.createElement('span', { key: 'name', style: { fontSize: 13, fontWeight: 500, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' } }, result.title || result.id),
          React.createElement('span', { key: 'path', style: { ...hintStyle, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }, title: result.path }, result.path),
        ]),
        React.createElement('span', {
          key: 'status',
          style: {
            flex: 'none',
            fontSize: 12,
            color: healthy
              ? 'var(--dsw-alias-state-success-primary, #188038)'
              : 'var(--dsw-alias-state-error-primary, #d93025)',
          },
        }, status),
        React.createElement('button', {
          key: 'reauth',
          type: 'button',
          onClick: () => reauth(result),
          disabled: disabled,
          style: buttonStyle,
        }, t('重新授权')),
      ]);
    })) : null,
  ]);
}

function validateProxyBase(value) {
  const text = String(value || '').trim();
  if (!text) return '';
  const url = new URL(text);
  if (url.protocol !== 'http:' && url.protocol !== 'https:') {
    throw new Error(t('反代地址只支持 http/https'));
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
    .dsh-fnos-file-row > button {
      padding-right: 34px;
    }
    .dsh-fnos-file-action {
      position: absolute;
      top: 0;
      right: 6px;
      box-sizing: border-box;
      display: grid;
      place-items: center;
      width: 24px;
      height: 28px;
      padding: 0;
      border: none;
      border-radius: 6px;
      background: transparent;
      color: var(--dsw-alias-label-tertiary, #888);
      cursor: pointer;
      opacity: 0;
      pointer-events: none;
      transition: opacity .12s ease, background-color .12s ease;
    }
    .dsh-fnos-file-row:hover > .dsh-fnos-file-action,
    .dsh-fnos-file-row:focus-within > .dsh-fnos-file-action,
    .dsh-fnos-file-row > .dsh-fnos-file-action:focus-visible {
      opacity: 1;
      pointer-events: auto;
    }
    .dsh-fnos-file-action:hover {
      background: transparent;
      color: var(--dsw-alias-label-primary, inherit);
    }
    .dsh-fnos-file-action:focus-visible {
      outline: var(--dsw-focus-ring-width, 2px) solid var(--dsw-alias-state-business-primary, #4f7cff);
      outline-offset: 1px;
    }
    @media (hover: none) {
      .dsh-fnos-file-row > .dsh-fnos-file-action {
        opacity: 1;
        pointer-events: auto;
      }
    }
    .dsh-fnos-file-menu {
      position: fixed;
      z-index: 10000;
      box-sizing: border-box;
      width: 200px;
      padding: 6px;
      border: 1px solid var(--dsw-alias-border-l2, rgba(127, 127, 127, .32));
      border-radius: 10px;
      background: var(--dsw-alias-bg-layer-1, #fff);
      color: var(--dsw-alias-label-primary, inherit);
      box-shadow: 0 12px 40px rgba(0, 0, 0, .18);
    }
    .dsh-fnos-file-menu-title {
      margin-bottom: 4px;
      padding: 2px 8px 6px;
      font-size: 13px;
      font-weight: 600;
      line-height: 18px;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .dsh-fnos-file-menu-item {
      box-sizing: border-box;
      display: flex;
      align-items: center;
      width: 100%;
      padding: 7px 8px;
      border: none;
      border-radius: 7px;
      background: transparent;
      color: inherit;
      font: inherit;
      text-align: left;
      cursor: pointer;
    }
    .dsh-fnos-file-menu-item:hover {
      background: var(--dsw-alias-interactive-bg-hover, rgba(127, 127, 127, .1));
    }
    .dsh-fnos-file-menu-label {
      font-size: 13px;
      font-weight: 500;
      line-height: 18px;
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

// Install the path bridge at module-evaluation time. dsh reads
// `globalThis.__DSH_HOST_PATHS__` once when its composer slot is injected, so
// this must not wait for `apply()` or a click.
if (typeof window !== 'undefined') {
  try { ensureHostPathBridge(); } catch { /* ignore a frozen global */ }
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
    findComposerFileInput,
    findActiveComposerInput,
    addPathsToComposer,
    ensureHostPathBridge,
    getSdk,
    pickPathsDirect,
    isDuplicateWorkspacePath,
  };
}
