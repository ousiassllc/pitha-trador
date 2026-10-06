// Shared harness for the pitha-price-chart tests. Importing this module
// installs the lightweight-charts mock (createChart needs a real canvas, so the
// options it is given are captured instead); import it before the component.

import { afterEach, beforeEach, mock } from 'bun:test';
import { FakeWebSocket, installFakeWebSocket } from '../lib/ws-test-support';

export { FakeWebSocket };

type MockFn = ReturnType<typeof mock>;

export const createChartOptions: unknown[] = [];
export const createdSeries: {
  setData: MockFn;
  update: MockFn;
  setMarkers: MockFn;
  priceScale: MockFn;
}[] = [];
export const createdCharts: { remove: MockFn }[] = [];
// applyOptions calls on any price scale (a series' own scale or the chart's
// 'right' scale), in call order, for the scaleMargins assertions.
export const priceScaleOptions: { scale: string; options: unknown }[] = [];
const priceScale = (scale: string) => ({
  applyOptions: (options: unknown) => priceScaleOptions.push({ scale, options }),
});
const series = () => {
  const s = {
    setData: mock(),
    update: mock(),
    setMarkers: mock(),
    priceScale: mock(() => priceScale('series')),
  };
  createdSeries.push(s);
  return s;
};
mock.module('lightweight-charts', () => ({
  createChart: (_container: unknown, options: unknown) => {
    createChartOptions.push(options);
    const chart = {
      addCandlestickSeries: series,
      addLineSeries: series,
      addHistogramSeries: series,
      priceScale: (id: string) => priceScale(id),
      remove: mock(),
    };
    createdCharts.push(chart);
    return chart;
  },
}));

let originalFetch: typeof fetch;
let restoreWebSocket: () => void;

// installChartHarness registers the per-test setup/teardown in the calling
// test file (hooks registered at import time would only reach the first one).
export function installChartHarness(): void {
  beforeEach(() => {
    originalFetch = globalThis.fetch;
    createChartOptions.length = 0;
    createdSeries.length = 0;
    createdCharts.length = 0;
    priceScaleOptions.length = 0;
    restoreWebSocket = installFakeWebSocket();
    globalThis.fetch = mock(() =>
      Promise.resolve(new Response(JSON.stringify({ symbol: '7203', candles: [] }))),
    ) as unknown as typeof fetch;
    document.body.innerHTML = '';
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    restoreWebSocket();
    document.body.innerHTML = '';
  });
}
