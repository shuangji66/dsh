/**
 * dsh-fnos-advanced — host half.
 *
 * The host half deliberately imports nothing: it only owns two same-origin
 * routes for the browser half:
 *
 *   GET /fnos-advanced/config              → { appName, authBase, version }
 *   GET /fnos-advanced/auth-callback.html  → fnOS authorization callback page
 *
 * The callback page is the bridge between the fnOS authorization popup and the
 * dsh page. It parses the query string written by fnOS, posts the result to the
 * opener and also stores it in localStorage, so the picker still receives the
 * result when a browser (or COOP) drops the opener relation.
 */

import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const ROUTE_PREFIX = '/fnos-advanced';
const CONFIG_PATH = `${ROUTE_PREFIX}/config`;
const CALLBACK_PATH = `${ROUTE_PREFIX}/auth-callback.html`;
const CALLBACK_PATH_ALT = `${ROUTE_PREFIX}/auth-callback`;
const RESULT_PATH = `${ROUTE_PREFIX}/auth-result`;
const SETTINGS_PATH = `${ROUTE_PREFIX}/settings`;
const WORKSPACE_PATH = `${ROUTE_PREFIX}/workspace-path`;
const HEALTH_PATH = `${ROUTE_PREFIX}/health`;

/** Package version shown by the config endpoint; keep in sync with package.json. */
const VERSION = '0.2.4';

/**
 * The fnOS app identity is injected by the system when it starts the app.
 * `Harness` is the shipped console app and also the fallback for source runs.
 */
/** Optional live host registry; the storage file is only a fallback. */
let workspaceRegistry = null;

function appName() {
  return process.env.TRIM_APPNAME || 'Harness';
}

/**
 * Optional override for deployments that do not expose the fnOS gateway on the
 * default 5666 port (or that proxy it under a different origin).
 */
function authBaseOverride() {
  return process.env.DSH_FNOS_AUTH_BASE || '';
}

/** In-memory hand-off for the authorization result (state → raw callback query). */
const pendingResults = new Map();
const RESULT_TTL_MS = 5 * 60 * 1000;
const MAX_RESULT_BODY_BYTES = 64 * 1024;

function pruneResults() {
  const now = Date.now();
  for (const [state, entry] of pendingResults) {
    if (now - entry.ts > RESULT_TTL_MS) pendingResults.delete(state);
  }
}

function readBody(req) {
  return new Promise((resolve, reject) => {
    let size = 0;
    let tooLarge = false;
    const chunks = [];
    req.on('data', (chunk) => {
      size += chunk.length;
      if (size > MAX_RESULT_BODY_BYTES) {
        tooLarge = true;
        chunks.length = 0;
        return;
      }
      if (!tooLarge) chunks.push(chunk);
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

const DEFAULT_SETTINGS = Object.freeze({
  proxyBase: '',
  lanTarget: '5666',
  authMode: 'ask',
});

/** Settings are small and non-secret, but still live under DSH_HOME. */
function settingsFilePath() {
  const home = process.env.DSH_HOME || path.join(os.homedir(), '.dsh');
  return path.join(home, 'fnos-advanced', 'settings.json');
}

function normalizeProxyBase(value) {
  const text = String(value ?? '').trim();
  if (!text) return '';
  let url;
  try {
    url = new URL(text);
  } catch {
    throw new Error('反代地址不是有效 URL');
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') {
    throw new Error('反代地址只支持 http/https');
  }
  return url.origin;
}

function normalizeLanTarget(value) {
  const text = String(value ?? '').trim();
  if (!text) return DEFAULT_SETTINGS.lanTarget;
  if (/^\d+$/.test(text)) {
    const port = Number(text);
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      throw new Error('内网端口必须在 1-65535 之间');
    }
    return String(port);
  }
  let url;
  try {
    url = new URL(/^[a-z][a-z0-9+.-]*:\/\//i.test(text) ? text : `http://${text}`);
  } catch {
    throw new Error('内网地址格式无效');
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') {
    throw new Error('内网地址只支持 http/https');
  }
  return url.origin;
}

function normalizeSettings(input) {
  const authMode = ['ask', 'proxy', 'lan'].includes(input?.authMode) ? input.authMode : DEFAULT_SETTINGS.authMode;
  return {
    proxyBase: normalizeProxyBase(input?.proxyBase),
    lanTarget: normalizeLanTarget(input?.lanTarget),
    authMode,
  };
}

function sanitizeSettings(input) {
  let proxyBase = DEFAULT_SETTINGS.proxyBase;
  let lanTarget = DEFAULT_SETTINGS.lanTarget;
  try { proxyBase = normalizeProxyBase(input?.proxyBase); } catch { /* keep default */ }
  try { lanTarget = normalizeLanTarget(input?.lanTarget); } catch { /* keep default */ }
  const authMode = ['ask', 'proxy', 'lan'].includes(input?.authMode)
    ? input.authMode
    : DEFAULT_SETTINGS.authMode;
  return { proxyBase, lanTarget, authMode };
}

function readSettings() {
  try {
    const raw = fs.readFileSync(settingsFilePath(), 'utf8');
    return sanitizeSettings(JSON.parse(raw));
  } catch {
    return { ...DEFAULT_SETTINGS };
  }
}

function writeSettings(settings) {
  const file = settingsFilePath();
  fs.mkdirSync(path.dirname(file), { recursive: true, mode: 0o700 });
  const tmp = `${file}.${process.pid}.${Date.now()}.tmp`;
  fs.writeFileSync(tmp, `${JSON.stringify({ version: 1, ...settings }, null, 2)}\n`, { mode: 0o600 });
  fs.renameSync(tmp, file);
}

function workspaceFromDisk(id) {
  try {
    const home = process.env.DSH_HOME || path.join(os.homedir(), '.dsh');
    const file = path.join(home, 'storages', 'workspace.json');
    const data = JSON.parse(fs.readFileSync(file, 'utf8'));
    const record = data?.tables?.workspaces?.[id];
    if (record && typeof record.path === 'string') {
      return { id, path: record.path, title: typeof record.title === 'string' ? record.title : '' };
    }
  } catch { /* storage may be absent or mid-write */ }
  return null;
}

function lookupWorkspace(id) {
  try {
    const list = workspaceRegistry && typeof workspaceRegistry.list === 'function'
      ? workspaceRegistry.list()
      : [];
    const found = list.find((workspace) => String(workspace.id) === id);
    if (found && typeof found.path === 'string') {
      return { id, path: found.path, title: typeof found.title === 'string' ? found.title : '' };
    }
  } catch { /* fall through to the disk registry */ }
  return workspaceFromDisk(id);
}

function writeJSON(res, status, body) {
  const text = JSON.stringify(body);
  res.writeHead(status, {
    'Content-Type': 'application/json; charset=utf-8',
    'Cache-Control': 'no-store',
    'Content-Length': Buffer.byteLength(text),
  });
  res.end(text);
}

function writeHTML(res, status, html) {
  res.writeHead(status, {
    'Content-Type': 'text/html; charset=utf-8',
    'Cache-Control': 'no-store',
    'Content-Security-Policy': "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'",
    'Referrer-Policy': 'no-referrer',
  });
  res.end(html);
}

/** Minimal, dependency-free callback page. */
const CALLBACK_HTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>飞牛目录授权</title>
<style>
  :root { color-scheme: light dark; }
  html, body { height: 100%; margin: 0; }
  body {
    display: flex; align-items: center; justify-content: center;
    font: 14px/1.6 -apple-system, BlinkMacSystemFont, "PingFang SC", "Segoe UI", system-ui, sans-serif;
    color: #1f2328; background: #f6f7f9;
  }
  @media (prefers-color-scheme: dark) {
    body { color: #e6e6e6; background: #17181c; }
    .card { background: #202127 !important; border-color: #31333b !important; }
    .path { background: #17181c !important; }
  }
  .card {
    width: min(460px, calc(100vw - 48px));
    box-sizing: border-box; padding: 24px;
    background: #fff; border: 1px solid #e3e5e8; border-radius: 14px;
    box-shadow: 0 12px 40px rgba(0, 0, 0, .08);
  }
  h1 { margin: 0 0 8px; font-size: 18px; font-weight: 600; }
  p { margin: 0 0 14px; color: #6a6f76; }
  .path {
    margin: 0 0 16px; padding: 10px 12px; border-radius: 8px;
    background: #f2f3f5; word-break: break-all;
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-size: 12px;
  }
  .error { color: #c62828; }
  button {
    height: 36px; padding: 0 16px; border-radius: 8px; cursor: pointer;
    border: 1px solid #d0d3d7; background: transparent; color: inherit; font: inherit;
  }
  button:hover { background: rgba(127, 127, 127, .08); }
</style>
</head>
<body>
<div class="card">
  <h1 id="title">正在返回 DeepSeek Harness…</h1>
  <p id="message">授权结果已收到。</p>
  <div class="path" id="path" hidden></div>
  <button type="button" id="close" hidden>关闭窗口</button>
</div>
<script>
(function () {
  var params = new URLSearchParams(window.location.search);
  var rawPath = params.get('path');
  var paths = [];
  if (rawPath) {
    try {
      var parsed = JSON.parse(rawPath);
      if (Array.isArray(parsed)) {
        paths = parsed.filter(function (item) { return typeof item === 'string' && item.length > 0; });
      }
    } catch (_) { /* keep paths empty */ }
  }
  var result = {
    status: params.get('status') || '',
    error: params.get('error') || '',
    method: params.get('method') || '',
    appName: params.get('appName') || '',
    state: params.get('state') || '',
    path: paths
  };
  var payload = {
    type: 'dsh-fnos-advanced:auth',
    state: result.state,
    result: result,
    search: window.location.search
  };

  // The opener may have been severed by a cross-origin redirect (COOP), so the
  // storage copy is the reliable channel; the postMessage is the fast path.
  try {
    window.localStorage.setItem('dsh-fnos-advanced:auth-result', JSON.stringify({
      state: result.state,
      result: result,
      search: window.location.search,
      ts: Date.now()
    }));
  } catch (_) { /* private mode / storage disabled */ }

  try {
    if (window.opener && !window.opener.closed) {
      window.opener.postMessage(payload, window.location.origin);
    }
  } catch (_) { /* cross-origin opener */ }

  // Third channel: hand the raw query to the same-origin host route. The dsh
  // page polls it, so neither COOP nor storage partitioning can lose the
  // result.
  try {
    var resultUrl = new URL('auth-result', window.location.href).toString();
    fetch(resultUrl, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ state: result.state, search: window.location.search }),
      keepalive: true
    }).catch(function () {});
  } catch (_) { /* fetch unavailable */ }

  var ok = result.status === 'success' && paths.length > 0;
  var title = document.getElementById('title');
  var message = document.getElementById('message');
  var path = document.getElementById('path');
  var close = document.getElementById('close');

  if (ok) {
    title.textContent = '授权成功';
    message.textContent = '目录已授权给飞牛应用，正在返回工作区选择器。';
    path.textContent = paths[0];
    path.hidden = false;
  } else {
    title.textContent = '未完成授权';
    title.classList.add('error');
    message.textContent = result.error || '授权已取消，请回到 DeepSeek Harness 重试。';
  }

  close.hidden = false;
  close.addEventListener('click', function () { window.close(); });
  // This page is normally opened by the picker, so the browser allows the
  // script to close it even when COOP severed window.opener.
  window.setTimeout(function () { window.close(); }, ok ? 900 : 1400);
})();
</script>
</body>
</html>`;

async function handle(req, res) {
  let pathname;
  try {
    pathname = new URL(req.url || '/', 'http://localhost').pathname;
  } catch {
    writeJSON(res, 400, { ok: false, error: 'bad request url' });
    return;
  }

  // The callback page POSTs the raw query here. This channel does not depend
  // on window.opener or localStorage, so COOP and storage partitioning cannot
  // lose the authorization result.
  if (pathname === RESULT_PATH) {
    pruneResults();
    if (req.method === 'POST') {
      try {
        const raw = await readBody(req);
        const body = JSON.parse(raw);
        const state = typeof body?.state === 'string' ? body.state : '';
        const search = typeof body?.search === 'string' ? body.search : '';
        if (!state || state.length > 200 || !search || search.length > 10000) {
          writeJSON(res, 400, { ok: false, error: 'invalid authorization result' });
          return;
        }
        if (pendingResults.size >= 200) {
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
      const state = new URL(req.url || '/', 'http://localhost').searchParams.get('state') || '';
      const entry = state ? pendingResults.get(state) : undefined;
      if (!entry) {
        writeJSON(res, 404, { ok: false, error: 'not found' });
        return;
      }
      pendingResults.delete(state);
      writeJSON(res, 200, { ok: true, state, search: entry.search });
      return;
    }
    res.writeHead(405, { Allow: 'GET, HEAD, POST', 'Cache-Control': 'no-store' });
    res.end();
    return;
  }

  if (pathname === WORKSPACE_PATH) {
    if (req.method !== 'GET' && req.method !== 'HEAD') {
      res.writeHead(405, { Allow: 'GET, HEAD', 'Cache-Control': 'no-store' });
      res.end();
      return;
    }
    const id = new URL(req.url || '/', 'http://localhost').searchParams.get('id') || '';
    if (!/^[A-Za-z0-9_-]{1,80}$/.test(id)) {
      writeJSON(res, 400, { ok: false, error: 'invalid workspace id' });
      return;
    }
    const workspace = lookupWorkspace(id);
    if (!workspace) {
      writeJSON(res, 404, { ok: false, error: 'workspace not found' });
      return;
    }
    writeJSON(res, 200, { ok: true, ...workspace });
    return;
  }

  if (pathname === SETTINGS_PATH) {
    if (req.method === 'GET' || req.method === 'HEAD') {
      writeJSON(res, 200, { ok: true, settings: readSettings(), defaults: DEFAULT_SETTINGS });
      return;
    }
    if (req.method === 'POST') {
      try {
        const raw = await readBody(req);
        const body = JSON.parse(raw);
        const settings = normalizeSettings(body);
        writeSettings(settings);
        writeJSON(res, 200, { ok: true, settings, defaults: DEFAULT_SETTINGS });
      } catch (error) {
        writeJSON(res, 400, { ok: false, error: error instanceof Error ? error.message : '设置格式无效' });
      }
      return;
    }
    res.writeHead(405, { Allow: 'GET, HEAD, POST', 'Cache-Control': 'no-store' });
    res.end();
    return;
  }

  if (req.method !== 'GET' && req.method !== 'HEAD') {
    res.writeHead(405, { Allow: 'GET, HEAD', 'Cache-Control': 'no-store' });
    res.end();
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
    writeJSON(res, 200, { ok: true, appName: appName(), version: VERSION });
    return;
  }

  writeJSON(res, 404, { ok: false, error: 'not found' });
}

export const name = 'dsh-fnos-advanced';

/**
 * Register the host routes once the web server service is available. No
 * `@deepseek-ai/*` import is used: the only contract is the stable
 * `ctx.webServer.register({ kind, path, handler })` service.
 *
 * @param {import('@deepseek-ai/cordis').Context} ctx
 */
export function apply(ctx) {
  if (!ctx || typeof ctx.inject !== 'function') return;

  ctx.inject(['workspaceRegistry'], (scope) => {
    const registry = scope.workspaceRegistry;
    if (!registry) return;
    workspaceRegistry = registry;
    if (typeof scope.effect === 'function') {
      scope.effect(() => () => {
        if (workspaceRegistry === registry) workspaceRegistry = null;
      }, 'dsh-fnos-advanced: workspace registry');
    }
  });

  ctx.inject(['webServer'], (scope) => {
    const server = scope.webServer;
    if (!server || typeof server.register !== 'function') return;

    const register = () => server.register({
      kind: 'prefix',
      path: ROUTE_PREFIX,
      handler: handle,
    });

    if (typeof scope.effect === 'function') {
      scope.effect(register, 'dsh-fnos-advanced: host routes');
    } else {
      register();
    }
  });
}
