/**
 * Composer intake: turn picked fnOS paths into dsh attachments.
 *
 * - non-images: an empty `File` plus a `__DSH_HOST_PATHS__` mapping, so dsh
 *   inserts a zero-copy `@path` reference instead of uploading bytes;
 * - images: read through the host's one-time token route (vision needs bytes)
 *   and fall back to a path reference when the host cannot serve them.
 */
import { LOG_PREFIX } from './constants.ts';
import { isVisible, showToast } from './dom.ts';
import { fileNameForPath, imageMimeForPath } from './files.ts';
import { dispatchComposerFiles, ensureHostPathBridge, setHostPath } from './host-paths.ts';
import { t } from './i18n.ts';
import { loadSettings, readImageFile } from './plugin-api.ts';

export interface ComposerTarget {
  input: HTMLInputElement;
  region: Element;
}

/** The visible composer's hidden file input (menus are portaled outside it). */
export function findComposerFileInput(region: Element): HTMLInputElement | null {
  const inputs = Array.from(region.querySelectorAll<HTMLInputElement>('input[type="file"]'));
  return inputs.find((input) => input.multiple && !input.disabled) || null;
}

/** Hidden file input of the visible conversation composer. */
export function findActiveComposerInput(): ComposerTarget | null {
  for (const region of document.querySelectorAll('[data-conversation-region="composer"]')) {
    if (!isVisible(region)) continue;
    const input = findComposerFileInput(region);
    if (input) return { input, region };
  }
  return null;
}

/** Attach the picked paths to the composer input. */
export async function addPathsToComposer(paths: readonly unknown[], input: HTMLInputElement): Promise<void> {
  const settings = await loadSettings();
  ensureHostPathBridge();

  const files: File[] = [];
  let images = 0;
  let unreadable = 0;

  for (const raw of paths) {
    const path = String(raw);
    const mime = imageMimeForPath(path);
    if (mime && settings.imageRead !== false) {
      try {
        const file = await readImageFile(path, mime);
        setHostPath(file, path);
        files.push(file);
        images += 1;
        continue;
      } catch (error) {
        console.warn(LOG_PREFIX, 'image bytes unavailable; adding a path reference instead', error);
        unreadable += 1;
      }
    }
    const file = new File([], fileNameForPath(path), { type: 'application/octet-stream' });
    setHostPath(file, path);
    files.push(file);
  }

  if (!files.length) throw new Error(t('没有可添加的文件'));
  dispatchComposerFiles(input, files);

  if (unreadable > 0) {
    showToast(t('{count} 张图片未能读取，已改为路径引用', { count: unreadable }));
  } else if (images > 0) {
    showToast(t('已添加 {count} 个文件（{images} 张图片会上传）', { count: files.length, images }));
  }
}

/** Quote one workspace file path into the active conversation. */
export async function quotePathToComposer(path: string): Promise<void> {
  const active = findActiveComposerInput();
  if (!active) {
    showToast(t('没有找到对话输入框，无法引用。'));
    return;
  }
  await addPathsToComposer([path], active.input);
}
