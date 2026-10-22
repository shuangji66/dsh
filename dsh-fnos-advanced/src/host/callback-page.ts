/**
 * The fnOS authorization callback page.
 *
 * fnOS redirects the popup here with the authorization result in the query
 * string. The page hands that result back through three independent channels,
 * because each one can be lost on its own:
 *
 *   1. `postMessage` to the opener — fastest, gone when COOP severs the opener;
 *   2. `localStorage` — survives the severed opener, gone with storage
 *      partitioning in some browsers;
 *   3. `POST auth-result` to the host half — same-origin, so the dsh page can
 *      always poll it.
 *
 * The markup is intentionally dependency-free and self-contained: it must load
 * with the plugin's strict CSP (`default-src 'none'`), which forbids any
 * external request.
 */
export const CALLBACK_HTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>fnOS / 飞牛授权</title>
<style>
  :root { color-scheme: light dark; }
  html, body { height: 100%; margin: 0; }
  body {
    display: flex; align-items: center; justify-content: center;
    font: 14px/1.6 -apple-system, BlinkMacSystemFont, "PingFang SC", "Segoe UI", system-ui, sans-serif;
    color: #1f2328; background: #f6f7f9;
  }
  @media (prefers-color-scheme: dark) {
    body { color: #e6e6e6; background: #17181c; }
    .card { background: #202127 !important; border-color: #31333b !important; }
    .path { background: #17181c !important; }
  }
  .card {
    width: min(460px, calc(100vw - 48px));
    box-sizing: border-box; padding: 24px;
    background: #fff; border: 1px solid #e3e5e8; border-radius: 14px;
    box-shadow: 0 12px 40px rgba(0, 0, 0, .08);
  }
  h1 { margin: 0 0 8px; font-size: 18px; font-weight: 600; }
  p { margin: 0 0 14px; color: #6a6f76; }
  .path {
    margin: 0 0 16px; padding: 10px 12px; border-radius: 8px;
    background: #f2f3f5; word-break: break-all;
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-size: 12px;
  }
  .error { color: #c62828; }
  button {
    height: 36px; padding: 0 16px; border-radius: 8px; cursor: pointer;
    border: 1px solid #d0d3d7; background: transparent; color: inherit; font: inherit;
  }
  button:hover { background: rgba(127, 127, 127, .08); }
</style>
</head>
<body>
<div class="card">
  <h1 id="title">正在返回 DeepSeek Harness…</h1>
  <p id="message">授权结果已收到。</p>
  <div class="path" id="path" hidden></div>
  <button type="button" id="close" hidden>关闭窗口</button>
</div>
<script>
(function () {
  var params = new URLSearchParams(window.location.search);
  var rawPath = params.get('path');
  var paths = [];
  if (rawPath) {
    try {
      var parsed = JSON.parse(rawPath);
      if (Array.isArray(parsed)) {
        paths = parsed.filter(function (item) { return typeof item === 'string' && item.length > 0; });
      }
    } catch (_) { /* keep paths empty */ }
  }
  var result = {
    status: params.get('status') || '',
    error: params.get('error') || '',
    method: params.get('method') || '',
    appName: params.get('appName') || '',
    state: params.get('state') || '',
    path: paths
  };
  var payload = {
    type: 'dsh-fnos-advanced:auth',
    state: result.state,
    result: result,
    search: window.location.search
  };

  // The opener may have been severed by a cross-origin redirect (COOP), so the
  // storage copy is the reliable channel; the postMessage is the fast path.
  try {
    window.localStorage.setItem('dsh-fnos-advanced:auth-result', JSON.stringify({
      state: result.state,
      result: result,
      search: window.location.search,
      ts: Date.now()
    }));
  } catch (_) { /* private mode / storage disabled */ }

  try {
    if (window.opener && !window.opener.closed) {
      window.opener.postMessage(payload, window.location.origin);
    }
  } catch (_) { /* cross-origin opener */ }

  // Third channel: hand the raw query to the same-origin host route. The dsh
  // page polls it, so neither COOP nor storage partitioning can lose the
  // result.
  try {
    var resultUrl = new URL('auth-result', window.location.href).toString();
    fetch(resultUrl, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ state: result.state, search: window.location.search }),
      keepalive: true
    }).catch(function () {});
  } catch (_) { /* fetch unavailable */ }

  var authorize = result.method === 'authorizeUserFile' || result.method === 'authorizeSharedFile';
  var ok = result.status === 'success' && (paths.length > 0 || authorize);
  var zh = /^zh/i.test(navigator.language || '');
  var title = document.getElementById('title');
  var message = document.getElementById('message');
  var path = document.getElementById('path');
  var close = document.getElementById('close');

  document.title = zh ? '飞牛授权' : 'fnOS authorization';
  document.documentElement.lang = zh ? 'zh-CN' : 'en';

  if (ok) {
    title.textContent = zh ? '授权成功' : 'Authorized';
    message.textContent = zh
      ? '已授权给飞牛应用，正在返回 DeepSeek Harness。'
      : 'Authorized for the fnOS app. Returning to DeepSeek Harness.';
    if (paths.length) {
      path.textContent = paths[0];
      path.hidden = false;
    }
  } else {
    title.textContent = zh ? '未完成授权' : 'Authorization not completed';
    title.classList.add('error');
    message.textContent = result.error || (zh
      ? '授权已取消，请回到 DeepSeek Harness 重试。'
      : 'Authorization was cancelled. Return to DeepSeek Harness and retry.');
  }

  close.hidden = false;
  close.addEventListener('click', function () { window.close(); });
  // This page is normally opened by the picker, so the browser allows the
  // script to close it even when COOP severed window.opener.
  window.setTimeout(function () { window.close(); }, ok ? 900 : 1400);
})();
</script>
</body>
</html>`;
