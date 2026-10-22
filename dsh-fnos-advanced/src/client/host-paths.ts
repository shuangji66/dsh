/**
 * dsh's host-path bridge.
 *
 * dsh decides between "upload these bytes" and "insert an `@path` reference"
 * from `globalThis.__DSH_HOST_PATHS__.pathFor(file)`. The plugin maps the empty
 * `File` objects it hands to the composer back to their real fnOS path.
 *
 * The composer captures the bridge object once, when its slot is injected, so
 * this module installs (or patches) the bridge at plugin load time — long before
 * a file is picked. Replacing a bridge that dsh Desktop already installed would
 * leave dsh holding the old object, hence the in-place patch.
 */

export interface HostPathBridge {
  /** Marker so a second evaluation reuses the same bridge. */
  __dshFnosAdvanced?: boolean;
  /** The store this plugin adds entries to. */
  __dshFnosFiles?: WeakMap<File, string>;
  pathFor?: (file: File) => string;
}

/** Files created by this plugin, when no bridge was installed yet. */
const pluginPaths = new WeakMap<File, string>();

/** The store the bridge dsh captured actually reads. */
let activeStore: WeakMap<File, string> = pluginPaths;

/** Remember the fnOS path behind a `File` handed to the composer. */
export function setHostPath(file: File, path: string): void {
  activeStore.set(file, path);
}

/**
 * Install the bridge, or patch the one another shell installed in place.
 * Returns the bridge that dsh will read.
 */
export function ensureHostPathBridge(): HostPathBridge {
  const existing = globalThis.__DSH_HOST_PATHS__;

  if (existing && existing.__dshFnosAdvanced) {
    activeStore = existing.__dshFnosFiles || activeStore;
    return existing;
  }

  // Patch a bridge that another shell (dsh Desktop) installed *in place*:
  // dsh captures that exact object when the composer mounts, so replacing it
  // with a wrapper would leave the captured reference without our files.
  if (existing && typeof existing === 'object') {
    const original = typeof existing.pathFor === 'function' ? existing.pathFor : null;
    const store = existing.__dshFnosFiles || new WeakMap<File, string>();
    try {
      existing.pathFor = function pathFor(file: File): string {
        const own = store.get(file);
        if (own) return own;
        try {
          return original ? original.call(existing, file) : '';
        } catch {
          return '';
        }
      };
      existing.__dshFnosFiles = store;
      existing.__dshFnosAdvanced = true;
      activeStore = store;
      return existing;
    } catch { /* frozen object: fall back to a wrapper */ }
  }

  const bridge: HostPathBridge = {
    __dshFnosAdvanced: true,
    __dshFnosFiles: pluginPaths,
    pathFor(file: File): string {
      const own = pluginPaths.get(file);
      if (own) return own;
      try {
        return existing && typeof existing.pathFor === 'function' ? existing.pathFor(file) : '';
      } catch {
        return '';
      }
    },
  };

  try {
    globalThis.__DSH_HOST_PATHS__ = bridge;
  } catch { /* frozen global */ }
  activeStore = pluginPaths;
  return bridge;
}

/** Hand `files` to dsh's hidden composer input as a real drop would. */
export function dispatchComposerFiles(input: HTMLInputElement, files: File[]): void {
  const transfer = new DataTransfer();
  for (const file of files) transfer.items.add(file);

  input.files = transfer.files;
  input.dispatchEvent(new Event('change', { bubbles: true }));
}
