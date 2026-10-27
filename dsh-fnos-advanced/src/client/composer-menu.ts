/**
 * Add "从飞牛选择" to dsh's composer command menu (the "+" menu: 文件 / 目标 /
 * 计划 / …) instead of adding a standalone toolbar button.
 *
 * A separate button would compete with dsh's own tool row layout on narrow
 * screens; the command menu already owns a stable list surface. The menu is the
 * input-trigger popup (`[data-trigger-menu]` + `role="listbox"` +
 * `button[role="option"]`), identified by the English command aliases
 * (file / goal / plan), which are language-independent.
 */
import { addPathsToComposer, findActiveComposerInput } from './composer.ts';
import { PICK_FILE_SVG, PICK_ITEM_ATTR } from './constants.ts';
import { dismissTriggerMenu, isVisible, showToast } from './dom.ts';
import type { PickFlow } from './flow.ts';
import { t } from './i18n.ts';
import { looksLikeComposerCommandMenu } from './menu.ts';
import { runPickFlow } from './picker-flow.ts';

interface PickItemOptions {
  attr: string;
  label: string;
  alias: string;
  description: string;
}

/** Add the rows to the open composer command menu, if that is what we found. */
export function scanCommandMenu(): void {
  for (const menu of document.querySelectorAll('[data-trigger-menu]')) {
    if (!isVisible(menu)) continue;
    if (!isComposerCommandMenu(menu)) continue;

    const options = Array.from(menu.querySelectorAll<HTMLButtonElement>('button[role="option"]'));
    const fileOption = options.find((option) => /\bfile\b/i.test(option.textContent || ''));
    if (!fileOption || !fileOption.parentElement) continue;

    if (menu.querySelector(`[${PICK_ITEM_ATTR}]`)) continue;
    const row = buildPickItem(options, fileOption, {
      attr: PICK_ITEM_ATTR,
      label: t('从飞牛选择'),
      alias: 'fnos',
      description: t('选择 NAS 上的文件，非图片作为路径引用'),
    });
    if (row) fileOption.after(row);
  }
}

function isComposerCommandMenu(menu: Element): boolean {
  const options = Array.from(menu.querySelectorAll('button[role="option"]'));
  return looksLikeComposerCommandMenu(options.map((option) => option.textContent || ''));
}

/** Clone one command row and turn it into one of our entries. */
function buildPickItem(
  options: readonly HTMLButtonElement[],
  fileOption: HTMLButtonElement,
  entry: PickItemOptions,
): HTMLButtonElement | null {
  const item = fileOption.cloneNode(true) as HTMLButtonElement;
  item.removeAttribute('id');
  item.removeAttribute('aria-selected');
  item.removeAttribute('aria-label');
  item.setAttribute(entry.attr, '1');
  // The source row may be the highlighted one; keep only one active row.
  if (item.className) {
    item.className = item.className.split(/\s+/).filter((name) => name && !/active/i.test(name)).join(' ');
  }

  // The row is: [icon] [name] [alias?] [description?]. English menus render the
  // same word for name and alias ("File"/"file"), so the alias row may be
  // absent; locate the icon by its SVG and walk siblings instead of matching
  // text. No CSS-module class names are used.
  const spans = Array.from(item.querySelectorAll<HTMLSpanElement>('span'));
  const icon = spans.find((span) => span.querySelector('svg')) || spans[0];
  const name = icon ? icon.nextElementSibling : spans[1];
  let alias: Element | null = name ? name.nextElementSibling : null;
  if (alias && (alias.querySelector('svg') || !(alias.textContent || '').trim())) alias = null;
  let description: Element | null = alias
    ? alias.nextElementSibling
    : name
      ? name.nextElementSibling
      : null;

  // The "file" row has no description; borrow the description element (and its
  // styling) from a sibling command row instead of inventing a class name.
  if (!description) {
    const template = options
      .map((option) => {
        const optionAlias = Array.from(option.querySelectorAll('span'))
          .find((span) => (span.textContent || '').trim().toLowerCase() === 'goal');
        return optionAlias ? optionAlias.nextElementSibling : null;
      })
      .find((node): node is HTMLElement => node instanceof HTMLElement && node.tagName === 'SPAN');
    if (template) {
      description = template.cloneNode(false) as HTMLElement;
      item.appendChild(description);
    }
  }

  if (icon) icon.innerHTML = PICK_FILE_SVG;
  if (name) name.textContent = entry.label;
  if (alias) alias.textContent = entry.alias;
  if (description) description.textContent = entry.description;

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

  return item;
}

/** Click handler for the injected command rows. */
export async function handlePickFiles(button: HTMLButtonElement): Promise<void> {
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

function composerFlow(input: HTMLInputElement, button: HTMLButtonElement): PickFlow {
  return {
    button,
    method: 'pickUserFile',
    directory: false,
    multiple: true,
    title: t('选择文件'),
    okText: t('添加引用'),
    anchor: input.closest('[data-composer-card]') || input.parentElement,
    status: (text) => showToast(text),
    onPaths: (paths) => addPathsToComposer(paths, input),
    doneText: (paths) => t('已添加 {count} 个文件', { count: paths.length }),
    setLabel: () => { /* icon-only row: progress goes to the toast */ },
    doneLabel: () => '',
    restore: () => {
      button.disabled = false;
      delete button.dataset.busy;
    },
  };
}
