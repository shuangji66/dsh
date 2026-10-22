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

/** Base name used for the `File` object (and therefore the attachment label). */
export function fileNameForPath(value: unknown): string {
  const text = String(value ?? '').replace(/[\\/]+$/, '');
  const base = text.slice(Math.max(text.lastIndexOf('/'), text.lastIndexOf('\\')) + 1);
  return base || 'file';
}
