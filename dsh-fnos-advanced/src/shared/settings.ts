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

export const DEFAULT_SETTINGS: Readonly<PluginSettings> = Object.freeze({
  proxyBase: '',
  lanTarget: '5666',
  authMode: 'ask',
  imageRead: true,
});

export const AUTH_MODES: readonly AuthMode[] = Object.freeze(['ask', 'proxy', 'lan']);

/** Type guard for values arriving from JSON / the settings form. */
export function isAuthMode(value: unknown): value is AuthMode {
  return typeof value === 'string' && (AUTH_MODES as readonly string[]).includes(value);
}

/** Strip trailing slashes so origins compare byte-for-byte. */
export function normalizeBase(value: unknown): string {
  return String(value ?? '').replace(/\/+$/, '');
}
