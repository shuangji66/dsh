import assert from 'node:assert/strict';
import http from 'node:http';
import type { AddressInfo } from 'node:net';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { chmod, mkdir, mkdtemp, open, rm, writeFile } from 'node:fs/promises';
import { apply } from '../src/index.ts';
import type { HostContext, HostScope } from '../src/host/types.ts';

type Handler = (req: http.IncomingMessage, res: http.ServerResponse) => void | Promise<void>;

interface RegisteredRoute {
  kind: string;
  path: string;
  handler: Handler;
}

/** Parse a JSON response body with a known shape. */
async function json<T>(response: Response): Promise<T> {
  return await response.json() as T;
}

/** Capture the route registered by the host half. */
function mountHost(): { route: RegisteredRoute; server: http.Server } {
  const routes: RegisteredRoute[] = [];

  const scope: HostScope = {
    webServer: {
      register(options) {
        routes.push(options);
        return () => {};
      },
    },
    effect(register) {
      register();
    },
  };

  const ctx: HostContext = {
    inject(_services, callback) {
      callback(scope);
    },
  };

  apply(ctx);
  const route = routes[0];
  if (!route) throw new Error('host half did not register a webServer route');
  const server = http.createServer((req, res) => { void route.handler(req, res); });
  return { route, server };
}

async function withServer(fn: (base: string, route: RegisteredRoute) => Promise<void>): Promise<void> {
  const { route, server } = mountHost();
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const { port } = server.address() as AddressInfo;
  try {
    await fn(`http://127.0.0.1:${port}`, route);
  } finally {
    await new Promise<void>((resolve) => { server.close(() => resolve()); });
  }
}

interface ConfigResponse {
  ok: boolean;
  appName: string;
  authBase: string;
  version: string;
}

interface HealthResponse {
  ok: boolean;
  appName: string;
  version: string;
  features: { filePicker: boolean; imageRead: boolean; workspaceHealth: boolean };
}

interface SettingsResponse {
  ok: boolean;
  error?: string;
  settings: { proxyBase: string; lanTarget: string; authMode: string; imageRead: boolean };
  defaults?: { proxyBase: string; lanTarget: string; authMode: string; imageRead: boolean };
}

interface WorkspacePathResponse {
  ok: boolean;
  id: string;
  path: string;
  title: string;
}

interface WorkspaceListResponse {
  ok: boolean;
  workspaces: Array<{ id: string; path: string; title: string }>;
}

interface WorkspaceHealthResponse {
  ok: boolean;
  results: Array<{
    id: string;
    path: string;
    title: string;
    exists: boolean;
    directory: boolean;
    readable: boolean;
    writable: boolean;
    error: string;
  }>;
}

interface FileTokenResponse {
  ok: boolean;
  error?: string;
  token: string;
  mime: string;
  name: string;
  size: number;
}

interface AuthResultResponse {
  ok: boolean;
  state?: string;
  search?: string;
}

test('registers the /fnos-advanced prefix route', async () => {
  await withServer(async (_base, route) => {
    assert.equal(route.kind, 'prefix');
    assert.equal(route.path, '/fnos-advanced');
  });
});

test('serves config, callback, health and a JSON 404', async () => {
  await withServer(async (base) => {
    const config = await json<ConfigResponse>(await fetch(`${base}/fnos-advanced/config`));
    assert.equal(config.ok, true);
    assert.equal(config.appName, process.env.TRIM_APPNAME || 'Harness');
    assert.equal(typeof config.version, 'string');

    const callback = await fetch(
      `${base}/fnos-advanced/auth-callback.html?status=success&method=pickUserFile&appName=Harness&state=s1&path=%5B%22%2Fvol1%2F1000%2Fworkspace%22%5D`,
    );
    assert.equal(callback.status, 200);
    const html = await callback.text();
    assert.match(html, /dsh-fnos-advanced:auth-result/);
    assert.match(html, /auth-result/);
    assert.match(html, /window\.close\(\)/);
    assert.match(html, /status/);

    const health = await json<HealthResponse>(await fetch(`${base}/fnos-advanced/health`));
    assert.equal(health.ok, true);

    const search = '?status=success&method=pickUserFile&appName=Harness&state=s2&path=%5B%22%2Fvol1%2F1000%2Fworkspace%22%5D';
    const posted = await fetch(`${base}/fnos-advanced/auth-result`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ state: 's2', search }),
    });
    assert.equal(posted.status, 200);
    const polled = await json<AuthResultResponse>(await fetch(`${base}/fnos-advanced/auth-result?state=s2`));
    assert.equal(polled.ok, true);
    assert.equal(polled.state, 's2');
    assert.equal(polled.search, search);
    assert.equal((await fetch(`${base}/fnos-advanced/auth-result?state=s2`)).status, 404);

    const missing = await fetch(`${base}/fnos-advanced/nope`);
    assert.equal(missing.status, 404);
  });
});

test('rejects non-GET methods', async () => {
  await withServer(async (base) => {
    const response = await fetch(`${base}/fnos-advanced/config`, { method: 'POST' });
    assert.equal(response.status, 405);
  });
});

test('looks up a workspace path from the storage registry', async () => {
  const home = await mkdtemp(path.join(os.tmpdir(), 'dsh-fnos-workspaces-'));
  const previous = process.env.DSH_HOME;
  process.env.DSH_HOME = home;
  const id = '11111111-1111-4111-8111-111111111111';
  try {
    await mkdir(path.join(home, 'storages'), { recursive: true });
    await writeFile(
      path.join(home, 'storages', 'workspace.json'),
      JSON.stringify({
        tables: {
          workspaces: {
            [id]: { path: '/vol1/1000/workspace', title: 'workspace' },
          },
        },
      }),
    );

    await withServer(async (base) => {
      const found = await json<WorkspacePathResponse>(
        await fetch(`${base}/fnos-advanced/workspace-path?id=${id}`),
      );
      assert.equal(found.ok, true);
      assert.equal(found.id, id);
      assert.equal(found.path, '/vol1/1000/workspace');
      assert.equal(found.title, 'workspace');

      assert.equal((await fetch(`${base}/fnos-advanced/workspace-path?id=missing`)).status, 404);
      assert.equal(
        (await fetch(`${base}/fnos-advanced/workspace-path?id=${encodeURIComponent('../etc/passwd')}`)).status,
        400,
      );
    });
  } finally {
    if (previous === undefined) delete process.env.DSH_HOME;
    else process.env.DSH_HOME = previous;
    await rm(home, { recursive: true, force: true });
  }
});

test('persists and validates settings under DSH_HOME', async () => {
  const home = await mkdtemp(path.join(os.tmpdir(), 'dsh-fnos-settings-'));
  const previous = process.env.DSH_HOME;
  process.env.DSH_HOME = home;
  try {
    await withServer(async (base) => {
      const initial = await json<SettingsResponse>(await fetch(`${base}/fnos-advanced/settings`));
      assert.equal(initial.ok, true);
      assert.deepEqual(initial.settings, { proxyBase: '', lanTarget: '5666', authMode: 'ask', imageRead: true });

      const posted = await fetch(`${base}/fnos-advanced/settings`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          proxyBase: 'https://fnos.example.com/',
          lanTarget: '192.168.1.10:5666',
          authMode: 'proxy',
        }),
      });
      assert.equal(posted.status, 200);
      const saved = await json<SettingsResponse>(posted);
      assert.equal(saved.settings.proxyBase, 'https://fnos.example.com');
      assert.equal(saved.settings.lanTarget, 'http://192.168.1.10:5666');
      assert.equal(saved.settings.authMode, 'proxy');

      const readBack = await json<SettingsResponse>(await fetch(`${base}/fnos-advanced/settings`));
      assert.deepEqual(readBack.settings, saved.settings);

      const invalid = await fetch(`${base}/fnos-advanced/settings`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ proxyBase: 'ftp://nope', lanTarget: '99999', authMode: 'ask' }),
      });
      assert.equal(invalid.status, 400);
    });
  } finally {
    if (previous === undefined) delete process.env.DSH_HOME;
    else process.env.DSH_HOME = previous;
    await rm(home, { recursive: true, force: true });
  }
});

test('lists workspaces and reports metadata-only access health', async () => {
  const home = await mkdtemp(path.join(os.tmpdir(), 'dsh-fnos-health-home-'));
  const good = await mkdtemp(path.join(os.tmpdir(), 'dsh-fnos-health-good-'));
  const readonly = await mkdtemp(path.join(os.tmpdir(), 'dsh-fnos-health-ro-'));
  const previous = process.env.DSH_HOME;
  process.env.DSH_HOME = home;
  const ids = {
    good: '11111111-1111-4111-8111-111111111111',
    missing: '22222222-2222-4222-8222-222222222222',
    readonly: '33333333-3333-4333-8333-333333333333',
  };
  const missing = path.join(home, 'missing-workspace');
  try {
    await chmod(readonly, 0o555);
    await mkdir(path.join(home, 'storages'), { recursive: true });
    await writeFile(
      path.join(home, 'storages', 'workspace.json'),
      JSON.stringify({
        tables: {
          workspaces: {
            [ids.good]: { path: good, title: 'good' },
            [ids.missing]: { path: missing, title: 'missing' },
            [ids.readonly]: { path: readonly, title: 'readonly' },
          },
        },
      }),
    );

    await withServer(async (base) => {
      const listed = await json<WorkspaceListResponse>(await fetch(`${base}/fnos-advanced/workspaces`));
      assert.equal(listed.ok, true);
      assert.equal(listed.workspaces.length, 3);

      const response = await fetch(`${base}/fnos-advanced/workspace-health`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ids: [ids.good, ids.missing, ids.readonly] }),
      });
      assert.equal(response.status, 200);
      const health = await json<WorkspaceHealthResponse>(response);
      const byId = Object.fromEntries(health.results.map((row) => [row.id, row]));
      assert.equal(byId[ids.good]?.exists, true);
      assert.equal(byId[ids.good]?.directory, true);
      assert.equal(byId[ids.good]?.readable, true);
      assert.equal(byId[ids.good]?.writable, true);
      assert.equal(byId[ids.missing]?.exists, false);
      assert.equal(byId[ids.readonly]?.exists, true);
      assert.equal(byId[ids.readonly]?.writable, false);
      assert.equal(byId[ids.readonly]?.error, 'EACCES');

      const all = await json<WorkspaceHealthResponse>(await fetch(`${base}/fnos-advanced/workspace-health`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ids: [] }),
      }));
      assert.equal(all.results.length, 3);

      const cross = await fetch(`${base}/fnos-advanced/workspace-health`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Origin: 'https://evil.example' },
        body: JSON.stringify({ ids: [ids.good] }),
      });
      assert.equal(cross.status, 403);

      const invalid = await fetch(`${base}/fnos-advanced/workspace-health`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ids: ['../etc'] }),
      });
      assert.equal(invalid.status, 400);
    });
  } finally {
    await chmod(readonly, 0o755).catch(() => {});
    if (previous === undefined) delete process.env.DSH_HOME;
    else process.env.DSH_HOME = previous;
    await rm(home, { recursive: true, force: true });
    await rm(good, { recursive: true, force: true });
    await rm(readonly, { recursive: true, force: true });
  }
});

test('serves a picked image through a one-time token', async () => {
  const home = await mkdtemp(path.join(os.tmpdir(), 'dsh-fnos-home-'));
  const photos = await mkdtemp(path.join(os.tmpdir(), 'dsh-fnos-photos-'));
  const previous = process.env.DSH_HOME;
  process.env.DSH_HOME = home;
  const image = path.join(photos, 'photo.png');
  const bytes = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3]);
  try {
    await writeFile(image, bytes);

    await withServer(async (base) => {
      const issued = await fetch(`${base}/fnos-advanced/file-token`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: image }),
      });
      assert.equal(issued.status, 200);
      const token = await json<FileTokenResponse>(issued);
      assert.equal(token.ok, true);
      assert.equal(token.mime, 'image/png');
      assert.equal(token.name, 'photo.png');
      assert.equal(token.size, bytes.length);

      const served = await fetch(`${base}/fnos-advanced/file-bytes?token=${encodeURIComponent(token.token)}`);
      assert.equal(served.status, 200);
      assert.equal(served.headers.get('content-type'), 'image/png');
      assert.equal(served.headers.get('x-content-type-options'), 'nosniff');
      assert.deepEqual(Buffer.from(await served.arrayBuffer()), bytes);

      // One-time: a replay finds no token.
      assert.equal(
        (await fetch(`${base}/fnos-advanced/file-bytes?token=${encodeURIComponent(token.token)}`)).status,
        404,
      );
    });
  } finally {
    if (previous === undefined) delete process.env.DSH_HOME;
    else process.env.DSH_HOME = previous;
    await rm(home, { recursive: true, force: true });
    await rm(photos, { recursive: true, force: true });
  }
});

test('rejects non-images, hidden paths, DSH_HOME and cross-origin token requests', async () => {
  const home = await mkdtemp(path.join(os.tmpdir(), 'dsh-fnos-home-'));
  const photos = await mkdtemp(path.join(os.tmpdir(), 'dsh-fnos-photos-'));
  const previous = process.env.DSH_HOME;
  process.env.DSH_HOME = home;
  try {
    const text = path.join(photos, 'notes.txt');
    const hidden = path.join(photos, '.secret', 'photo.png');
    const secret = path.join(home, 'photo.png');
    const large = path.join(photos, 'large.png');
    const okImage = path.join(photos, 'ok.png');
    await writeFile(text, 'hello');
    await writeFile(okImage, Buffer.from([1, 2, 3]));
    await mkdir(path.dirname(hidden), { recursive: true });
    await writeFile(hidden, Buffer.from([1]));
    await writeFile(secret, Buffer.from([1]));
    const handle = await open(large, 'w');
    // 图片上限是 8 MB（src/host/images.ts 的 MAX_IMAGE_BYTES），这里刚好越界 1 字节。
    await handle.truncate(8 * 1024 * 1024 + 1);
    await handle.close();

    await withServer(async (base) => {
      const ask = (body: Record<string, unknown>, headers: Record<string, string> = {}) => fetch(
        `${base}/fnos-advanced/file-token`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', ...headers },
          body: JSON.stringify(body),
        },
      );

      assert.equal((await ask({ path: text })).status, 415);
      assert.equal((await ask({ path: hidden })).status, 403);
      assert.equal((await ask({ path: secret })).status, 403);
      assert.equal((await ask({ path: 'relative/photo.png' })).status, 400);
      assert.equal((await ask({ path: path.join(photos, 'missing.png') })).status, 404);
      assert.equal((await ask({ path: large })).status, 413);
      assert.equal((await ask({ path: path.join(photos, 'photo.png') }, { Origin: 'https://evil.example' })).status, 403);

      // Harness' reverse proxy rewrites Host/Origin to the dsh upstream but
      // leaves Referer on the public origin; X-Forwarded-Host bridges them.
      const proxied = await ask({ path: okImage }, {
        Origin: base,
        Referer: 'https://fnos.example/app/Harness/dsh/',
        'X-Forwarded-Host': 'fnos.example',
      });
      assert.equal(proxied.status, 200);
    });
  } finally {
    if (previous === undefined) delete process.env.DSH_HOME;
    else process.env.DSH_HOME = previous;
    await rm(home, { recursive: true, force: true });
    await rm(photos, { recursive: true, force: true });
  }
});

test('honours the imageRead setting', async () => {
  const home = await mkdtemp(path.join(os.tmpdir(), 'dsh-fnos-home-'));
  const photos = await mkdtemp(path.join(os.tmpdir(), 'dsh-fnos-photos-'));
  const previous = process.env.DSH_HOME;
  process.env.DSH_HOME = home;
  const image = path.join(photos, 'photo.png');
  try {
    await writeFile(image, Buffer.from([1, 2, 3]));
    await withServer(async (base) => {
      const saved = await fetch(`${base}/fnos-advanced/settings`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ imageRead: false }),
      });
      assert.equal(saved.status, 200);
      const savedBody = await json<SettingsResponse>(saved);
      assert.equal(savedBody.settings.imageRead, false);

      const health = await json<HealthResponse>(await fetch(`${base}/fnos-advanced/health`));
      assert.equal(health.features.imageRead, false);

      const denied = await fetch(`${base}/fnos-advanced/file-token`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: image }),
      });
      assert.equal(denied.status, 403);
    });
  } finally {
    if (previous === undefined) delete process.env.DSH_HOME;
    else process.env.DSH_HOME = previous;
    await rm(home, { recursive: true, force: true });
    await rm(photos, { recursive: true, force: true });
  }
});
