/**
 * Workspace lookup and metadata-only access checks.
 *
 * dsh's live `workspaceRegistry` service is authoritative; the on-disk storage
 * (`<dsh home>/storages/workspace.json`) is the fallback for a route call that
 * arrives before the service is injected (and for older builds). Everything here
 * reads metadata only — no workspace file content is opened.
 */
import { accessSync, constants, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { dshHome, errorCode } from "./paths.js";
/** The live registry, while the host has it injected. */
let workspaceRegistry = null;
/** Bind / unbind the live registry (the effect disposer passes `null`). */
export function setWorkspaceRegistry(registry) {
    workspaceRegistry = registry;
}
/** Turn one registry entry into a workspace record, or `null`. */
function toRecord(value) {
    if (typeof value !== 'object' || value === null)
        return null;
    const entry = value;
    if (entry.id === null || entry.id === undefined)
        return null;
    const id = String(entry.id);
    if (!id || typeof entry.path !== 'string' || !entry.path)
        return null;
    return { id, path: entry.path, title: typeof entry.title === 'string' ? entry.title : '' };
}
function registryRecords() {
    try {
        if (!workspaceRegistry || typeof workspaceRegistry.list !== 'function')
            return [];
        const list = workspaceRegistry.list();
        if (!Array.isArray(list))
            return [];
        return list.map(toRecord).filter((record) => record !== null);
    }
    catch {
        return [];
    }
}
/** One workspace from the disk registry. */
function workspaceFromDisk(id) {
    try {
        const file = join(dshHome(), 'storages', 'workspace.json');
        const data = JSON.parse(readFileSync(file, 'utf8'));
        const record = data?.tables?.workspaces?.[id];
        if (record && typeof record.path === 'string') {
            return { id, path: record.path, title: typeof record.title === 'string' ? record.title : '' };
        }
    }
    catch { /* storage may be absent or mid-write */ }
    return null;
}
/** One workspace, live registry first and the storage file second. */
export function lookupWorkspace(id) {
    const found = registryRecords().find((workspace) => workspace.id === id);
    return found ?? workspaceFromDisk(id);
}
/** All known workspaces, deduplicated by id (registry wins). */
export function listWorkspaces() {
    const found = new Map();
    for (const record of registryRecords())
        found.set(record.id, record);
    try {
        const file = join(dshHome(), 'storages', 'workspace.json');
        const data = JSON.parse(readFileSync(file, 'utf8'));
        const records = data?.tables?.workspaces;
        if (records && typeof records === 'object') {
            for (const [id, record] of Object.entries(records)) {
                if (found.has(id))
                    continue;
                if (!record || typeof record.path !== 'string' || !record.path)
                    continue;
                found.set(id, {
                    id,
                    path: record.path,
                    title: typeof record.title === 'string' ? record.title : '',
                });
            }
        }
    }
    catch { /* storage may be absent or mid-write */ }
    return [...found.values()];
}
/** Metadata-only read/write check for one workspace. */
export function workspaceHealth(id) {
    const workspace = lookupWorkspace(id);
    if (!workspace)
        return null;
    const result = {
        id: workspace.id,
        path: workspace.path,
        title: workspace.title,
        exists: false,
        directory: false,
        readable: false,
        writable: false,
        error: '',
    };
    try {
        const stat = statSync(workspace.path);
        result.exists = true;
        result.directory = stat.isDirectory();
        if (!result.directory) {
            result.error = 'ENOTDIR';
            return result;
        }
    }
    catch (error) {
        result.error = errorCode(error, 'ENOENT');
        return result;
    }
    try {
        accessSync(workspace.path, constants.R_OK);
        result.readable = true;
    }
    catch (error) {
        result.error = errorCode(error, 'EACCES');
    }
    try {
        accessSync(workspace.path, constants.W_OK);
        result.writable = true;
    }
    catch (error) {
        if (!result.error)
            result.error = errorCode(error, 'EACCES');
    }
    return result;
}
