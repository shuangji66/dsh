/**
 * Settings shared by both halves.
 *
 * The host owns validation and persistence; the client mirrors the shape so the
 * two stay in step. Keep this module dependency-free: it is imported by the
 * Node host and bundled into the browser client.
 */
export const DEFAULT_SETTINGS = Object.freeze({
    proxyBase: '',
    lanTarget: '5666',
    authMode: 'ask',
    imageRead: true,
});
export const AUTH_MODES = Object.freeze(['ask', 'proxy', 'lan']);
/** Type guard for values arriving from JSON / the settings form. */
export function isAuthMode(value) {
    return typeof value === 'string' && AUTH_MODES.includes(value);
}
/** Strip trailing slashes so origins compare byte-for-byte. */
export function normalizeBase(value) {
    return String(value ?? '').replace(/\/+$/, '');
}
