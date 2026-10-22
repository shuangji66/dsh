import assert from 'node:assert/strict';
import test from 'node:test';
import { isCancellationText, isPickerCancelled } from '../src/client/cancel.ts';

// 取消判定是**启发式**（宿主用 resolve 而不是 reject 表示「没选目录」），因此把观察到
// 的形状与「不能被吞掉的真实错误」都钉住：判错会把取消显示成红色报错，或者反过来把
// Permission denied 静默说成「已取消」。

test('isPickerCancelled: 有路径就不是取消', () => {
  assert.equal(isPickerCancelled({ code: 0, data: ['/vol1/1000/work'] }), false);
  // code 非 0 但带路径：交给上层按错误处理，不在这里判成取消。
  assert.equal(isPickerCancelled({ code: 1, msg: 'weird', data: ['/a'] }), false);
});

test('isPickerCancelled: 空结果的两种宿主形状都算取消', () => {
  // 常见形状：code 1 / "Operation failed" / data: []
  assert.equal(isPickerCancelled({ code: 1, msg: 'Operation failed', data: [] }), true);
  // 有的宿主用「成功但没选」表示取消。
  assert.equal(isPickerCancelled({ code: 0, data: [] }), true);
  // SDK 直接 resolve 空值。
  assert.equal(isPickerCancelled(undefined), true);
  assert.equal(isPickerCancelled(null), true);
});

test('isPickerCancelled: 真实错误必须保持可见', () => {
  assert.equal(isPickerCancelled({ code: 1, msg: 'Permission denied', data: [] }), false);
  assert.equal(isPickerCancelled({ code: 2, msg: 'boom', data: [] }), false);
  assert.equal(isPickerCancelled({ code: 2, data: [] }), false);
  // 1 且无 msg 是历史形状（旧版宿主），仍按取消处理。
  assert.equal(isPickerCancelled({ code: 1, data: [] }), true);
});

test('isCancellationText: 已知取消文案', () => {
  for (const text of [
    'Operation failed',
    'operation FAILED',
    '操作失败',
    'cancel',
    'Cancelled',
    'abort',
    '取消',
    '已取消',
    '用户取消',
    '授权已取消，请回到 DeepSeek Harness 重试。',
  ]) {
    assert.equal(isCancellationText(text), true, text);
  }
});

test('isCancellationText: 普通错误与空值不是取消', () => {
  for (const text of ['', '   ', null, undefined, 'Permission denied', '授权超时，请重试', 'boom']) {
    assert.equal(isCancellationText(text), false, String(text));
  }
});
