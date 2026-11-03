import assert from 'node:assert/strict';
import test from 'node:test';
import { contextFromRowKey } from '../src/client/menu-target.ts';

test('reads the workspace and session rows a context menu belongs to', () => {
  assert.deepEqual(contextFromRowKey('workspace:ws-1'), { kind: 'workspace', id: 'ws-1' });
  assert.deepEqual(contextFromRowKey('session:ses-1'), { kind: 'session', id: 'ses-1' });
});

test('ignores rows without an id and keys of other kinds', () => {
  assert.equal(contextFromRowKey('workspace:'), null);
  assert.equal(contextFromRowKey('session:'), null);
  assert.equal(contextFromRowKey('file:/tmp/a'), null);
  assert.equal(contextFromRowKey(''), null);
  assert.equal(contextFromRowKey(null), null);
});
