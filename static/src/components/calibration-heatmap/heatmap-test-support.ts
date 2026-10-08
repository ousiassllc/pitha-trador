import { afterEach, beforeEach, mock } from 'bun:test';
import type { CalibrationAPIResponse, CalibrationBucket } from './calibration-view';

// createChart needs a real canvas: the real library arms deferred timers under
// happy-dom that throw "Value is null" after the test ends. Stub it and
// capture the series to assert the plotted points instead.
export const createdCharts: { remove: ReturnType<typeof mock> }[] = [];
export const createdSeries: { setData: ReturnType<typeof mock> }[] = [];
mock.module('lightweight-charts', () => ({
  LineStyle: { Dashed: 2 },
  createChart: () => {
    const addLineSeries = () => {
      const series = { setData: mock() };
      createdSeries.push(series);
      return series;
    };
    const chart = { addLineSeries, remove: mock() };
    createdCharts.push(chart);
    return chart;
  },
}));

export type HeatmapElement = HTMLElement & { updateComplete: Promise<boolean> };

export const bucket = (overrides: Partial<CalibrationBucket> = {}): CalibrationBucket => ({
  range: '0.70-0.80',
  avg_confidence: 0.75,
  direction_accuracy: 0.63,
  avg_future_return_pct: 0.11,
  sample_count: 20,
  trade_count: 0,
  total_pnl: 0,
  avg_pnl_pct: 0,
  trade_win_rate: 0,
  ...overrides,
});

export const response = (
  overrides: Partial<CalibrationAPIResponse> = {},
): CalibrationAPIResponse => ({
  horizon: 'all',
  buckets: [bucket()],
  by_direction: [],
  brier_score: 0.19,
  log_loss: 0.52,
  expected_calibration_error: 0.06,
  trade_count: 0,
  pnl_brier_score: 0,
  pnl_log_loss: 0,
  ...overrides,
});

let originalFetch: typeof fetch;

// installHeatmapHarness registers the per-test setup/teardown in the calling
// test file (hooks registered at import time would only reach the first one).
export function installHeatmapHarness(): void {
  beforeEach(() => {
    originalFetch = globalThis.fetch;
    createdCharts.length = 0;
    createdSeries.length = 0;
    document.body.innerHTML = '';
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    document.body.innerHTML = '';
  });
}

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

export async function flush(el: HeatmapElement): Promise<void> {
  await nextMacrotask();
  await el.updateComplete;
}

export async function mount(body: CalibrationAPIResponse) {
  const fetchMock = mock((_input: RequestInfo | URL) =>
    Promise.resolve(new Response(JSON.stringify(body))),
  );
  globalThis.fetch = fetchMock as unknown as typeof fetch;

  const el = document.createElement('pitha-calibration-heatmap') as HeatmapElement;
  el.setAttribute('calibration-url', '/api/v1/calibration');
  document.body.appendChild(el);
  await flush(el);
  return { el, fetchMock };
}
