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
import type { PickFlow } from './flow.ts';
import { t } from './i18n.ts';
import { isDuplicateWorkspacePath } from './picker.ts';
import { runPickFlow } from './picker-flow.ts';
import { knownWorkspacePaths } from './plugin-api.ts';

const statusTimers = new WeakMap<HTMLElement, number>();

/** Add the button to every workspace picker currently in the DOM. */
export function scanWorkspacePickers(): void {
  for (const dialog of document.querySelectorAll('[role="dialog"]')) {
    if (!isWorkspacePicker(dialog)) {
      dialog.querySelectorAll(`[${BUTTON_ATTR}], .${STATUS_CLASS}`).forEach((node) => node.remove());
      continue;
    }
    if (dialog.querySelector(`[${BUTTON_ATTR}]`)) continue;
    injectButton(dialog);
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

function injectButton(dialog: Element): void {
  const footer = findFooter(dialog);
  if (!footer) return;

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

  const previous = statusTimers.get(status);
  if (previous) window.clearTimeout(previous);
  statusTimers.set(status, window.setTimeout(() => {
    status.style.display = 'none';
  }, isError ? 10000 : 6000));
}
