import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';
import { EN, setLanguagePreference, t, translateError } from '../src/client/i18n.js';

const source = await readFile(new URL('../src/client/index.js', import.meta.url), 'utf8');
const keys = new Set(
  [...source.matchAll(/(?<![A-Za-z0-9_$])t\('((?:[^'\\]|\\.)+)'/g)].map((match) => match[1].replace(/\\'/g, "'")),
);
for (const key of ['在飞牛中打开', '引用到对话', '复制路径']) keys.add(key);

test('every t() source string has an English translation', () => {
  const missing = [...keys].filter((key) => !(key in EN));
  assert.deepEqual(missing, []);
});

test('language preference switches translations and falls back to Chinese', () => {
  setLanguagePreference('en');
  assert.equal(t('取消'), 'Cancel');
  assert.equal(t('已添加 {count} 个文件', { count: 2 }), 'Added 2 file(s)');
  assert.equal(translateError('反代地址不是有效 URL'), 'The proxy address is not a valid URL');

  setLanguagePreference('zh');
  assert.equal(t('取消'), '取消');
  assert.equal(translateError('反代地址不是有效 URL'), '反代地址不是有效 URL');

  setLanguagePreference('auto');
});
