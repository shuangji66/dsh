/**
 * Add "从飞牛选择" to dsh's "@" completion menu (files / folders / sessions).
 *
 * The menu is the same input-trigger popup as the "+" command menu, so it is
 * recognized structurally: one candidate source is named `reference`, so its
 * rows carry `id="dsh-slash-option-reference-<n>"` (the "+" menu uses
 * `command`). The injected row is a clone of the first candidate row, which
 * keeps dsh's own styling without importing any of its UI packages. It sits at
 * the top of the list because file, folder and session candidates share one
 * source and cannot be told apart reliably; it is not part of dsh's React
 * candidate list, so arrow-key navigation does not include it (mouse/touch
 * does); the "+" menu entry remains the keyboard-reachable path.
 */
import { PICK_FILE_SVG, REF_PICK_ITEM_ATTR } from './constants.ts';
import { dismissTriggerMenu, isVisible } from './dom.ts';
import { t } from './i18n.ts';
import { handlePickFiles } from './composer-menu.ts';

/** Source prefix of one "@" candidate row. */
const REFERENCE_OPTION_ID_RE = /^dsh-slash-option-reference-/;

/** Add the row to every open "@" menu that lists candidates. */
export function scanReferenceMenu(): void {
  for (const menu of document.querySelectorAll('[data-trigger-menu]')) {
    if (!isVisible(menu)) continue;
    if (menu.querySelector(`[${REF_PICK_ITEM_ATTR}]`)) continue;

    const options = Array.from(menu.querySelectorAll<HTMLButtonElement>('button[role="option"]'));
    const first = options.find((option) => REFERENCE_OPTION_ID_RE.test(option.id));
    if (!first) continue;
    injectPickItem(first);
  }
}

function injectPickItem(template: HTMLButtonElement): void {
  const item = template.cloneNode(true) as HTMLButtonElement;
  item.removeAttribute('id');
  item.removeAttribute('aria-selected');
  item.removeAttribute('aria-label');
  item.setAttribute(REF_PICK_ITEM_ATTR, '1');
  if (item.className) {
    item.className = item.className.split(/\s+/).filter((name) => name && !/active/i.test(name)).join(' ');
  }

  const spans = Array.from(item.querySelectorAll<HTMLSpanElement>('span'));
  const icon = spans.find((span) => span.querySelector('svg')) || spans[0];
  const name = icon ? icon.nextElementSibling : spans[1];
  const detail = name ? name.nextElementSibling : null;

  if (icon) icon.innerHTML = PICK_FILE_SVG;
  if (name) name.textContent = t('从飞牛选择');
  if (detail) detail.textContent = t('NAS 文件');

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

  template.before(item);
}
