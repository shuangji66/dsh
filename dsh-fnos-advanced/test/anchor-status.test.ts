import assert from 'node:assert/strict';
import test from 'node:test';
import {
  anchorObservation,
  classifyAnchor,
  pickComposerObservation,
  pickFileTreeObservation,
  rememberComposerObservation,
  rememberFileTreeObservation,
  resetAnchorRecordingForTest,
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

// 0.6.3 的状态机本身：null（当前页没挂载对应容器）不能把上一次的观测抹掉，
// 否则从对话页切到插件页、再点一次「重新检测」就会退化成「未检测」。
test('a null observation keeps the previous one', () => {
  resetAnchorRecordingForTest();
  assert.equal(anchorObservation('composer'), null);

  rememberComposerObservation({ at: AT, found: true });
  rememberComposerObservation(null);
  assert.deepEqual(anchorObservation('composer'), { at: AT, found: true });
  assert.equal(classifyAnchor(anchorObservation('composer')), 'found');

  // 后一次真实观测覆盖前一次（诊断显示「最近一次」，不是「曾经有过」）。
  rememberComposerObservation({ at: AT + 1000, found: false });
  assert.deepEqual(anchorObservation('composer'), { at: AT + 1000, found: false });
  assert.equal(classifyAnchor(anchorObservation('composer')), 'missing');
});

test('file tree observation records the row count and survives a null', () => {
  resetAnchorRecordingForTest();

  rememberFileTreeObservation(pickFileTreeObservation([{ rows: 3, rowLike: 3 }], AT));
  rememberFileTreeObservation(null);
  assert.deepEqual(anchorObservation('fileTree'), { at: AT, found: true, count: 3 });

  rememberFileTreeObservation(pickFileTreeObservation([{ rows: 0, rowLike: 0 }], AT + 1));
  assert.equal(classifyAnchor(anchorObservation('fileTree')), 'empty');
});
