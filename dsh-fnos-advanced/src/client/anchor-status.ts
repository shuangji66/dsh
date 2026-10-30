/**
 * Diagnostics: where the DOM anchors were last seen.
 *
 * The plugin-center settings page is not the conversation page: dsh unmounts the
 * conversation (composer and right sidebar) while the plugin page is open, so a
 * live `document.querySelector` there can never find either anchor. The scanners
 * that run on the conversation page record their observation here instead, and
 * the panel reports the last one with its time. That is the difference between
 * "this browser has not opened the conversation yet" and "the composer region is
 * there but its file input is gone" (a dsh markup change).
 */

export interface AnchorObservation {
  /** Epoch milliseconds of the observation. */
  at: number;
  /** Whether the expected child anchor was present. */
  found: boolean;
  /** Matched rows (the file tree only). */
  count?: number;
  /** The container was mounted but had no rows at all (an empty folder). */
  empty?: boolean;
}

export type AnchorName = 'composer' | 'fileTree';

export type AnchorKind = 'never' | 'found' | 'missing' | 'empty';

const observations: Record<AnchorName, AnchorObservation | null> = {
  composer: null,
  fileTree: null,
};

/**
 * The composer region must contain dsh's multi-file input; the picker attaches
 * picked paths to exactly that input. No region means "not on the conversation
 * page" and returns null so an earlier observation is kept.
 */
export function pickComposerObservation(
  regions: readonly { hasMultiFileInput: boolean }[],
  at: number,
): AnchorObservation | null {
  if (!regions.length) return null;
  return { at, found: regions.some((region) => region.hasMultiFileInput) };
}

/**
 * The file tree's real contract is one `data-files-entry` / `data-files-path`
 * per row. With zero rows, `rowLike` separates a genuinely empty folder (nothing
 * to inject into, so nothing to report) from rows whose anchors disappeared
 * (markup changed).
 */
export function pickFileTreeObservation(
  trees: readonly { rows: number; rowLike: number }[],
  at: number,
): AnchorObservation | null {
  if (!trees.length) return null;
  const rows = trees.reduce((total, tree) => total + tree.rows, 0);
  if (rows > 0) return { at, found: true, count: rows };
  const rowLike = trees.reduce((total, tree) => total + tree.rowLike, 0);
  return { at, found: false, empty: rowLike === 0 };
}

export function classifyAnchor(observation: AnchorObservation | null): AnchorKind {
  if (!observation) return 'never';
  if (observation.found) return 'found';
  return observation.empty ? 'empty' : 'missing';
}

export function rememberComposerObservation(observation: AnchorObservation | null): void {
  if (observation) observations.composer = observation;
}

export function rememberFileTreeObservation(observation: AnchorObservation | null): void {
  if (observation) observations.fileTree = observation;
}

export function anchorObservation(name: AnchorName): AnchorObservation | null {
  return observations[name];
}

const RECORD_MIN_INTERVAL_MS = 750;
let lastRecordAt = Number.NEGATIVE_INFINITY;

/**
 * Re-read both anchors from the current document. Absent containers are a
 * no-op: only the page that actually renders them can judge them.
 *
 * Throttled: the caller runs this from the 80 ms-debounced mutation scan, and a
 * streaming reply keeps the DOM mutating continuously — two full-document
 * queries per scan bought nothing (the panel only shows "last seen + time").
 * `resetAnchorRecordingForTest` drops the throttle for unit tests.
 */
export function recordAnchorObservations(now: number = Date.now()): void {
  if (now - lastRecordAt < RECORD_MIN_INTERVAL_MS) return;
  lastRecordAt = now;
  const at = now;
  rememberComposerObservation(pickComposerObservation(
    Array.from(document.querySelectorAll('[data-conversation-region="composer"]'), (region) => ({
      hasMultiFileInput: Boolean(region.querySelector('input[type="file"][multiple]')),
    })),
    at,
  ));
  rememberFileTreeObservation(pickFileTreeObservation(
    Array.from(document.querySelectorAll('[data-files-root]'), (root) => ({
      rows: root.querySelectorAll('[data-files-entry][data-files-path]').length,
      // Row-shaped elements without the anchors mean the markup changed. `li` is
      // dsh's row element in every version this plugin has shipped against, but
      // `[data-files-entry]` alone already proves "rows exist" even if the tag
      // changes — without it a renamed element was reported as an empty folder.
      rowLike: root.querySelectorAll('[data-files-entry], li, [role="treeitem"]').length,
    })),
    at,
  ));
}

/** Test hook: forget the throttle window and every recorded observation. */
export function resetAnchorRecordingForTest(): void {
  lastRecordAt = Number.NEGATIVE_INFINITY;
  observations.composer = null;
  observations.fileTree = null;
}
