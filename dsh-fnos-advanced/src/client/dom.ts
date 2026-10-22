/**
 * Small DOM utilities shared by the scanners and the picker flows.
 *
 * Everything here is deliberately dependency-free: the plugin injects plain DOM
 * nodes into dsh's React tree and must not assume any of dsh's UI packages.
 */
import { t } from './i18n.ts';

/** Whether an element currently takes up layout space. */
export function isVisible(element: Element | null | undefined): boolean {
  if (!element) return false;
  const node = element as HTMLElement;
  return Boolean(node.offsetWidth || node.offsetHeight || node.getClientRects().length);
}

export function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

/** A state value safe for popup names and query strings. */
export function randomId(): string {
  try {
    if (window.crypto && typeof window.crypto.randomUUID === 'function') {
      return window.crypto.randomUUID();
    }
  } catch { /* insecure context */ }
  return `fnos-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

/**
 * Copy text, with the legacy fallback: the console is often opened over plain
 * HTTP through a reverse proxy, where `navigator.clipboard` does not exist.
 */
export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard && typeof navigator.clipboard.writeText === 'function') {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch { /* insecure context or permission denied: use the fallback */ }

  try {
    const area = document.createElement('textarea');
    area.value = text;
    area.setAttribute('readonly', '');
    area.style.position = 'fixed';
    area.style.top = '-1000px';
    area.style.opacity = '0';
    document.body.appendChild(area);
    area.select();
    const ok = typeof document.execCommand === 'function' && document.execCommand('copy');
    area.remove();
    return Boolean(ok);
  } catch {
    return false;
  }
}

const toastTimers = new WeakMap<HTMLElement, number>();

/** Show a transient status line at the bottom of the page. */
export function showToast(text: string): void {
  let toast = document.querySelector<HTMLElement>('.dsh-fnos-advanced-toast');
  if (!toast) {
    toast = document.createElement('div');
    toast.className = 'dsh-fnos-advanced-toast';
    toast.setAttribute('role', 'status');
    document.body.appendChild(toast);
  }
  toast.textContent = text;
  toast.style.display = 'block';

  const previous = toastTimers.get(toast);
  if (previous) window.clearTimeout(previous);
  toastTimers.set(toast, window.setTimeout(() => {
    toast.style.display = 'none';
  }, 5000));
}

/** Set an input's value through React's own setter, then submit with Enter. */
export function fillAndEnter(input: HTMLInputElement, path: string): void {
  input.focus();

  const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')?.set;
  if (setter) setter.call(input, path);
  else input.value = path;

  input.dispatchEvent(new Event('input', { bubbles: true }));
  input.dispatchEvent(new Event('change', { bubbles: true }));

  // Let React commit the controlled value before the Enter handler reads it.
  window.setTimeout(() => {
    input.focus();
    const options: KeyboardEventInit & { keyCode: number; which: number; charCode: number } = {
      key: 'Enter',
      code: 'Enter',
      keyCode: 13,
      which: 13,
      charCode: 13,
      bubbles: true,
      cancelable: true,
    };
    input.dispatchEvent(new KeyboardEvent('keydown', options));
    input.dispatchEvent(new KeyboardEvent('keypress', options));
    input.dispatchEvent(new KeyboardEvent('keyup', options));
  }, 0);
}

/**
 * Close the composer command menu. It dismisses itself on a pointerdown outside
 * its surface, so the flow only has to publish one.
 */
export function dismissTriggerMenu(): void {
  try {
    const event = typeof PointerEvent === 'function'
      ? new PointerEvent('pointerdown', { bubbles: true, cancelable: true })
      : new MouseEvent('mousedown', { bubbles: true, cancelable: true });
    (document.body || document.documentElement).dispatchEvent(event);
  } catch { /* React will also close it on the next outside click */ }
}

/** Close a popup without letting a browser exception escape the flow. */
export function closePopup(popup: Window | null | undefined): void {
  if (!popup) return;
  try { popup.close(); } catch { /* already closed */ }
}

/** Placeholder content shown in the popup while the authorization page loads. */
export function writePopupLoading(popup: Window): void {
  try {
    popup.document.title = t('飞牛授权');
    popup.document.body.innerHTML = `<p style="font:14px/1.6 -apple-system,BlinkMacSystemFont,'PingFang SC',sans-serif;padding:24px;color:#555">${t('正在打开飞牛授权页面…')}</p>`;
  } catch { /* about:blank may already be navigating */ }
}
