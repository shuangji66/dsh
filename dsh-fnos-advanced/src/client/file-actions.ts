/**
 * Which actions one file / directory offers (pure, unit-tested).
 *
 * Shared by the workspace file tree, the delivery cards, the change-review tab
 * and the document preview. "Open in fnOS" and "File details" need the host
 * bridge, so they only exist inside fnOS desktop / the mobile app. "Quote in
 * chat" is a client-side `@path` reference and works anywhere, but only for
 * files: the composer's file input cannot express a folder reference (dsh's own
 * drop path marks folders separately).
 */

export type FileRowAction = 'open' | 'details' | 'quote' | 'copy';

export function fileRowActions(entryType: string, host: boolean): FileRowAction[] {
  if (entryType !== 'file' && entryType !== 'directory') return [];
  const actions: FileRowAction[] = [];
  if (host) actions.push('open', 'details');
  if (entryType === 'file') actions.push('quote');
  actions.push('copy');
  return actions;
}
