import type { HostContext } from './host/types.js';
export declare const name = "dsh-fnos-advanced";
export type { AuthMode, PluginSettings } from './shared/settings.js';
/**
 * Register the host routes once the web server service is available, and keep
 * the workspace registry bound while the plugin is loaded.
 */
export declare function apply(ctx: HostContext): void;
