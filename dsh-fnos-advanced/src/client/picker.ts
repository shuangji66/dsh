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
export const SIDEBAR_GROUPS: readonly string[] = Object.freeze([
  'myFiles',
  'otherShare',
  'external',
  'remote',
  'favorites',
  'team',
]);

/** Per-surface picker description (directory picker vs composer intake). */
export interface PickerFlowParams {
  directory?: boolean;
  creatable?: boolean;
  multiple?: boolean;
  title: string;
  okText: string;
}

/** Serialized picker parameters (`buildAppAuthUrl` and the native bridge). */
export interface PickerParams {
  title: string;
  okText: string;
  sidebarGroup: string[];
  directory?: boolean;
  creatable?: boolean;
  multiple?: boolean;
}

export function pickerParams(flow: PickerFlowParams): PickerParams {
  const params: PickerParams = {
    title: flow.title,
    okText: flow.okText,
    sidebarGroup: [...SIDEBAR_GROUPS],
  };
  if (flow.directory !== undefined) params.directory = flow.directory === true;
  if (flow.creatable) params.creatable = true;
  if (flow.multiple !== undefined) params.multiple = flow.multiple === true;
  return params;
}

/**
 * Stable form for exact workspace comparison: strip trailing separators and
 * collapse duplicate slashes. Linux paths are case-sensitive.
 */
export function normalizeWorkspacePath(value: unknown): string {
  const text = String(value ?? '');
  if (!text) return '';
  return text.replace(/\/+/g, '/').replace(/\/+$/, '') || '/';
}

/**
 * Whether a picked path is already one of the workspace roots.
 *
 * This is an exact match on purpose: `/vol1/work` being a workspace must not
 * prevent adding `/vol1/work/project` as another workspace.
 */
export function isDuplicateWorkspacePath(candidate: unknown, workspacePaths: readonly string[]): boolean {
  const target = normalizeWorkspacePath(candidate);
  if (!target || !Array.isArray(workspacePaths)) return false;
  return workspacePaths.some((path) => normalizeWorkspacePath(path) === target);
}
