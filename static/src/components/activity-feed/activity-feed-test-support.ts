// Shared fixtures for the pitha-activity-feed tests: a fake WebSocket, fetch
// stubbing helpers and the per-test global setup/teardown.
import { afterEach, beforeEach, mock } from 'bun:test';
import { FakeWebSocket, installFakeWebSocket } from '../lib/ws-test-support';
import './pitha-activity-feed';
import type { ActivityEvent, QueueStatus } from './activity-feed-types';

export type FeedElement = HTMLElement & { updateComplete: Promise<boolean> };

export const queue = (overrides: Partial<QueueStatus> = {}): QueueStatus => ({
  queue: 'jev-scout',
  pending: 3,
  running: 1,
  failed_recent: 0,
  ...overrides,
});

export const event = (overrides: Partial<ActivityEvent> = {}): ActivityEvent => ({
  type: 'jev_trader',
  timestamp: '2026-09-29T01:15:00Z',
  symbol: '7203',
  detail: 'direction=LONG confidence=0.74',
  latency_ms: 820,
  ...overrides,
});

let originalFetch: typeof fetch;
let restoreWebSocket: () => void;

// installFakes registers the beforeEach/afterEach pair that swaps in
// FakeWebSocket and restores the real globals; call it once per test file.
export function installFakes(): void {
  beforeEach(() => {
    originalFetch = globalThis.fetch;
    restoreWebSocket = installFakeWebSocket();
    document.body.innerHTML = '';
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    restoreWebSocket();
    document.body.innerHTML = '';
  });
}

// The component fetches fire-and-forget from connectedCallback, so there
// is no promise to await; yielding one setImmediate macrotask boundary
// (no wall-clock duration involved) lets the whole promise-based
// fetch -> json -> state-assignment chain settle first.
export async function flush(el: FeedElement): Promise<void> {
  await new Promise<void>((resolve) => setImmediate(resolve));
  await el.updateComplete;
}

// Builds an element with the URLs Templ injects (pages.ActivityLogPage);
// the component has no defaults.
export function createFeed(): FeedElement {
  const el = document.createElement('pitha-activity-feed') as FeedElement;
  el.setAttribute('api-url', '/api/v1/activity');
  el.setAttribute('ws-url', '/ws/activity');
  el.setAttribute('kill-switch-events-url', '/api/v1/activity?type=kill_switch&limit=10');
  return el;
}

// mount stubs `fetch` so that `type=kill_switch` requests return
// killSwitchEvents and every other request returns { queues, events }.
export async function mount(
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

  const el = createFeed();
  document.body.appendChild(el);
  await flush(el);
  return { el, fetchMock };
}

export function emit(message: unknown): void {
  FakeWebSocket.instances[0].emit('message', { data: JSON.stringify(message) });
}
