import assert from 'node:assert/strict';
import test from 'node:test';
import { MAX_AUTHORIZED_DIRS, pickAuthorizedDirs } from '../src/client/authorized-dirs.ts';

/** Minimal workspace record; the picker only cares about `path`. */
function workspace(path: string, id = path): { id: string; path: string; title: string } {
  return { id, path, title: '' };
}

test('keeps registry order, normalizes paths and drops duplicates', () => {
  assert.deepEqual(
    pickAuthorizedDirs([
      workspace('/vol1/work', 'a'),
      workspace('/vol1//photos/', 'b'),
      workspace('/vol1/work/', 'c'),
    ]),
    ['/vol1/work', '/vol1/photos'],
  );
});

test('only absolute folders reach the picker input', () => {
  assert.deepEqual(
    pickAuthorizedDirs([
      workspace('relative/path'),
      workspace(''),
      { id: 'x', path: undefined as unknown as string, title: '' },
      workspace('/'),
    ]),
    ['/'],
  );
});

test('caps the row at the documented maximum', () => {
  const many = Array.from({ length: 12 }, (_, index) => workspace(`/vol1/dir-${index}`));
  const picked = pickAuthorizedDirs(many);
  assert.equal(picked.length, MAX_AUTHORIZED_DIRS);
  assert.equal(picked[0], '/vol1/dir-0');
  assert.equal(picked.at(-1), `/vol1/dir-${MAX_AUTHORIZED_DIRS - 1}`);
  assert.deepEqual(pickAuthorizedDirs(many, 0), []);
  assert.deepEqual(pickAuthorizedDirs(many, -1), []);
});
