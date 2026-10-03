// Shared fakes for the pitha-scanner-table test files.
import { afterEach, beforeEach, mock } from 'bun:test';
import './pitha-scanner-table';
import type { ScannerItem } from './pitha-scanner-table';

type Listener = (event: unknown) => void;
export type ScannerTableElement = HTMLElement & { updateComplete: Promise<boolean> };

export class FakeWebSocket {
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

export const item = (overrides: Partial<ScannerItem> = {}): ScannerItem => ({
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

// installFakes registers the beforeEach/afterEach pair that swaps in
// FakeWebSocket and restores the real globals; call it once per test file.
export function installFakes(): void {
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
}

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

export async function flush(el: ScannerTableElement): Promise<void> {
  await nextMacrotask();
  await el.updateComplete;
}

// createScannerTable builds an element with the URLs Templ injects in
// production (organisms.ScannerTableFallback); the component has no defaults.
export function createScannerTable(): ScannerTableElement {
  const el = document.createElement('pitha-scanner-table') as ScannerTableElement;
  el.setAttribute('api-url', '/api/v1/scanner');
  el.setAttribute('ws-url', '/ws/scanner');
  return el;
}

// mount stubs `fetch` to resolve with { items, as_of }, appends a fresh
// <pitha-scanner-table>, and waits for its initial render to complete.
export async function mount(items: ScannerItem[]) {
  const fetchMock = mock(() =>
    Promise.resolve(new Response(JSON.stringify({ items, as_of: '2026-09-26T10:15:00+09:00' }))),
  );
  globalThis.fetch = fetchMock as unknown as typeof fetch;

  const el = createScannerTable();
  document.body.appendChild(el);
  await flush(el);
  return { el, fetchMock };
}
