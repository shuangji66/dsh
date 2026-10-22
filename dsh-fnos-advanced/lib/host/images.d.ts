import type { IncomingMessage, ServerResponse } from 'node:http';
/** `POST /fnos-advanced/file-token { path }` → one-time download token. */
export declare function handleFileToken(req: IncomingMessage, res: ServerResponse): Promise<void>;
/** `GET|HEAD /fnos-advanced/file-bytes?token=…` → the image bytes, once. */
export declare function handleFileBytes(req: IncomingMessage, res: ServerResponse): void;
