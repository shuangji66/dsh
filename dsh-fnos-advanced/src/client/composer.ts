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
import { fileNameForPath, imageMimeForPath, unsupportedImageFormat } from './files.ts';
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
  let unsupported = 0;
  let disabled = 0;

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
    } else if (mime) {
      disabled += 1;
    } else if (unsupportedImageFormat(path)) {
      // HEIC/HEIF/RAW…: the model cannot see these, so say so instead of
      // silently handing it a path reference that looks like a normal file.
      unsupported += 1;
    }
    const file = new File([], fileNameForPath(path), { type: 'application/octet-stream' });
    setHostPath(file, path);
    files.push(file);
  }

  if (!files.length) throw new Error(t('没有可添加的文件'));
  dispatchComposerFiles(input, files);

  const notices: string[] = [];
  if (unreadable > 0) {
    notices.push(t('{count} 张图片读取失败（可能超过 8 MB），已改为路径引用', { count: unreadable }));
  }
  if (unsupported > 0) {
    notices.push(t('{count} 张图片格式不支持视觉（如 HEIC），已改为路径引用', { count: unsupported }));
  }
  if (disabled > 0) {
    notices.push(t('{count} 张图片未读取（设置已关闭图片读取），已改为路径引用', { count: disabled }));
  }

  if (notices.length) {
    showToast(notices.join(' · '));
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
