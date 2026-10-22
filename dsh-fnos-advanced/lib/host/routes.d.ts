/**
 * The plugin's single host route table.
 *
 * Everything is mounted under one prefix (`/fnos-advanced`), registered on
 * dsh's `webServer` service. Routes are same-origin only; there is no dsh
 * server-side auth to lean on, so the sensitive ones (workspace metadata, image
 * bytes) additionally require a same-origin request.
 */
import type { IncomingMessage, ServerResponse } from 'node:http';
export declare const ROUTE_PREFIX = "/fnos-advanced";
/** Prefix handler registered on dsh's web server. */
export declare function handle(req: IncomingMessage, res: ServerResponse): Promise<void>;
