/**
 * Plugin settings: validation, defaults and persistence.
 *
 * Settings are small and non-secret, but they still live under `DSH_HOME`
 * (`<dsh home>/fnos-advanced/settings.json`, mode 0600, atomic rename) so a
 * backup of the dsh data directory carries them along.
 */
import { mkdirSync, readFileSync, renameSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { dshHome } from "./paths.js";
import { DEFAULT_SETTINGS, isAuthMode, } from "../shared/settings.js";
/** Read one field from an unknown JSON object. */
function field(input, key) {
    if (typeof input !== 'object' || input === null)
        return undefined;
    return input[key];
}
/** Absolute path of the settings file. */
export function settingsFilePath() {
    return join(dshHome(), 'fnos-advanced', 'settings.json');
}
/** Reverse-proxy origin: protocol + host[:port], path stripped. */
export function normalizeProxyBase(value) {
    const text = String(value ?? '').trim();
    if (!text)
        return '';
    let url;
    try {
        url = new URL(text);
    }
    catch {
        throw new Error('反代地址不是有效 URL');
    }
    if (url.protocol !== 'http:' && url.protocol !== 'https:') {
        throw new Error('反代地址只支持 http/https');
    }
    return url.origin;
}
/** LAN target: a bare port (kept as-is) or an http(s) origin. */
export function normalizeLanTarget(value) {
    const text = String(value ?? '').trim();
    if (!text)
        return DEFAULT_SETTINGS.lanTarget;
    if (/^\d+$/.test(text)) {
        const port = Number(text);
        if (!Number.isInteger(port) || port < 1 || port > 65535) {
            throw new Error('内网端口必须在 1-65535 之间');
        }
        return String(port);
    }
    let url;
    try {
        url = new URL(/^[a-z][a-z0-9+.-]*:\/\//i.test(text) ? text : `http://${text}`);
    }
    catch {
        throw new Error('内网地址格式无效');
    }
    if (url.protocol !== 'http:' && url.protocol !== 'https:') {
        throw new Error('内网地址只支持 http/https');
    }
    return url.origin;
}
/** Strict normalization for a save request: any invalid field rejects it. */
export function normalizeSettings(input) {
    const authMode = field(input, 'authMode');
    return {
        proxyBase: normalizeProxyBase(field(input, 'proxyBase')),
        lanTarget: normalizeLanTarget(field(input, 'lanTarget')),
        authMode: isAuthMode(authMode) ? authMode : DEFAULT_SETTINGS.authMode,
        imageRead: field(input, 'imageRead') !== false,
    };
}
/** Lenient normalization for a stored file: bad fields fall back to defaults. */
export function sanitizeSettings(input) {
    let proxyBase = DEFAULT_SETTINGS.proxyBase;
    let lanTarget = DEFAULT_SETTINGS.lanTarget;
    try {
        proxyBase = normalizeProxyBase(field(input, 'proxyBase'));
    }
    catch { /* keep default */ }
    try {
        lanTarget = normalizeLanTarget(field(input, 'lanTarget'));
    }
    catch { /* keep default */ }
    const authMode = field(input, 'authMode');
    return {
        proxyBase,
        lanTarget,
        authMode: isAuthMode(authMode) ? authMode : DEFAULT_SETTINGS.authMode,
        imageRead: field(input, 'imageRead') !== false,
    };
}
/** Current settings; a missing or unreadable file means "defaults". */
export function readSettings() {
    try {
        const raw = readFileSync(settingsFilePath(), 'utf8');
        return sanitizeSettings(JSON.parse(raw));
    }
    catch {
        return { ...DEFAULT_SETTINGS };
    }
}
/** Atomic write (temp file + rename), private to the dsh user. */
export function writeSettings(settings) {
    const file = settingsFilePath();
    mkdirSync(dirname(file), { recursive: true, mode: 0o700 });
    const tmp = `${file}.${process.pid}.${Date.now()}.tmp`;
    writeFileSync(tmp, `${JSON.stringify({ version: 1, ...settings }, null, 2)}\n`, { mode: 0o600 });
    renameSync(tmp, file);
}
