/**
 * Session cwd lookup shared by the path menu, the native menu and the slots.
 *
 * The resolver is installed from the live `sessions` client service (see
 * `registerSessionCwd` in `slot-actions.tsx`). It lives in its own module so the
 * native-menu adapter can resolve workspace-relative paths without importing
 * `slot-actions.tsx` back (that module imports the adapter).
 */

let sessionCwdOf: (sessionId: string) => string = () => '';

/** Install the resolver backed by dsh's `sessions` service. */
export function setSessionCwdResolver(resolve: (sessionId: string) => string): void {
  sessionCwdOf = resolve;
}

/** The Session workspace root, or an empty string when it is not known. */
export function sessionCwd(sessionId: string): string {
  return sessionCwdOf(sessionId);
}
