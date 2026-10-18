/**
 * Which actions a workspace file-tree row offers (pure, unit-tested).
 *
 * "Open in fnOS" needs the host bridge, so it only exists inside fnOS desktop /
 * the mobile app. "Quote in chat" is a client-side `@path` reference and works
 * anywhere, but only for files: the composer's file input cannot express a
 * folder reference (dsh's own drop path marks folders separately).
 */

/**
 * @param {'file' | 'directory' | 'other' | string} entryType
 * @param {boolean} host
 * @returns {Array<'open' | 'quote' | 'copy'>}
 */
export function fileRowActions(entryType, host) {
  if (entryType !== 'file' && entryType !== 'directory') return [];
  const actions = [];
  if (host) actions.push('open');
  if (entryType === 'file') actions.push('quote');
  actions.push('copy');
  return actions;
}
