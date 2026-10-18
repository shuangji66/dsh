/**
 * Build the fnOS picker parameters from a picker flow (pure, unit-tested).
 *
 * Shared pickers omit `directory` entirely: the host decides what a shared
 * picker returns. `creatable` / `disabledPaths` only apply to the user folder
 * picker; they are forwarded to the native bridge and appended to the web
 * authorization URL by the client.
 */

/** Sidebar groups shown by both folder and shared pickers. */
export const SIDEBAR_GROUPS = Object.freeze([
  'myFiles',
  'otherShare',
  'external',
  'remote',
  'favorites',
  'team',
]);

/**
 * @param {{directory?: boolean, creatable?: boolean, disabledPaths?: string[], title: string, okText: string}} flow
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
  if (Array.isArray(flow.disabledPaths) && flow.disabledPaths.length) {
    params.disabledPaths = [...flow.disabledPaths];
  }
  return params;
}
