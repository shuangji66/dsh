import assert from 'node:assert/strict';
import { readdir, readFile } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('..', import.meta.url));

test('the source tree is TypeScript only', async () => {
  const entries = await readdir(path.join(root, 'src'), { recursive: true, withFileTypes: true });
  const files = entries.filter((entry) => entry.isFile()).map((entry) => entry.name);
  const foreign = files.filter((name) => !/\.(?:ts|tsx)$/.test(name));
  assert.deepEqual(foreign, [], 'src must not contain .js sources');
});

test('the published host artifact is the compiled TypeScript entry', async () => {
  const host = await readFile(path.join(root, 'lib/index.js'), 'utf8');
  assert.match(host, /export const name = 'dsh-fnos-advanced'/);
  assert.match(host, /export function apply\(/);
  // Compiled ESM: relative imports must point at emitted .js files.
  assert.doesNotMatch(host, /from '\.\/host\/[a-z-]+\.ts'/);
  const types = await readFile(path.join(root, 'lib/index.d.ts'), 'utf8');
  assert.match(types, /declare function apply/);
});

test('declarations import emitted .js paths', async () => {
  const entries = await readdir(path.join(root, 'lib'), { recursive: true, withFileTypes: true });
  const declarations = entries.filter((entry) => entry.isFile() && entry.name.endsWith('.d.ts'));
  assert.ok(declarations.length > 0, 'build emitted declarations');
  for (const entry of declarations) {
    const file = path.join(entry.parentPath, entry.name);
    const source = await readFile(file, 'utf8');
    assert.doesNotMatch(source, /\bfrom\s+['"]\.{1,2}\/[^'"]+\.ts['"]/, file);
  }
});

test('the client bundle stays in the dsh loader contract', async () => {
  const bundle = await readFile(path.join(root, 'lib/client.js'), 'utf8');
  assert.match(bundle, /window\.__ModuleLoader__\.load\(/);
  assert.match(bundle, /id: "dsh-fnos-advanced"/);
  assert.match(bundle, /require\("react"\)/);
  // The SDK is bundled; nothing else may be requested from the runtime.
  assert.doesNotMatch(bundle, /require\(["']@trimjs\/web-app["']\)/);
  assert.doesNotMatch(bundle, /require\(["']react\/jsx-runtime["']\)/);
  assert.match(bundle, /buildAppAuthUrl/);
  assert.match(bundle, /parseAppAuthCallback/);
  assert.match(bundle, /fnos-advanced\/auth-result/);
  assert.match(bundle, /fnos-advanced\/workspace-path/);
  assert.match(bundle, /openFileManager/);
  assert.match(bundle, /plugins\.bundle\.config/);
  assert.doesNotMatch(bundle, /plugins\.row\.config/);
  assert.match(bundle, /__DSH_HOST_PATHS__/);
  assert.match(bundle, /data-conversation-region/);
  assert.match(bundle, /data-trigger-menu/);
  assert.match(bundle, /data-dsh-fnos-pick-file/);
  assert.match(bundle, /data-files-entry/);
  assert.match(bundle, /data-dsh-fnos-file-action/);
  assert.match(bundle, /fnos-advanced\/file-token/);
  assert.match(bundle, /fnos-advanced\/file-bytes/);
});
