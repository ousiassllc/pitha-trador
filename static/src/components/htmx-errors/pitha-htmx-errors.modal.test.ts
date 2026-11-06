import { afterEach, beforeEach, describe, expect, test } from 'bun:test';
import { watchToastRegion } from './pitha-htmx-errors';

const TOAST_HTML =
  '<div data-toast role="alert"><span data-toast-message></span><button type="button" data-toast-dismiss>×</button></div>';

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

// issue #321: `dialog.showModal()` puts the dialog in the top layer, above any
// z-index, so the toast region must be re-shown as a popover when a toast lands.
describe('toast region above modal dialogs', () => {
  type PopoverEl = HTMLElement & { showPopover(): void; hidePopover(): void };
  const flush = () => new Promise<void>((resolve) => setTimeout(resolve, 0));
  let calls: string[];

  beforeEach(() => {
    calls = [];
    const el = document.getElementById('toast-region') as PopoverEl;
    let open = false;
    el.showPopover = () => {
      open = true;
      calls.push('show');
    };
    el.hidePopover = () => {
      open = false;
      calls.push('hide');
    };
    el.matches = ((selector: string) => selector === ':popover-open' && open) as typeof el.matches;
    watchToastRegion(el);
  });

  test('a toast shown by script opens the region popover', async () => {
    fire('htmx:sendError');
    await flush();
    expect(calls).toEqual(['show']);
  });

  test('an htmx beforeend swap of a toast fragment re-raises an already open popover', async () => {
    const region = document.getElementById('toast-region') as PopoverEl;
    fire('htmx:sendError');
    await flush();
    calls.length = 0;

    region.insertAdjacentHTML('beforeend', TOAST_HTML);
    await flush();

    expect(calls).toEqual(['hide', 'show']);
  });

  test('removing a toast does not touch the popover', async () => {
    const region = document.getElementById('toast-region') as PopoverEl;
    fire('htmx:sendError');
    await flush();
    calls.length = 0;

    region.querySelector('[data-toast]')?.remove();
    await flush();

    expect(calls).toEqual([]);
  });
});

// issue #353: an open modal dialog makes everything outside it inert, so
// `#toast-region` cannot be clicked; toasts must land in the dialog's own region.
describe('toast region inside an open modal dialog', () => {
  const flush = () => new Promise<void>((resolve) => setTimeout(resolve, 0));
  let dialog: HTMLElement;
  let inner: HTMLElement;

  beforeEach(() => {
    document.body.insertAdjacentHTML(
      'afterbegin',
      '<dialog id="modal" data-modal><div data-toast-region></div></dialog>',
    );
    dialog = document.getElementById('modal') as HTMLElement;
    inner = dialog.querySelector('[data-toast-region]') as HTMLElement;
    watchToastRegion(inner);
  });

  const innerTexts = () =>
    [...inner.querySelectorAll('[data-toast-message]')].map((el) => el.textContent);

  test('a script toast lands in the dialog region while the modal is open, and its close button works', () => {
    dialog.setAttribute('open', '');
    fire('htmx:sendError');

    expect(innerTexts()).toEqual(['サーバーに接続できませんでした。']);
    expect(toastTexts()).toEqual([]);

    inner.querySelector<HTMLElement>('[data-toast-dismiss]')?.click();
    expect(innerTexts()).toEqual([]);
  });

  test('a closed dialog is ignored: the toast goes to #toast-region', () => {
    fire('htmx:sendError');

    expect(toastTexts()).toEqual(['サーバーに接続できませんでした。']);
    expect(innerTexts()).toEqual([]);
  });

  test('an error fragment from htmx is retargeted to the open dialog region', () => {
    dialog.setAttribute('open', '');
    const detail = fire('htmx:beforeSwap', { xhr: fakeXhr(409, TOAST_HTML), shouldSwap: true });
    expect(detail.target).toBe(inner);
    expect(detail.shouldSwap).toBe(true);
  });

  test('an error fragment keeps the configured target when no modal is open', () => {
    const detail = fire('htmx:beforeSwap', { xhr: fakeXhr(409, TOAST_HTML), shouldSwap: true });
    expect(detail.target).toBeUndefined();
  });

  test('a toast in the dialog region never touches the page popover logic', async () => {
    let calls = 0;
    (inner as HTMLElement & { showPopover(): void }).showPopover = () => {
      calls += 1;
    };
    dialog.setAttribute('open', '');
    fire('htmx:sendError');
    await flush();
    expect(calls).toBe(0);
  });
});
