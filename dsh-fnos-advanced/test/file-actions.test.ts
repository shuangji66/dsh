import assert from 'node:assert/strict';
import test from 'node:test';
import { fileRowActions, nativeFileActions } from '../src/client/file-actions.ts';

test('file rows offer open/details/quote/copy in a fnOS host', () => {
  assert.deepEqual(fileRowActions('file', true), ['open', 'details', 'quote', 'copy']);
});

test('directories cannot be quoted through the composer file input', () => {
  assert.deepEqual(fileRowActions('directory', true), ['open', 'details', 'copy']);
});

test('a plain browser keeps client-side actions only', () => {
  assert.deepEqual(fileRowActions('file', false), ['quote', 'copy']);
  assert.deepEqual(fileRowActions('directory', false), ['copy']);
});

test('unsupported entries get no actions', () => {
  assert.deepEqual(fileRowActions('other', true), []);
  assert.deepEqual(fileRowActions('', true), []);
});

test('native menu skips the directory open when its reveal row is re-wired', () => {
  assert.deepEqual(nativeFileActions('directory', true, true), ['details']);
});

test('native menu keeps a directory open action without the reveal adapter', () => {
  assert.deepEqual(nativeFileActions('directory', true, false), ['open', 'details']);
});

test('native menu keeps file-only actions in a plain browser', () => {
  assert.deepEqual(nativeFileActions('file', false, true), ['quote']);
  assert.deepEqual(nativeFileActions('directory', false, true), []);
});

test('native menu keeps open/details/quote for a file in a fnOS host', () => {
  assert.deepEqual(nativeFileActions('file', true, true), ['open', 'details', 'quote']);
});

test('native menu ignores unsupported entries', () => {
  assert.deepEqual(nativeFileActions('other', true, true), []);
});
