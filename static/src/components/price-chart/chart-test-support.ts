// Shared harness for the pitha-price-chart tests. Importing this module
// installs the lightweight-charts mock (createChart needs a real canvas, so the
// options it is given are captured instead); import it before the component.

import { afterEach, beforeEach, mock } from 'bun:test';

export const createChartOptions: unknown[] = [];
export const createdSeries: {
  setData: ReturnType<typeof mock>;
  update: ReturnType<typeof mock>;
  setMarkers: ReturnType<typeof mock>;
}[] = [];
export const createdCharts: { remove: ReturnType<typeof mock> }[] = [];
const series = () => {
  const s = { setData: mock(), update: mock(), setMarkers: mock() };
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
      remove: mock(),
    };
    createdCharts.push(chart);
    return chart;
  },
}));

export class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  closed = false;
  private readonly listeners: Record<string, ((event: unknown) => void)[]> = {};
  constructor(public readonly url: string) {
    FakeWebSocket.instances.push(this);
  }
  addEventListener(type: string, listener: (event: unknown) => void): void {
    this.listeners[type] = [...(this.listeners[type] ?? []), listener];
  }
  emit(type: string, event: unknown): void {
    for (const listener of this.listeners[type] ?? []) listener(event);
  }
  close(): void {
    this.closed = true;
  }
}

let originalFetch: typeof fetch;
let originalWebSocket: typeof WebSocket;

// installChartHarness registers the per-test setup/teardown in the calling
// test file (hooks registered at import time would only reach the first one).
export function installChartHarness(): void {
  beforeEach(() => {
    originalFetch = globalThis.fetch;
    originalWebSocket = globalThis.WebSocket;
    createChartOptions.length = 0;
    createdSeries.length = 0;
    createdCharts.length = 0;
    FakeWebSocket.instances = [];
    globalThis.WebSocket = FakeWebSocket as unknown as typeof WebSocket;
    globalThis.fetch = mock(() =>
      Promise.resolve(new Response(JSON.stringify({ symbol: '7203', candles: [] }))),
    ) as unknown as typeof fetch;
    document.body.innerHTML = '';
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    globalThis.WebSocket = originalWebSocket;
    document.body.innerHTML = '';
  });
}
