import { queryParam, readBody, sameOriginRequest, writeJSON, writeMethodNotAllowed } from "./http.js";
export const RESULT_PATH = '/fnos-advanced/auth-result';
const RESULT_TTL_MS = 5 * 60 * 1000;
const MAX_RESULTS = 200;
/** `status=…&method=…&path=…` plus room for a long serialized path list. */
const MAX_STATE_LENGTH = 200;
const MAX_SEARCH_LENGTH = 10000;
/** state → raw callback query. */
const pendingResults = new Map();
function pruneResults() {
    const now = Date.now();
    for (const [state, entry] of pendingResults) {
        if (now - entry.ts > RESULT_TTL_MS)
            pendingResults.delete(state);
    }
}
/**
 * `POST /fnos-advanced/auth-result` publishes one result;
 * `GET|HEAD ?state=…` reads it exactly once.
 */
export async function handleAuthResult(req, res) {
    pruneResults();
    if (req.method === 'POST') {
        // 写入口只接受同源请求（与其它写入路由同一道闸）：state 虽然不可猜，
        // 但跨站表单没有理由能往这里塞一份「授权结果」。
        if (!sameOriginRequest(req)) {
            writeJSON(res, 403, { ok: false, error: 'cross-origin request rejected' });
            return;
        }
        try {
            const body = JSON.parse(await readBody(req));
            const state = typeof body?.state === 'string' ? body.state : '';
            const search = typeof body?.search === 'string' ? body.search : '';
            if (!state || state.length > MAX_STATE_LENGTH || !search || search.length > MAX_SEARCH_LENGTH) {
                writeJSON(res, 400, { ok: false, error: 'invalid authorization result' });
                return;
            }
            if (pendingResults.size >= MAX_RESULTS) {
                const oldest = pendingResults.keys().next().value;
                if (oldest !== undefined)
                    pendingResults.delete(oldest);
            }
            pendingResults.set(state, { search, ts: Date.now() });
            writeJSON(res, 200, { ok: true });
        }
        catch {
            writeJSON(res, 400, { ok: false, error: 'invalid authorization result' });
        }
        return;
    }
    if (req.method === 'GET' || req.method === 'HEAD') {
        const state = queryParam(req, 'state');
        const entry = state ? pendingResults.get(state) : undefined;
        if (!entry) {
            writeJSON(res, 404, { ok: false, error: 'not found' });
            return;
        }
        pendingResults.delete(state);
        writeJSON(res, 200, { ok: true, state, search: entry.search });
        return;
    }
    writeMethodNotAllowed(res, 'GET, HEAD, POST');
}
