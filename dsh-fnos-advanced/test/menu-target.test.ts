import assert from 'node:assert/strict';
import test from 'node:test';
import { activePathTarget, contextFromRowKey, pathMenuTarget } from '../src/client/menu-target.ts';

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


test('builds native file/directory targets from row anchors', () => {
  assert.deepEqual(pathMenuTarget('directory', '/w/a', 'session-1', 'pane1'), {
    kind: 'directory',
    sessionId: 'session-1',
    path: '/w/a',
  });
  assert.deepEqual(pathMenuTarget('file', '/w/a.txt', 'session-1', 'pane1'), {
    kind: 'file',
    sessionId: 'session-1',
    path: '/w/a.txt',
    paneId: 'pane1',
  });
});

test('does not build a native target without a session or path', () => {
  assert.equal(pathMenuTarget('file', '/w/a.txt', ''), null);
  assert.equal(pathMenuTarget('file', '', 'session-1'), null);
  assert.equal(pathMenuTarget('other', '/w/a', 'session-1'), null);
});

test('reads the captured path target from the native menu snapshot', () => {
  assert.deepEqual(activePathTarget({ kind: 'file', value: { sessionId: 's', path: '/w/a' } }), {
    kind: 'file',
    path: '/w/a',
  });
  assert.deepEqual(activePathTarget({ kind: 'directory', value: { sessionId: 's', path: '/w/a' } }), {
    kind: 'directory',
    path: '/w/a',
  });
});

test('ignores other native targets and malformed snapshots', () => {
  assert.equal(activePathTarget(null), null);
  assert.equal(activePathTarget({ kind: 'workspace', value: { path: '/w' } }), null);
  assert.equal(activePathTarget({ kind: 'file', value: {} }), null);
  assert.equal(activePathTarget({ kind: 'file', value: { path: '' } }), null);
});
