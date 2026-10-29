import assert from 'node:assert/strict';
import test from 'node:test';
import { isFnosApp } from '../src/client/env.ts';

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
