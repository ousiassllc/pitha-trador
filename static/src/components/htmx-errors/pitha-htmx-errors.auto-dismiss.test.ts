import { afterEach, beforeEach, describe, expect, test, vi } from 'bun:test';
import { watchToastRegion } from './pitha-htmx-errors';

const TOAST_HTML =
  '<div data-toast role="alert"><span data-toast-message></span><button type="button" hidden data-toast-reload>再読み込み</button><button type="button" data-toast-dismiss>×</button></div>';

// The fade animations (design guide §5) are async; these tests run without the
// Web Animations API, where a toast appears and is removed immediately.
const realAnimate = HTMLElement.prototype.animate;

beforeEach(() => {
  HTMLElement.prototype.animate = undefined as unknown as typeof realAnimate;
  document.body.innerHTML = `<div id="toast-region"></div><template id="toast-template">${TOAST_HTML}</template>`;
});

afterEach(() => {
  HTMLElement.prototype.animate = realAnimate;
});

function fire(name: string, detail: Record<string, unknown> = {}): Record<string, unknown> {
  document.dispatchEvent(new CustomEvent(name, { detail, bubbles: true }));
  return detail;
}

// WCAG 2.2.1: an error toast must not vanish while the user is still reading
// it (pointer over it) or has focus inside it.
describe('auto-dismiss', () => {
  // --toast-duration of the design guide (O3): 3 seconds.
  const DISMISS_AFTER_MS = 3000;
  const flushObserver = () => Promise.resolve();

  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  async function landToast(): Promise<HTMLElement> {
    const region = document.getElementById('toast-region') as HTMLElement;
    watchToastRegion(region);
    fire('htmx:sendError');
    await flushObserver();
    return region.querySelector('[data-toast]') as HTMLElement;
  }

  test('removes the toast after 3 seconds when nothing interacts with it', async () => {
    const toast = await landToast();

    vi.advanceTimersByTime(DISMISS_AFTER_MS - 1);
    expect(toast.isConnected).toBe(true);
    vi.advanceTimersByTime(1);
    expect(toast.isConnected).toBe(false);
  });

  test('keeps a hovered toast until the pointer leaves, then restarts the countdown', async () => {
    const toast = await landToast();

    vi.advanceTimersByTime(DISMISS_AFTER_MS - 1000);
    toast.dispatchEvent(new Event('mouseenter'));
    vi.advanceTimersByTime(DISMISS_AFTER_MS * 3);
    expect(toast.isConnected).toBe(true);

    toast.dispatchEvent(new Event('mouseleave'));
    vi.advanceTimersByTime(DISMISS_AFTER_MS - 1);
    expect(toast.isConnected).toBe(true);
    vi.advanceTimersByTime(1);
    expect(toast.isConnected).toBe(false);
  });

  test('keeps a toast with focus inside it, and stays while hovered after focus leaves', async () => {
    const toast = await landToast();

    toast.dispatchEvent(new Event('focusin'));
    toast.dispatchEvent(new Event('mouseenter'));
    vi.advanceTimersByTime(DISMISS_AFTER_MS * 3);
    expect(toast.isConnected).toBe(true);

    toast.dispatchEvent(new Event('focusout'));
    vi.advanceTimersByTime(DISMISS_AFTER_MS * 3);
    expect(toast.isConnected).toBe(true);

    toast.dispatchEvent(new Event('mouseleave'));
    vi.advanceTimersByTime(DISMISS_AFTER_MS);
    expect(toast.isConnected).toBe(false);
  });

  // Design guide O3: a toast carrying an action stays 5 seconds.
  test('keeps a toast with a reload action for 5 seconds', async () => {
    const region = document.getElementById('toast-region') as HTMLElement;
    watchToastRegion(region);
    fire('htmx:responseError', {
      xhr: {
        status: 403,
        responseText: '',
        getResponseHeader: (name: string) => (name === 'X-CSRF-Reject' ? 'stale' : null),
      },
    });
    await flushObserver();
    const toast = region.querySelector('[data-toast]') as HTMLElement;

    vi.advanceTimersByTime(5000 - 1);
    expect(toast.isConnected).toBe(true);
    vi.advanceTimersByTime(1);
    expect(toast.isConnected).toBe(false);
  });
});
