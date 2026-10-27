import assert from 'node:assert/strict';
import test from 'node:test';
import { MAX_RECENT_DIRS, pushRecentPath } from '../src/client/recent-dirs.ts';

test('keeps the newest absolute folder first and drops duplicates', () => {
  assert.deepEqual(pushRecentPath([], '/vol1/work'), ['/vol1/work']);
  assert.deepEqual(pushRecentPath(['/vol1/a'], '/vol1/a/'), ['/vol1/a']);
  assert.deepEqual(
    pushRecentPath(['/vol1/a', '/vol1/b'], '/vol1/b'),
    ['/vol1/b', '/vol1/a'],
  );
});

test('caps the list and ignores relative or empty paths', () => {
  assert.deepEqual(
    pushRecentPath(['/vol1/a', '/vol1/b', '/vol1/c'], '/vol1/d', 2),
    ['/vol1/d', '/vol1/a'],
  );
  assert.deepEqual(pushRecentPath(['/vol1/a'], 'relative/path'), ['/vol1/a']);
  assert.deepEqual(pushRecentPath(['/vol1/a'], ''), ['/vol1/a']);
  assert.equal(MAX_RECENT_DIRS, 8);
});

test('normalizes the filesystem root', () => {
  assert.deepEqual(pushRecentPath([], '/'), ['/']);
  assert.deepEqual(pushRecentPath(['/'], '///'), ['/']);
});
