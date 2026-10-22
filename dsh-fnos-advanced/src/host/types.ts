/**
 * Structural types for the two Cordis services this plugin touches.
 *
 * The plugin deliberately imports no `@deepseek-ai/*` package (host or client):
 * it only relies on the stable `ctx.inject(...)` / `ctx.effect(...)` surface and
 * the services below. Every field is optional so an older dsh that does not
 * provide one degrades instead of throwing at load time.
 */
import type { IncomingMessage, ServerResponse } from 'node:http';

export type Disposer = () => void;

export interface EffectScope {
  /** Register an effect; Cordis runs it now and disposes it on unload. */
  effect?(register: () => Disposer | void, label: string): void;
}

export interface RouteHandler {
  (req: IncomingMessage, res: ServerResponse): void | Promise<void>;
}

export interface WebServerService {
  register(options: { kind: 'prefix'; path: string; handler: RouteHandler }): unknown;
}

export interface WorkspaceRegistryLike {
  list(): unknown;
}

export interface HostScope extends EffectScope {
  webServer?: WebServerService;
  workspaceRegistry?: WorkspaceRegistryLike;
}

/** The subset of the Cordis host context used by `apply()`. */
export interface HostContext {
  inject?(services: string[], callback: (scope: HostScope) => void): void;
}
