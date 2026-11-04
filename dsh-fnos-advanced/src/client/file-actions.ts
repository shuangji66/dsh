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

/** Plugin actions contributed to dsh's native context menu (no copy). */
export type NativeFileAction = 'open' | 'details' | 'quote';

export function fileRowActions(entryType: string, host: boolean): FileRowAction[] {
  if (entryType !== 'file' && entryType !== 'directory') return [];
  const actions: FileRowAction[] = [];
  if (host) actions.push('open', 'details');
  if (entryType === 'file') actions.push('quote');
  actions.push('copy');
  return actions;
}

/**
 * Which actions to contribute to dsh's native context menu.
 *
 * The native menu already renders "Show in folder" and "Copy path". On a fnOS
 * host the plugin re-wires the native reveal row, so a directory needs no
 * second "open" action; when that adapter is unavailable it falls back to one.
 * A file keeps "Open in fnOS" because opening the file is not the same as
 * revealing its folder.
 */
export function nativeFileActions(
  entryType: string,
  host: boolean,
  revealAdapted: boolean,
): NativeFileAction[] {
  if (entryType !== 'file' && entryType !== 'directory') return [];
  if (!host) return entryType === 'file' ? ['quote'] : [];
  const actions: NativeFileAction[] = [];
  if (entryType === 'file' || !revealAdapted) actions.push('open');
  actions.push('details');
  if (entryType === 'file') actions.push('quote');
  return actions;
}
