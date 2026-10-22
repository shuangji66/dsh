import assert from 'node:assert/strict';
import test from 'node:test';
import { fileRowActions } from '../src/client/file-actions.ts';

test('file rows offer open/quote/copy in a fnOS host', () => {
  assert.deepEqual(fileRowActions('file', true), ['open', 'quote', 'copy']);
});

test('directories cannot be quoted through the composer file input', () => {
  assert.deepEqual(fileRowActions('directory', true), ['open', 'copy']);
});

test('a plain browser keeps client-side actions only', () => {
  assert.deepEqual(fileRowActions('file', false), ['quote', 'copy']);
  assert.deepEqual(fileRowActions('directory', false), ['copy']);
});

test('unsupported entries get no actions', () => {
  assert.deepEqual(fileRowActions('other', true), []);
  assert.deepEqual(fileRowActions('', true), []);
});
