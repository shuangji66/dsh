/**
 * One-shot hand-off of the fnOS authorization result.
 *
 * The callback page POSTs the raw query string it received. Polling that route
 * is the only channel that survives COOP and storage partitioning, so it is
 * kept independent of `window.opener`, `postMessage` and `localStorage` (the
 * callback page uses all three; the host route is the reliable one).
 */
import type { IncomingMessage, ServerResponse } from 'node:http';
export declare const RESULT_PATH = "/fnos-advanced/auth-result";
/**
 * `POST /fnos-advanced/auth-result` publishes one result;
 * `GET|HEAD ?state=…` reads it exactly once.
 */
export declare function handleAuthResult(req: IncomingMessage, res: ServerResponse): Promise<void>;
