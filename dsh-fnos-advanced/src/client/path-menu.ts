/**
 * The shared "fnOS actions" menu for one path.
 *
 * The same menu is used by the file tree, the delivery cards, the change-review
 * tab and the document preview, so it lives here instead of inside the file-tree
 * scanner. Anchors are plain elements: the file tree injects its button into
 * dsh's markup, while slot-rendered buttons hand their own React node in.
 */
import { quotePathToComposer } from './composer.ts';
import { LOG_PREFIX } from './constants.ts';
import { copyText, showToast } from './dom.ts';
import { fileRowActions, type FileRowAction } from './file-actions.ts';
import { t, translateError, type UiKey } from './i18n.ts';
import { getSdk, isFnosHost } from './sdk.ts';

/** One path the menu acts on. */
export interface PathMenuTarget {
  path: string;
  /** `file` / `directory`; anything else gets no menu at all. */
  entry: string;
}

/** The open menu (only one at a time) and the anchor it belongs to. */
let pathMenu: HTMLElement | null = null;
let pathMenuAnchor: HTMLElement | null = null;
let pathMenuCleanup: (() => void) | null = null;

const FILE_ACTION_TEXT = {
  open: '在飞牛中打开',
  details: '文件详情',
  quote: '引用到对话',
  copy: '复制路径',
} satisfies Record<FileRowAction, UiKey>;

/** Open the menu for an anchor, or close it when the same anchor asks again. */
export function togglePathMenu(anchor: HTMLElement, target: PathMenuTarget): void {
  if (pathMenu && pathMenuAnchor === anchor) {
    closePathMenu();
    return;
  }
  openPathMenu(anchor, target);
}

function openPathMenu(anchor: HTMLElement, target: PathMenuTarget): void {
  closePathMenu();
  const { path, entry } = target;
  const actions = fileRowActions(entry, isFnosHost());
  if (!path || !actions.length) return;

  const menu = document.createElement('div');
  menu.className = 'dsh-fnos-file-menu';
  menu.setAttribute('role', 'menu');

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
      closePathMenu();
      void runPathAction(action, path, entry);
    });
    menu.appendChild(item);
  }

  document.body.appendChild(menu);
  positionMenu(menu, anchor);
  pathMenu = menu;
  pathMenuAnchor = anchor;

  const onPointerDown = (event: Event) => {
    if (!(event.target instanceof Node)) return;
    if (menu.contains(event.target) || anchor.contains(event.target)) return;
    closePathMenu();
  };
  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key === 'Escape') closePathMenu();
  };
  document.addEventListener('pointerdown', onPointerDown, true);
  document.addEventListener('keydown', onKeyDown, true);
  window.addEventListener('scroll', closePathMenu, true);
  window.addEventListener('resize', closePathMenu);

  pathMenuCleanup = () => {
    document.removeEventListener('pointerdown', onPointerDown, true);
    document.removeEventListener('keydown', onKeyDown, true);
    window.removeEventListener('scroll', closePathMenu, true);
    window.removeEventListener('resize', closePathMenu);
  };
}

function positionMenu(menu: HTMLElement, anchor: HTMLElement): void {
  const margin = 8;
  const rect = anchor.getBoundingClientRect();
  const width = menu.offsetWidth || 200;
  const height = menu.offsetHeight || 100;
  menu.style.left = `${Math.min(Math.max(margin, rect.right - width), window.innerWidth - width - margin)}px`;
  let top = rect.bottom + 6;
  if (top + height > window.innerHeight - margin) top = Math.max(margin, rect.top - height - 6);
  menu.style.top = `${top}px`;
}

/** Close the menu and detach its document/window listeners. */
export function closePathMenu(): void {
  if (pathMenuCleanup) {
    try { pathMenuCleanup(); } catch { /* ignore */ }
    pathMenuCleanup = null;
  }
  if (pathMenu) {
    pathMenu.remove();
    pathMenu = null;
  }
  pathMenuAnchor = null;
}

async function runPathAction(action: FileRowAction, path: string, entry: string): Promise<void> {
  const isDirectory = entry === 'directory';
  try {
    if (action === 'open') {
      await openPathInFnos(path, isDirectory);
    } else if (action === 'details') {
      await showPathDetails(path);
    } else if (action === 'quote') {
      await quotePathToComposer(path);
    } else if (action === 'copy') {
      await copyPathToClipboard(path);
    }
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    console.warn(LOG_PREFIX, 'path action failed', action, message, error);
    if (action === 'open') {
      showToast(t('在飞牛中打开失败，当前宿主可能不支持该操作。'));
    } else if (action === 'details') {
      showToast(t('无法显示文件详情，当前宿主可能不支持该操作。'));
    } else {
      showToast(translateError(message));
    }
  }
}

async function openPathInFnos(path: string, isDirectory: boolean): Promise<void> {
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

/** Show fnOS's own details sheet (size, permissions, location). */
async function showPathDetails(path: string): Promise<void> {
  const sdk = getSdk();
  try { await sdk.ready(); } catch { /* fall through to the host call */ }
  if (typeof sdk.showFileDetails !== 'function') {
    throw new Error('showFileDetails is not supported by this host');
  }
  const result = await sdk.showFileDetails([path]);
  if (result === null || result === false) {
    throw new Error('showFileDetails is not supported by this host');
  }
}

async function copyPathToClipboard(path: string): Promise<void> {
  const ok = await copyText(path);
  showToast(ok ? t('已复制路径') : t('复制失败，请手动复制'));
}
