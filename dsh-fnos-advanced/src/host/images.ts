/**
 * Image byte bridge.
 *
 * The picker only ever reports paths. A file reference (`@path`) needs no bytes,
 * but an image does: dsh uploads it so the model can see it. This route hands
 * out a one-time token for one supported image, then streams exactly that file.
 *
 * It is deliberately narrow: absolute path, no hidden segment, not inside
 * `DSH_HOME`, PNG/JPEG/WebP/GIF only, regular non-empty file, size capped, one
 * read per token, same-origin requests only. This is a picker bridge, not a
 * general file server.
 */
import { randomBytes } from 'node:crypto';
import { closeSync, createReadStream, fstatSync, openSync, realpathSync } from 'node:fs';
import { basename, extname, isAbsolute, normalize } from 'node:path';
import type { IncomingMessage, ServerResponse } from 'node:http';
import {
  httpError,
  isHttpError,
  queryParam,
  readBody,
  sameOriginRequest,
  writeJSON,
  writeMethodNotAllowed,
} from './http.ts';
import { dshHome, errorMessage, hasHiddenSegment, isInsidePath } from './paths.ts';
import { readSettings } from './settings.ts';

/** Image formats dsh accepts as vision attachments. */
const IMAGE_MIME_BY_EXTENSION: Readonly<Record<string, string>> = Object.freeze({
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.webp': 'image/webp',
  '.gif': 'image/gif',
});

const MAX_IMAGE_BYTES = 8 * 1024 * 1024;
const MAX_PATH_LENGTH = 4096;
const FILE_TOKEN_TTL_MS = 60 * 1000;
const MAX_PENDING_FILES = 200;

interface OpenedImage {
  fd: number;
  path: string;
  mime: string;
  size: number;
  name: string;
}

interface PendingFile extends OpenedImage {
  ts: number;
}

/** token → opened file, valid for {@link FILE_TOKEN_TTL_MS}. */
const pendingFiles = new Map<string, PendingFile>();

function closeQuietly(fd: number): void {
  try { closeSync(fd); } catch { /* already closed */ }
}

/** Close and forget expired tokens so an abandoned pick leaks no descriptors. */
function pruneFileTokens(): void {
  const now = Date.now();
  for (const [token, entry] of pendingFiles) {
    if (now - entry.ts <= FILE_TOKEN_TTL_MS) continue;
    closeQuietly(entry.fd);
    pendingFiles.delete(token);
  }
}

/**
 * Validate a path for the byte bridge and open it.
 *
 * @throws {HttpError} with the status the route should answer with.
 */
function openImageForRead(input: unknown): OpenedImage {
  if (typeof input !== 'string' || !input || input.length > MAX_PATH_LENGTH || input.includes('\0')) {
    throw httpError(400, '图片路径无效');
  }
  if (!isAbsolute(input)) throw httpError(400, '图片路径必须是绝对路径');

  const normalized = normalize(input);
  if (hasHiddenSegment(normalized)) throw httpError(403, '不允许读取隐藏路径');

  let real: string;
  try {
    real = realpathSync(normalized);
  } catch {
    throw httpError(404, '图片不存在');
  }
  // Re-check after resolving symlinks: the link itself may live in a visible
  // directory while pointing at a hidden one.
  if (hasHiddenSegment(real)) throw httpError(403, '不允许读取隐藏路径');
  if (isInsidePath(real, dshHome())) throw httpError(403, '不允许读取 dsh 数据目录');

  const mime = IMAGE_MIME_BY_EXTENSION[extname(real).toLowerCase()];
  if (!mime) throw httpError(415, '只支持 PNG/JPEG/WebP/GIF 图片');

  let fd: number;
  try {
    fd = openSync(real, 'r');
  } catch {
    throw httpError(404, '图片不存在');
  }

  let stat;
  try {
    stat = fstatSync(fd);
  } catch {
    closeQuietly(fd);
    throw httpError(404, '图片不存在');
  }
  if (!stat.isFile()) {
    closeQuietly(fd);
    throw httpError(400, '不是普通文件');
  }
  if (stat.size <= 0) {
    closeQuietly(fd);
    throw httpError(400, '图片内容为空');
  }
  if (stat.size > MAX_IMAGE_BYTES) {
    closeQuietly(fd);
    throw httpError(413, `图片超过 ${MAX_IMAGE_BYTES / 1024 / 1024} MB`);
  }

  return { fd, path: real, mime, size: stat.size, name: basename(real) };
}

/** `POST /fnos-advanced/file-token { path }` → one-time download token. */
export async function handleFileToken(req: IncomingMessage, res: ServerResponse): Promise<void> {
  if (req.method !== 'POST') {
    writeMethodNotAllowed(res, 'POST');
    return;
  }
  if (!sameOriginRequest(req)) {
    writeJSON(res, 403, { ok: false, error: 'cross-origin request rejected' });
    return;
  }
  if (readSettings().imageRead === false) {
    writeJSON(res, 403, { ok: false, error: '插件设置已关闭图片读取' });
    return;
  }

  let body: { path?: unknown };
  try {
    body = JSON.parse(await readBody(req)) as { path?: unknown };
  } catch {
    writeJSON(res, 400, { ok: false, error: 'invalid request body' });
    return;
  }

  let opened: OpenedImage;
  try {
    opened = openImageForRead(body?.path);
  } catch (error) {
    writeJSON(res, isHttpError(error) ? error.status : 400, {
      ok: false,
      error: isHttpError(error) ? error.message : errorMessage(error, 'invalid image path'),
    });
    return;
  }

  pruneFileTokens();
  if (pendingFiles.size >= MAX_PENDING_FILES) {
    const oldest = pendingFiles.keys().next().value;
    if (oldest !== undefined) {
      const entry = pendingFiles.get(oldest);
      if (entry) closeQuietly(entry.fd);
      pendingFiles.delete(oldest);
    }
  }

  const token = randomBytes(24).toString('base64url');
  pendingFiles.set(token, { ...opened, ts: Date.now() });
  writeJSON(res, 200, { ok: true, token, mime: opened.mime, size: opened.size, name: opened.name });
}

/** `GET|HEAD /fnos-advanced/file-bytes?token=…` → the image bytes, once. */
export function handleFileBytes(req: IncomingMessage, res: ServerResponse): void {
  if (req.method !== 'GET' && req.method !== 'HEAD') {
    writeMethodNotAllowed(res, 'GET, HEAD');
    return;
  }
  if (!sameOriginRequest(req)) {
    writeJSON(res, 403, { ok: false, error: 'cross-origin request rejected' });
    return;
  }

  pruneFileTokens();
  const token = queryParam(req, 'token');
  const entry = token ? pendingFiles.get(token) : undefined;
  if (!entry) {
    writeJSON(res, 404, { ok: false, error: 'token not found' });
    return;
  }
  // One-time: a token cannot be replayed even while it is still fresh.
  pendingFiles.delete(token);

  let size = entry.size;
  try { size = fstatSync(entry.fd).size; } catch { /* keep the token-time size */ }
  if (size <= 0) {
    closeQuietly(entry.fd);
    writeJSON(res, 404, { ok: false, error: 'file not found' });
    return;
  }

  res.writeHead(200, {
    'Content-Type': entry.mime,
    'Content-Length': size,
    'Cache-Control': 'no-store',
    'X-Content-Type-Options': 'nosniff',
    'Content-Disposition': 'inline',
  });
  if (req.method === 'HEAD') {
    closeQuietly(entry.fd);
    res.end();
    return;
  }

  const stream = createReadStream(entry.path, { fd: entry.fd, start: 0, end: size - 1, autoClose: true });
  stream.on('error', () => {
    try { res.destroy(); } catch { /* already gone */ }
  });
  stream.pipe(res);
}
