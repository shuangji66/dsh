/**
 * Workspace and Session context menus.
 *
 * dsh renders a workspace row's "more" menu with localized labels ("Rename",
 * "Delete workspace", …). The plugin's workspace rows are clones of the first
 * existing item so they inherit its styling without CSS-module class names.
 *
 * Both menus also carry dsh's native "Show in folder" row. dsh disables it on
 * this host because its `open-in-app` plugin found no desktop file manager;
 * inside fnOS the fnOS bridge can open the folder instead, so the existing row
 * is enabled and re-wired in place (rather than adding a second, duplicate
 * item). The menu's owner is read from the `aria-controls` bridge dsh's Menu
 * primitive publishes, which survives UI language changes.
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
  REVEAL_ITEM_ATTR,
} from './constants.ts';
import { closePopup, isVisible, randomId, showToast } from './dom.ts';
import { isFnosAppEmbedded } from './env.ts';
import { t, translateError } from './i18n.ts';
import { contextFromRowKey, type ContextMenuTarget } from './menu-target.ts';
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
import { sessionCwd } from './slot-actions.tsx';

interface WorkspaceMenuContext {
  id: string;
  at: number;
}

/** Only the most recently opened workspace menu is augmented (pre-aria fallback). */
let lastWorkspaceMenu: WorkspaceMenuContext | null = null;

const MENU_TTL_MS = 5 * 60 * 1000;

/** dsh's folder-open icon identifies the native reveal row across languages. */
const REVEAL_ICON = '#dsw-icon-folder-open-outline-regular';
const REVEAL_LABEL_RE = /^(?:Show in folder|Reveal in folder|在文件夹显示|在文件夹中显示|在資料夾顯示|在資料夾中顯示|Im Ordner anzeigen|Afficher dans le dossier|Mostrar en la carpeta|フォルダーで表示|Открыть папку|Abrir na pasta)$/i;

/** Bound native click handlers, so unload can restore the row's disabled state. */
const revealHandlers = new WeakMap<HTMLButtonElement, EventListener>();

/** Remember which workspace the row click belongs to (older dsh without aria-controls). */
export function noteWorkspaceRowClick(row: Element): void {
  const key = row.getAttribute('data-row-key') || '';
  const target = contextFromRowKey(key);
  if (target?.kind === 'workspace') lastWorkspaceMenu = { id: target.id, at: Date.now() };
}

/** Augment the visible workspace / session menus. */
export function scanContextMenus(): void {
  for (const menu of document.querySelectorAll('[role="menu"]')) {
    if (!isVisible(menu)) continue;

    const target = findMenuTarget(menu);
    const isWorkspace = target?.kind === 'workspace' || isWorkspaceActionsMenu(menu);
    let workspaceId = '';
    if (isWorkspace) {
      workspaceId = target?.kind === 'workspace' ? target.id : freshWorkspaceId();
      if (workspaceId && !menu.querySelector(`[${CHECK_DIR_ATTR}]`)) {
        injectOpenDirItem(menu, workspaceId);
      }
    }

    // Older dsh has no aria-controls bridge; the clicked workspace row still
    // identifies the menu, and its reveal row must be adapted the same way.
    const revealTarget = target ?? (workspaceId ? { kind: 'workspace' as const, id: workspaceId } : null);
    adaptRevealItem(menu, revealTarget);
  }
}

/** The workspace id from the pre-aria click tracking, while it is still fresh. */
function freshWorkspaceId(): string {
  if (!lastWorkspaceMenu || Date.now() - lastWorkspaceMenu.at > MENU_TTL_MS) return '';
  return lastWorkspaceMenu.id;
}

/**
 * The row a visible contextual menu belongs to.
 *
 * dsh's Menu primitive sets `aria-controls` on the element that opened it (the
 * "..." button or the row itself); that element sits inside the row that owns
 * `data-row-key`.
 */
function findMenuTarget(menu: Element): ContextMenuTarget | null {
  const menuId = menu.id;
  if (!menuId) return null;
  for (const trigger of document.querySelectorAll<HTMLElement>('[aria-controls]')) {
    const controls = (trigger.getAttribute('aria-controls') || '').split(/\s+/);
    if (!controls.includes(menuId)) continue;
    const row = trigger.closest<HTMLElement>('[data-row-key]');
    const target = contextFromRowKey(row?.getAttribute('data-row-key'));
    if (target) return target;
  }
  return null;
}

function isWorkspaceActionsMenu(menu: Element): boolean {
  const text = (menu.textContent || '').replace(/\s+/g, ' ');
  return /Delete workspace|删除工作区|删除工作空间|删除工作區/i.test(text);
}

/** dsh's native "Show in folder" row, identified by its stable icon. */
function findRevealItem(menu: Element): HTMLButtonElement | null {
  for (const button of menu.querySelectorAll<HTMLButtonElement>('button[role="menuitem"]')) {
    if (button.querySelector(`use[href="${REVEAL_ICON}"]`)) return button;
    // Future icon names keep the same family suffix; the row is unique.
    if (button.querySelector('use[href*="folder-open"]')) return button;
    if (REVEAL_LABEL_RE.test((button.textContent || '').trim())) return button;
  }
  return null;
}

/**
 * Enable dsh's disabled reveal row and open the folder through the fnOS bridge.
 *
 * The native row carries no run function when disabled, so React's select path
 * cannot be reused: a native listener stops the click before React sees it.
 * The row is only touched while fnOS reports a host bridge; a session row also
 * needs a known cwd, otherwise it stays disabled.
 */
function adaptRevealItem(menu: Element, target: ContextMenuTarget | null): void {
  if (!target || !isFnosHost()) return;
  const button = findRevealItem(menu);
  if (!button) return;

  const bound = button.dataset.dshFnosRevealBound === '1';
  const wasDisabled = button.disabled || button.getAttribute('aria-disabled') === 'true';
  if (!wasDisabled && !bound) return;
  if (target.kind === 'session' && !sessionCwd(target.id)) return;

  // React may re-apply disabled on a re-render; keep the row enabled.
  if (button.disabled) button.disabled = false;
  if (button.getAttribute('aria-disabled') === 'true') button.removeAttribute('aria-disabled');
  button.setAttribute(REVEAL_ITEM_ATTR, '1');
  button.dataset.dshFnosRevealKind = target.kind;
  button.dataset.dshFnosRevealId = target.id;

  if (bound) return;
  button.dataset.dshFnosRevealBound = '1';
  const handler: EventListener = (event) => {
    event.preventDefault();
    event.stopPropagation();
    const kind = button.dataset.dshFnosRevealKind;
    const id = button.dataset.dshFnosRevealId || '';
    closeMenu();
    if (kind === 'workspace') void openWorkspaceDirectory(id);
    else if (kind === 'session') void openSessionDirectory(id);
  };
  revealHandlers.set(button, handler);
  button.addEventListener('click', handler);
}

/** Restore every native reveal row the plugin touched (plugin unload). */
export function resetRevealItems(): void {
  for (const button of document.querySelectorAll<HTMLButtonElement>(`[${REVEAL_ITEM_ATTR}]`)) {
    const handler = revealHandlers.get(button);
    if (handler) button.removeEventListener('click', handler);
    revealHandlers.delete(button);
    button.disabled = true;
    button.removeAttribute(REVEAL_ITEM_ATTR);
    delete button.dataset.dshFnosRevealBound;
    delete button.dataset.dshFnosRevealKind;
    delete button.dataset.dshFnosRevealId;
  }
}

function injectOpenDirItem(menu: Element, workspaceId: string): void {
  const viewport = menu.querySelector('[role="presentation"]') || menu;
  const templateButton = menu.querySelector('button[role="menuitem"]');
  const template = templateButton && templateButton.closest('div');
  if (!template) return;

  const make = (
    attr: string,
    label: string,
    icon: string,
    onClick: () => void,
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
      closeMenu();
      onClick();
    });
    return item;
  };

  // When dsh already renders its reveal row, the plugin enables and reuses it
  // instead of adding a second "open folder" item.
  const open = isFnosHost() && !findRevealItem(menu)
    ? make(OPEN_DIR_ATTR, t('打开目录'), FOLDER_OPEN_SVG, () => {
      void openWorkspaceDirectory(workspaceId);
    })
    : null;
  const check = make(CHECK_DIR_ATTR, t('检查目录权限'), FOLDER_OPEN_SVG, () => {
    void checkWorkspaceFromMenu(workspaceId);
  });
  const reauth = make(REAUTH_DIR_ATTR, t('重新授权目录'), FOLDER_OPEN_SVG, () => {
    // The app's embedded console dsh opens an iframe inside
    // authorizeWorkspacePath; do not pre-open a popup there.
    let popup: Window | null = null;
    if (!isFnosHost() && !isFnosAppEmbedded()) popup = openAuthPopup();
    void reauthWorkspaceFromMenu(workspaceId, popup);
  });

  // Keep the existing order at the top: open, check, re-authorize.
  if (reauth) viewport.insertBefore(reauth, viewport.firstChild);
  if (check) viewport.insertBefore(check, viewport.firstChild);
  if (open) viewport.insertBefore(open, viewport.firstChild);
}

/**
 * Close dsh's context menu through the dismissal paths dsh itself listens for.
 *
 * The menu belongs to dsh's React overlay. Removing its node by hand is what
 * used to break this: React still owns that node, so its later unmount threw
 * `NotFoundError: Failed to execute 'removeChild' on 'Node'` inside the
 * `shell.overlay` slot, which tore the whole context-menu overlay down — the
 * workspace menu could not be opened again until the page reloaded (dsh
 * 0.2.2-alpha.3). Dispatch Escape and one outside pointerdown instead; both
 * are handled by dsh's Menu, so React unmounts the menu normally.
 */
function closeMenu(): void {
  try {
    const options: KeyboardEventInit & { keyCode: number; which: number } = {
      key: 'Escape',
      code: 'Escape',
      keyCode: 27,
      which: 27,
      bubbles: true,
      cancelable: true,
    };
    document.dispatchEvent(new KeyboardEvent('keydown', options));
    window.dispatchEvent(new KeyboardEvent('keydown', options));
  } catch { /* ignore */ }
  try {
    const outside = typeof PointerEvent === 'function'
      ? new PointerEvent('pointerdown', { bubbles: true, cancelable: true })
      : new MouseEvent('pointerdown', { bubbles: true, cancelable: true });
    (document.body || document.documentElement).dispatchEvent(outside);
  } catch { /* React also closes the menu on the next real outside click */ }
}

/** Hand one absolute path to the fnOS file manager (host environments only). */
async function openPathInFileManager(path: string): Promise<void> {
  const sdk = getSdk();
  try { await sdk.ready(); } catch { /* fall through to the bridge call */ }
  try {
    await sdk.openFileManager(path);
  } catch (error) {
    console.warn(LOG_PREFIX, 'openFileManager failed', error);
    showToast(t('打开目录失败，当前宿主可能不支持该操作。'));
  }
}

/** Open one workspace in the fnOS file manager (host environments only). */
export async function openWorkspaceDirectory(id: string): Promise<void> {
  const path = await resolveWorkspacePath(id);
  if (!path) {
    showToast(t('无法获取工作区目录'));
    return;
  }
  await openPathInFileManager(path);
}

/** Open the current folder of one session in the fnOS file manager. */
export async function openSessionDirectory(id: string): Promise<void> {
  const path = sessionCwd(id);
  if (!path) {
    showToast(t('无法获取会话目录'));
    return;
  }
  await openPathInFileManager(path);
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
