/**
 * File-path helpers shared by the composer intake (pure, unit-tested).
 *
 * dsh decides between an uploaded image and a zero-copy `@path` reference from
 * the browser `File` object: images are always uploaded (vision needs bytes),
 * everything else becomes a reference when `__DSH_HOST_PATHS__` reports a real
 * path. These helpers keep that split in one place instead of scattering
 * extension checks through the DOM code.
 */

/** Image formats dsh accepts as vision attachments. */
export const IMAGE_MIME_BY_EXTENSION: Readonly<Record<string, string>> = Object.freeze({
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.webp': 'image/webp',
  '.gif': 'image/gif',
});

/** Lower-cased extension of a path, including the leading dot. */
export function fileExtension(value: unknown): string {
  const text = String(value ?? '');
  const base = text.slice(Math.max(text.lastIndexOf('/'), text.lastIndexOf('\\')) + 1);
  const dot = base.lastIndexOf('.');
  if (dot <= 0 || dot === base.length - 1) return '';
  return base.slice(dot).toLowerCase();
}

/** MIME type for a supported image path, or an empty string. */
export function imageMimeForPath(value: unknown): string {
  return IMAGE_MIME_BY_EXTENSION[fileExtension(value)] || '';
}

/** Whether the path is an image whose bytes must be uploaded for vision. */
export function isImagePath(value: unknown): boolean {
  return imageMimeForPath(value) !== '';
}

/**
 * Image formats users commonly have on a NAS but dsh cannot feed to vision.
 *
 * HEIC/HEIF is the iPhone camera default, so silently turning it into a path
 * reference (the model then cannot see it) is the most confusing outcome. The
 * composer uses this only for a clear notice; no conversion is attempted.
 */
export const UNSUPPORTED_IMAGE_FORMATS: Readonly<Record<string, string>> = Object.freeze({
  '.heic': 'HEIC',
  '.heif': 'HEIF',
  '.avif': 'AVIF',
  '.tif': 'TIFF',
  '.tiff': 'TIFF',
  '.bmp': 'BMP',
  '.svg': 'SVG',
  '.raw': 'RAW',
  '.dng': 'DNG',
  '.cr2': 'CR2',
  '.nef': 'NEF',
  '.arw': 'ARW',
});

/** Human-readable format name for an unsupported image, or an empty string. */
export function unsupportedImageFormat(value: unknown): string {
  return UNSUPPORTED_IMAGE_FORMATS[fileExtension(value)] || '';
}

/**
 * Resolve a workspace-relative path against its session root.
 *
 * dsh's change review publishes paths relative to the session cwd (absolute
 * only when the file lies outside it), while "open in fnOS" always needs the
 * absolute path. An unknown root leaves the result empty instead of guessing.
 */
export function resolveAbsolutePath(cwd: unknown, value: unknown): string {
  const text = String(value ?? '');
  if (!text) return '';
  if (text.startsWith('/')) return text;
  const root = String(cwd ?? '');
  if (root === '/') return `/${text.replace(/^\.\//, '')}`;
  const base = root.replace(/\/+$/, '');
  if (!base.startsWith('/')) return '';
  return `${base}/${text.replace(/^\.\//, '')}`;
}

/** Base name used for the `File` object (and therefore the attachment label). */
export function fileNameForPath(value: unknown): string {
  const text = String(value ?? '').replace(/[\\/]+$/, '');
  const base = text.slice(Math.max(text.lastIndexOf('/'), text.lastIndexOf('\\')) + 1);
  return base || 'file';
}
