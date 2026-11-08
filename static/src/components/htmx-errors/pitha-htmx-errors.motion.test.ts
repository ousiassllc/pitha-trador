import { afterEach, beforeEach, describe, expect, test } from 'bun:test';
import { dismissToast, watchToastRegion } from './toast-lifecycle';

// Toast motion (design guide §4/§5): the Web Animations API is replaced by a
// recorder, so the keyframes and durations can be asserted without a browser.

describe('toast motion (design guide §4/§5)', () => {
  type Call = { keyframes: Keyframe[]; options: KeyframeAnimationOptions };
  let calls: Call[];
  let finish: (() => void) | undefined;
  const realMatchMedia = globalThis.matchMedia;

  beforeEach(() => {
    document.body.innerHTML = '<div id="toast-region"></div>';
    calls = [];
    finish = undefined;
    HTMLElement.prototype.animate = (keyframes, options) => {
      calls.push({
        keyframes: keyframes as Keyframe[],
        options: options as KeyframeAnimationOptions,
      });
      const animation = {} as Animation;
      Object.defineProperty(animation, 'onfinish', {
        set(fn: () => void) {
          finish = fn;
        },
      });
      return animation;
    };
    globalThis.matchMedia = (() => ({ matches: false })) as unknown as typeof matchMedia;
    watchToastRegion(document.getElementById('toast-region') as HTMLElement);
  });

  afterEach(() => {
    globalThis.matchMedia = realMatchMedia;
  });

  async function landToast(): Promise<HTMLElement> {
    const toast = document.createElement('div');
    toast.setAttribute('data-toast', '');
    document.getElementById('toast-region')?.append(toast);
    await Promise.resolve();
    return toast;
  }

  test('a toast fades in over 320ms ease-out with a small rise', async () => {
    await landToast();
    expect(calls).toHaveLength(1);
    expect(calls[0].options).toMatchObject({
      duration: 320,
      easing: 'cubic-bezier(0.2, 0.8, 0.2, 1)',
    });
    expect(calls[0].keyframes[0]).toMatchObject({ opacity: 0, transform: 'translateY(8px)' });
  });

  test('dismissing fades out over 240ms ease-in, then removes the toast', async () => {
    const toast = await landToast();
    calls.length = 0;

    dismissToast(toast);
    expect(toast.isConnected).toBe(true);
    expect(calls[0].options).toMatchObject({ duration: 240, easing: 'cubic-bezier(0.4, 0, 1, 1)' });

    finish?.();
    expect(toast.isConnected).toBe(false);
  });

  test('prefers-reduced-motion: no movement on entry and a fade of at most 100ms on exit', async () => {
    globalThis.matchMedia = (() => ({ matches: true })) as unknown as typeof matchMedia;
    const toast = await landToast();
    expect(calls[0].keyframes.every((frame) => !('transform' in frame))).toBe(true);
    expect(calls[0].options.duration).toBeLessThanOrEqual(100);

    dismissToast(toast);
    expect(calls[1].options.duration).toBeLessThanOrEqual(100);
  });
});

// An inline alert that a new response replaces fades out first (design guide
// §4/§5): the swap is held for the fade, during which htmx marks the slot
// `htmx-swapping` (app.css animates the alert out). An empty slot is swapped
// at once.
describe('inline alert exit (design guide §4/§5)', () => {
  const realMatchMedia = globalThis.matchMedia;

  beforeEach(() => {
    document.body.innerHTML =
      '<div id="slot"><div data-inline-alert role="alert"></div></div><div id="empty-slot"></div>';
    watchToastRegion(document.createElement('div'));
    globalThis.matchMedia = (() => ({ matches: false })) as unknown as typeof matchMedia;
  });

  afterEach(() => {
    globalThis.matchMedia = realMatchMedia;
  });

  function swap(targetId: string): Record<string, unknown> {
    const detail: Record<string, unknown> = {
      xhr: {
        status: 422,
        responseText: '<div data-inline-alert></div>',
        getResponseHeader: () => null,
      } as unknown as XMLHttpRequest,
      shouldSwap: true,
      target: document.getElementById(targetId),
      swapOverride: 'innerHTML',
    };
    document.dispatchEvent(new CustomEvent('htmx:beforeSwap', { detail, bubbles: true }));
    return detail;
  }

  test('holds the swap for 240ms while the old alert fades out', () => {
    const detail = swap('slot');
    expect(detail.swapOverride).toBe('innerHTML swap:240ms');
    expect(detail.shouldSwap).toBe(true);
  });

  test('prefers-reduced-motion: holds it for at most 100ms', () => {
    globalThis.matchMedia = (() => ({ matches: true })) as unknown as typeof matchMedia;
    expect(swap('slot').swapOverride).toBe('innerHTML swap:100ms');
  });

  test('swaps into an empty slot at once', () => {
    expect(swap('empty-slot').swapOverride).toBe('innerHTML');
  });
});
