import assert from 'node:assert/strict';
import test from 'node:test';
import {
  fileExtension,
  fileNameForPath,
  imageMimeForPath,
  isImagePath,
  resolveAbsolutePath,
  unsupportedImageFormat,
} from '../src/client/files.ts';

test('classifies supported image paths', () => {
  assert.equal(imageMimeForPath('/vol1/1000/photos/a.png'), 'image/png');
  assert.equal(imageMimeForPath('/vol1/1000/photos/a.JPG'), 'image/jpeg');
  assert.equal(imageMimeForPath('/vol1/1000/photos/a.jpeg'), 'image/jpeg');
  assert.equal(imageMimeForPath('/vol1/1000/photos/a.webp'), 'image/webp');
  assert.equal(imageMimeForPath('/vol1/1000/photos/a.gif'), 'image/gif');
  assert.equal(isImagePath('/vol1/1000/photos/a.png'), true);
});

test('keeps non-images and unsupported formats out of the vision path', () => {
  for (const filePath of [
    '/vol1/1000/a.pdf',
    '/vol1/1000/a.txt',
    '/vol1/1000/a.heic',
    '/vol1/1000/a.svg',
    '/vol1/1000/.png',
    '/vol1/1000/a.',
  ]) {
    assert.equal(imageMimeForPath(filePath), '', filePath);
    assert.equal(isImagePath(filePath), false, filePath);
  }
});

test('extracts extensions and file names from POSIX and Windows paths', () => {
  assert.equal(fileExtension('/vol1/a.tar.GZ'), '.gz');
  assert.equal(fileExtension('C:\\photos\\a.PNG'), '.png');
  assert.equal(fileExtension('no-extension'), '');
  assert.equal(fileNameForPath('/vol1/1000/photos/a.png'), 'a.png');
  assert.equal(fileNameForPath('C:\\photos\\a.png'), 'a.png');
  assert.equal(fileNameForPath('/vol1/1000/photos/'), 'photos');
  assert.equal(fileNameForPath(''), 'file');
});

test('names image formats that cannot be sent to vision', () => {
  assert.equal(unsupportedImageFormat('/vol1/IMG_0001.HEIC'), 'HEIC');
  assert.equal(unsupportedImageFormat('/vol1/a.heif'), 'HEIF');
  assert.equal(unsupportedImageFormat('/vol1/a.dng'), 'DNG');
  assert.equal(unsupportedImageFormat('/vol1/a.png'), '');
  assert.equal(unsupportedImageFormat('/vol1/a.pdf'), '');
});

test('resolves review paths against the session cwd', () => {
  assert.equal(resolveAbsolutePath('/vol1/work', 'src/a.ts'), '/vol1/work/src/a.ts');
  assert.equal(resolveAbsolutePath('/vol1/work/', './src/a.ts'), '/vol1/work/src/a.ts');
  assert.equal(resolveAbsolutePath('/', 'src/a.ts'), '/src/a.ts');
  // Absolute paths (changed files outside the workspace) are kept verbatim.
  assert.equal(resolveAbsolutePath('/vol1/work', '/vol1/other/a.ts'), '/vol1/other/a.ts');
  // An unknown session root must not turn into a bogus relative path.
  assert.equal(resolveAbsolutePath('', 'src/a.ts'), '');
  assert.equal(resolveAbsolutePath(undefined, 'src/a.ts'), '');
  assert.equal(resolveAbsolutePath('/vol1/work', ''), '');
});
