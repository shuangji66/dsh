/**
 * Directory-picker cancellation helpers (pure, unit-tested).
 *
 * The fnOS host bridge does not reject when the user dismisses the native
 * picker: `pickUserFile` resolves with an empty result instead. Two shapes have
 * been observed:
 *
 *   { code: 1, msg: 'Operation failed', data: [] }   // most host versions
 *   { code: 0, data: [] }                            // "success", nothing picked
 *
 * Both mean "nothing was selected"; reporting them as picker failures makes
 * users think the bridge is broken. Real failures resolve with a non-empty
 * `data` array or a non-cancellation `msg` (e.g. "Permission denied") and must
 * stay visible.
 */

/** The subset of the SDK's `AppBridgeResponse` this module reads. */
export interface PickerResponseLike {
  code?: number;
  msg?: string;
  data?: unknown;
}

// Text a host uses when the user cancelled rather than failed. English hosts
// use a small fixed vocabulary (the bridge's generic message is exactly
// "Operation failed"); Chinese hosts may wrap the verb in a sentence, hence the
// substring check in isCancellationText().
const CANCEL_TEXTS = new Set([
  'operation failed',
  '操作失败',
  'cancel',
  'canceled',
  'cancelled',
  'abort',
  'aborted',
  '取消',
  '已取消',
  '用户取消',
]);

/** Whether an error/message text describes a cancellation. */
export function isCancellationText(text: unknown): boolean {
  const value = String(text ?? '').trim().toLowerCase();
  if (!value) return false;
  return CANCEL_TEXTS.has(value) || value.includes('取消');
}

/**
 * Whether a `pickUserFile` result means "the user picked nothing".
 *
 * Returning false keeps the host's message visible as an error, so an unknown
 * shape (or a genuine failure such as "Permission denied") is never silently
 * downgraded to a cancellation.
 */
export function isPickerCancelled(result: PickerResponseLike | null | undefined): boolean {
  // The SDK resolves with no payload when the picker closes without a result.
  if (!result) return true;
  const data = Array.isArray(result.data) ? result.data : [];
  // A path came back: never a cancellation, even if the code is unexpected.
  if (data.length > 0) return false;
  // code 0 + empty data: the host reports "success" for "nothing picked".
  if (result.code === 0) return true;
  const msg = String(result.msg || '').trim();
  // code 1 with no message is the historical host shape for dismissal.
  if (!msg) return result.code === 1;
  return isCancellationText(msg);
}

/** The subset of an app-auth result this module reads. */
export interface AuthResultLike {
  status?: string;
  error?: string;
}

// The fnOS gateway answers a dismissed authorization page with `status=cancel`
// (sometimes plus `error=access_denied`); the plugin's own waiters synthesize
// `status=cancelled` when the popup/frame is closed. Both mean "user backed
// out", and neither is a failure worth a red message.
const CANCEL_STATUSES = new Set(['cancel', 'cancelled', 'canceled', 'abort', 'aborted']);
const CANCEL_ERRORS = new Set(['access_denied', 'user_cancelled', 'user_canceled', 'user_cancel']);

/**
 * Whether an authorization result means "the user dismissed the page".
 *
 * Checked before the generic error path: the gateway's `status=cancel` carries
 * no `error`, so matching only the error text (the previous behaviour) turned a
 * plain cancellation into "Could not open the picker".
 */
export function isAuthCancelled(auth: AuthResultLike | null | undefined): boolean {
  if (!auth) return false;
  const status = String(auth.status ?? '').trim().toLowerCase();
  const error = String(auth.error ?? '').trim().toLowerCase();
  // A success with a stray error string is still a success.
  if (status === 'success') return false;
  if (CANCEL_STATUSES.has(status)) return true;
  if (CANCEL_ERRORS.has(error)) return true;
  return isCancellationText(error);
}
