import assert from 'node:assert/strict';
import test from 'node:test';
import { looksLikeComposerCommandMenu } from '../src/client/menu.ts';

test('recognizes the composer command menu by its stable command aliases', () => {
  assert.equal(looksLikeComposerCommandMenu([
    '文件 file',
    '目标 goal',
    '计划 plan',
    '反馈 feedback',
  ]), true);
  assert.equal(looksLikeComposerCommandMenu([
    'File file',
    'Goal goal',
    'Plan plan',
    'Compact compact',
  ]), true);
});

test('ignores other listboxes and filtered command lists', () => {
  assert.equal(looksLikeComposerCommandMenu(['文件 file']), false);
  assert.equal(looksLikeComposerCommandMenu(['文件 file', '目标 goal']), false);
  assert.equal(looksLikeComposerCommandMenu(['model', 'reasoning', 'auto']), false);
  assert.equal(looksLikeComposerCommandMenu(['file', 'goal', 'plan']), true);
});
