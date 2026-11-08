import { afterEach, beforeEach, describe, expect, test, vi } from 'bun:test';
import './pitha-htmx-errors';
import { watchToastRegion } from './toast-lifecycle';

// M5 of PR #1044: the stale-session toast (the only toast with a "再読み込み"
// action) vanishes after 5 seconds, taking the page's only recovery action with
// it. A persistent notice with the same action stays until the problem is gone.
const TOAST_HTML =
  '<div data-toast role="alert"><span data-toast-message></span><button type="button" hidden data-toast-reload>再読み込み</button><button type="button" data-toast-dismiss>×</button></div>';
const NOTICE_HTML =
  '<div data-reload-notice><p data-reload-notice-message></p><button type="button" data-reload-notice-reload>再読み込み</button></div>';

const TOAST_MS = 5000;
const realAnimate = HTMLElement.prototype.animate;

function render(): void {
  document.body.innerHTML = `
    <div id="reload-notice-region"></div>
    <button id="outside" type="button">outside</button>
    <dialog id="modal">
      <div data-reload-notice-region></div>
      <input id="field">
      <div data-toast-region></div>
    </dialog>
    <div id="toast-region"></div>
    <template id="toast-template">${TOAST_HTML}</template>
    <template id="reload-notice-template">${NOTICE_HTML}</template>`;
  for (const region of document.querySelectorAll<HTMLElement>('#toast-region, [data-toast-region]'))
    watchToastRegion(region);
}

beforeEach(() => {
  HTMLElement.prototype.animate = undefined as unknown as typeof realAnimate;
  vi.useFakeTimers();
  render();
});

afterEach(() => {
  vi.useRealTimers();
  HTMLElement.prototype.animate = realAnimate;
});

function fire(name: string, detail: Record<string, unknown> = {}): void {
  document.dispatchEvent(new CustomEvent(name, { detail, bubbles: true }));
}

function staleError(): void {
  fire('htmx:responseError', {
    xhr: {
      status: 403,
      responseText: '',
      getResponseHeader: (name: string) => (name === 'X-CSRF-Reject' ? 'stale' : null),
    },
  });
}

// MutationObserver callbacks (toast lifecycle, dialog open/close) run in a microtask.
const flush = () => Promise.resolve();

const notices = () => document.querySelectorAll<HTMLElement>('[data-reload-notice]');
const pageRegion = () => document.getElementById('reload-notice-region') as HTMLElement;
const dialog = () => document.getElementById('modal') as HTMLDialogElement;
const dialogRegion = () => dialog().querySelector('[data-reload-notice-region]') as HTMLElement;

async function expire(): Promise<void> {
  staleError();
  await flush();
  vi.advanceTimersByTime(TOAST_MS);
  await flush();
}

describe('persistent reload notice', () => {
  test('stays on the page after the 5-second toast is gone', async () => {
    await expire();

    expect(document.querySelector('#toast-region [data-toast]')).toBeNull();
    expect(notices().length).toBe(1);
    expect(pageRegion().contains(notices()[0] as HTMLElement)).toBe(true);
    expect(notices()[0]?.querySelector('[data-reload-notice-message]')?.textContent).toBe(
      'ページの有効期限が切れました。',
    );
    expect(notices()[0]?.querySelector('[data-reload-notice-reload]')).not.toBeNull();

    vi.advanceTimersByTime(TOAST_MS * 12);
    await flush();
    expect(notices().length).toBe(1);
  });

  test('keeps the toast timings (3s plain, 5s with the action) unchanged', async () => {
    staleError();
    await flush();
    const toast = document.querySelector('#toast-region [data-toast]') as HTMLElement;
    vi.advanceTimersByTime(TOAST_MS - 1);
    expect(toast.isConnected).toBe(true);
    vi.advanceTimersByTime(1);
    expect(toast.isConnected).toBe(false);
  });

  test('is not created by errors that have no reload action', async () => {
    fire('htmx:sendError');
    fire('htmx:responseError', {
      xhr: { status: 500, responseText: '', getResponseHeader: () => null },
    });
    await flush();
    expect(notices().length).toBe(0);
  });

  test('does not announce itself (the toast already does) and never takes focus', async () => {
    const field = document.getElementById('outside') as HTMLButtonElement;
    field.focus();

    await expire();

    const notice = notices()[0] as HTMLElement;
    expect(document.activeElement).toBe(field);
    for (const el of [notice, ...notice.querySelectorAll('*')]) {
      expect(el.hasAttribute('role')).toBe(false);
      expect(el.hasAttribute('aria-live')).toBe(false);
    }
  });

  test('is not duplicated when the error recurs, with the toast on screen or gone', async () => {
    staleError();
    await flush();
    staleError();
    await flush();
    expect(notices().length).toBe(1);

    vi.advanceTimersByTime(TOAST_MS);
    await flush();
    staleError();
    await flush();
    expect(notices().length).toBe(1);
    expect(document.querySelectorAll('#toast-region [data-toast]').length).toBe(1);
  });

  test('reloads only on an explicit click, never by itself', async () => {
    const realReload = Object.getOwnPropertyDescriptor(location, 'reload');
    let reloads = 0;
    Object.defineProperty(location, 'reload', { configurable: true, value: () => reloads++ });
    try {
      await expire();
      vi.advanceTimersByTime(TOAST_MS * 12);
      expect(reloads).toBe(0);
      (document.querySelector('[data-reload-notice-reload]') as HTMLElement).click();
    } finally {
      if (realReload) Object.defineProperty(location, 'reload', realReload);
      else Reflect.deleteProperty(location, 'reload');
    }
    expect(reloads).toBe(1);
  });

  describe('clearing', () => {
    const afterRequest = (verb: string, successful: boolean) =>
      fire('htmx:afterRequest', { successful, requestConfig: { verb } });

    test('a successful state-changing request proves the session works again', async () => {
      await expire();
      expect(notices().length).toBe(1);
      afterRequest('post', true);
      expect(notices().length).toBe(0);
    });

    test('polling GETs, failed requests and other errors do not clear it', async () => {
      await expire();
      afterRequest('get', true);
      afterRequest('post', false);
      fire('htmx:sendError');
      await flush();
      expect(notices().length).toBe(1);
    });

    test('can come back after being cleared', async () => {
      await expire();
      afterRequest('post', true);
      await expire();
      expect(notices().length).toBe(1);
    });
  });

  describe('modal dialogs', () => {
    test('lands in the open dialog, not on the inert page', async () => {
      dialog().setAttribute('open', '');
      await flush();
      await expire();

      expect(notices().length).toBe(1);
      expect(dialogRegion().contains(notices()[0] as HTMLElement)).toBe(true);
      expect(pageRegion().children.length).toBe(0);
    });

    test('moves to the page when the dialog closes, so it never hides with the dialog', async () => {
      dialog().setAttribute('open', '');
      await flush();
      await expire();

      dialog().removeAttribute('open');
      await flush();

      expect(notices().length).toBe(1);
      expect(pageRegion().contains(notices()[0] as HTMLElement)).toBe(true);
      expect(dialogRegion().children.length).toBe(0);
    });

    test('moves into a dialog opened later and back out when it closes', async () => {
      await expire();
      expect(pageRegion().children.length).toBe(1);

      dialog().setAttribute('open', '');
      await flush();
      expect(notices().length).toBe(1);
      expect(dialogRegion().contains(notices()[0] as HTMLElement)).toBe(true);

      dialog().removeAttribute('open');
      await flush();
      expect(notices().length).toBe(1);
      expect(pageRegion().contains(notices()[0] as HTMLElement)).toBe(true);
    });

    test('a recurrence while the dialog is open does not add a second notice', async () => {
      await expire();
      dialog().setAttribute('open', '');
      await flush();
      await expire();
      expect(notices().length).toBe(1);
    });

    test('clearing removes it from wherever it sits', async () => {
      dialog().setAttribute('open', '');
      await flush();
      await expire();
      expect(dialogRegion().children.length).toBe(1);
      fire('htmx:afterRequest', { successful: true, requestConfig: { verb: 'delete' } });
      expect(notices().length).toBe(0);
    });
  });
});
