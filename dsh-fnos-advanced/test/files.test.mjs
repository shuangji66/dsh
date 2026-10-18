import assert from 'node:assert/strict';
import test from 'node:test';
import {
  fileExtension,
  fileNameForPath,
  imageMimeForPath,
  isImagePath,
} from '../src/client/files.js';

test('classifies supported image paths', () => {
  assert.equal(imageMimeForPath('/vol1/1000/photos/a.png'), 'image/png');
  assert.equal(imageMimeForPath('/vol1/1000/photos/a.JPG'), 'image/jpeg');
  assert.equal(imageMimeForPath('/vol1/1000/photos/a.jpeg'), 'image/jpeg');
  assert.equal(imageMimeForPath('/vol1/1000/photos/a.webp'), 'image/webp');
  assert.equal(imageMimeForPath('/vol1/1000/photos/a.gif'), 'image/gif');
  assert.equal(isImagePath('/vol1/1000/photos/a.png'), true);
});

test('keeps non-images and unsupported formats out of the vision path', () => {
  for (const path of [
    '/vol1/1000/a.pdf',
    '/vol1/1000/a.txt',
    '/vol1/1000/a.heic',
    '/vol1/1000/a.svg',
    '/vol1/1000/.png',
    '/vol1/1000/a.',
  ]) {
    assert.equal(imageMimeForPath(path), '', path);
    assert.equal(isImagePath(path), false, path);
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
