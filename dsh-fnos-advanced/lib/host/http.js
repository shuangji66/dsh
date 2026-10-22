/** Authorization results and settings payloads are tiny; cap the body. */
export const MAX_BODY_BYTES = 64 * 1024;
/** JSON response with the plugin's no-store policy. */
export function writeJSON(res, status, body) {
    const text = JSON.stringify(body);
    res.writeHead(status, {
        'Content-Type': 'application/json; charset=utf-8',
        'Cache-Control': 'no-store',
        'Content-Length': Buffer.byteLength(text),
    });
    res.end(text);
}
/** HTML response for the fnOS authorization callback page. */
export function writeHTML(res, status, html) {
    res.writeHead(status, {
        'Content-Type': 'text/html; charset=utf-8',
        'Cache-Control': 'no-store',
        'Content-Security-Policy': "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'",
        'Referrer-Policy': 'no-referrer',
    });
    res.end(html);
}
/** 405 with an `Allow` header; the body stays empty on purpose. */
export function writeMethodNotAllowed(res, allow) {
    res.writeHead(405, { Allow: allow, 'Cache-Control': 'no-store' });
    res.end();
}
/** Read a UTF-8 body, refusing anything above {@link MAX_BODY_BYTES}. */
export function readBody(req) {
    return new Promise((resolve, reject) => {
        let size = 0;
        let tooLarge = false;
        const chunks = [];
        req.on('data', (chunk) => {
            size += chunk.length;
            if (size > MAX_BODY_BYTES) {
                tooLarge = true;
                chunks.length = 0;
                return;
            }
            if (!tooLarge)
                chunks.push(chunk);
        });
        req.on('end', () => {
            if (tooLarge) {
                reject(new Error('request body too large'));
                return;
            }
            resolve(Buffer.concat(chunks).toString('utf8'));
        });
        req.on('error', reject);
    });
}
/** An error that carries the HTTP status the route should answer with. */
export class HttpError extends Error {
    status;
    constructor(status, message) {
        super(message);
        this.name = 'HttpError';
        this.status = status;
    }
}
export function httpError(status, message) {
    return new HttpError(status, message);
}
export function isHttpError(error) {
    return error instanceof HttpError;
}
/**
 * Only same-origin browsers may call the byte/health routes. This is not
 * authentication (dsh itself has no server-side auth); it keeps the route from
 * becoming a cross-site read from another origin.
 */
export function sameOriginRequest(req) {
    const site = String(req.headers['sec-fetch-site'] || '');
    // Modern browsers label the request; same-origin/none is enough and avoids
    // depending on how each gateway rewrites Origin/Referer.
    if (site === 'same-origin' || site === 'none')
        return true;
    if (site)
        return false;
    // Harness' reverse proxy rewrites Host/Origin to the dsh upstream but leaves
    // Referer pointing at the public origin, so accept the forwarded host too.
    const hosts = new Set([
        String(req.headers.host || ''),
        String(req.headers['x-forwarded-host'] || ''),
    ].filter(Boolean));
    for (const header of ['origin', 'referer']) {
        const value = String(req.headers[header] || '');
        if (!value)
            continue;
        let valueHost;
        try {
            valueHost = new URL(value).host;
        }
        catch {
            return false;
        }
        if (!hosts.has(valueHost))
            return false;
    }
    return true;
}
/** First value of a query parameter from a request URL. */
export function queryParam(req, name) {
    try {
        return new URL(req.url || '/', 'http://localhost').searchParams.get(name) || '';
    }
    catch {
        return '';
    }
}
/** Pathname of a request URL, or `null` when it cannot be parsed. */
export function requestPathname(req) {
    try {
        return new URL(req.url || '/', 'http://localhost').pathname;
    }
    catch {
        return null;
    }
}
