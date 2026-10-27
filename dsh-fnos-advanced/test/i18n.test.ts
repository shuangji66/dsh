import assert from 'node:assert/strict';
import { readdir, readFile } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import { EN, normalizeHostLanguage, resolveHostLanguage, setHostLanguageResolver, setLanguagePreference, t, translateError } from '../src/client/i18n.ts';

const clientDir = fileURLToPath(new URL('../src/client', import.meta.url));

/** Every browser source file (`.ts` / `.tsx`, no declarations). */
async function clientSources(): Promise<string[]> {
  const entries = await readdir(clientDir, { recursive: true, withFileTypes: true });
  return entries
    .filter((entry) => entry.isFile() && /\.tsx?$/.test(entry.name) && !entry.name.endsWith('.d.ts'))
    .map((entry) => path.join(entry.parentPath, entry.name));
}

const keys = new Set<string>();
for (const file of await clientSources()) {
  const source = await readFile(file, 'utf8');
  for (const match of source.matchAll(/(?<![A-Za-z0-9_$])t\('((?:[^'\\]|\\.)+)'/g)) {
    keys.add(match[1].replace(/\\'/g, "'"));
  }
}
// Keys reached through a map instead of a literal call site.
for (const key of ['在飞牛中打开', '文件详情', '引用到对话', '复制路径']) keys.add(key);

test('every t() source string has an English translation', () => {
  const missing = [...keys].filter((key) => !(key in EN));
  assert.deepEqual(missing, []);
});

test('language preference switches translations and falls back to Chinese', () => {
  setLanguagePreference('en');
  assert.equal(t('取消'), 'Cancel');
  assert.equal(t('已添加 {count} 个文件', { count: 2 }), 'Added 2 file(s)');
  assert.equal(translateError('反代地址不是有效 URL'), 'The proxy address is not a valid URL');
  // Unknown runtime text (a host error) passes through untouched.
  assert.equal(translateError('Permission denied'), 'Permission denied');

  setLanguagePreference('zh');
  assert.equal(t('取消'), '取消');
  assert.equal(translateError('反代地址不是有效 URL'), '反代地址不是有效 URL');

  setLanguagePreference('system');
});

test('normalizes fnOS platform languages', () => {
  assert.equal(normalizeHostLanguage('zh-CN'), 'zh');
  assert.equal(normalizeHostLanguage('zh_TW'), 'zh');
  assert.equal(normalizeHostLanguage('en-US'), 'en');
  assert.equal(normalizeHostLanguage('en'), 'en');
  assert.equal(normalizeHostLanguage('ja-JP'), null);
  assert.equal(normalizeHostLanguage(undefined), null);
});

test('retired language values fold into system', () => {
  assert.equal(setLanguagePreference('auto'), 'system');
  assert.equal(setLanguagePreference('host'), 'system');
  setLanguagePreference('system');
});

test('follow-fnOS uses the platform language once resolved', async () => {
  setHostLanguageResolver(async () => ({ language: 'en-US' }));
  setLanguagePreference('system');
  await resolveHostLanguage(true);
  assert.equal(t('取消'), 'Cancel');

  setLanguagePreference('system');
  setHostLanguageResolver(null);
});
