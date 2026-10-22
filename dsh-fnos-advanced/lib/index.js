/**
 * dsh-fnos-advanced — host half.
 *
 * Registers one prefix route (`/fnos-advanced`) on dsh's `webServer` service and
 * optionally binds the live `workspaceRegistry` service for workspace lookups.
 * Everything the browser half needs is same-origin; nothing is stored outside
 * the dsh data directory and no credential is persisted.
 *
 * The plugin deliberately imports no `@deepseek-ai/*` package: the only
 * contracts are `ctx.inject(...)`, `ctx.effect(...)` and
 * `webServer.register({ kind, path, handler })`.
 */
import { ROUTE_PREFIX, handle } from "./host/routes.js";
import { setWorkspaceRegistry } from "./host/workspaces.js";
export const name = 'dsh-fnos-advanced';
/**
 * Register the host routes once the web server service is available, and keep
 * the workspace registry bound while the plugin is loaded.
 */
export function apply(ctx) {
    const inject = ctx?.inject;
    if (typeof inject !== 'function')
        return;
    inject(['workspaceRegistry'], (scope) => {
        const registry = scope.workspaceRegistry;
        if (!registry)
            return;
        setWorkspaceRegistry(registry);
        scope.effect?.(() => () => {
            setWorkspaceRegistry(null);
        }, 'dsh-fnos-advanced: workspace registry');
    });
    inject(['webServer'], (scope) => {
        const server = scope.webServer;
        if (!server || typeof server.register !== 'function')
            return;
        const register = () => {
            server.register({
                kind: 'prefix',
                path: ROUTE_PREFIX,
                handler: handle,
            });
        };
        if (typeof scope.effect === 'function')
            scope.effect(register, 'dsh-fnos-advanced: host routes');
        else
            register();
    });
}
