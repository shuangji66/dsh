/**
 * Workspace, Session and native file context menus.
 *
 * dsh renders a workspace row's "more" menu with localized labels ("Rename",
 * "Delete workspace", …). The plugin's workspace rows are clones of the first
 * existing item so they inherit its styling without CSS-module class names.
 *
 * Both menu families also carry dsh's native "Show in folder" row. dsh disables
 * it on this host because its `open-in-app` plugin found no desktop file
 * manager; inside fnOS the fnOS bridge can open the folder instead, so the
 * existing row is enabled and re-wired in place (rather than adding a second,
 * duplicate item). For dsh 0.2.2's native file menu the same row is matched via
 * the `contextMenus.active` target and opened with `openFileManager` — always
 * with the **containing folder**, since that call navigates to a directory.
 *
 * The file menu's "Open in default application" row gets the same treatment:
 * dsh greys it out because the NAS has no desktop application associations,
 * while fnOS's own `openFile` is exactly "open with the default app". Once the
 * native row is live the plugin's own "在飞牛中打开" row is hidden, so the menu
 * carries one action instead of two identical ones.
 *
 * Workspace/session ownership is read from the `aria-controls` bridge dsh's
 * Menu primitive publishes; the native file target comes from the context-menu
 * service, which survives UI language changes for both.
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
  HIDDEN_ITEM_ATTR,
  LOG_PREFIX,
  OPEN_DIR_ATTR,
  OPEN_ITEM_ATTR,
  REAUTH_DIR_ATTR,
  REVEAL_ITEM_ATTR,
} from './constants.ts';
import { closePopup, copyText, isVisible, randomId, showToast } from './dom.ts';
import { isFnosAppEmbedded } from './env.ts';
import { folderOfPath } from './files.ts';
import { t, translateError } from './i18n.ts';
import {
  absoluteTargetPath,
  contextFromRowKey,
  type ActivePathTarget,
  type ContextMenuTarget,
} from './menu-target.ts';
import { activeNativeTarget } from './native-menu.ts';
import { openPathInFnos } from './path-menu.ts';
import {
  COPY_ICON,
  COPY_LABEL_RE,
  DEFAULT_APP_LABEL_RE,
  REVEAL_ICON,
  REVEAL_LABEL_RE,
  isLiveMenuPhase,
} from './native-menu-contract.ts';
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
import { sessionCwd } from './session-cwd.ts';

interface WorkspaceMenuContext {
  id: string;
  at: number;
}

/** Only the most recently opened workspace menu is augmented (pre-aria fallback). */
let lastWorkspaceMenu: WorkspaceMenuContext | null = null;

const MENU_TTL_MS = 5 * 60 * 1000;

/** Bound native click handlers, so unload can restore the row's disabled state. */
const revealHandlers = new WeakMap<HTMLButtonElement, EventListener>();

/** Remember which workspace the row click belongs to (older dsh without aria-controls). */
export function noteWorkspaceRowClick(row: Element): void {
  const key = row.getAttribute('data-row-key') || '';
  const target = contextFromRowKey(key);
  if (target?.kind === 'workspace') lastWorkspaceMenu = { id: target.id, at: Date.now() };
}

/** One visible menu's captured target: workspace/session or file/directory. */
type RevealTarget = ContextMenuTarget | ActivePathTarget;

/** Augment the visible workspace / session / native file menus. */
export function scanContextMenus(): void {
  // The native popup carries `data-context-menu` on its `role="menu"` element
  // while it is open (dsh 0.2.2); older menus only have the role.
  for (const menu of document.querySelectorAll('[role="menu"], [data-context-menu]')) {
    if (!isVisible(menu)) continue;
    // dsh 0.2.2 phases are `enter` / `dismiss` / `replace` (see the menu CSS).
    // A dismissing or replacing menu is fading out with `pointer-events: none`
    // and still carries the outgoing items, so only the live phase is augmented.
    if (!isLiveMenuPhase(menu.getAttribute('data-menu-phase'))) continue;

    // The native popup (0.2.2+) publishes its captured target through the
    // contextMenus service; workspace/session menus still use aria-controls.
    const captured = menu.hasAttribute('data-context-menu') ? activeNativeTarget() : null;
    const native = captured
      ? { ...captured, path: absoluteTargetPath(captured, sessionCwd) }
      : null;
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
    const revealTarget: RevealTarget | null = native
      ?? target
      ?? (workspaceId ? { kind: 'workspace' as const, id: workspaceId } : null);
    adaptRevealItem(menu, revealTarget);
    if (captured) {
      adaptCopyItem(menu);
      // The default-app row only exists in dsh's native file menu.
      adaptOpenItem(menu, native);
    }
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

/** Whether one menu row carries dsh's icon (sprite `<use>` or `data-icon`). */
function hasIcon(button: Element, icon: string): boolean {
  return Boolean(
    button.querySelector(`use[href="#${icon}"]`)
    || button.querySelector(`svg[data-icon="${icon}"]`)
    || button.querySelector(`[data-icon="${icon}"]`),
  );
}

/** dsh's native "Show in folder" row, identified by its stable icon. */
function findRevealItem(menu: Element): HTMLButtonElement | null {
  for (const button of menu.querySelectorAll<HTMLButtonElement>('button[role="menuitem"]')) {
    if (hasIcon(button, REVEAL_ICON)) return button;
    // Future icon names keep the same family suffix; the row is unique.
    if (button.querySelector('use[href*="folder-open"]')) return button;
    if (REVEAL_LABEL_RE.test((button.textContent || '').trim())) return button;
  }
  return null;
}

const copyHandlers = new WeakMap<HTMLButtonElement, EventListener>();

function findCopyItem(menu: Element): HTMLButtonElement | null {
  for (const button of menu.querySelectorAll<HTMLButtonElement>('button[role="menuitem"]')) {
    if (hasIcon(button, COPY_ICON)) return button;
    if (COPY_LABEL_RE.test((button.textContent || '').trim())) return button;
  }
  return null;
}

/** Absolute path of the captured native target, or an empty string. */
function copyPathOf(): string {
  const captured = activeNativeTarget();
  if (!captured) return '';
  return absoluteTargetPath(captured, sessionCwd);
}

/**
 * Route dsh's native "Copy path" through the plugin's clipboard helper.
 *
 * `open-in-app` writes with `navigator.clipboard` directly; the console is
 * often served over plain HTTP where that API is missing. The plugin already
 * has an execCommand fallback, so the shared menu keeps working there. The
 * copied text is dsh's own absolute path (dsh resolves it through its API;
 * the plugin resolves workspace-relative targets against the Session cwd).
 */
function adaptCopyItem(menu: Element): void {
  const button = findCopyItem(menu);
  if (!button) return;
  const path = copyPathOf();
  if (!path) return;
  button.dataset.dshFnosCopyPath = path;
  if (copyHandlers.has(button)) return;
  const handler: EventListener = (event) => {
    event.preventDefault();
    event.stopPropagation();
    // Read the live snapshot first: React reuses the same button node for the
    // same action id in the next menu, so the dataset can lag one scan behind.
    const current = copyPathOf() || button.dataset.dshFnosCopyPath || '';
    if (!current) return;
    closeMenu();
    void copyText(current).then((ok) => {
      showToast(ok ? t('已复制路径') : t('复制失败，请手动复制'));
    });
  };
  copyHandlers.set(button, handler);
  button.addEventListener('click', handler);
  button.dataset.dshFnosCopyBound = '1';
}

/**
 * Enable dsh's disabled reveal row and open the folder through the fnOS bridge.
 *
 * The native row carries no run function when disabled, so React's select path
 * cannot be reused: a native listener stops the click before React sees it.
 * The row is only touched while fnOS reports a host bridge; a session row also
 * needs a known cwd, otherwise it stays disabled.
 */
function adaptRevealItem(menu: Element, target: RevealTarget | null): void {
  if (!target || !isFnosHost()) return;
  const button = findRevealItem(menu);
  if (!button) return;

  const bound = button.dataset.dshFnosRevealBound === '1';
  const wasDisabled = button.disabled || button.getAttribute('aria-disabled') === 'true';
  if (!wasDisabled && !bound) return;
  if (target.kind === 'session' && !sessionCwd(target.id)) return;

  const value = target.kind === 'workspace' || target.kind === 'session' ? target.id : target.path;
  if (!value) return;

  // React may re-apply disabled on a re-render; keep the row enabled.
  if (button.disabled) button.disabled = false;
  if (button.getAttribute('aria-disabled') === 'true') {
    button.removeAttribute('aria-disabled');
    // Remember that the row was disabled so unload can restore it verbatim.
    button.dataset.dshFnosRevealAria = '1';
  }
  button.setAttribute(REVEAL_ITEM_ATTR, '1');
  button.dataset.dshFnosRevealKind = target.kind;
  button.dataset.dshFnosRevealId = value;

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
    // 「显示所在文件夹」= 打开文件管理器并定位到**目录**：`openFileManager` 只认目录，
    // 把文件路径原样递过去会落到错误的位置。
    else if (kind === 'file') void openPathInFileManager(folderOfPath(id));
    else if (kind === 'directory') void openPathInFileManager(id);
  };
  revealHandlers.set(button, handler);
  button.addEventListener('click', handler);
}

/**
 * Take over dsh's disabled "Open in default application" row.
 *
 * On the NAS dsh never resolves a desktop application, so the row ships without
 * a `run` and disabled — but fnOS's own `openFile` *is* "open with the default
 * app", which is why the plugin used to add a separate "在飞牛中打开" row for
 * files. Enabling dsh's row instead keeps one action with dsh's own label, and
 * the plugin's duplicate row is hidden by {@link hideRedundantOpenItem}.
 *
 * Only the disabled `menu.defaultApp` row is touched: an enabled row carries a
 * resolved application (`open.title`, "Open with …") and belongs to dsh.
 */
function adaptOpenItem(menu: Element, target: RevealTarget | null): void {
  if (!target || target.kind !== 'file' || !isFnosHost()) return;
  const button = findDefaultAppItem(menu);
  if (!button) return;
  const path = target.path;
  if (!path) return;

  const bound = button.dataset.dshFnosOpenBound === '1';
  // React may re-apply disabled on a re-render; keep the row enabled.
  if (button.disabled) button.disabled = false;
  if (button.getAttribute('aria-disabled') === 'true') {
    button.removeAttribute('aria-disabled');
    button.dataset.dshFnosOpenAria = '1';
  }
  button.setAttribute(OPEN_ITEM_ATTR, '1');
  button.dataset.dshFnosOpenPath = path;
  hideRedundantOpenItem(menu);
  if (bound) return;

  button.dataset.dshFnosOpenBound = '1';
  const handler: EventListener = (event) => {
    event.preventDefault();
    event.stopPropagation();
    // Read the live snapshot first: React reuses the same button node for the
    // same action id in the next menu, so the dataset can lag one scan behind.
    const current = button.dataset.dshFnosOpenPath || '';
    closeMenu();
    if (current) void openPathInFnos(current, false);
  };
  revealHandlers.set(button, handler);
  button.addEventListener('click', handler);
}

/** dsh's "Open in default application" row, matched while it is still disabled. */
function findDefaultAppItem(menu: Element): HTMLButtonElement | null {
  for (const button of menu.querySelectorAll<HTMLButtonElement>('button[role="menuitem"]')) {
    if (button.hasAttribute(OPEN_ITEM_ATTR)) return button;
    const disabled = button.disabled || button.getAttribute('aria-disabled') === 'true';
    if (!disabled) continue;
    if (DEFAULT_APP_LABEL_RE.test((button.textContent || '').trim())) return button;
  }
  return null;
}

/**
 * Hide the plugin's own "在飞牛中打开" row once dsh's row does the same job.
 *
 * It is hidden (not removed): the node belongs to dsh's React tree, and taking
 * it out from under React is what used to break the whole overlay. The row is
 * only marked when the native takeover actually happened, so a dsh that stops
 * rendering that row still leaves the user a working action.
 */
function hideRedundantOpenItem(menu: Element): void {
  const label = t('在飞牛中打开');
  for (const button of menu.querySelectorAll<HTMLButtonElement>('button[role="menuitem"]')) {
    if (button.hasAttribute(OPEN_ITEM_ATTR)) continue;
    if ((button.textContent || '').trim() !== label) continue;
    // dsh renders one `<div><button role="menuitem">…</button></div>` per row;
    // hide that wrapper, but never a container that holds other rows too.
    const parent = button.parentElement;
    const ownsOneRow = parent !== null && parent !== menu
      && parent.querySelectorAll('button[role="menuitem"]').length === 1;
    const row = ownsOneRow ? parent : button;
    row.setAttribute(HIDDEN_ITEM_ATTR, '1');
    row.classList.add('dsh-fnos-menu-hidden');
  }
}

/** Restore every native menu row the plugin touched (plugin unload). */
export function resetRevealItems(): void {
  for (const button of document.querySelectorAll<HTMLButtonElement>(`[${REVEAL_ITEM_ATTR}]`)) {
    const handler = revealHandlers.get(button);
    if (handler) button.removeEventListener('click', handler);
    revealHandlers.delete(button);
    button.disabled = true;
    button.removeAttribute(REVEAL_ITEM_ATTR);
    if (button.dataset.dshFnosRevealAria === '1') button.setAttribute('aria-disabled', 'true');
    delete button.dataset.dshFnosRevealAria;
    delete button.dataset.dshFnosRevealBound;
    delete button.dataset.dshFnosRevealKind;
    delete button.dataset.dshFnosRevealId;
  }
  for (const button of document.querySelectorAll<HTMLButtonElement>(`[${OPEN_ITEM_ATTR}]`)) {
    const handler = revealHandlers.get(button);
    if (handler) button.removeEventListener('click', handler);
    revealHandlers.delete(button);
    button.disabled = true;
    button.removeAttribute(OPEN_ITEM_ATTR);
    if (button.dataset.dshFnosOpenAria === '1') button.setAttribute('aria-disabled', 'true');
    delete button.dataset.dshFnosOpenAria;
    delete button.dataset.dshFnosOpenBound;
    delete button.dataset.dshFnosOpenPath;
  }
  for (const row of document.querySelectorAll<HTMLElement>(`[${HIDDEN_ITEM_ATTR}]`)) {
    row.classList.remove('dsh-fnos-menu-hidden');
    row.removeAttribute(HIDDEN_ITEM_ATTR);
  }
  for (const button of document.querySelectorAll<HTMLButtonElement>('[data-dsh-fnos-copy-bound]')) {
    const handler = copyHandlers.get(button);
    if (handler) button.removeEventListener('click', handler);
    copyHandlers.delete(button);
    delete button.dataset.dshFnosCopyBound;
    delete button.dataset.dshFnosCopyPath;
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

/**
 * Hand one **absolute folder** path to the fnOS file manager.
 *
 * `openFileManager` navigates to a directory; a file path (or a relative one,
 * which the host would resolve against the fnOS home) opens the wrong place.
 * Callers pass `folderOfPath(...)` for files and refuse the empty result here
 * instead of guessing a root.
 */
async function openPathInFileManager(path: string): Promise<void> {
  if (!path.startsWith('/')) {
    showToast(t('无法确定文件路径'));
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
