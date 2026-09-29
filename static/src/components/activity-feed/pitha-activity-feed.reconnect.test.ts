import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test';
import './pitha-activity-feed';

type Listener = (event: unknown) => void;
type FeedElement = HTMLElement & { updateComplete: Promise<boolean> };

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

// One setImmediate boundary lets the fire-and-forget fetch chain settle.
async function flush(el: FeedElement): Promise<void> {
  await new Promise<void>((resolve) => setImmediate(resolve));
  await el.updateComplete;
}

const snapshot = (pending: number, detail: string) =>
  JSON.stringify({
    queues: [{ queue: 'jev-scout', pending, running: 1, failed_recent: 0 }],
    events: [
      {
        type: 'kill_switch',
        timestamp: '2026-09-29T01:15:00Z',
        symbol: '',
        detail,
        latency_ms: 0,
      },
    ],
    as_of: 'x',
  });

const isBackground = (call: unknown): boolean => {
  const init = (call as [unknown, RequestInit])[1];
  return (init.headers as Record<string, string>)['X-Pitha-Background'] === '1';
};

async function mount() {
  const fetchMock = mock(() => Promise.resolve(new Response(snapshot(3, 'initial'))));
  globalThis.fetch = fetchMock as unknown as typeof fetch;
  const el = document.createElement('pitha-activity-feed') as FeedElement;
  document.body.appendChild(el);
  await flush(el);
  return { el, fetchMock };
}

// Drops the socket and reopens it (the client backs off ~500ms first).
async function reconnect(el: FeedElement): Promise<void> {
  FakeWebSocket.instances[0].emit('open'); // first connect: no re-fetch
  FakeWebSocket.instances[0].emit('close', { code: 1006 });
  await el.updateComplete;
  await new Promise((resolve) => setTimeout(resolve, 600));
  FakeWebSocket.instances[1].emit('open');
  await flush(el);
}

describe('pitha-activity-feed reconnect resync', () => {
  // Events missed while the socket was down only return via re-fetch (#221).
  test('re-fetches the snapshot and kill switch events once the WebSocket reconnects', async () => {
    const { el, fetchMock } = await mount();
    expect(fetchMock.mock.calls.length).toBe(2);

    fetchMock.mockClear();
    fetchMock.mockImplementation((() =>
      Promise.resolve(new Response(snapshot(9, 'missed while offline')))) as never);
    await reconnect(el);

    const urls = fetchMock.mock.calls.map((c) => (c as unknown as [string])[0]);
    expect(urls).toHaveLength(2);
    expect(urls.some((u) => u.includes('type=kill_switch'))).toBe(true);
    expect(el.querySelector('#queue-status tbody tr td:nth-child(2)')?.textContent).toBe('9');
    expect(el.querySelector('#activity-feed tbody tr td:nth-child(4)')?.textContent).toBe(
      'missed while offline',
    );
    expect(el.querySelector('#kill-switch-events li')?.textContent).toContain(
      'missed while offline',
    );
  });

  // FR-RISK-6: an unattended reconnect must not extend the dead-man's switch (#224, #225).
  test('sends the reconnect resync as background requests but operator fetches as heartbeat', async () => {
    const { el, fetchMock } = await mount();
    expect(fetchMock.mock.calls.every((c) => !isBackground(c))).toBe(true); // initial load

    fetchMock.mockClear();
    await reconnect(el);
    expect(fetchMock.mock.calls).toHaveLength(2);
    expect(fetchMock.mock.calls.every(isBackground)).toBe(true);

    fetchMock.mockClear();
    const select = el.querySelector('select[data-filter="type"]') as HTMLSelectElement;
    select.value = 'job';
    select.dispatchEvent(new Event('change'));
    await flush(el);
    expect(fetchMock.mock.calls).toHaveLength(1);
    expect(isBackground(fetchMock.mock.calls[0])).toBe(false);
  });
});
