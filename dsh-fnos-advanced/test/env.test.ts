import assert from 'node:assert/strict';
import test from 'node:test';
import { isConsoleEmbeddedDsh, isFnosApp } from '../src/client/env.ts';

test('recognizes the fnOS app WebView on iOS and Android', () => {
  assert.equal(isFnosApp(
    'Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 '
    + '(KHTML, like Gecko) Mobile/15E148 FNOS/1.2.0800 FNAppType/iOS FNAppVer/1.37.3',
  ), true);
  assert.equal(isFnosApp(
    'Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 (KHTML, like Gecko) '
    + 'Chrome/140.0.0.0 Mobile Safari/537.36 FNAppType/Android FNAppVer/1.37.3',
  ), true);
});

test('plain browsers and the fnOS desktop iframe are not the app WebView', () => {
  assert.equal(isFnosApp('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15'), false);
  assert.equal(isFnosApp(''), false);
  assert.equal(isFnosApp('Mozilla/5.0 (iPhone) FNOS/1.2.0800 Mobile/15E148'), false);
});

test('recognizes the console embedded dsh mount but not the direct dsh entry', () => {
  assert.equal(isConsoleEmbeddedDsh('/app/Harness/gui/'), true);
  assert.equal(isConsoleEmbeddedDsh('/app/Harness/gui'), true);
  assert.equal(isConsoleEmbeddedDsh('/app/Harness/gui/fnos-advanced/x'), true);
  // 产品里的入口形态只有这两种：控制台入口下的 /gui/，以及 App 图标直连的 /dsh/。
  assert.equal(isConsoleEmbeddedDsh('/app/Harness/dsh/'), false);
  assert.equal(isConsoleEmbeddedDsh('/app/Harness/'), false);
  assert.equal(isConsoleEmbeddedDsh('/'), false);
  // 判定是「路径里出现 /gui/ 段」，其它挂载下的同名段也算内嵌 dsh（宁可按内嵌处理：
  // 误判只会少用宿主桥接，多一次网页授权；反过来则会在无桥接处调用原生选择器）。
  assert.equal(isConsoleEmbeddedDsh('/gui/'), true);
});
