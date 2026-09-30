import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test';
import './pitha-scanner-table';
import type { ScannerItem } from './pitha-scanner-table';

type Listener = (event: unknown) => void;
type ScannerTableElement = HTMLElement & { updateComplete: Promise<boolean> };

class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  private listeners: Record<string, Listener[]> = {};

  constructor(public readonly url: string) {
    FakeWebSocket.instances.push(this);
  }

  addEventListener(type: string, listener: Listener): void {
    if (!this.listeners[type]) {
      this.listeners[type] = [];
    }
    this.listeners[type].push(listener);
  }

  emit(type: string, event: unknown = {}): void {
    for (const listener of this.listeners[type] ?? []) {
      listener(event);
    }
  }

  close(): void {
    this.emit('close', { code: 1000 });
  }
}

const item = (overrides: Partial<ScannerItem> = {}): ScannerItem => ({
  symbol: '7203',
  price: 2831.5,
  return_1m: 0.12,
  return_5m: 0.42,
  volume_ratio_5m: 3.4,
  price_vs_vwap_bps: 38,
  spread_bps: 7,
  jev_direction: 'LONG',
  jev_confidence: 0.74,
  entry_quality: 'strong',
  current_position: null,
  ...overrides,
});

let originalFetch: typeof fetch;
let originalWebSocket: typeof WebSocket;

beforeEach(() => {
  originalFetch = globalThis.fetch;
  originalWebSocket = globalThis.WebSocket;
  FakeWebSocket.instances = [];
  globalThis.WebSocket = FakeWebSocket as unknown as typeof WebSocket;
  document.body.innerHTML = '';
});

afterEach(() => {
  globalThis.fetch = originalFetch;
  globalThis.WebSocket = originalWebSocket;
  document.body.innerHTML = '';
});

// `pitha-scanner-table` fetches its initial data fire-and-forget from
// connectedCallback (matching docs/components/overview.md §5.1's
// `pitha-price-chart` example: `loadInitial()` isn't awaited or exposed as
// a property), so there is no promise the test can await directly for
// "the fetch mock's response has been parsed and rendered". A single real
// macrotask tick is the deterministic way to let that fire-and-forget
// microtask chain (fetch -> json() -> state assignment -> Lit's own
// update scheduling, all promise-based) fully settle before asserting -
// it is not a guessed race-condition duration, it relies on the
// microtasks-before-macrotasks ordering the spec guarantees.
async function nextMacrotask(): Promise<void> {
  const { promise, resolve } = Promise.withResolvers<void>();
  setTimeout(resolve, 0);
  await promise;
}

async function flush(el: ScannerTableElement): Promise<void> {
  await nextMacrotask();
  await el.updateComplete;
}

// mount stubs `fetch` to resolve with { items, as_of }, appends a fresh
// <pitha-scanner-table>, and waits for its initial render to complete.
async function mount(items: ScannerItem[]) {
  const fetchMock = mock(() =>
    Promise.resolve(new Response(JSON.stringify({ items, as_of: '2026-09-26T10:15:00+09:00' }))),
  );
  globalThis.fetch = fetchMock as unknown as typeof fetch;

  const el = document.createElement('pitha-scanner-table') as ScannerTableElement;
  document.body.appendChild(el);
  await flush(el);
  return { el, fetchMock };
}

describe('pitha-scanner-table', () => {
  test('loads the initial candidate list from api-url and renders a row per item', async () => {
    const { el } = await mount([
      item(),
      item({ symbol: '9984', price: 7000, jev_direction: null }),
    ]);

    const rows = el.querySelectorAll('tbody tr');
    expect(rows).toHaveLength(2);
    expect(rows[0].getAttribute('data-symbol')).toBe('7203');
    expect(rows[0].textContent).toContain('LONG');
    expect(rows[1].getAttribute('data-symbol')).toBe('9984');
    expect(rows[1].textContent).toContain('—'); // no Jev evaluation yet
  });

  test('renders symbol cells as plain <a href> links (real navigation, not HTMX)', async () => {
    const { el } = await mount([item()]);

    const link = el.querySelector('td a') as HTMLAnchorElement;
    expect(link.getAttribute('href')).toBe('/symbols/7203');
    expect(link.getAttribute('hx-get')).toBeNull();
    expect(link.hasAttribute('hx-boost')).toBe(false);
  });

  test('applies /ws/scanner scanner_update pushes without re-fetching api-url', async () => {
    const { el, fetchMock } = await mount([item()]);
    expect(fetchMock).toHaveBeenCalledTimes(1);

    const socket = FakeWebSocket.instances[0];
    socket.emit('message', {
      data: JSON.stringify({
        type: 'scanner_update',
        items: [item({ symbol: '6758', price: 1500 })],
      }),
    });
    await el.updateComplete;

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const rows = el.querySelectorAll('tbody tr');
    expect(rows).toHaveLength(1);
    expect(rows[0].getAttribute('data-symbol')).toBe('6758');
  });

  test('sorts client-side on header click without an additional fetch', async () => {
    const { el, fetchMock } = await mount([item({ symbol: 'BBBB' }), item({ symbol: 'AAAA' })]);

    // Default sort key is 'symbol' ascending.
    let rows = el.querySelectorAll('tbody tr');
    expect(rows[0].getAttribute('data-symbol')).toBe('AAAA');

    const symbolHeader = el.querySelector('thead th') as HTMLElement;
    symbolHeader.click(); // same column again -> toggles to descending
    await el.updateComplete;

    expect(fetchMock).toHaveBeenCalledTimes(1);
    rows = el.querySelectorAll('tbody tr');
    expect(rows[0].getAttribute('data-symbol')).toBe('BBBB');
  });

  test('shows the candidate count and as-of caption, and colors returns by sign', async () => {
    const { el } = await mount([
      item({ symbol: 'UP', return_1m: 0.12, return_5m: -0.3 }),
      item({ symbol: 'FLAT', return_1m: 0, return_5m: null }),
    ]);

    expect(el.querySelector('[data-testid="scanner-count"]')?.textContent).toContain('2');
    expect(el.querySelector('caption')?.textContent).toContain('2026-09-26T10:15:00+09:00');

    const cells = (symbol: string) => el.querySelectorAll(`tr[data-symbol="${symbol}"] td`);
    expect(cells('UP')[2].textContent?.trim()).toBe('+0.12');
    expect(cells('UP')[2].className).toContain('text-green-700');
    expect(cells('UP')[3].textContent?.trim()).toBe('-0.30');
    expect(cells('UP')[3].className).toContain('text-red-700');
    expect(cells('FLAT')[2].className).toContain('text-slate-500');
    expect(cells('FLAT')[3].textContent?.trim()).toBe('—');
  });

  test('renders Jev direction and entry quality as badges, pending when not evaluated', async () => {
    const { el } = await mount([
      item({ symbol: 'AAAA', jev_direction: 'SHORT', entry_quality: 'exceptional' }),
      item({ symbol: 'BBBB', jev_direction: null, jev_confidence: null, entry_quality: null }),
    ]);

    const evaluated = el.querySelectorAll('tr[data-symbol="AAAA"] td');
    expect(evaluated[7].querySelector('span')?.className).toContain('bg-red-100');
    expect(evaluated[8].textContent?.trim()).toBe('74%');
    expect(evaluated[9].querySelector('span')?.className).toContain('bg-emerald-600');

    const pending = el.querySelectorAll('tr[data-symbol="BBBB"] td');
    expect(pending[7].textContent?.trim()).toBe('pending');
    expect(pending[8].textContent?.trim()).toBe('—');
    expect(pending[9].textContent?.trim()).toBe('—');
  });

  test('gives every column header a tooltip explaining it', async () => {
    const { el } = await mount([item()]);

    const headers = el.querySelectorAll('thead th');
    expect(headers).toHaveLength(11);
    for (const th of headers) {
      expect(th.getAttribute('title')?.length).toBeGreaterThan(0);
    }
  });

  test('shows an empty-state message with a zero count when there are no candidates', async () => {
    const { el } = await mount([]);

    expect(el.querySelector('[data-testid="scanner-empty"]')).not.toBeNull();
    expect(el.querySelector('[data-testid="scanner-count"]')?.textContent).toContain('0');

    FakeWebSocket.instances[0].emit('message', {
      data: JSON.stringify({ type: 'scanner_update', items: [item()] }),
    });
    await el.updateComplete;

    expect(el.querySelector('[data-testid="scanner-empty"]')).toBeNull();
    expect(el.querySelector('[data-testid="scanner-count"]')?.textContent).toContain('1');
  });

  test('renders a fetch error message instead of throwing', async () => {
    globalThis.fetch = mock(() =>
      Promise.resolve(new Response('', { status: 500 })),
    ) as unknown as typeof fetch;

    const el = document.createElement('pitha-scanner-table') as ScannerTableElement;
    document.body.appendChild(el);
    await flush(el);

    const alert = el.querySelector('[role="alert"]');
    expect(alert).not.toBeNull();
    expect(alert?.textContent).toContain('500');
  });

  // The first update cycle used to close and reopen the socket (issue #170).
  test('opens a single WebSocket on mount and reconnects once when ws-url changes', async () => {
    const { el } = await mount([item()]);
    expect(FakeWebSocket.instances).toHaveLength(1);

    el.setAttribute('ws-url', '/ws/scanner-2');
    await el.updateComplete;

    expect(FakeWebSocket.instances).toHaveLength(2);
    expect(FakeWebSocket.instances[1].url).toContain('/ws/scanner-2');
  });
});
