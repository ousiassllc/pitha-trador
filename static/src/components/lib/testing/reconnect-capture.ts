// Test helper: fires a WsClient reconnect by hand instead of waiting out its
// ~500ms backoff. Lives in its own directory so lib/ stays within the linterly
// per-directory limit; like ws-test-support.ts it never reaches the bundle.
import { spyOn } from 'bun:test';

let restoreCapture: (() => void) | null = null;

/**
 * WsClient schedules its backoff reconnect through `window.setTimeout`; this
 * captures that callback instead of waiting out the real delay and returns the
 * function that fires it (a no-op when none was scheduled). Zero-delay timers
 * (the tests' own flushes) still run. Undo it with `restoreReconnectCapture`
 * in the suite's afterEach.
 */
export function captureReconnect(): () => void {
  restoreReconnectCapture();
  let reconnect: (() => void) | null = null;
  const setTimeoutOriginal = window.setTimeout.bind(window);
  const spy = spyOn(window, 'setTimeout').mockImplementation(((fn: () => void, ms?: number) => {
    if (!ms) return setTimeoutOriginal(fn, ms);
    reconnect = fn;
    return 1;
  }) as never);
  restoreCapture = () => spy.mockRestore();
  return () => reconnect?.();
}

export function restoreReconnectCapture(): void {
  restoreCapture?.();
  restoreCapture = null;
}
