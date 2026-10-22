/**
 * The plugin's single host route table.
 *
 * Everything is mounted under one prefix (`/fnos-advanced`), registered on
 * dsh's `webServer` service. Routes are same-origin only; there is no dsh
 * server-side auth to lean on, so the sensitive ones (workspace metadata, image
 * bytes) additionally require a same-origin request.
 */
import type { IncomingMessage, ServerResponse } from 'node:http';
import { VERSION } from '../version.ts';
import { handleAuthResult, RESULT_PATH } from './auth-result.ts';
import { CALLBACK_HTML } from './callback-page.ts';
import {
  queryParam,
  readBody,
  requestPathname,
  sameOriginRequest,
  writeHTML,
  writeJSON,
  writeMethodNotAllowed,
} from './http.ts';
import { handleFileBytes, handleFileToken } from './images.ts';
import { errorMessage } from './paths.ts';
import { DEFAULT_SETTINGS } from '../shared/settings.ts';
import { normalizeSettings, readSettings, writeSettings } from './settings.ts';
import { listWorkspaces, lookupWorkspace, workspaceHealth } from './workspaces.ts';

export const ROUTE_PREFIX = '/fnos-advanced';

const CONFIG_PATH = `${ROUTE_PREFIX}/config`;
const CALLBACK_PATH = `${ROUTE_PREFIX}/auth-callback.html`;
const CALLBACK_PATH_ALT = `${ROUTE_PREFIX}/auth-callback`;
const SETTINGS_PATH = `${ROUTE_PREFIX}/settings`;
const WORKSPACE_PATH = `${ROUTE_PREFIX}/workspace-path`;
const WORKSPACES_PATH = `${ROUTE_PREFIX}/workspaces`;
const WORKSPACE_HEALTH_PATH = `${ROUTE_PREFIX}/workspace-health`;
const FILE_TOKEN_PATH = `${ROUTE_PREFIX}/file-token`;
const FILE_BYTES_PATH = `${ROUTE_PREFIX}/file-bytes`;
const HEALTH_PATH = `${ROUTE_PREFIX}/health`;

const WORKSPACE_ID_RE = /^[A-Za-z0-9_-]{1,80}$/;

/**
 * The fnOS app identity is injected by the system when it starts the app.
 * `Harness` is the shipped console app and also the fallback for source runs.
 */
function appName(): string {
  return process.env.TRIM_APPNAME || 'Harness';
}

/**
 * Optional override for deployments that do not expose the fnOS gateway on the
 * default 5666 port (or that proxy it under a different origin).
 */
function authBaseOverride(): string {
  return process.env.DSH_FNOS_AUTH_BASE || '';
}

/** `GET /workspace-path?id=…` → one workspace's path. */
function handleWorkspacePath(req: IncomingMessage, res: ServerResponse): void {
  if (req.method !== 'GET' && req.method !== 'HEAD') {
    writeMethodNotAllowed(res, 'GET, HEAD');
    return;
  }
  const id = queryParam(req, 'id');
  if (!WORKSPACE_ID_RE.test(id)) {
    writeJSON(res, 400, { ok: false, error: 'invalid workspace id' });
    return;
  }
  const workspace = lookupWorkspace(id);
  if (!workspace) {
    writeJSON(res, 404, { ok: false, error: 'workspace not found' });
    return;
  }
  writeJSON(res, 200, { ok: true, ...workspace });
}

/** `GET /workspaces` → every known workspace. */
function handleWorkspaces(req: IncomingMessage, res: ServerResponse): void {
  if (req.method !== 'GET' && req.method !== 'HEAD') {
    writeMethodNotAllowed(res, 'GET, HEAD');
    return;
  }
  writeJSON(res, 200, { ok: true, workspaces: listWorkspaces() });
}

/** `POST /workspace-health { ids }` → metadata-only read/write checks. */
async function handleWorkspaceHealth(req: IncomingMessage, res: ServerResponse): Promise<void> {
  if (req.method !== 'POST') {
    writeMethodNotAllowed(res, 'POST');
    return;
  }
  if (!sameOriginRequest(req)) {
    writeJSON(res, 403, { ok: false, error: 'cross-origin request rejected' });
    return;
  }

  let body: { ids?: unknown };
  try {
    body = JSON.parse(await readBody(req)) as { ids?: unknown };
  } catch {
    writeJSON(res, 400, { ok: false, error: 'invalid request body' });
    return;
  }

  const requested = Array.isArray(body?.ids) ? (body.ids as unknown[]) : [];
  const ids = requested.length === 0 ? listWorkspaces().map((workspace) => workspace.id) : requested;
  if (ids.length > 200) {
    writeJSON(res, 400, { ok: false, error: 'too many workspaces' });
    return;
  }
  if (ids.some((id) => typeof id !== 'string' || !WORKSPACE_ID_RE.test(id))) {
    writeJSON(res, 400, { ok: false, error: 'invalid workspace id' });
    return;
  }

  const results = [];
  for (const id of ids as string[]) {
    const health = workspaceHealth(id);
    if (health) results.push(health);
  }
  writeJSON(res, 200, { ok: true, results });
}

/** `GET|POST /settings` → read or replace the plugin settings. */
async function handleSettings(req: IncomingMessage, res: ServerResponse): Promise<void> {
  if (req.method === 'GET' || req.method === 'HEAD') {
    writeJSON(res, 200, { ok: true, settings: readSettings(), defaults: DEFAULT_SETTINGS });
    return;
  }
  if (req.method === 'POST') {
    try {
      const settings = normalizeSettings(JSON.parse(await readBody(req)));
      writeSettings(settings);
      writeJSON(res, 200, { ok: true, settings, defaults: DEFAULT_SETTINGS });
    } catch (error) {
      writeJSON(res, 400, { ok: false, error: errorMessage(error, '设置格式无效') });
    }
    return;
  }
  writeMethodNotAllowed(res, 'GET, HEAD, POST');
}

/** Prefix handler registered on dsh's web server. */
export async function handle(req: IncomingMessage, res: ServerResponse): Promise<void> {
  const pathname = requestPathname(req);
  if (pathname === null) {
    writeJSON(res, 400, { ok: false, error: 'bad request url' });
    return;
  }

  if (pathname === RESULT_PATH) {
    await handleAuthResult(req, res);
    return;
  }

  if (pathname === WORKSPACE_PATH) {
    handleWorkspacePath(req, res);
    return;
  }

  if (pathname === WORKSPACES_PATH) {
    handleWorkspaces(req, res);
    return;
  }

  if (pathname === WORKSPACE_HEALTH_PATH) {
    await handleWorkspaceHealth(req, res);
    return;
  }

  if (pathname === SETTINGS_PATH) {
    await handleSettings(req, res);
    return;
  }

  if (pathname === FILE_TOKEN_PATH) {
    await handleFileToken(req, res);
    return;
  }

  if (pathname === FILE_BYTES_PATH) {
    handleFileBytes(req, res);
    return;
  }

  if (req.method !== 'GET' && req.method !== 'HEAD') {
    writeMethodNotAllowed(res, 'GET, HEAD');
    return;
  }

  if (pathname === CONFIG_PATH) {
    writeJSON(res, 200, {
      ok: true,
      appName: appName(),
      authBase: authBaseOverride(),
      version: VERSION,
    });
    return;
  }

  if (pathname === CALLBACK_PATH || pathname === CALLBACK_PATH_ALT) {
    writeHTML(res, 200, CALLBACK_HTML);
    return;
  }

  if (pathname === HEALTH_PATH) {
    writeJSON(res, 200, {
      ok: true,
      appName: appName(),
      version: VERSION,
      features: { filePicker: true, imageRead: readSettings().imageRead, workspaceHealth: true },
    });
    return;
  }

  writeJSON(res, 404, { ok: false, error: 'not found' });
}
