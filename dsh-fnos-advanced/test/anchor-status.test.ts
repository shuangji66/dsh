import assert from 'node:assert/strict';
import test from 'node:test';
import {
  classifyAnchor,
  pickComposerObservation,
  pickFileTreeObservation,
} from '../src/client/anchor-status.ts';

const AT = 1_700_000_000_000;

test('composer: nothing to observe without a mounted region', () => {
  assert.equal(pickComposerObservation([], AT), null);
});

test('composer: the region counts only with a multi-file input', () => {
  assert.deepEqual(pickComposerObservation([{ hasMultiFileInput: true }], AT), {
    at: AT,
    found: true,
  });

  const missing = pickComposerObservation([{ hasMultiFileInput: false }], AT);
  assert.deepEqual(missing, { at: AT, found: false });
  assert.equal(classifyAnchor(missing), 'missing');
});

test('file tree: rows carry the anchors', () => {
  const seen = pickFileTreeObservation([{ rows: 20, rowLike: 20 }], AT);
  assert.deepEqual(seen, { at: AT, found: true, count: 20 });
  assert.equal(classifyAnchor(seen), 'found');
});

test('file tree: an empty folder is not a missing anchor', () => {
  const empty = pickFileTreeObservation([{ rows: 0, rowLike: 0 }], AT);
  assert.deepEqual(empty, { at: AT, found: false, empty: true });
  assert.equal(classifyAnchor(empty), 'empty');
});

test('file tree: row-shaped elements without anchors mean the markup changed', () => {
  const missing = pickFileTreeObservation([{ rows: 0, rowLike: 12 }], AT);
  assert.deepEqual(missing, { at: AT, found: false, empty: false });
  assert.equal(classifyAnchor(missing), 'missing');
});

test('classification keeps "never observed" distinct from "missing"', () => {
  assert.equal(classifyAnchor(null), 'never');
});
