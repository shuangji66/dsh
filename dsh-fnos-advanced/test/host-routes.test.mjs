import assert from 'node:assert/strict';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { apply } from '../lib/index.js';

/** Capture the route registered by the host half. */
function mountHost() {
  let route;
  const ctx = {
    inject(_services, callback) {
      callback({
        webServer: {
          register(value) {
            route = value;
            return () => {};
          },
        },
        effect(fn) {
          return fn();
        },
      });
    },
  };
  apply(ctx);
  assert.ok(route, 'host half registered a webServer route');
  const server = http.createServer((req, res) => route.handler(req, res));
  return { route, server };
}

async function withServer(fn) {
  const { route, server } = mountHost();
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const { port } = server.address();
  try {
    await fn(`http://127.0.0.1:${port}`, route);
  } finally {
    await new Promise((resolve) => server.close(resolve));
  }
}

test('registers the /fnos-advanced prefix route', async () => {
  await withServer(async (_base, route) => {
    assert.equal(route.kind, 'prefix');
    assert.equal(route.path, '/fnos-advanced');
  });
});

test('serves config, callback, health and a JSON 404', async () => {
  await withServer(async (base) => {
    const config = await (await fetch(`${base}/fnos-advanced/config`)).json();
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

    const health = await (await fetch(`${base}/fnos-advanced/health`)).json();
    assert.equal(health.ok, true);

    const search = '?status=success&method=pickUserFile&appName=Harness&state=s2&path=%5B%22%2Fvol1%2F1000%2Fworkspace%22%5D';
    const posted = await fetch(`${base}/fnos-advanced/auth-result`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ state: 's2', search }),
    });
    assert.equal(posted.status, 200);
    const polled = await (await fetch(`${base}/fnos-advanced/auth-result?state=s2`)).json();
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
      const found = await (await fetch(`${base}/fnos-advanced/workspace-path?id=${id}`)).json();
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
      const initial = await (await fetch(`${base}/fnos-advanced/settings`)).json();
      assert.equal(initial.ok, true);
      assert.deepEqual(initial.settings, { proxyBase: '', lanTarget: '5666', authMode: 'ask' });

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
      const saved = await posted.json();
      assert.equal(saved.settings.proxyBase, 'https://fnos.example.com');
      assert.equal(saved.settings.lanTarget, 'http://192.168.1.10:5666');
      assert.equal(saved.settings.authMode, 'proxy');

      const readBack = await (await fetch(`${base}/fnos-advanced/settings`)).json();
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

test('client bundle is the dsh lazy-factory shape and bundles the SDK', async () => {
  const bundle = await readFile(new URL('../lib/client.js', import.meta.url), 'utf8');
  assert.match(bundle, /window\.__ModuleLoader__\.load\(/);
  assert.match(bundle, /id: "dsh-fnos-advanced"/);
  assert.doesNotMatch(bundle, /require\(["']@trimjs\/web-app["']\)/);
  assert.match(bundle, /buildAppAuthUrl/);
  assert.match(bundle, /parseAppAuthCallback/);
  assert.match(bundle, /fnos-advanced\/auth-result/);
  assert.match(bundle, /fnos-advanced\/workspace-path/);
  assert.match(bundle, /openFileManager/);
  assert.match(bundle, /require\("react"\)/);
  assert.match(bundle, /plugins\.bundle\.config/);
  assert.doesNotMatch(bundle, /plugins\.row\.config/);
});
