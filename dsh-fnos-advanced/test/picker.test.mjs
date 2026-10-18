import assert from 'node:assert/strict';
import test from 'node:test';
import { pickerParams } from '../src/client/picker.js';

test('directory pickers enable folder creation and disable existing workspaces', () => {
  const params = pickerParams({
    directory: true,
    creatable: true,
    disabledPaths: ['/vol1/1000/a', '/vol1/1000/b'],
    title: 'Choose',
    okText: 'OK',
  });
  assert.equal(params.directory, true);
  assert.equal(params.creatable, true);
  assert.deepEqual(params.disabledPaths, ['/vol1/1000/a', '/vol1/1000/b']);
  assert.equal(params.title, 'Choose');
  assert.ok(Array.isArray(params.sidebarGroup));
});

test('shared pickers leave directory to the host', () => {
  const params = pickerParams({ title: 'Shared', okText: 'OK' });
  assert.equal('directory' in params, false);
  assert.equal('creatable' in params, false);
  assert.equal('disabledPaths' in params, false);
});

test('composer pickers are file pickers and copy disabled paths defensively', () => {
  const disabledPaths = ['/vol1/1000/a'];
  const params = pickerParams({ directory: false, disabledPaths, title: 'Files', okText: 'Add' });
  assert.equal(params.directory, false);
  assert.deepEqual(params.disabledPaths, disabledPaths);
  assert.notEqual(params.disabledPaths, disabledPaths);
});
