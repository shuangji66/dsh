import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';
import { VERSION } from '../src/version.ts';

const pkg = JSON.parse(await readFile(new URL('../package.json', import.meta.url), 'utf8')) as {
  version: string;
};

test('VERSION tracks package.json', () => {
  // Both halves report this value (host `/config` + `/health`, client
  // diagnostics); a stale copy shows up as a bogus "restart dsh" warning.
  assert.equal(VERSION, pkg.version);
});
