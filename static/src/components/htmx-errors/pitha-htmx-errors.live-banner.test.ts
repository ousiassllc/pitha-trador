import { beforeEach, describe, expect, test } from 'bun:test';
import './pitha-htmx-errors';

beforeEach(() => {
  document.body.innerHTML = '<div id="toast-region"></div>';
});

function fakeXhr(status: number, responseText: string): XMLHttpRequest {
  return {
    status,
    responseText,
    getResponseHeader: () => null,
  } as unknown as XMLHttpRequest;
}

function fire(name: string, detail: Record<string, unknown> = {}): Record<string, unknown> {
  document.dispatchEvent(new CustomEvent(name, { detail, bubbles: true }));
  return detail;
}

describe('polled live banners (issue #626)', () => {
  const BANNER = '<div data-testid="marketdata-banner">市況データを取得できません。</div>';

  function poll(target: HTMLElement, body: string): Record<string, unknown> {
    return fire('htmx:beforeSwap', {
      xhr: fakeXhr(200, body),
      serverResponse: body,
      target,
      shouldSwap: true,
    });
  }

  function liveBanner(): HTMLElement {
    const el = document.createElement('div');
    el.setAttribute('data-live-banner', '');
    document.body.appendChild(el);
    return el;
  }

  test('swaps the first response and any changed one', () => {
    const target = liveBanner();
    expect(poll(target, BANNER).shouldSwap).toBe(true);
    expect(poll(target, `${BANNER}<p>changed</p>`).shouldSwap).toBe(true);
  });

  test('skips a poll answering the markup already shown', () => {
    const target = liveBanner();
    expect(poll(target, BANNER).shouldSwap).toBe(true);
    expect(poll(target, BANNER).shouldSwap).toBe(false);
  });

  test('swaps again when a cleared banner reappears', () => {
    const target = liveBanner();
    poll(target, BANNER);
    expect(poll(target, '').shouldSwap).toBe(true);
    expect(poll(target, BANNER).shouldSwap).toBe(true);
  });

  test('never skips a target that is not a live banner', () => {
    const plain = document.createElement('div');
    document.body.appendChild(plain);
    expect(poll(plain, BANNER).shouldSwap).toBe(true);
    expect(poll(plain, BANNER).shouldSwap).toBe(true);
  });
});
