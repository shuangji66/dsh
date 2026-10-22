/**
 * Small HTTP helpers for the same-origin routes (`/fnos-advanced/*`).
 *
 * No framework: the host half registers one prefix handler on dsh's web server,
 * so requests are plain Node `IncomingMessage` / `ServerResponse`.
 */
import type { IncomingMessage, ServerResponse } from 'node:http';
/** Authorization results and settings payloads are tiny; cap the body. */
export declare const MAX_BODY_BYTES: number;
/** JSON response with the plugin's no-store policy. */
export declare function writeJSON(res: ServerResponse, status: number, body: unknown): void;
/** HTML response for the fnOS authorization callback page. */
export declare function writeHTML(res: ServerResponse, status: number, html: string): void;
/** 405 with an `Allow` header; the body stays empty on purpose. */
export declare function writeMethodNotAllowed(res: ServerResponse, allow: string): void;
/** Read a UTF-8 body, refusing anything above {@link MAX_BODY_BYTES}. */
export declare function readBody(req: IncomingMessage): Promise<string>;
/** An error that carries the HTTP status the route should answer with. */
export declare class HttpError extends Error {
    readonly status: number;
    constructor(status: number, message: string);
}
export declare function httpError(status: number, message: string): HttpError;
export declare function isHttpError(error: unknown): error is HttpError;
/**
 * Only same-origin browsers may call the byte/health routes. This is not
 * authentication (dsh itself has no server-side auth); it keeps the route from
 * becoming a cross-site read from another origin.
 */
export declare function sameOriginRequest(req: IncomingMessage): boolean;
/** First value of a query parameter from a request URL. */
export declare function queryParam(req: IncomingMessage, name: string): string;
/** Pathname of a request URL, or `null` when it cannot be parsed. */
export declare function requestPathname(req: IncomingMessage): string | null;
