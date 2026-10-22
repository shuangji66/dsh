/**
 * Settings shared by both halves.
 *
 * The host owns validation and persistence; the client mirrors the shape so the
 * two stay in step. Keep this module dependency-free: it is imported by the
 * Node host and bundled into the browser client.
 */
/** How an out-of-gateway browser picks its fnOS authorization entry. */
export type AuthMode = 'ask' | 'proxy' | 'lan';
export interface PluginSettings {
    /** Reverse-proxy origin of the fnOS gateway (`https://fnos.example.com`). */
    proxyBase: string;
    /** LAN port (`5666`) or origin (`http://192.168.1.10:5666`). */
    lanTarget: string;
    authMode: AuthMode;
    /** Read picked image bytes so the model can see them. */
    imageRead: boolean;
}
export declare const DEFAULT_SETTINGS: Readonly<PluginSettings>;
export declare const AUTH_MODES: readonly AuthMode[];
/** Type guard for values arriving from JSON / the settings form. */
export declare function isAuthMode(value: unknown): value is AuthMode;
/** Strip trailing slashes so origins compare byte-for-byte. */
export declare function normalizeBase(value: unknown): string;
