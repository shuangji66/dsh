/**
 * The workspace directory picker: find dsh's dialog and add one button to its
 * footer.
 *
 * dsh owns this React dialog and the plugin imports none of its UI packages, so
 * the dialog is identified by accessible text (`role="dialog"` + workspace
 * title + a path-labelled input or the edit-path affordance). The picked path is
 * written into that input and submitted with Enter, which navigates the picker.
 *
 * The fnOS shared-folder picker was removed on purpose:
 * `/app-auth/pick-shared-file` needs a shared-scope grant this app does not
 * have, so it always ended in an authorization error.
 */
import {
  BUTTON_ATTR,
  CANCEL_BUTTON_RE,
  OPEN_BUTTON_RE,
  PATH_LABEL_RE,
  STATUS_CLASS,
  WORKSPACE_TITLE_RE,
} from './constants.ts';
import { pickAuthorizedDirs } from './authorized-dirs.ts';
import { fillAndEnter, isVisible, sleep } from './dom.ts';
import { fileNameForPath } from './files.ts';
import type { PickFlow } from './flow.ts';
import { currentLanguage, t } from './i18n.ts';
import { isDuplicateWorkspacePath, normalizeWorkspacePath } from './picker.ts';
import { runPickFlow } from './picker-flow.ts';
import { knownWorkspaces, knownWorkspacePaths } from './plugin-api.ts';
import { clearRecentPaths, loadRecentPaths, recordRecentPath } from './recent-dirs.ts';

const statusTimers = new WeakMap<HTMLElement, number>();

/** Which shortcut row a chip belongs to. */
type ShortcutRowKind = 'authorized' | 'recent';

/** Every shortcut row carries the shared chip-row class for styling/cleanup. */
const SHORTCUT_ROW_CLASS = 'dsh-fnos-recent';

/** Dialogs whose host-side workspace lookup was already attempted. */
const workspaceRefreshTried = new WeakSet<Element>();

/** Add the button to every workspace picker currently in the DOM. */
export function scanWorkspacePickers(): void {
  for (const dialog of document.querySelectorAll('[role="dialog"]')) {
    if (!isWorkspacePicker(dialog)) {
      dialog.querySelectorAll(`[${BUTTON_ATTR}], .${STATUS_CLASS}, .dsh-fnos-recent`).forEach((node) => node.remove());
      continue;
    }
    const footer = findFooter(dialog);
    if (!footer) continue;

    const button = dialog.querySelector<HTMLButtonElement>(`[${BUTTON_ATTR}]`);
    if (button) refreshButtonLabels(button);
    else injectButton(dialog, footer);

    renderShortcutRows(dialog, footer);
  }
}

export function isWorkspacePicker(dialog: Element): boolean {
  if (!isVisible(dialog)) return false;

  const title = [
    dialog.getAttribute('aria-label') || '',
    ...Array.from(dialog.querySelectorAll('h1, h2, h3')).map((node) => node.textContent || ''),
  ].join(' ');
  if (!WORKSPACE_TITLE_RE.test(title)) return false;

  // A workspace rename dialog also has an input and a footer. Requiring a
  // path-labelled input (or the picker's edit-path affordance) keeps us out of
  // those dialogs.
  if (!findPathInput(dialog) && !findEditButton(dialog)) return false;
  return Boolean(findFooter(dialog));
}

function injectButton(dialog: Element, footer: Element): void {
  const primary = document.createElement('button');
  primary.type = 'button';
  primary.setAttribute(BUTTON_ATTR, '1');
  primary.dataset.pick = 'directory';
  primary.className = 'dsh-fnos-advanced-button';
  primary.textContent = t('选择飞牛目录');
  primary.title = t('通过飞牛授权选择一个目录（会授予当前飞牛应用访问权限）');

  // Keep it before the leading footer action (New folder / 新建文件夹), so it
  // does not sit between Cancel and Open.
  footer.insertBefore(primary, footer.firstChild);
}

/** Keep a button that is already mounted in the current interface language. */
function refreshButtonLabels(button: HTMLButtonElement): void {
  if (button.dataset.busy === '1') return;
  const label = t('选择飞牛目录');
  const hint = t('通过飞牛授权选择一个目录（会授予当前飞牛应用访问权限）');
  if (button.textContent !== label) button.textContent = label;
  if (button.title !== hint) button.title = hint;
}

interface ShortcutRowOptions {
  kind: ShortcutRowKind;
  label: string;
  /** Tooltip on the row label (the chips carry their own full paths). */
  hint?: string;
  paths: readonly string[];
  /** Render the "forget everything" button (recent row only). */
  clearable?: boolean;
}

/**
 * Two compact chip rows inside the picker footer.
 *
 * "已授权" lists the folders dsh already has as workspaces: they are in use,
 * so access was granted when they were added, and the same folder is often
 * reopened to promote a subfolder. "最近使用" keeps whatever this browser
 * picked but is not a workspace yet; a folder already shown above is dropped
 * from it so the same chip does not appear twice.
 *
 * Chips only pre-fill the path input; they never re-authorize, so a stale
 * folder surfaces as an empty listing instead of an unexpected permission
 * prompt. Each row is rebuilt only when its list or the interface language
 * changes (a MutationObserver scans often).
 */
function renderShortcutRows(dialog: Element, footer: Element): void {
  const authorized = pickAuthorizedDirs(knownWorkspaces());
  const seen = new Set(authorized);
  const recent = loadRecentPaths().filter((path) => !seen.has(normalizeWorkspacePath(path)));

  renderShortcutRow(dialog, footer, {
    kind: 'authorized',
    label: t('已授权'),
    hint: t('已有工作区目录，点击填入并进入'),
    paths: authorized,
  });
  renderShortcutRow(dialog, footer, {
    kind: 'recent',
    label: t('最近使用'),
    paths: recent,
    clearable: true,
  });

  // The live `workspaces` service may inject after the first dialog opens;
  // fall back to the host registry so the authorized row still appears.
  if (!authorized.length) void requestWorkspaceRefresh(dialog);
  repositionStatus(dialog);
}

/** Render (or remove) one shortcut row. */
function renderShortcutRow(dialog: Element, footer: Element, options: ShortcutRowOptions): void {
  const selector = `.${SHORTCUT_ROW_CLASS}[data-dsh-fnos-row="${options.kind}"]`;
  let container = dialog.querySelector<HTMLElement>(selector);
  if (!options.paths.length) {
    container?.remove();
    return;
  }

  if (!container) {
    container = document.createElement('div');
    container.className = SHORTCUT_ROW_CLASS;
    container.dataset.dshFnosRow = options.kind;
  }
  placeShortcutRow(footer, container, options.kind);

  const signature = `${currentLanguage()}\n${options.paths.join('\n')}`;
  if (container.dataset.signature === signature) return;
  container.dataset.signature = signature;
  container.replaceChildren();

  const label = document.createElement('span');
  label.className = 'dsh-fnos-recent-label';
  label.textContent = options.label;
  if (options.hint) label.title = options.hint;
  container.append(label);

  for (const path of options.paths) container.append(createPathChip(dialog, path, options.kind));
  if (options.clearable) container.append(createClearChip(dialog, footer));
}

/** Authorized row on top, recent row right below it, both above the buttons. */
function placeShortcutRow(footer: Element, container: HTMLElement, kind: ShortcutRowKind): void {
  const anchor = kind === 'recent'
    ? footer.querySelector(`.${SHORTCUT_ROW_CLASS}[data-dsh-fnos-row="authorized"]`)
    : null;
  if (!anchor) {
    if (footer.firstElementChild !== container) footer.insertBefore(container, footer.firstChild);
    return;
  }
  if (anchor.nextElementSibling !== container) footer.insertBefore(container, anchor.nextElementSibling);
}

/** One folder chip; clicking fills the path input and submits it. */
function createPathChip(dialog: Element, path: string, kind: ShortcutRowKind): HTMLButtonElement {
  const chip = document.createElement('button');
  chip.type = 'button';
  chip.className = 'dsh-fnos-recent-chip';
  chip.textContent = fileNameForPath(path);
  chip.title = path;
  chip.setAttribute(
    'aria-label',
    kind === 'authorized'
      ? t('填入已授权的目录：{path}', { path })
      : t('填入最近使用的目录：{path}', { path }),
  );
  chip.addEventListener('mousedown', (event) => {
    event.preventDefault();
    event.stopPropagation();
  });
  chip.addEventListener('click', (event) => {
    event.preventDefault();
    event.stopPropagation();
    void useShortcutPath(dialog, path, kind === 'recent');
  });
  return chip;
}

/** "×" that forgets the recent list but keeps the authorized row. */
function createClearChip(dialog: Element, footer: Element): HTMLButtonElement {
  const clear = document.createElement('button');
  clear.type = 'button';
  clear.className = 'dsh-fnos-recent-clear';
  clear.textContent = '×';
  clear.title = t('清除最近记录');
  clear.setAttribute('aria-label', t('清除最近记录'));
  clear.addEventListener('click', (event) => {
    event.preventDefault();
    event.stopPropagation();
    clearRecentPaths();
    renderShortcutRows(dialog, footer);
  });
  return clear;
}

/**
 * Ask the host for the workspace registry once per dialog, when the live
 * service has not reported anything yet. The lookup populates the shared map,
 * so the row renders on the next scan.
 */
async function requestWorkspaceRefresh(dialog: Element): Promise<void> {
  if (workspaceRefreshTried.has(dialog)) return;
  workspaceRefreshTried.add(dialog);

  try {
    await knownWorkspacePaths();
  } catch {
    return; // older host or a transient failure: the row stays hidden
  }
  if (!dialog.isConnected) return;
  const footer = findFooter(dialog);
  if (footer) renderShortcutRows(dialog, footer);
}

/**
 * Keep the transient status line above the footer. The footer grows when the
 * recent-folder row is present, so the old fixed `bottom: 70px` covered it.
 */
function repositionStatus(dialog: Element): void {
  const status = dialog.querySelector<HTMLElement>(`.${STATUS_CLASS}`);
  if (!status || status.style.display !== 'block') return;
  const footer = findFooter(dialog);
  status.style.bottom = `${(footer instanceof HTMLElement ? footer.offsetHeight : 56) + 12}px`;
}

/**
 * Navigate the picker to one listed folder.
 *
 * Recent chips also bump their folder to the front of the recent list;
 * authorized chips do not, because the folder is already pinned above and
 * recording it would only churn localStorage.
 */
async function useShortcutPath(dialog: Element, path: string, record: boolean): Promise<void> {
  const input = await ensurePathInput(dialog);
  if (!input) {
    showStatus(dialog, t('没有找到目录选择器的路径输入框'), true);
    return;
  }
  fillAndEnter(input, path);
  if (record) recordRecentPath(path);
  showStatus(dialog, t('已填入路径'), false);
}

export function findPathInput(dialog: Element): HTMLInputElement | null {
  for (const input of dialog.querySelectorAll<HTMLInputElement>('input')) {
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

export function findEditButton(dialog: Element): HTMLButtonElement | null {
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

export function findFooter(dialog: Element): Element | null {
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

function buttonText(button: Element): string {
  return (button.textContent || '').replace(/\s+/g, ' ').trim();
}

/** The picker's path input, opening the edit-path mode when it is hidden. */
export async function ensurePathInput(dialog: Element): Promise<HTMLInputElement | null> {
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

/** Click handler for the injected picker button. */
export async function handlePick(button: HTMLButtonElement): Promise<void> {
  const dialog = button.closest('[role="dialog"]');
  if (!dialog) return;
  await runPickFlow(button, directoryFlow(dialog, button));
}

function directoryFlow(dialog: Element, button: HTMLButtonElement): PickFlow {
  const initialText = button.textContent || t('选择飞牛目录');
  return {
    button,
    method: 'pickUserFile',
    directory: true,
    creatable: true,
    title: t('选择工作区目录'),
    okText: t('确认授权'),
    anchor: dialog,
    status: (text, isError) => showStatus(dialog, text, isError === true),
    isDuplicate: async (paths) => isDuplicateWorkspacePath(paths[0], await knownWorkspacePaths()),
    onPaths: async (paths) => {
      const input = await ensurePathInput(dialog);
      if (!input) throw new Error(t('没有找到目录选择器的路径输入框'));
      fillAndEnter(input, paths[0]);
      recordRecentPath(paths[0]);
      const footer = findFooter(dialog);
      if (footer) renderShortcutRows(dialog, footer);
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

function showStatus(dialog: Element, text: string, isError: boolean): void {
  const host = dialog as HTMLElement;
  if (getComputedStyle(host).position === 'static') host.style.position = 'relative';

  let status = dialog.querySelector<HTMLElement>(`.${STATUS_CLASS}`);
  if (!status) {
    status = document.createElement('div');
    status.className = STATUS_CLASS;
    status.setAttribute('role', 'status');
    dialog.appendChild(status);
  }
  status.textContent = text;
  status.dataset.error = isError ? '1' : '0';
  status.style.display = 'block';
  repositionStatus(dialog);

  const previous = statusTimers.get(status);
  if (previous) window.clearTimeout(previous);
  statusTimers.set(status, window.setTimeout(() => {
    status.style.display = 'none';
  }, isError ? 10000 : 6000));
}
