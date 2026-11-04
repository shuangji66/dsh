/**
 * dsh 0.2.2's shared context menu, which the file-tree "⋯" entry now reuses.
 *
 * dsh 0.2.2 exposes a `contextMenus` client service: plugins contribute actions
 * for `file` / `directory` targets, and any surface can open the one shared
 * popup for a captured target. The file tree already renders the same target
 * anchors (`data-files-entry` / `data-files-path`) and, since 0.2.2, a native
 * right-click menu on those rows. Opening the shared popup from our injected
 * "⋯" button keeps both entry points identical by construction instead of
 * maintaining a second menu with drifting labels.
 *
 * Older dsh releases have no such service. Everything here degrades to `null` /
 * `false`, and the caller keeps using the plugin's own menu.
 */
import { quotePathToComposer } from './composer.ts';
import { LOG_PREFIX } from './constants.ts';
import type { ClientContext, NativeContextMenusLike, NativeMenuActionLike } from './context.ts';
import { nativeFileActions, type NativeFileAction } from './file-actions.ts';
import { t } from './i18n.ts';
import { activePathTarget, type ActivePathTarget, type PathMenuTarget } from './menu-target.ts';
import { openPathInFnos, showPathDetails } from './path-menu.ts';
import { isFnosHost } from './sdk.ts';

let service: NativeContextMenusLike | null = null;

/** The service may arrive after the plugin's first scan; it is optional. */
export function connectNativeContextMenus(ctx: ClientContext): void {
  const inject = ctx?.inject;
  if (typeof inject !== 'function') return;

  inject(['contextMenus'], (scope) => {
    const menus = scope.contextMenus;
    if (!menus || typeof menus.register !== 'function' || typeof menus.open !== 'function') return;
    service = menus;

    const install = (): (() => void) => {
      const disposers: Array<() => void> = [];
      const register = (target: 'file' | 'directory'): void => {
        try {
          disposers.push(menus.register({
            id: `dsh-fnos-advanced.${target}`,
            target,
            order: 300,
            resolve: (input, signal) => {
              signal.throwIfAborted();
              const path = typeof input?.path === 'string' ? input.path : '';
              return path ? actionsFor(target, path) : [];
            },
          }));
        } catch (error) {
          // A duplicate id after a hot reload must not take the plugin down;
          // the injected "⋯" button falls back to the local menu.
          console.warn(LOG_PREFIX, 'native context menu registration failed', target, error);
        }
      };
      register('directory');
      register('file');
      return () => {
        for (const dispose of disposers.splice(0)) {
          try { dispose(); } catch { /* ignore */ }
        }
        if (service === menus) service = null;
      };
    };

    if (typeof scope.effect === 'function') {
      scope.effect(install, 'dsh-fnos-advanced: native context menu');
    } else {
      install();
    }
  });
}

/**
 * Open dsh's shared menu for a file-tree / slot "⋯" entry.
 *
 * @returns false when this dsh has no menu service (or opening threw), so the
 * caller can render the plugin's own menu instead.
 */
export function openNativePathMenu(anchor: HTMLElement, target: PathMenuTarget): boolean {
  const menus = service;
  if (!menus || typeof menus.open !== 'function') return false;
  try {
    const rect = anchor.getBoundingClientRect();
    const value = target.kind === 'file'
      ? {
        sessionId: target.sessionId,
        path: target.path,
        ...(target.paneId ? { paneId: target.paneId } : {}),
      }
      : { sessionId: target.sessionId, path: target.path };
    menus.open({ kind: target.kind, value }, { x: rect.left, y: rect.bottom + 2 });
    return true;
  } catch (error) {
    console.warn(LOG_PREFIX, 'native context menu open failed', error);
    return false;
  }
}

/**
 * Path of the currently open native context menu (file/directory targets only).
 *
 * The menu adapter uses this to re-wire dsh's disabled "Show in folder" row and
 * to route "Copy path" through the plugin's HTTP clipboard fallback. Null when
 * no native menu is open or the service predates the `active` snapshot.
 */
export function activeNativeTarget(): ActivePathTarget | null {
  const menus = service;
  if (!menus?.active?.getSnapshot) return null;
  try {
    return activePathTarget(menus.active.getSnapshot());
  } catch {
    return null;
  }
}

/** Whether the open menu can be adapted through the `active` snapshot. */
export function canAdaptNativeReveal(): boolean {
  return Boolean(service?.active?.getSnapshot);
}

/** Build the actions for one native contribution; resolved when the menu opens. */
function actionsFor(kind: 'file' | 'directory', path: string): NativeMenuActionLike[] {
  const host = isFnosHost();
  const revealAdapted = canAdaptNativeReveal();
  return nativeFileActions(kind, host, revealAdapted).map((action) => actionFor(action, kind, path));
}

function actionFor(
  action: NativeFileAction,
  kind: 'file' | 'directory',
  path: string,
): NativeMenuActionLike {
  if (action === 'open') {
    return {
      id: 'open',
      label: t('在飞牛中打开'),
      failureLabel: t('在飞牛中打开失败，当前宿主可能不支持该操作。'),
      run: () => openPathInFnos(path, kind === 'directory'),
    };
  }
  if (action === 'details') {
    return {
      id: 'details',
      label: t('文件详情'),
      failureLabel: t('无法显示文件详情，当前宿主可能不支持该操作。'),
      run: () => showPathDetails(path),
    };
  }
  return {
    id: 'quote',
    label: t('引用到对话'),
    run: () => quotePathToComposer(path),
  };
}
