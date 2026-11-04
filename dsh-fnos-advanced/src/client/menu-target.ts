/**
 * Context-menu target helpers.
 *
 * Workspace and Session menus are identified by the row key of the element that
 * owns their `aria-controls` bridge. File-tree and file-action menus use dsh's
 * native context-menu targets instead (`{ kind, value }`); the path and session
 * come from the row's `data-*` anchors. Both shapes are pure data so the DOM
 * scanners can share one menu adapter.
 */
export type ContextMenuKind = 'workspace' | 'session';

export type ContextMenuTarget =
  | { kind: 'workspace'; id: string }
  | { kind: 'session'; id: string };

/** An absolute path acted on by the file tree / file-action "⋯" menus. */
export interface PathMenuTarget {
  kind: 'file' | 'directory';
  sessionId: string;
  path: string;
  /** Sidebar pane that owns the row; native file targets carry it when known. */
  paneId?: string;
}

/** The path half of a captured native target (file / directory only). */
export interface ActivePathTarget {
  kind: 'file' | 'directory';
  path: string;
}

/** Parse a row key (`workspace:<id>` / `session:<id>`) into a menu target. */
export function contextFromRowKey(key: string | null | undefined): ContextMenuTarget | null {
  const value = typeof key === 'string' ? key : '';
  if (value.startsWith('workspace:')) {
    const id = value.slice('workspace:'.length);
    return id ? { kind: 'workspace', id } : null;
  }
  if (value.startsWith('session:')) {
    const id = value.slice('session:'.length);
    return id ? { kind: 'session', id } : null;
  }
  return null;
}

/**
 * Build the native context-menu target for a file-tree row or a slot entry.
 *
 * dsh's native menu needs the owning session; without it the older DOM menu is
 * still usable, so callers treat `null` as "fall back".
 */
export function pathMenuTarget(
  entryType: string,
  path: string,
  sessionId: string,
  paneId?: string,
): PathMenuTarget | null {
  if (entryType !== 'file' && entryType !== 'directory') return null;
  if (!path || !sessionId) return null;
  const target: PathMenuTarget = { kind: entryType, sessionId, path };
  if (entryType === 'file' && paneId) target.paneId = paneId;
  return target;
}

/** Read one captured `{ kind, value: { path } }` target from dsh's menu store. */
export function activePathTarget(value: unknown): ActivePathTarget | null {
  if (!value || typeof value !== 'object') return null;
  const kind = (value as { kind?: unknown }).kind;
  const target = (value as { value?: unknown }).value;
  if (kind !== 'file' && kind !== 'directory') return null;
  if (!target || typeof target !== 'object') return null;
  const path = (target as { path?: unknown }).path;
  if (typeof path !== 'string' || !path) return null;
  return { kind, path };
}
