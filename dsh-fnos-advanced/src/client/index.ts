/**
 * dsh-fnos-advanced — browser half.
 *
 * The workspace directory picker is a React dialog owned by dsh. Rather than
 * importing version-specific slots or UI packages, this plugin watches the DOM
 * for four surfaces and augments each in place:
 *
 *   - the workspace picker dialog (one "选择飞牛目录" footer button);
 *   - the composer "+" command menu (one "从飞牛选择" row);
 *   - the file tree (one "⋯" action per file / directory row);
 *   - the workspace context menu (open / check / re-authorize).
 *
 * Where dsh publishes an official file-action slot (delivery cards, the change
 * review tab and the document preview), the "⋯" entry is registered there
 * instead of being injected into the DOM; see `slot-actions.tsx`.
 *
 * Clicking a control asks fnOS for a user-directory authorization through the
 * official `@trimjs/web-app` SDK. In a fnOS host the native picker is used; in a
 * browser the authorization page opens in a popup and its callback page (served
 * by the host half) returns the path. The picked path is then written into the
 * picker's path input and Enter is dispatched, or attached to the composer.
 *
 * No `@deepseek-ai/*` package is imported. Only `window.__ModuleLoader__`, the
 * accessible dialog roles/labels, and `ctx.effect` are used; the optional
 * plugin-center settings page uses the `plugins.bundle.config` slot and the
 * shared `react` platform module.
 */
import { recordAnchorObservations } from './anchor-status.ts';
import { isCancellationText, isPickerCancelled } from './cancel.ts';
import { addPathsToComposer, findActiveComposerInput, findComposerFileInput } from './composer.ts';
import { scanCommandMenu } from './composer-menu.ts';
import { BUTTON_ATTR, FILE_ACTION_ATTR, PICK_ITEM_ATTR, STATUS_CLASS, STYLE_ID } from './constants.ts';
import type { ClientContext } from './context.ts';
import { fillAndEnter } from './dom.ts';
import { scanFileTree } from './file-tree.ts';
import { ensureHostPathBridge } from './host-paths.ts';
import {
  getLanguagePreference,
  onLanguageChange,
  resolveHostLanguage,
  setHostLanguageResolver,
} from './i18n.ts';
import { isDuplicateWorkspacePath } from './picker.ts';
import { pickPathsDirect } from './picker-flow.ts';
import { closePathMenu } from './path-menu.ts';
import { scanReferenceMenu } from './reference-menu.ts';
import { loadSettings, resolveAuthBase, resolveAuthTargets, setWorkspacePaths } from './plugin-api.ts';
import { getSdk } from './sdk.ts';
import { registerSettingsSlot } from './settings-slot.ts';
import { registerPathActionSlots, registerSessionCwd } from './slot-actions.tsx';
import { injectStyle } from './style.ts';
import { VERSION } from '../version.ts';
import {
  findEditButton,
  findFooter,
  findPathInput,
  handlePick,
  isWorkspacePicker,
  scanWorkspacePickers,
} from './workspace-picker.ts';
import { noteWorkspaceRowClick, scanWorkspaceMenus } from './workspace-menu.ts';

let installedDispose: (() => void) | null = null;

/**
 * Cordis client plugin entry.
 *
 * `effect` ties the DOM observers to the plugin's lifetime; the settings slot and
 * the live workspace registry are separate injections.
 */
export function apply(ctx: ClientContext): void {
  if (typeof window === 'undefined' || typeof document === 'undefined') return;
  // The fnOS platform language is only read when the user picked "follow host".
  setHostLanguageResolver(() => getSdk().getPlatformConfig());
  if (getLanguagePreference() === 'system') void resolveHostLanguage();

  const dispose = install();

  ctx?.effect?.(() => dispose, 'dsh-fnos-advanced: workspace directory picker');

  registerSettingsSlot(ctx);
  registerPathActionSlots(ctx);
  registerSessionCwd(ctx);
  registerWorkspacePaths(ctx);
}

/** Install the DOM scanners once; a second call returns the existing disposer. */
export function install(): () => void {
  if (installedDispose) return installedDispose;

  injectStyle();
  // dsh captures the host-path bridge once when the composer slot is injected
  // (`const bridge = hostPathBridge()`), so install ours at plugin load time,
  // long before a file is picked. Waiting until the click would be too late.
  ensureHostPathBridge();
  const cleanups: Array<() => void> = [];

  let scanTimer = 0;
  const scheduleScan = (): void => {
    if (scanTimer) return;
    scanTimer = window.setTimeout(() => {
      scanTimer = 0;
      scan();
    }, 80);
  };

  const observer = new MutationObserver(scheduleScan);
  observer.observe(document.documentElement, { childList: true, subtree: true });
  cleanups.push(() => observer.disconnect());

  // Re-label injected controls (and re-render slot entries) when the effective
  // language changes: a late host-language resolution or a settings change.
  cleanups.push(onLanguageChange(() => { scan(); }));

  const onClick = (event: MouseEvent): void => {
    const target = event.target instanceof Element
      ? event.target.closest<HTMLButtonElement>(`[${BUTTON_ATTR}]`)
      : null;
    if (!target) return;
    event.preventDefault();
    event.stopPropagation();
    void handlePick(target);
  };
  document.addEventListener('click', onClick, true);
  cleanups.push(() => document.removeEventListener('click', onClick, true));

  const onWorkspaceRowClick = (event: MouseEvent): void => {
    const row = event.target instanceof Element
      ? event.target.closest('[data-row-key^="workspace:"]')
      : null;
    if (!row) return;
    noteWorkspaceRowClick(row);
    scheduleScan();
  };
  document.addEventListener('click', onWorkspaceRowClick, true);
  cleanups.push(() => document.removeEventListener('click', onWorkspaceRowClick, true));

  // The SDK learns whether this page is a standalone web app or a host (fnOS
  // desktop / mobile app) during init. Re-scan once it settles.
  getSdk().ready().then(scheduleScan).catch(() => {});

  const dispose = (): void => {
    if (scanTimer) {
      window.clearTimeout(scanTimer);
      scanTimer = 0;
    }
    for (const cleanup of cleanups.splice(0)) {
      try { cleanup(); } catch { /* ignore */ }
    }
    closePathMenu();
    for (const node of document.querySelectorAll(
      `[${BUTTON_ATTR}], [${PICK_ITEM_ATTR}], .${STATUS_CLASS}, .dsh-fnos-recent, [data-dsh-fnos-open-dir], [data-dsh-fnos-check-dir], [data-dsh-fnos-reauth-dir]`,
    )) {
      node.remove();
    }
    for (const row of document.querySelectorAll<HTMLElement>('.dsh-fnos-file-row')) {
      row.classList.remove('dsh-fnos-file-row');
      if (row.dataset.dshFnosPosition === '1') {
        row.style.position = '';
        delete row.dataset.dshFnosPosition;
      }
    }
    for (const node of document.querySelectorAll(`[${FILE_ACTION_ATTR}]`)) node.remove();
    const style = document.getElementById(STYLE_ID);
    if (style) style.remove();
    installedDispose = null;
  };

  installedDispose = dispose;
  scan();
  return dispose;
}

function scan(): void {
  scanWorkspacePickers();
  scanCommandMenu();
  scanReferenceMenu();
  scanFileTree();
  scanWorkspaceMenus();
  // The diagnostics panel lives on the plugin page, where neither anchor is
  // mounted; record them here, on the page that renders them.
  recordAnchorObservations();
}

/** Mirror dsh's live workspace list so duplicate picks can be rejected exactly. */
function registerWorkspacePaths(ctx: ClientContext): void {
  const inject = ctx?.inject;
  if (typeof inject !== 'function') return;

  inject(['workspaces'], (scope) => {
    const list = scope.workspaces?.list;
    if (!list || typeof list.getSnapshot !== 'function') return;

    const update = (): void => {
      try {
        const snapshot = list.getSnapshot?.();
        const items = Array.isArray(snapshot?.items) ? snapshot.items : [];
        const entries = items
          .filter((item): item is { workspaceId: string | number; path: string } => (
            Boolean(item) && item.workspaceId !== null && item.workspaceId !== undefined
            && typeof item.path === 'string'
          ))
          .map((item) => [String(item.workspaceId), item.path] as const);
        setWorkspacePaths(entries);
      } catch { /* ignore a transient snapshot */ }
    };

    update();
    if (typeof list.subscribe === 'function' && typeof scope.effect === 'function') {
      try {
        scope.effect(() => list.subscribe?.(update), 'dsh-fnos-advanced: workspace paths');
      } catch { /* the host route remains as a fallback */ }
    }
  });
}

// Install the path bridge at module-evaluation time. dsh reads
// `globalThis.__DSH_HOST_PATHS__` once when its composer slot is injected, so
// this must not wait for `apply()` or a click.
if (typeof window !== 'undefined') {
  try { ensureHostPathBridge(); } catch { /* frozen global */ }
}

// Debug surface for a real-device browser console. Not a public API; the pure
// helpers it exposes are unit-tested in test/*.test.ts.
if (typeof window !== 'undefined') {
  window.__dshFnosAdvanced = {
    version: VERSION,
    install,
    handlePick,
    isPickerCancelled,
    isCancellationText,
    pickUserFile: (options?: Parameters<ReturnType<typeof getSdk>['pickUserFile']>[0]) =>
      getSdk().pickUserFile(options),
    openFileManager: (path: string) => getSdk().openFileManager(path),
    loadSettings,
    resolveAuthTargets,
    resolveAuthBase,
    findPathInput,
    findEditButton,
    findFooter,
    isWorkspacePicker,
    fillAndEnter,
    findComposerFileInput,
    findActiveComposerInput,
    addPathsToComposer,
    ensureHostPathBridge,
    getSdk,
    pickPathsDirect,
    isDuplicateWorkspacePath,
  };
}
