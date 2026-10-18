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
export const IMAGE_MIME_BY_EXTENSION = Object.freeze({
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.webp': 'image/webp',
  '.gif': 'image/gif',
});

/**
 * Lower-cased extension of a path, including the leading dot.
 *
 * @param {unknown} value
 * @returns {string}
 */
export function fileExtension(value) {
  const text = String(value == null ? '' : value);
  const base = text.slice(Math.max(text.lastIndexOf('/'), text.lastIndexOf('\\')) + 1);
  const dot = base.lastIndexOf('.');
  if (dot <= 0 || dot === base.length - 1) return '';
  return base.slice(dot).toLowerCase();
}

/**
 * MIME type for a supported image path, or an empty string.
 *
 * @param {unknown} value
 * @returns {string}
 */
export function imageMimeForPath(value) {
  return IMAGE_MIME_BY_EXTENSION[fileExtension(value)] || '';
}

/**
 * Whether the path is an image whose bytes must be uploaded for vision.
 *
 * @param {unknown} value
 * @returns {boolean}
 */
export function isImagePath(value) {
  return imageMimeForPath(value) !== '';
}

/**
 * Base name used for the `File` object (and therefore the attachment label).
 *
 * @param {unknown} value
 * @returns {string}
 */
export function fileNameForPath(value) {
  const text = String(value == null ? '' : value).replace(/[\\/]+$/, '');
  const base = text.slice(Math.max(text.lastIndexOf('/'), text.lastIndexOf('\\')) + 1);
  return base || 'file';
}
