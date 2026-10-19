import assert from 'node:assert/strict';
import test from 'node:test';
import {
  isDuplicateWorkspacePath,
  normalizeWorkspacePath,
  pickerParams,
} from '../src/client/picker.js';

test('directory pickers enable folder creation without disabling subtrees', () => {
  const params = pickerParams({
    directory: true,
    creatable: true,
    title: 'Choose',
    okText: 'OK',
  });
  assert.equal(params.directory, true);
  assert.equal(params.creatable, true);
  assert.equal('disabledPaths' in params, false);
  assert.equal(params.title, 'Choose');
  assert.ok(Array.isArray(params.sidebarGroup));
});

test('shared pickers leave directory to the host', () => {
  const params = pickerParams({ title: 'Shared', okText: 'OK' });
  assert.equal('directory' in params, false);
  assert.equal('creatable' in params, false);
});

test('composer pickers are file pickers', () => {
  const params = pickerParams({ directory: false, title: 'Files', okText: 'Add' });
  assert.equal(params.directory, false);
});

test('normalizes trailing and duplicate separators', () => {
  assert.equal(normalizeWorkspacePath('/vol1//work/'), '/vol1/work');
  assert.equal(normalizeWorkspacePath('/'), '/');
  assert.equal(normalizeWorkspacePath(''), '');
});

test('blocks only the exact workspace path, never its children', () => {
  const workspaces = ['/vol1/1000/work/', '/vol1/1000/other'];
  assert.equal(isDuplicateWorkspacePath('/vol1/1000/work', workspaces), true);
  assert.equal(isDuplicateWorkspacePath('/vol1/1000/work/', workspaces), true);
  assert.equal(isDuplicateWorkspacePath('/vol1/1000/work/project', workspaces), false);
  assert.equal(isDuplicateWorkspacePath('/vol1/1000/work2', workspaces), false);
  assert.equal(isDuplicateWorkspacePath('', workspaces), false);
});
