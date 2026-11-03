import assert from 'node:assert/strict';
import test from 'node:test';
import { isConsoleEmbeddedDsh, isConsoleEmbeddedPath, isFnosApp } from '../src/client/env.ts';

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

test('recognizes the console-embedded dsh by its parent page, not by the mount path', () => {
  // 控制台 iframe：父页面就是同一 base 下的控制台 SPA。
  assert.equal(isConsoleEmbeddedPath('/app/Harness/dsh/', '/app/Harness/'), true);
  assert.equal(isConsoleEmbeddedPath('/app/Harness/dsh/session/1', '/app/Harness/'), true);
  assert.equal(isConsoleEmbeddedPath('/app/Harness/dsh', '/app/Harness'), true);
  // 父页面是 fnOS 外壳（图标入口）：不是内嵌，保留原生桥接。
  assert.equal(isConsoleEmbeddedPath('/app/Harness/dsh/', '/'), false);
  assert.equal(isConsoleEmbeddedPath('/app/Harness/dsh/', '/index'), false);
  // 父页面跨源读不到：按内嵌处理（宁可按无桥接）。
  assert.equal(isConsoleEmbeddedPath('/app/Harness/dsh/', null), true);
  // 不是 <base>/dsh 形态：认不出，按非内嵌处理。
  assert.equal(isConsoleEmbeddedPath('/app/Harness/', '/app/Harness/'), false);
  assert.equal(isConsoleEmbeddedPath('/', '/'), false);
});

test('recognizes the console-embedded dsh when the console is mounted at the site root', () => {
  // AdminBaseURL 为空时 dsh 就挂在 /dsh/：base 是合法的空串，不能和「认不出」混为一谈。
  assert.equal(isConsoleEmbeddedPath('/dsh/', '/'), true);
  assert.equal(isConsoleEmbeddedPath('/dsh/session/1', '/index.html'), true);
  assert.equal(isConsoleEmbeddedPath('/dsh/', '/?view=overview'), true);
  assert.equal(isConsoleEmbeddedPath('/dsh/', null), true);
  // 路径里没有 /dsh 段时仍然认不出。
  assert.equal(isConsoleEmbeddedPath('/api/x', '/'), false);
});

test('isConsoleEmbeddedDsh reads the parent window path at runtime', () => {
  const g = globalThis as { window?: unknown };
  const prev = g.window;
  try {
    const embedded: any = { location: { pathname: '/app/Harness/dsh/' } };
    embedded.parent = { location: { pathname: '/app/Harness/' } };
    g.window = embedded;
    assert.equal(isConsoleEmbeddedDsh(), true);

    // 顶层窗口（App 图标入口直接加载 / 浏览器新标签页）不是内嵌。
    const top: any = { location: { pathname: '/app/Harness/dsh/' } };
    top.parent = top;
    g.window = top;
    assert.equal(isConsoleEmbeddedDsh(), false);

    // 父页面跨源、读不到 location：按内嵌处理（宁可按无宿主桥接）。
    const cross: any = { location: { pathname: '/app/Harness/dsh/' } };
    cross.parent = { get location(): any { throw new Error('cross-origin'); } };
    g.window = cross;
    assert.equal(isConsoleEmbeddedDsh(), true);
  } finally {
    g.window = prev;
  }
});
