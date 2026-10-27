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
import { fillAndEnter, isVisible, sleep } from './dom.ts';
import { fileNameForPath } from './files.ts';
import type { PickFlow } from './flow.ts';
import { currentLanguage, t } from './i18n.ts';
import { isDuplicateWorkspacePath } from './picker.ts';
import { runPickFlow } from './picker-flow.ts';
import { knownWorkspacePaths } from './plugin-api.ts';
import { clearRecentPaths, loadRecentPaths, recordRecentPath } from './recent-dirs.ts';

const statusTimers = new WeakMap<HTMLElement, number>();

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

    renderRecentChips(dialog, footer);
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

/**
 * One compact row of recently authorized folders inside the picker footer.
 * Chips only pre-fill the path input; they never re-authorize, so a stale
 * folder surfaces as an empty listing instead of an unexpected permission
 * prompt. The row is rebuilt only when the list or the interface language
 * changes (a MutationObserver scans often).
 */
function renderRecentChips(dialog: Element, footer: Element): void {
  const paths = loadRecentPaths();
  let container = dialog.querySelector<HTMLElement>('.dsh-fnos-recent');
  if (!paths.length) {
    container?.remove();
    repositionStatus(dialog);
    return;
  }

  if (!container) {
    container = document.createElement('div');
    container.className = 'dsh-fnos-recent';
  }
  if (footer.firstElementChild !== container) footer.insertBefore(container, footer.firstChild);

  const signature = `${currentLanguage()}\n${paths.join('\n')}`;
  if (container.dataset.signature === signature) return;
  container.dataset.signature = signature;
  container.replaceChildren();

  const label = document.createElement('span');
  label.className = 'dsh-fnos-recent-label';
  label.textContent = t('最近使用');
  container.append(label);

  for (const path of paths) {
    const chip = document.createElement('button');
    chip.type = 'button';
    chip.className = 'dsh-fnos-recent-chip';
    chip.textContent = fileNameForPath(path);
    chip.title = path;
    chip.setAttribute('aria-label', t('填入最近使用的目录：{path}', { path }));
    chip.addEventListener('mousedown', (event) => {
      event.preventDefault();
      event.stopPropagation();
    });
    chip.addEventListener('click', (event) => {
      event.preventDefault();
      event.stopPropagation();
      void useRecentPath(dialog, path);
    });
    container.append(chip);
  }

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
    renderRecentChips(dialog, footer);
  });
  container.append(clear);
  repositionStatus(dialog);
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

/** Navigate the picker to one remembered folder. */
async function useRecentPath(dialog: Element, path: string): Promise<void> {
  const input = await ensurePathInput(dialog);
  if (!input) {
    showStatus(dialog, t('没有找到目录选择器的路径输入框'), true);
    return;
  }
  fillAndEnter(input, path);
  recordRecentPath(path);
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
      if (footer) renderRecentChips(dialog, footer);
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
