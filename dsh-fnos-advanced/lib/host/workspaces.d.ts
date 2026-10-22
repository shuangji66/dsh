import type { WorkspaceRegistryLike } from './types.js';
export interface WorkspaceRecord {
    id: string;
    path: string;
    title: string;
}
export interface WorkspaceHealth extends WorkspaceRecord {
    exists: boolean;
    directory: boolean;
    readable: boolean;
    writable: boolean;
    /** `errno` code of the first failing check; empty when fully healthy. */
    error: string;
}
/** Bind / unbind the live registry (the effect disposer passes `null`). */
export declare function setWorkspaceRegistry(registry: WorkspaceRegistryLike | null): void;
/** One workspace, live registry first and the storage file second. */
export declare function lookupWorkspace(id: string): WorkspaceRecord | null;
/** All known workspaces, deduplicated by id (registry wins). */
export declare function listWorkspaces(): WorkspaceRecord[];
/** Metadata-only read/write check for one workspace. */
export declare function workspaceHealth(id: string): WorkspaceHealth | null;
