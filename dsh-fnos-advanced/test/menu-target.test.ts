import assert from 'node:assert/strict';
import test from 'node:test';
import {
  absoluteTargetPath,
  activePathTarget,
  contextFromRowKey,
  pathMenuTarget,
} from '../src/client/menu-target.ts';

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
    sessionId: 's',
  });
  assert.deepEqual(activePathTarget({ kind: 'directory', value: { sessionId: 's', path: '/w/a' } }), {
    kind: 'directory',
    path: '/w/a',
    sessionId: 's',
  });
  // The owning session is optional in the target value; a missing one must not
  // throw (older surfaces publish path-only values).
  assert.deepEqual(activePathTarget({ kind: 'file', value: { path: '/w/a' } }), {
    kind: 'file',
    path: '/w/a',
    sessionId: '',
  });
});

test('ignores other native targets and malformed snapshots', () => {
  assert.equal(activePathTarget(null), null);
  assert.equal(activePathTarget({ kind: 'workspace', value: { path: '/w' } }), null);
  assert.equal(activePathTarget({ kind: 'file', value: {} }), null);
  assert.equal(activePathTarget({ kind: 'file', value: { path: '' } }), null);
});

test('resolves captured paths against the owning Session root', () => {
  const cwdOf = (id: string): string => (id === 's1' ? '/vol1/work' : '');
  assert.equal(absoluteTargetPath({ path: '/vol1/work/a.txt', sessionId: 's1' }, cwdOf), '/vol1/work/a.txt');
  assert.equal(absoluteTargetPath({ path: 'src/a.ts', sessionId: 's1' }, cwdOf), '/vol1/work/src/a.ts');
  assert.equal(absoluteTargetPath({ path: './src/a.ts', sessionId: 's1' }, cwdOf), '/vol1/work/src/a.ts');
  // An unknown root must not invent one; the raw value is kept (the same
  // behaviour the plugin's own menu has for a path it cannot resolve).
  assert.equal(absoluteTargetPath({ path: 'src/a.ts', sessionId: 'missing' }, cwdOf), 'src/a.ts');
  assert.equal(absoluteTargetPath({ path: '', sessionId: 's1' }, cwdOf), '');
});
