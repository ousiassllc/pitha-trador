import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test';
import './pitha-calibration-heatmap';
import type { CalibrationAPIResponse, CalibrationBucket } from './pitha-calibration-heatmap';

type HeatmapElement = HTMLElement & { updateComplete: Promise<boolean> };

const bucket = (overrides: Partial<CalibrationBucket> = {}): CalibrationBucket => ({
  range: '0.70-0.80',
  direction_accuracy: 0.63,
  avg_future_return_pct: 0.11,
  ...overrides,
});

const response = (overrides: Partial<CalibrationAPIResponse> = {}): CalibrationAPIResponse => ({
  buckets: [bucket()],
  brier_score: 0.19,
  log_loss: 0.52,
  expected_calibration_error: 0.06,
  ...overrides,
});

let originalFetch: typeof fetch;

beforeEach(() => {
  originalFetch = globalThis.fetch;
  document.body.innerHTML = '';
});

afterEach(() => {
  globalThis.fetch = originalFetch;
  document.body.innerHTML = '';
});

// pitha-calibration-heatmap fetches fire-and-forget from firstUpdated (not
// awaited or exposed as a property, matching pitha-scanner-table's own
// test - see its nextMacrotask), so there is no promise this test can
// await directly for "the fetch mock's response has been parsed and
// rendered". A single real macrotask tick (zero-delay, not a guessed
// race-condition duration) is the deterministic way to let that
// fire-and-forget microtask chain (fetch -> json() -> state assignment ->
// Lit's own update scheduling, all promise-based) fully settle before
// asserting - it relies on the microtasks-before-macrotasks ordering the
// spec guarantees, not on wall-clock timing (bun:test has no fake-timer
// integration with Lit's internal scheduler, so this mirrors the existing
// pitha-scanner-table.test.ts helper rather than introducing a second way
// to await this same fire-and-forget pattern).
async function nextMacrotask(): Promise<void> {
  const { promise, resolve } = Promise.withResolvers<void>();
  setTimeout(resolve, 0);
  await promise;
}

async function flush(el: HeatmapElement): Promise<void> {
  await nextMacrotask();
  await el.updateComplete;
}

async function mount(body: CalibrationAPIResponse) {
  const fetchMock = mock(() => Promise.resolve(new Response(JSON.stringify(body))));
  globalThis.fetch = fetchMock as unknown as typeof fetch;

  const el = document.createElement('pitha-calibration-heatmap') as HeatmapElement;
  document.body.appendChild(el);
  await flush(el);
  return { el, fetchMock };
}

describe('pitha-calibration-heatmap', () => {
  test('loads calibration metrics from calibration-url and renders a heatmap cell per bucket', async () => {
    const { el } = await mount(
      response({
        buckets: [
          bucket({ range: '0.50-0.60', direction_accuracy: 0.51, avg_future_return_pct: -0.05 }),
          bucket({ range: '0.90-1.00', direction_accuracy: 0.78, avg_future_return_pct: 0.39 }),
        ],
      }),
    );

    const cells = el.shadowRoot?.querySelectorAll('[data-testid="calibration-heatmap-cell"]');
    expect(cells?.length).toBe(2);
    expect(el.shadowRoot?.textContent).toContain('0.50-0.60');
    expect(el.shadowRoot?.textContent).toContain('0.90-1.00');
    expect(el.shadowRoot?.textContent).toContain('51.0%');
    expect(el.shadowRoot?.textContent).toContain('0.190'); // brier_score
  });

  test('the refresh button re-fetches calibration-url without opening a WebSocket', async () => {
    const originalWebSocket = globalThis.WebSocket;
    // @ts-expect-error - deliberately undefined to prove no WebSocket is used
    globalThis.WebSocket = undefined;
    try {
      const { el, fetchMock } = await mount(response());
      expect(fetchMock.mock.calls.length).toBe(1);

      const button = el.shadowRoot?.querySelector('button');
      button?.click();
      await flush(el);

      expect(fetchMock.mock.calls.length).toBe(2);
    } finally {
      globalThis.WebSocket = originalWebSocket;
    }
  });

  test('renders an error message when the fetch fails', async () => {
    const fetchMock = mock(() => Promise.resolve(new Response('fail', { status: 500 })));
    globalThis.fetch = fetchMock as unknown as typeof fetch;

    const el = document.createElement('pitha-calibration-heatmap') as HeatmapElement;
    document.body.appendChild(el);
    await flush(el);

    expect(el.shadowRoot?.querySelector('[role="alert"]')).not.toBeNull();
  });
});
