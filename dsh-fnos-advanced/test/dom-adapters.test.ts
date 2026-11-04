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
  assert.match(source, /adaptCopyItem\(menu\)/);
  assert.match(source, /openPathInFileManager\(id\)/);
  assert.match(source, /copyText\(current\)/);
  // Only the live menu is augmented; dsh's exit phases are dismiss/replace.
  assert.match(source, /isLiveMenuPhase\(menu\.getAttribute\('data-menu-phase'\)\)/);
  // Captured targets are resolved against the owning Session before use.
  assert.match(source, /absoluteTargetPath\(captured, sessionCwd\)/);
});

test('the reveal row opens the containing folder, never the file itself', async () => {
  const source = await read('workspace-menu.ts');
  // `openFileManager` navigates to a directory; handing it the file path is what
  // made "Show in folder" land somewhere else on the device.
  assert.match(source, /kind === 'file'\) void openPathInFileManager\(folderOfPath\(id\)\)/);
  assert.match(source, /kind === 'directory'\) void openPathInFileManager\(id\)/);
  // A path without a known root is refused instead of being handed to the host.
  assert.match(source, /if \(!path\.startsWith\('\/'\)\)/);
  assert.match(source, /无法确定文件路径/);
});

test('the default-app row is taken over instead of duplicated', async () => {
  const source = await read('workspace-menu.ts');
  assert.match(source, /adaptOpenItem\(menu, native\)/);
  assert.match(source, /DEFAULT_APP_LABEL_RE/);
  // Only the disabled row is ours; an enabled one carries a resolved app.
  assert.match(source, /const disabled = button\.disabled \|\| button\.getAttribute\('aria-disabled'\) === 'true'/);
  // The duplicate is hidden by class: dsh's React tree keeps the node.
  assert.match(source, /classList\.add\('dsh-fnos-menu-hidden'\)/);
  assert.doesNotMatch(source, /openItem\.remove\(\)/);
  assert.match(source, /void openPathInFnos\(current, false\)/);
  const style = await read('style.ts');
  assert.match(style, /\.dsh-fnos-menu-hidden/);
});

test('native contributions resolve relative paths and fall back on a failed registration', async () => {
  const source = await read('native-menu.ts');
  assert.match(source, /absoluteTargetPath\(\{ path, sessionId \}, sessionCwd\)/);
  assert.match(source, /registered = !failed/);
  assert.match(source, /if \(!menus \|\| !registered \|\| typeof menus\.open/);
  assert.match(source, /disposeNativeContextMenus/);
});

test('slot entries pass the pane id of the native target', async () => {
  const source = await read('slot-actions.tsx');
  assert.match(source, /data-dockkit-pane/);
  assert.match(source, /pathMenuTarget\('file', path, sessionId, paneId\)/);
});

test('native contributions are registered for both file and directory targets', async () => {
  const source = await read('native-menu.ts');
  assert.match(source, /target: 'file' \| 'directory'/);
  assert.match(source, /register\('directory'\)/);
  assert.match(source, /register\('file'\)/);
  assert.match(source, /order: 300/);
});
