/**
 * The workspace file tree: one "fnOS actions" control per row.
 *
 * Rows expose `data-files-entry` (`file` / `directory` / `other`) and an
 * absolute `data-files-path`; both are deliberate automation anchors in
 * 0.2.0-rc.2 and 0.2.1-rc.2. 0.2.2-alpha.3 wrapped the row's main button in a
 * `div` (`<li><div><button>…</button><div class="accessory"/></div></li>`), so
 * the button lookup accepts both shapes. The control sits outside the row
 * <button> so a click never opens/toggles the file itself. The menu itself is
 * shared with the slot-rendered surfaces and lives in `path-menu.ts`.
 */
import { FILE_ACTION_ATTR, FILE_ACTION_SVG } from './constants.ts';
import { t } from './i18n.ts';
import { togglePathMenu } from './path-menu.ts';

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
      const rowButton = findRowButton(row);
      const height = rowButton ? rowButton.offsetHeight : 0;
      if (height && action.style.height !== `${height}px`) action.style.height = `${height}px`;
      const label = t('飞牛操作');
      if (action.title !== label) action.title = label;
      if (action.getAttribute('aria-label') !== label) action.setAttribute('aria-label', label);
      continue;
    }

    injectFileAction(row);
  }
}

/**
 * The row's main action button across dsh versions.
 *
 * 0.2.0–0.2.1 rendered it as a direct child of the row `<li>`; 0.2.2-alpha.3
 * renders `<li><div class=row><button class=action>…</button><div class=accessory/></div></li>`.
 * In both shapes the main button is the first button in the row.
 */
function findRowButton(row: HTMLElement): HTMLElement | null {
  return row.querySelector<HTMLElement>(':scope > button')
    || row.querySelector<HTMLElement>(':scope > div > button');
}

function injectFileAction(row: HTMLElement): void {
  const rowButton = findRowButton(row);
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
    togglePathMenu(action, {
      path: row.getAttribute('data-files-path') || '',
      entry: row.getAttribute('data-files-entry') || '',
    });
  });

  row.appendChild(action);
}
