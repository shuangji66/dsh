/**
 * Build fnOS picker parameters and compare workspace paths (pure, unit-tested).
 *
 * A picker only carries `directory` / `creatable` when the caller asks for
 * them; `creatable` only applies to the user folder picker.
 *
 * We deliberately do NOT send `disabledPaths` for existing workspaces: fnOS
 * disables the whole subtree of a disabled path, which would also prevent
 * adding a subdirectory as a new workspace. Duplicate protection is an exact
 * path comparison after the user picks, not a picker-level block.
 */

/** Sidebar groups shown by the native folder picker. */
export const SIDEBAR_GROUPS = Object.freeze([
  'myFiles',
  'otherShare',
  'external',
  'remote',
  'favorites',
  'team',
]);

/**
 * @param {{directory?: boolean, creatable?: boolean, title: string, okText: string}} flow
 * @returns {Record<string, unknown>}
 */
export function pickerParams(flow) {
  const params = {
    title: flow.title,
    okText: flow.okText,
    sidebarGroup: [...SIDEBAR_GROUPS],
  };
  if (flow.directory !== undefined) params.directory = flow.directory === true;
  if (flow.creatable) params.creatable = true;
  return params;
}

/**
 * Stable form for exact workspace comparison: strip trailing separators and
 * collapse duplicate slashes. Linux paths are case-sensitive.
 *
 * @param {unknown} value
 * @returns {string}
 */
export function normalizeWorkspacePath(value) {
  const text = String(value == null ? '' : value);
  if (!text) return '';
  return text.replace(/\/+/g, '/').replace(/\/+$/, '') || '/';
}

/**
 * Whether a picked path is already one of the workspace roots.
 *
 * This is an exact match on purpose: `/vol1/work` being a workspace must not
 * prevent adding `/vol1/work/project` as another workspace.
 *
 * @param {unknown} candidate
 * @param {string[]} workspacePaths
 * @returns {boolean}
 */
export function isDuplicateWorkspacePath(candidate, workspacePaths) {
  const target = normalizeWorkspacePath(candidate);
  if (!target || !Array.isArray(workspacePaths)) return false;
  return workspacePaths.some((path) => normalizeWorkspacePath(path) === target);
}
