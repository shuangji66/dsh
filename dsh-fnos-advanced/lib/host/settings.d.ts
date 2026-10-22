import { type PluginSettings } from '../shared/settings.js';
/** Absolute path of the settings file. */
export declare function settingsFilePath(): string;
/** Reverse-proxy origin: protocol + host[:port], path stripped. */
export declare function normalizeProxyBase(value: unknown): string;
/** LAN target: a bare port (kept as-is) or an http(s) origin. */
export declare function normalizeLanTarget(value: unknown): string;
/** Strict normalization for a save request: any invalid field rejects it. */
export declare function normalizeSettings(input: unknown): PluginSettings;
/** Lenient normalization for a stored file: bad fields fall back to defaults. */
export declare function sanitizeSettings(input: unknown): PluginSettings;
/** Current settings; a missing or unreadable file means "defaults". */
export declare function readSettings(): PluginSettings;
/** Atomic write (temp file + rename), private to the dsh user. */
export declare function writeSettings(settings: PluginSettings): void;
