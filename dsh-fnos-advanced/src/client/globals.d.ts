/**
 * Ambient globals owned by dsh and by this plugin's own bridge.
 *
 * `Window.__ModuleLoader__` is dsh's client bundle contract; `__dshFnosAdvanced`
 * is a diagnostic handle for real-device consoles. The bridge global is
 * declared as a `var` so `globalThis.__DSH_HOST_PATHS__` typechecks.
 */
import type { HostPathBridge } from './host-paths.ts';

declare global {
  interface Window {
    __ModuleLoader__?: {
      load(options: { id: string; factory: (require: (id: string) => unknown) => unknown }): void;
    };
    /** Debug surface; see index.ts. Not a public API. */
    __dshFnosAdvanced?: Record<string, unknown>;
  }

  var __DSH_HOST_PATHS__: HostPathBridge | undefined;
}

export {};
