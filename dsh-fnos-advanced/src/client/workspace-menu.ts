/**
 * Workspace context menu: open / check / re-authorize one workspace.
 *
 * dsh renders a workspace row's "more" menu with localized labels ("Rename",
 * "Delete workspace", …). The menu is recognized by the delete row it always
 * has, and the plugin's rows are clones of the first existing item so they
 * inherit its styling without relying on CSS-module class names.
 */
import {
  buildAuthUrl,
  openAuthFrame,
  openAuthPopup,
  requestAuth,
  windowSurface,
  type AuthFrame,
  type AuthSurface,
} from './auth.ts';
import { isAuthCancelled } from './cancel.ts';
import {
  CHECK_DIR_ATTR,
  FOLDER_OPEN_SVG,
  LOG_PREFIX,
  OPEN_DIR_ATTR,
  REAUTH_DIR_ATTR,
} from './constants.ts';
import { closePopup, isVisible, randomId, showToast } from './dom.ts';
import { isFnosAppEmbedded } from './env.ts';
import { t, translateError } from './i18n.ts';
import {
  checkWorkspaceHealth,
  loadConfig,
  loadSettings,
  resolveAuthTargets,
  resolveAuthBase,
  resolveWorkspacePath,
  workspaceHealthText,
} from './plugin-api.ts';
import { getSdk, isFnosHost } from './sdk.ts';

interface MenuContext {
  id: string;
  at: number;
}

/** Only the most recently opened workspace menu is augmented. */
let lastWorkspaceMenu: MenuContext | null = null;

const MENU_TTL_MS = 5 * 60 * 1000;

/** Remember which workspace the row click belongs to. */
export function noteWorkspaceRowClick(row: Element): void {
  const key = row.getAttribute('data-row-key') || '';
  const id = key.startsWith('workspace:') ? key.slice('workspace:'.length) : '';
  if (id) lastWorkspaceMenu = { id, at: Date.now() };
}

/** Add the plugin's rows to a just-opened workspace menu. */
export function scanWorkspaceMenus(): void {
  if (!lastWorkspaceMenu || Date.now() - lastWorkspaceMenu.at > MENU_TTL_MS) return;

  for (const menu of document.querySelectorAll('[role="menu"]')) {
    if (!isVisible(menu)) continue;
    if (menu.querySelector(`[${CHECK_DIR_ATTR}]`)) continue;
    if (!isWorkspaceActionsMenu(menu)) continue;
    injectOpenDirItem(menu);
  }
}

function isWorkspaceActionsMenu(menu: Element): boolean {
  const text = (menu.textContent || '').replace(/\s+/g, ' ');
  return /Delete workspace|删除工作区|删除工作空间|删除工作區/i.test(text);
}

function injectOpenDirItem(menu: Element): void {
  const viewport = menu.querySelector('[role="presentation"]') || menu;
  const templateButton = menu.querySelector('button[role="menuitem"]');
  const template = templateButton && templateButton.closest('div');
  if (!template) return;

  const make = (
    attr: string,
    label: string,
    icon: string,
    onClick: (id: string) => void,
  ): HTMLElement | null => {
    const item = template.cloneNode(true) as HTMLElement;
    item.setAttribute(attr, '1');
    const button = item.querySelector<HTMLElement>('button[role="menuitem"]');
    if (!button) return null;

    const spans = Array.from(button.querySelectorAll<HTMLSpanElement>('span'));
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
    ? make(OPEN_DIR_ATTR, t('打开目录'), FOLDER_OPEN_SVG, (id) => {
      void openWorkspaceDirectory(id);
    })
    : null;
  const check = make(CHECK_DIR_ATTR, t('检查目录权限'), FOLDER_OPEN_SVG, (id) => {
    void checkWorkspaceFromMenu(id);
  });
  const reauth = make(REAUTH_DIR_ATTR, t('重新授权目录'), FOLDER_OPEN_SVG, (id) => {
    // The app's embedded console dsh opens an iframe inside
    // authorizeWorkspacePath; do not pre-open a popup there.
    let popup: Window | null = null;
    if (!isFnosHost() && !isFnosAppEmbedded()) popup = openAuthPopup();
    void reauthWorkspaceFromMenu(id, popup);
  });

  // Keep the existing order at the top: open, check, re-authorize.
  if (reauth) viewport.insertBefore(reauth, viewport.firstChild);
  if (check) viewport.insertBefore(check, viewport.firstChild);
  if (open) viewport.insertBefore(open, viewport.firstChild);
}

function closeMenu(menu: Element): void {
  try {
    const options: KeyboardEventInit & { keyCode: number; which: number } = {
      key: 'Escape',
      code: 'Escape',
      keyCode: 27,
      which: 27,
      bubbles: true,
    };
    document.dispatchEvent(new KeyboardEvent('keydown', options));
    window.dispatchEvent(new KeyboardEvent('keydown', options));
  } catch { /* ignore */ }
  window.setTimeout(() => {
    if (menu.isConnected) menu.remove();
  }, 0);
}

/** Open one workspace in the fnOS file manager (host environments only). */
export async function openWorkspaceDirectory(id: string): Promise<void> {
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

/** Check one workspace from the menu and report the outcome as a toast. */
export async function checkWorkspaceFromMenu(id: string): Promise<void> {
  showToast(t('正在检查目录权限…'));
  try {
    const [result] = await checkWorkspaceHealth([id]);
    showToast(workspaceHealthText(result));
  } catch (error) {
    showToast(t('检查失败：{error}', {
      error: translateError(error instanceof Error ? error.message : error),
    }));
  }
}

/** Re-authorize one workspace from the menu, then re-check its access. */
export async function reauthWorkspaceFromMenu(id: string, popup: Window | null): Promise<void> {
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
    showToast(t('重新授权失败：{error}', {
      error: translateError(error instanceof Error ? error.message : error),
    }));
  }
}

/**
 * Re-authorize one known path. A real fnOS host opens the native authorization
 * sheet; a browser (including the console's embedded dsh, which has no usable
 * host bridge) uses the app-auth web page. The caller must open a blank popup
 * inside its click handler for the browser/popup path; the fnOS App's embedded
 * console dsh uses an iframe instead.
 */
export async function authorizeWorkspacePath(path: string, popup: Window | null): Promise<boolean> {
  if (isFnosHost()) {
    const sdk = getSdk();
    try { await sdk.ready(); } catch { /* fall through to the host call */ }
    const result = await sdk.authorizeUserFile(path);
    if (result && result.code === 0) return true;
    throw new Error(result && result.msg
      ? translateError(result.msg)
      : t('重新授权失败：{error}', { error: 'EACCES' }));
  }

  const config = await loadConfig();
  const settings = await loadSettings();
  const target = await resolveAuthTargets(config, settings);

  let base: string;
  if (target.mode === 'choose') {
    const preferred = target.choices.find((choice) => choice.id === settings.authMode) || target.choices[0];
    base = preferred ? preferred.base : resolveAuthBase();
  } else {
    base = target.base;
  }

  let frame: AuthFrame | null = null;
  let windowRef = popup;
  let surface: AuthSurface;
  if (isFnosAppEmbedded()) {
    frame = openAuthFrame();
    if (!frame) throw new Error(t('无法打开授权页面，请重试。'));
    surface = frame.surface;
  } else {
    if (!windowRef || windowRef.closed) windowRef = openAuthPopup();
    if (!windowRef) throw new Error(t('浏览器拦截了授权窗口，请允许弹窗后重试。'));
    surface = windowSurface(windowRef);
  }

  try {
    const state = randomId();
    const auth = await requestAuth({
      surface,
      state,
      buildUrl: () => buildAuthUrl(base, state, 'authorizeUserFile', { path }, config.appName || 'Harness'),
    });
    if (auth && auth.status === 'success') return true;
    // 平台取消授权回的是 `status=cancel`（有时带 `error=access_denied`），
    // 与弹窗/iframe 被关掉时合成的 `cancelled` 一样都该报「已取消」而不是失败。
    if (isAuthCancelled(auth)) {
      throw new Error(t('已取消授权'));
    }
    throw new Error(auth && auth.error ? translateError(auth.error) : t('打开选择器失败'));
  } catch (error) {
    closePopup(windowRef);
    throw error;
  } finally {
    frame?.close();
  }
}
