/**
 * Workspace folders offered as one-click entry points in the picker footer.
 *
 * "Authorized" here means "already in use by this dsh": every entry is a live
 * workspace folder, so its access was granted when the workspace was added and
 * the same folder is usually opened again when a subfolder becomes its own
 * workspace. The row only fills the picker's path input and submits it — it
 * never re-authorizes and never checks the ACL, exactly like the recent-folder
 * row. FnOS exposes no API that lists "folders granted to this app", so the
 * workspace registry is the only reliable source.
 */
import { normalizeWorkspacePath } from './picker.ts';
import type { WorkspaceSummary } from './plugin-api.ts';

/** How many workspace folders the picker offers. */
export const MAX_AUTHORIZED_DIRS = 8;

/**
 * Deduplicated absolute workspace paths, in registry order.
 *
 * The registry can repeat a path (two ids pointing at the same folder after a
 * re-add), and non-absolute leftovers are dropped instead of being handed to
 * the picker's path input.
 */
export function pickAuthorizedDirs(
  workspaces: readonly WorkspaceSummary[],
  max = MAX_AUTHORIZED_DIRS,
): string[] {
  if (max <= 0) return [];
  const seen = new Set<string>();
  const paths: string[] = [];
  for (const workspace of workspaces) {
    const path = normalizeWorkspacePath(workspace?.path);
    if (!path.startsWith('/') || seen.has(path)) continue;
    seen.add(path);
    paths.push(path);
    if (paths.length >= max) break;
  }
  return paths;
}
