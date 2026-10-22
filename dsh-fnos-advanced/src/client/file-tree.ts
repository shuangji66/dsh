/**
 * The workspace file tree: one "fnOS actions" control per row.
 *
 * Rows expose `data-files-entry` (`file` / `directory` / `other`) and an
 * absolute `data-files-path`; both are deliberate automation anchors in
 * 0.2.0-rc.2 and 0.2.1-rc.2. The control sits outside the row <button> so a
 * click never opens/toggles the file itself.
 */
import { quotePathToComposer } from './composer.ts';
import { FILE_ACTION_ATTR, FILE_ACTION_SVG, LOG_PREFIX } from './constants.ts';
import { copyText, showToast } from './dom.ts';
import { fileRowActions, type FileRowAction } from './file-actions.ts';
import { t, translateError, type UiKey } from './i18n.ts';
import { getSdk, isFnosHost } from './sdk.ts';

/** Row action menu (only one open at a time). */
let fileMenu: HTMLElement | null = null;
let fileMenuRow: Element | null = null;
let fileMenuCleanup: (() => void) | null = null;

const FILE_ACTION_TEXT = {
  open: '在飞牛中打开',
  quote: '引用到对话',
  copy: '复制路径',
} satisfies Record<FileRowAction, UiKey>;

/** Add / maintain the row control across the current file tree. */
export function scanFileTree(): void {
  for (const row of document.querySelectorAll<HTMLElement>('[data-files-entry][data-files-path]')) {
    const entry = row.getAttribute('data-files-entry') || '';
    const action = row.querySelector<HTMLElement>(`:scope > [${FILE_ACTION_ATTR}]`);

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
      const rowButton = row.querySelector<HTMLElement>(':scope > button');
      const height = rowButton ? rowButton.offsetHeight : 0;
      if (height && action.style.height !== `${height}px`) action.style.height = `${height}px`;
      continue;
    }

    injectFileAction(row);
  }
}

function injectFileAction(row: HTMLElement): void {
  const rowButton = row.querySelector<HTMLElement>(':scope > button');
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
    if (fileMenu && fileMenuRow === row) {
      closeFileMenu();
      return;
    }
    openFileMenu(action, row);
  });

  row.appendChild(action);
}

function openFileMenu(anchor: HTMLElement, row: Element): void {
  closeFileMenu();
  const path = row.getAttribute('data-files-path') || '';
  const entry = row.getAttribute('data-files-entry') || '';
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
      closeFileMenu();
      void runFileAction(action, path, entry);
    });
    menu.appendChild(item);
  }

  document.body.appendChild(menu);
  positionFileMenu(menu, anchor);
  fileMenu = menu;
  fileMenuRow = row;

  const onPointerDown = (event: Event) => {
    if (!(event.target instanceof Node)) return;
    if (menu.contains(event.target) || anchor.contains(event.target)) return;
    closeFileMenu();
  };
  const onKeyDown = (event: KeyboardEvent) => {
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

function positionFileMenu(menu: HTMLElement, anchor: HTMLElement): void {
  const margin = 8;
  const rect = anchor.getBoundingClientRect();
  const width = menu.offsetWidth || 200;
  const height = menu.offsetHeight || 100;
  menu.style.left = `${Math.min(Math.max(margin, rect.right - width), window.innerWidth - width - margin)}px`;
  let top = rect.bottom + 6;
  if (top + height > window.innerHeight - margin) top = Math.max(margin, rect.top - height - 6);
  menu.style.top = `${top}px`;
}

export function closeFileMenu(): void {
  if (fileMenuCleanup) {
    try { fileMenuCleanup(); } catch { /* ignore */ }
    fileMenuCleanup = null;
  }
  if (fileMenu) {
    fileMenu.remove();
    fileMenu = null;
  }
  fileMenuRow = null;
}

async function runFileAction(action: FileRowAction, path: string, entry: string): Promise<void> {
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
    const message = error instanceof Error ? error.message : String(error);
    console.warn(LOG_PREFIX, 'file-tree action failed', action, message, error);
    showToast(action === 'open'
      ? t('在飞牛中打开失败，当前宿主可能不支持该操作。')
      : translateError(message));
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

async function copyPathToClipboard(path: string): Promise<void> {
  const ok = await copyText(path);
  showToast(ok ? t('已复制路径') : t('复制失败，请手动复制'));
}
