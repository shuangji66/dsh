/**
 * Which Workspace or Session a context menu belongs to.
 *
 * dsh marks the element that owns a contextual menu with `aria-controls` (the
 * same Menu primitive that renders the workspace and session menus), and that
 * owner lives inside the row carrying `data-row-key`. Reading the row key is
 * language-independent and works for both the "..." trigger and right-click.
 */
export type ContextMenuKind = 'workspace' | 'session';

export interface ContextMenuTarget {
  kind: ContextMenuKind;
  id: string;
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
