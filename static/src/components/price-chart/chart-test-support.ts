// Shared harness for the pitha-price-chart tests. Importing this module
// installs the lightweight-charts mock (createChart needs a real canvas, so the
// options it is given are captured instead); import it before the component.

import { afterEach, beforeEach, mock } from 'bun:test';
import { FakeWebSocket, installFakeWebSocket } from '../lib/ws-test-support';

export { FakeWebSocket };

type MockFn = ReturnType<typeof mock>;

export const createChartOptions: unknown[] = [];
export const createdSeries: {
  options: Record<string, unknown>;
  setData: MockFn;
  update: MockFn;
  setMarkers: MockFn;
  applyOptions: MockFn;
  priceScale: MockFn;
}[] = [];
export const createdCharts: {
  remove: MockFn;
  applyOptions: MockFn;
  takeScreenshot: MockFn;
  live: HTMLElement;
}[] = [];
// applyOptions calls on any price scale (a series' own scale or the chart's
// 'right' scale), in call order, for the scaleMargins assertions.
export const priceScaleOptions: { scale: string; options: unknown }[] = [];
const priceScale = (scale: string) => ({
  applyOptions: (options: unknown) => priceScaleOptions.push({ scale, options }),
});
const series = (options: Record<string, unknown> = {}) => {
  const s = {
    options,
    setData: mock(),
    update: mock(),
    setMarkers: mock(),
    applyOptions: mock(),
    priceScale: mock(() => priceScale('series')),
  };
  createdSeries.push(s);
  return s;
};
mock.module('lightweight-charts', () => ({
  createChart: (container: HTMLElement, options: unknown) => {
    createChartOptions.push(options);
    const live = document.createElement('div');
    container.append(live); // like the real chart element
    const chart = {
      addCandlestickSeries: series,
      addLineSeries: series,
      addHistogramSeries: series,
      priceScale: (id: string) => priceScale(id),
      remove: mock(),
      applyOptions: mock(),
      takeScreenshot: mock(() => document.createElement('canvas')),
      chartElement: () => live,
      live,
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
