import assert from 'node:assert/strict';
import test from 'node:test';
import { isAuthCancelled, isCancellationText } from '../src/client/cancel.ts';

// The fnOS authorization page reports a dismissal as `status=cancel` (sometimes
// with `error=access_denied`); the plugin's own waiters synthesize
// `status=cancelled`. Both must be treated as "user backed out", not as a red
// "Could not open the picker" failure.

test('gateway cancel statuses are cancellations', () => {
  assert.equal(isAuthCancelled({ status: 'cancel' }), true);
  assert.equal(isAuthCancelled({ status: 'cancelled' }), true);
  assert.equal(isAuthCancelled({ status: 'CANCEL' }), true);
  assert.equal(isAuthCancelled({ status: 'cancel', error: 'access_denied' }), true);
});

test('an access-denied error without a status is a cancellation too', () => {
  assert.equal(isAuthCancelled({ error: 'access_denied' }), true);
  assert.equal(isAuthCancelled({ error: 'user_cancelled' }), true);
  assert.equal(isAuthCancelled({ status: 'error', error: 'access_denied' }), true);
});

test('success is never a cancellation, even with a stray error string', () => {
  assert.equal(isAuthCancelled({ status: 'success', error: 'access_denied' }), false);
  assert.equal(isAuthCancelled({ status: 'success' }), false);
});

test('genuine failures stay failures', () => {
  assert.equal(isAuthCancelled(undefined), false);
  assert.equal(isAuthCancelled({ status: 'error', error: 'E_APP_AUTH' }), false);
  assert.equal(isAuthCancelled({ status: 'error', error: 'Permission denied' }), false);
  assert.equal(isAuthCancelled({ status: 'error' }), false);
});

test('the legacy text heuristic still recognises dismissals', () => {
  assert.equal(isCancellationText('Operation failed'), true);
  assert.equal(isCancellationText('用户取消'), true);
  assert.equal(isCancellationText('Permission denied'), false);
  assert.equal(isAuthCancelled({ status: 'error', error: 'Operation failed' }), true);
});
