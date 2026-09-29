import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test';
import './pitha-activity-feed';
import type { ActivityEvent, QueueStatus } from './pitha-activity-feed';
import { PithaActivityFeed } from './pitha-activity-feed';

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

const queue = (overrides: Partial<QueueStatus> = {}): QueueStatus => ({
  queue: 'jev-scout',
  pending: 3,
  running: 1,
  failed_recent: 0,
  ...overrides,
});

const event = (overrides: Partial<ActivityEvent> = {}): ActivityEvent => ({
  type: 'jev_trader',
  timestamp: '2026-09-29T01:15:00Z',
  symbol: '7203',
  detail: 'direction=LONG confidence=0.74',
  latency_ms: 820,
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

// The component fetches fire-and-forget from connectedCallback, so there
// is no promise to await; yielding one setImmediate macrotask boundary
// (no wall-clock duration involved) lets the whole promise-based
// fetch -> json -> state-assignment chain settle first.
async function flush(el: FeedElement): Promise<void> {
  await new Promise<void>((resolve) => setImmediate(resolve));
  await el.updateComplete;
}

// mount stubs `fetch` so that `type=kill_switch` requests return
// killSwitchEvents and every other request returns { queues, events }.
async function mount(
  queues: QueueStatus[],
  events: ActivityEvent[],
  killSwitchEvents: ActivityEvent[] = [],
) {
  const fetchMock = mock((url: string) => {
    const body = url.includes('type=kill_switch')
      ? { queues, events: killSwitchEvents, as_of: 'x' }
      : { queues, events, as_of: 'x' };
    return Promise.resolve(new Response(JSON.stringify(body)));
  });
  globalThis.fetch = fetchMock as unknown as typeof fetch;

  const el = document.createElement('pitha-activity-feed') as FeedElement;
  document.body.appendChild(el);
  await flush(el);
  return { el, fetchMock };
}

function emit(message: unknown): void {
  FakeWebSocket.instances[0].emit('message', { data: JSON.stringify(message) });
}

describe('pitha-activity-feed', () => {
  // Same upgrade-after-existing-children timing as
  // pitha-scanner-table.hydration.test.ts: the SSR markup must already be
  // in the DOM when the (subclass) element is defined, or this would be a
  // fresh construction that never sees children to clear.
  test('replaces the server-rendered fallback instead of duplicating it', async () => {
    globalThis.fetch = mock(() =>
      Promise.resolve(
        new Response(JSON.stringify({ queues: [queue()], events: [event()], as_of: 'x' })),
      ),
    ) as unknown as typeof fetch;
    document.body.innerHTML =
      '<pitha-activity-feed-hydration-test><section id="queue-status"><table><tbody><tr data-queue="ssr"></tr></tbody></table></section></pitha-activity-feed-hydration-test>';
    class PithaActivityFeedUnderTest extends PithaActivityFeed {}
    customElements.define('pitha-activity-feed-hydration-test', PithaActivityFeedUnderTest);
    const el = document.querySelector('pitha-activity-feed-hydration-test') as FeedElement;
    await flush(el);

    expect(el.querySelectorAll('#queue-status')).toHaveLength(1);
    expect(el.querySelector('[data-queue="ssr"]')).toBeNull();
    expect(el.querySelector('[data-queue="jev-scout"]')).not.toBeNull();
  });

  test('renders queue counts and feed rows from the snapshot', async () => {
    const { el } = await mount(
      [queue(), queue({ queue: 'jev-trader', pending: 0, running: 0, failed_recent: 2 })],
      [
        event(),
        event({
          type: 'job',
          symbol: undefined,
          latency_ms: undefined,
          detail: 'queue=jev-scout status=running attempts=1',
        }),
      ],
    );

    const queueRows = el.querySelectorAll('#queue-status tbody tr');
    expect(queueRows).toHaveLength(2);
    expect(queueRows[1].textContent).toContain('jev-trader');
    expect(queueRows[1].querySelectorAll('td')[3].textContent).toBe('2');

    const rows = el.querySelectorAll('#activity-feed tbody tr');
    expect(rows).toHaveLength(2);
    expect(rows[0].textContent).toContain('direction=LONG');
    expect(rows[0].textContent).toContain('820');
    expect(rows[1].querySelectorAll('td')[2].textContent).toBe('—'); // no symbol
    expect(rows[1].querySelectorAll('td')[4].textContent).toBe('—'); // no latency
  });

  test('lists kill switch events in their own section', async () => {
    const ks = event({
      type: 'kill_switch',
      symbol: undefined,
      latency_ms: undefined,
      detail: 'reason=daily_loss_limit',
    });
    const { el } = await mount([queue()], [event()], [ks]);

    expect(el.querySelector('#kill-switch-events')?.textContent).toContain(
      'reason=daily_loss_limit',
    );
  });

  test('job_update pushes replace only that queue counts', async () => {
    const { el, fetchMock } = await mount(
      [queue(), queue({ queue: 'jev-trader', pending: 5 })],
      [],
    );
    const before = fetchMock.mock.calls.length;

    emit({ type: 'job_update', queue: 'jev-scout', pending: 0, running: 2, failed_recent: 1 });
    await el.updateComplete;

    const cells = (name: string) =>
      [...el.querySelectorAll(`[data-queue="${name}"] td`)].map((td) => td.textContent);
    expect(cells('jev-scout')).toEqual(['jev-scout', '0', '2', '1']);
    expect(cells('jev-trader')).toEqual(['jev-trader', '5', '1', '0']);
    expect(fetchMock.mock.calls.length).toBe(before); // no re-fetch
  });

  test('activity_event pushes are prepended newest first', async () => {
    const { el } = await mount([queue()], [event({ symbol: '7203' })]);

    emit({
      type: 'activity_event',
      event: event({ type: 'jev_scout', symbol: '9984', timestamp: '2026-09-29T01:16:00Z' }),
    });
    await el.updateComplete;

    const rows = el.querySelectorAll('#activity-feed tbody tr');
    expect(rows).toHaveLength(2);
    expect(rows[0].getAttribute('data-event-type')).toBe('jev_scout');
    expect(rows[0].textContent).toContain('9984');
  });

  test('type filter re-fetches with ?type= and drops non-matching pushes', async () => {
    const { el, fetchMock } = await mount([queue()], [event()]);

    const select = el.querySelector('select[data-filter="type"]') as HTMLSelectElement;
    select.value = 'jev_scout';
    select.dispatchEvent(new Event('change'));
    await flush(el);
    const urls = fetchMock.mock.calls.map((c) => c[0] as string);
    expect(urls.some((u) => u.includes('?type=jev_scout') && !u.includes('kill_switch'))).toBe(
      true,
    );

    const rowsBefore = el.querySelectorAll('#activity-feed tbody tr').length;
    emit({ type: 'activity_event', event: event({ type: 'jev_trader', symbol: '1111' }) });
    await el.updateComplete;
    expect(el.querySelectorAll('#activity-feed tbody tr')).toHaveLength(rowsBefore);

    emit({ type: 'activity_event', event: event({ type: 'jev_scout', symbol: '2222' }) });
    await el.updateComplete;
    expect(el.querySelectorAll('#activity-feed tbody tr')).toHaveLength(rowsBefore + 1);
  });

  test('queue filter matches only job events on that queue', async () => {
    const { el, fetchMock } = await mount([queue()], []);

    const select = el.querySelector('select[data-filter="queue"]') as HTMLSelectElement;
    select.value = 'jev-scout';
    select.dispatchEvent(new Event('change'));
    await flush(el);
    expect((fetchMock.mock.calls.at(-1)?.[0] as string) ?? '').toContain('queue=jev-scout');

    emit({
      type: 'activity_event',
      event: event({ type: 'job', queue: 'jev-trader', symbol: undefined }),
    });
    emit({ type: 'activity_event', event: event({ type: 'jev_scout' }) });
    emit({
      type: 'activity_event',
      event: event({ type: 'job', queue: 'jev-scout', symbol: undefined }),
    });
    await el.updateComplete;

    const rows = el.querySelectorAll('#activity-feed tbody tr');
    expect(rows).toHaveLength(1);
    expect(rows[0].getAttribute('data-event-type')).toBe('job');
  });

  test('caps the live feed at 500 rows', async () => {
    const { el } = await mount([queue()], []);
    for (let i = 0; i < 510; i++) {
      emit({ type: 'activity_event', event: event({ symbol: String(i) }) });
    }
    await el.updateComplete;

    expect(el.querySelectorAll('#activity-feed tbody tr')).toHaveLength(500);
  });

  // The first update cycle used to close and reopen the socket (issue #170).
  test('opens a single WebSocket on mount and reconnects once when ws-url changes', async () => {
    const { el } = await mount([queue()], []);
    expect(FakeWebSocket.instances).toHaveLength(1);

    el.setAttribute('ws-url', '/ws/activity-2');
    await el.updateComplete;

    expect(FakeWebSocket.instances).toHaveLength(2);
    expect(FakeWebSocket.instances[1].url).toContain('/ws/activity-2');
  });
});
