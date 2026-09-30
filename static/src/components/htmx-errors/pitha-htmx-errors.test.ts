import { beforeEach, describe, expect, test } from 'bun:test';
import './pitha-htmx-errors';

const TOAST_HTML =
  '<div data-toast role="alert"><span data-toast-message></span><button type="button" data-toast-dismiss>×</button></div>';

beforeEach(() => {
  document.body.innerHTML = `<div id="toast-region"></div><template id="toast-template">${TOAST_HTML}</template>`;
});

function fakeXhr(
  status: number,
  responseText: string,
  headers: Record<string, string> = {},
): XMLHttpRequest {
  return {
    status,
    responseText,
    getResponseHeader: (name: string) => headers[name] ?? null,
  } as XMLHttpRequest;
}

function fire(name: string, detail: Record<string, unknown> = {}): Record<string, unknown> {
  document.dispatchEvent(new CustomEvent(name, { detail, bubbles: true }));
  return detail;
}

function toastTexts(): string[] {
  return [...document.querySelectorAll('#toast-region [data-toast-message]')].map(
    (el) => el.textContent ?? '',
  );
}

describe('htmx:beforeSwap', () => {
  test('blocks an error response that carries no toast fragment', () => {
    const detail = fire('htmx:beforeSwap', {
      xhr: fakeXhr(500, '404 page not found'),
      shouldSwap: true,
    });
    expect(detail.shouldSwap).toBe(false);
  });

  test('keeps the swap for an error response carrying a toast fragment', () => {
    const detail = fire('htmx:beforeSwap', { xhr: fakeXhr(409, TOAST_HTML), shouldSwap: true });
    expect(detail.shouldSwap).toBe(true);
  });

  test('leaves successful responses alone', () => {
    const detail = fire('htmx:beforeSwap', { xhr: fakeXhr(200, '<tr></tr>'), shouldSwap: true });
    expect(detail.shouldSwap).toBe(true);
  });
});

describe('htmx:responseError', () => {
  test('shows a generic 5xx toast when the body is empty', () => {
    fire('htmx:responseError', { xhr: fakeXhr(500, '') });
    expect(toastTexts()).toEqual(['サーバーでエラーが発生しました（HTTP 500）。']);
  });

  test('shows a generic 4xx toast when the body is empty', () => {
    fire('htmx:responseError', { xhr: fakeXhr(409, '') });
    expect(toastTexts()).toEqual(['操作を完了できませんでした（HTTP 409）。']);
  });

  test('asks for a page reload when the server marks a 403 as stale', () => {
    fire('htmx:responseError', {
      xhr: fakeXhr(403, 'forbidden: missing or invalid CSRF token', { 'X-CSRF-Reject': 'stale' }),
    });
    expect(toastTexts()).toHaveLength(1);
    expect(toastTexts()[0]).toContain('再読み込み');
  });

  test('an unmarked 403 keeps the generic toast', () => {
    fire('htmx:responseError', { xhr: fakeXhr(403, 'forbidden: host not allowed') });
    expect(toastTexts()).toEqual(['操作を完了できませんでした（HTTP 403）。']);
  });

  test('adds nothing when the server already sent a toast fragment', () => {
    fire('htmx:responseError', { xhr: fakeXhr(409, TOAST_HTML) });
    expect(toastTexts()).toEqual([]);
  });

  test('does not stack identical toasts', () => {
    fire('htmx:responseError', { xhr: fakeXhr(500, '') });
    fire('htmx:responseError', { xhr: fakeXhr(500, '') });
    expect(toastTexts()).toHaveLength(1);
  });
});

describe('requests without a response', () => {
  test('sendError and timeout show the connection toast', () => {
    fire('htmx:sendError');
    expect(toastTexts()).toEqual(['サーバーに接続できませんでした。']);
    document.getElementById('toast-region')?.replaceChildren();
    fire('htmx:timeout');
    expect(toastTexts()).toEqual(['サーバーに接続できませんでした。']);
  });
});

describe('dismissing', () => {
  test('the close button removes its toast only', () => {
    fire('htmx:responseError', { xhr: fakeXhr(500, '') });
    fire('htmx:sendError');
    expect(toastTexts()).toHaveLength(2);

    document.querySelector<HTMLElement>('[data-toast-dismiss]')?.click();

    expect(toastTexts()).toEqual(['サーバーに接続できませんでした。']);
  });
});
