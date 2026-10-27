/**
 * Recently authorized fnOS folders, kept per browser.
 *
 * The list is a convenience for the directory picker: the same NAS folder is
 * usually chosen again and again, and the native picker remembers nothing.
 * It holds absolute paths only and never stores authorization results, so a
 * stale entry can only pre-fill a path — dsh still needs the folder to be
 * readable (the ACL is granted to the app, not to this list).
 */
const STORAGE_KEY = 'dsh-fnos-advanced:recent-dirs';

/** How many folders the picker shows. */
export const MAX_RECENT_DIRS = 8;

/** Normalize one path: absolute, no trailing slash (except the root). */
function normalizePath(value: unknown): string {
  const text = String(value ?? '');
  if (!text.startsWith('/')) return '';
  const trimmed = text.replace(/\/+$/, '');
  return trimmed || '/';
}

/**
 * Put `path` at the front of `list`, drop duplicates, and cap the result.
 * Pure so the ordering rules are unit-tested without a browser.
 */
export function pushRecentPath(
  list: readonly string[],
  path: unknown,
  max = MAX_RECENT_DIRS,
): string[] {
  const value = normalizePath(path);
  if (!value || max <= 0) return list.slice(0, Math.max(0, max));
  const seen = new Set<string>([value]);
  const next = [value];
  for (const item of list) {
    const normalized = normalizePath(item);
    if (!normalized || seen.has(normalized)) continue;
    seen.add(normalized);
    next.push(normalized);
    if (next.length >= max) break;
  }
  return next;
}

/** Read the stored list; malformed payloads are treated as empty. */
export function loadRecentPaths(): string[] {
  try {
    if (typeof window === 'undefined') return [];
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed
      .map(normalizePath)
      .filter(Boolean)
      .slice(0, MAX_RECENT_DIRS);
  } catch {
    return [];
  }
}

/** Record a successful directory pick and return the updated list. */
export function recordRecentPath(path: unknown): string[] {
  const next = pushRecentPath(loadRecentPaths(), path);
  try {
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
    }
  } catch { /* private mode / quota: the picker still works without history */ }
  return next;
}

/** Forget every remembered folder. */
export function clearRecentPaths(): void {
  try {
    if (typeof window !== 'undefined') window.localStorage.removeItem(STORAGE_KEY);
  } catch { /* ignore */ }
}
