/**
 * The "fnOS actions" entry rendered through dsh's official slots.
 *
 * dsh's delivery cards, change-review tab and document preview expose slots for
 * per-file actions, so this half does not need to watch the DOM for those
 * surfaces. The slot contract only fixes placement, however: the delivery and
 * review slots hand entries opaque event coordinates (`actionUrl`) rather than a
 * path, so the path still comes from the owning surface's own anchors
 * (`data-presented-file` / `data-review-file`). The document preview slot passes
 * `absolutePath` directly and needs no lookup.
 */
import * as React from 'react';
import { SLOT_ACTION_ATTR, FILE_ACTION_SVG } from './constants.ts';
import { showToast } from './dom.ts';
import { resolveAbsolutePath } from './files.ts';
import { closePathMenu, togglePathMenu } from './path-menu.ts';
import { onLanguageChange, t } from './i18n.ts';
import type { ClientContext } from './context.ts';

/** Slot id used for every entry this plugin registers. */
const SLOT_ID = 'dsh-fnos-advanced';

/** Slot names whose entries open the shared path menu. */
const SLOT_NAMES = [
  'deliverables.file.actions',
  'deliverables.review.file.actions',
  'sidebar.right.tab.document.actions',
  'sidebar.right.tab.document.unpreviewable',
] as const;

interface SlotActionProps {
  /** Set by the document-preview slots. */
  absolutePath?: unknown;
  /** Standard session prop; also available from the sidebar container. */
  sessionId?: unknown;
  /** Opaque event coordinates for delivery / review entries. */
  actionUrl?: unknown;
}

/**
 * Session cwd lookup, installed from the live `sessions` client service.
 *
 * Kept module-level (instead of a React hook) so the component also works in
 * slots that do not forward the standard `useSessions` hook.
 */
let sessionCwd: (sessionId: string) => string = () => '';

/** Session owning the slot entry: the standard prop, else the sidebar container. */
function sessionIdOf(props: SlotActionProps, root: HTMLElement | null): string {
  if (typeof props.sessionId === 'string' && props.sessionId) return props.sessionId;
  return root?.closest<HTMLElement>('[data-sidebar-right-session]')
    ?.getAttribute('data-sidebar-right-session') || '';
}

/** Resolve the absolute path the menu should act on, or an empty string. */
function resolveSlotPath(root: HTMLElement | null, props: SlotActionProps): string {
  if (typeof props.absolutePath === 'string' && props.absolutePath.startsWith('/')) {
    return props.absolutePath;
  }

  // Delivery card: the preview button carries the workspace-resolved path.
  const card = root?.closest('[data-presented-file]');
  const cardPath = card?.querySelector<HTMLElement>(':scope > button[title]')?.getAttribute('title') || '';
  if (cardPath) return resolveAbsolutePath(sessionCwd(sessionIdOf(props, root)), cardPath);

  // Change review: publishes a cwd-relative path (absolute when outside it).
  const review = root?.closest<HTMLElement>('[data-changes-review]');
  if (review) {
    const relative = review.querySelector<HTMLElement>('[data-review-file]')?.getAttribute('data-review-file') || '';
    return resolveAbsolutePath(sessionCwd(sessionIdOf(props, review)), relative);
  }

  return '';
}

/** One "⋯" button; the menu itself is shared with the file tree. */
export function FnosPathAction(props: SlotActionProps): React.ReactElement {
  const anchorRef = React.useRef<HTMLButtonElement>(null);
  const label = t('飞牛操作');
  const [, forceLanguage] = React.useReducer((value: number) => value + 1, 0);

  // Close a menu this (or another) entry opened when the slot goes away.
  React.useEffect(() => () => closePathMenu(), []);
  // Re-render the title/aria-label when "follow fnOS" resolves late.
  React.useEffect(() => onLanguageChange(forceLanguage), [forceLanguage]);

  const onClick = (event: React.MouseEvent<HTMLButtonElement>): void => {
    event.preventDefault();
    event.stopPropagation();
    const anchor = anchorRef.current;
    if (!anchor) return;
    const path = resolveSlotPath(anchor, props);
    if (!path) {
      showToast(t('无法确定文件路径'));
      return;
    }
    togglePathMenu(anchor, { path, entry: 'file' });
  };

  return (
    <button
      ref={anchorRef}
      type="button"
      className="dsh-fnos-slot-action"
      {...{ [SLOT_ACTION_ATTR]: '1' }}
      title={label}
      aria-label={label}
      onMouseDown={(event) => { event.stopPropagation(); }}
      onClick={onClick}
      dangerouslySetInnerHTML={{ __html: FILE_ACTION_SVG }}
    />
  );
}

/**
 * Register the entry on every file-action slot dsh provides. Slots that this
 * dsh version does not declare simply never fire, so the older surfaces keep
 * working unchanged.
 */
export function registerPathActionSlots(ctx: ClientContext): void {
  const inject = ctx?.inject;
  if (typeof inject !== 'function') return;

  inject(['slots'], (scope) => {
    const slots = scope.slots;
    if (!slots || typeof slots.inject !== 'function' || typeof slots.register !== 'function') return;

    const install = (): void => {
      for (const name of SLOT_NAMES) {
        slots.inject(name, () => slots.register({ name, id: SLOT_ID }, FnosPathAction));
      }
    };

    if (typeof scope.effect === 'function') scope.effect(install, 'dsh-fnos-advanced: file actions');
    else install();
  });
}

/** Keep a session cwd resolver for the review tab's relative paths. */
export function registerSessionCwd(ctx: ClientContext): void {
  const inject = ctx?.inject;
  if (typeof inject !== 'function') return;

  inject(['sessions'], (scope) => {
    const list = scope.sessions?.list;
    if (!list || typeof list.getSnapshot !== 'function') return;
    sessionCwd = (sessionId: string): string => {
      if (!sessionId) return '';
      try {
        const cwd = list.getSnapshot?.()?.byId?.[sessionId]?.cwd;
        return typeof cwd === 'string' ? cwd : '';
      } catch {
        return '';
      }
    };
  });
}
