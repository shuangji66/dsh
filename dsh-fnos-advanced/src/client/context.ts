/**
 * Structural types for the Cordis client context.
 *
 * Like the host half, the browser half imports no `@deepseek-ai/*` package: the
 * only contracts are `ctx.inject(...)`, `ctx.effect(...)` and the services
 * below, all optional so an older dsh degrades instead of throwing.
 */
export type Disposer = () => void;

export interface EffectScope {
  effect?(register: () => Disposer | void, label: string): void;
}

export interface WorkspaceSnapshotItem {
  workspaceId?: unknown;
  path?: unknown;
  title?: unknown;
}

export interface WorkspacesListLike {
  getSnapshot?(): { items?: WorkspaceSnapshotItem[] } | undefined;
  subscribe?(update: () => void): Disposer | void;
}

export interface WorkspacesServiceLike {
  list?: WorkspacesListLike;
}

export interface SessionSnapshotItem {
  cwd?: unknown;
}

export interface SessionListLike {
  getSnapshot?(): { byId?: Record<string, SessionSnapshotItem> } | undefined;
}

export interface SessionsServiceLike {
  list?: SessionListLike;
}

export interface SlotsServiceLike {
  inject(slot: string, callback: () => unknown): void;
  register(
    options: { name: string; key?: string; id?: string; order?: number },
    component: unknown,
  ): unknown;
}

export interface ClientScope extends EffectScope {
  workspaces?: WorkspacesServiceLike;
  sessions?: SessionsServiceLike;
  slots?: SlotsServiceLike;
}

export interface ClientContext extends EffectScope {
  inject?(services: string[], callback: (scope: ClientScope) => void): void;
}
