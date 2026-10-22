/**
 * One-shot hand-off of the fnOS authorization result.
 *
 * The callback page POSTs the raw query string it received. Polling that route
 * is the only channel that survives COOP and storage partitioning, so it is
 * kept independent of `window.opener`, `postMessage` and `localStorage` (the
 * callback page uses all three; the host route is the reliable one).
 */
import type { IncomingMessage, ServerResponse } from 'node:http';
import { queryParam, readBody, writeJSON, writeMethodNotAllowed } from './http.ts';

export const RESULT_PATH = '/fnos-advanced/auth-result';

const RESULT_TTL_MS = 5 * 60 * 1000;
const MAX_RESULTS = 200;
/** `status=…&method=…&path=…` plus room for a long serialized path list. */
const MAX_STATE_LENGTH = 200;
const MAX_SEARCH_LENGTH = 10000;

interface PendingResult {
  search: string;
  ts: number;
}

/** state → raw callback query. */
const pendingResults = new Map<string, PendingResult>();

function pruneResults(): void {
  const now = Date.now();
  for (const [state, entry] of pendingResults) {
    if (now - entry.ts > RESULT_TTL_MS) pendingResults.delete(state);
  }
}

/**
 * `POST /fnos-advanced/auth-result` publishes one result;
 * `GET|HEAD ?state=…` reads it exactly once.
 */
export async function handleAuthResult(req: IncomingMessage, res: ServerResponse): Promise<void> {
  pruneResults();

  if (req.method === 'POST') {
    try {
      const body = JSON.parse(await readBody(req)) as { state?: unknown; search?: unknown };
      const state = typeof body?.state === 'string' ? body.state : '';
      const search = typeof body?.search === 'string' ? body.search : '';
      if (!state || state.length > MAX_STATE_LENGTH || !search || search.length > MAX_SEARCH_LENGTH) {
        writeJSON(res, 400, { ok: false, error: 'invalid authorization result' });
        return;
      }
      if (pendingResults.size >= MAX_RESULTS) {
        const oldest = pendingResults.keys().next().value;
        if (oldest !== undefined) pendingResults.delete(oldest);
      }
      pendingResults.set(state, { search, ts: Date.now() });
      writeJSON(res, 200, { ok: true });
    } catch {
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
