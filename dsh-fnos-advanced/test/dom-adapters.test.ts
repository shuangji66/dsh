import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('..', import.meta.url));
const read = (file: string): Promise<string> => readFile(path.join(root, 'src/client', file), 'utf8');

test('the workspace menu is dismissed through dsh, never by removing its React-owned node', async () => {
  const source = await read('workspace-menu.ts');
  const start = source.indexOf('function closeMenu');
  assert.ok(start >= 0, 'closeMenu exists');
  const body = source.slice(start, source.indexOf('\n}\n', start));
  assert.match(body, /pointerdown/);
  assert.doesNotMatch(body, /\.remove\(/);
  assert.match(source, /closeMenu\(\);/);
});

test('the file-tree action finds the 0.2.2 row wrapper as well as the old direct button', async () => {
  const source = await read('file-tree.ts');
  assert.match(source, /:scope > button/);
  assert.match(source, /:scope > div > button/);
  const style = await read('style.ts');
  assert.match(style, /\.dsh-fnos-file-row > div > button:first-child/);
});

test('the file-tree menu reuses the native popup before falling back', async () => {
  const source = await read('file-tree.ts');
  assert.match(source, /pathMenuTarget\(/);
  assert.match(source, /data-sidebar-right-session/);
  assert.match(source, /data-files-navigator/);
  assert.match(source, /openNativePathMenu\(action, target\)/);
  assert.match(source, /togglePathMenu\(action, \{ path, entry \}\)/);
});

test('slot entries use the same native popup when the session is known', async () => {
  const source = await read('slot-actions.tsx');
  assert.match(source, /sessionIdOf\(props, anchor\)/);
  assert.match(source, /openNativePathMenu\(anchor, target\)/);
  assert.match(source, /togglePathMenu\(anchor, \{ path, entry: 'file' \}\)/);
});

test('the native menu adapter re-wires reveal and copy rows', async () => {
  const source = await read('workspace-menu.ts');
  assert.match(source, /activeNativeTarget\(\)/);
  assert.match(source, /data-context-menu/);
  assert.match(source, /adaptCopyItem\(menu, native\.path\)/);
  assert.match(source, /openPathInFileManager\(id\)/);
  assert.match(source, /copyText\(current\)/);
});

test('native contributions are registered for both file and directory targets', async () => {
  const source = await read('native-menu.ts');
  assert.match(source, /target: 'file' \| 'directory'/);
  assert.match(source, /register\('directory'\)/);
  assert.match(source, /register\('file'\)/);
  assert.match(source, /order: 300/);
});
