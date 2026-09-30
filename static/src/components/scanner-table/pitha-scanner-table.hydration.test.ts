import { afterEach, beforeEach, expect, mock, test } from 'bun:test';
import { PithaScannerTable } from './pitha-scanner-table';

// Regression test for the Scanner Dashboard showing two <table>s (every
// column header doubled - once as `organisms.ScannerTableFallback`'s
// plain "Jev" column, once as Lit's own "Jev Direction"/"Jev
// Confidence"/"Entry Quality" columns) once `pitha-scanner-table`
// hydrated. Root cause: `scanner_page.templ`'s `<script type="module">`
// loads after the page (deferred by default), so in a real page load
// this element is a custom-element *upgrade* of an already-parsed node -
// the SSR `<table>` is already attached as a child by the time
// createRenderRoot() runs - not a fresh construction. createRenderRoot()
// returning `this` unmodified left that SSR markup in place, and Lit's
// own first render() appended a second <table> beside it instead of
// replacing it.
//
// This exercises that exact upgrade-after-existing-children timing by
// registering a trivial subclass of PithaScannerTable under its own
// unique tag name *after* the SSR markup below is already in the DOM
// (mirroring `scanner_page.templ`'s deferred `<script type="module">`
// loading after the page), rather than relying on the module's
// top-level `@customElement('pitha-scanner-table')` side effect: that
// fixed tag name may already be registered by pitha-scanner-
// table.test.ts's own static import (customElements is a single
// process-wide registry bun:test's suite run shares across files), and
// defining this test's own tag too early - before its markup exists -
// would make this a fresh-construction case indistinguishable from the
// bug this test exists to catch (fresh construction never sees a
// pre-existing child to clear, since the parser attaches an element's
// children only after all of *that* element's own synchronous
// construction-time reactions, including createRenderRoot(), have
// already run). A subclass (rather than the same constructor under a
// second name) is required: the Custom Elements spec's `define()`
// algorithm rejects reusing a constructor that already has a
// definition, even under a different tag name.
class PithaScannerTableUnderTest extends PithaScannerTable {}
const TAG = 'pitha-scanner-table-hydration-test';

type ScannerTableElement = HTMLElement & { updateComplete: Promise<boolean> };
type Listener = (event: unknown) => void;

class FakeWebSocket {
  private listeners: Record<string, Listener[]> = {};
  constructor(public readonly url: string) {}
  addEventListener(type: string, listener: Listener): void {
    if (!this.listeners[type]) {
      this.listeners[type] = [];
    }
    this.listeners[type].push(listener);
  }
  close(): void {}
}

let originalFetch: typeof fetch;
let originalWebSocket: typeof WebSocket;

beforeEach(() => {
  originalFetch = globalThis.fetch;
  originalWebSocket = globalThis.WebSocket;
  globalThis.WebSocket = FakeWebSocket as unknown as typeof WebSocket;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
  globalThis.WebSocket = originalWebSocket;
  document.body.innerHTML = '';
});

// Same deterministic macrotask-boundary wait as pitha-scanner-table.test.ts's
// own nextMacrotask/flush: lets the fire-and-forget fetch().then(json)
// .then(state-assign) microtask chain fully settle before asserting.
async function flush(el: ScannerTableElement): Promise<void> {
  const { promise, resolve } = Promise.withResolvers<void>();
  setTimeout(resolve, 0);
  await promise;
  await el.updateComplete;
}

// Upgrades already-parsed SSR markup (see the file comment above) into a
// <pitha-scanner-table>. Each test needs the tag to be undefined while the
// markup is inserted, and custom element definitions can't be removed, so
// the first call defines the tag and later calls create a fresh element
// via document.createElement + innerHTML and an explicit upgrade.
const SSR_MARKUP = `
  <table>
    <caption>Scanner Dashboard — as of 2026-09-28T00:00:00+09:00</caption>
    <thead><tr><th>銘柄</th></tr></thead>
    <tbody><tr data-symbol="SSR1"><td>SSR1</td></tr></tbody>
  </table>
`;

let defined = false;
function mountSsr(): ScannerTableElement {
  document.body.innerHTML = `<${TAG} api-url="/api/v1/scanner" ws-url="/ws/scanner">${SSR_MARKUP}</${TAG}>`;
  if (!defined) {
    // Defining the tag now - after the markup above already exists -
    // synchronously triggers the custom-element *upgrade* reaction on
    // that already-connected, already-childed node (spec: "upgrade an
    // element"), matching a deferred module script's real timing.
    customElements.define(TAG, PithaScannerTableUnderTest);
    defined = true;
  } else {
    customElements.upgrade(document.body);
  }
  return document.querySelector(TAG) as ScannerTableElement;
}

test('replaces the server-rendered fallback table in place instead of duplicating it', async () => {
  globalThis.fetch = mock(() =>
    Promise.resolve(
      new Response(JSON.stringify({ items: [], as_of: '2026-09-26T10:15:00+09:00' })),
    ),
  ) as unknown as typeof fetch;

  const el = mountSsr();
  await flush(el);

  expect(el.querySelectorAll('table').length).toBe(1);
  expect(el.querySelector('tr[data-symbol="SSR1"]')).toBeNull();
});

test('keeps the server-rendered table visible until the first data arrives', async () => {
  globalThis.fetch = mock(() => new Promise<Response>(() => {})) as unknown as typeof fetch;

  const el = mountSsr();
  await el.updateComplete;

  expect(el.querySelectorAll('table').length).toBe(1);
  expect(el.querySelector('tr[data-symbol="SSR1"]')).not.toBeNull();
});

test('keeps the server-rendered table when the initial fetch fails', async () => {
  globalThis.fetch = mock(() =>
    Promise.resolve(new Response('', { status: 500 })),
  ) as unknown as typeof fetch;

  const el = mountSsr();
  await flush(el);

  expect(el.querySelector('tr[data-symbol="SSR1"]')).not.toBeNull();
  expect(el.querySelectorAll('table').length).toBe(1);
  expect(el.querySelector('[role="alert"]')?.textContent).toContain('500');
});
