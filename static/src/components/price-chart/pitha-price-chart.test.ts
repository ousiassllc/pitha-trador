import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test';

// createChart needs a real canvas; capture the options it is given instead.
const createChartOptions: unknown[] = [];
const series = () => ({ setData: mock(), update: mock(), setMarkers: mock() });
mock.module('lightweight-charts', () => ({
  createChart: (_container: unknown, options: unknown) => {
    createChartOptions.push(options);
    return {
      addCandlestickSeries: series,
      addLineSeries: series,
      addHistogramSeries: series,
      remove: mock(),
    };
  },
}));

const { PithaPriceChart } = await import('./pitha-price-chart');

class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  constructor(public readonly url: string) {
    FakeWebSocket.instances.push(this);
  }
  addEventListener(): void {}
  close(): void {}
}

let originalFetch: typeof fetch;
let originalWebSocket: typeof WebSocket;

beforeEach(() => {
  originalFetch = globalThis.fetch;
  originalWebSocket = globalThis.WebSocket;
  createChartOptions.length = 0;
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

describe('pitha-price-chart', () => {
  // A fixed pixel width never followed window/container resizes (issue #145).
  test('creates the chart with autoSize so it follows its container', async () => {
    const el = document.createElement('pitha-price-chart') as InstanceType<typeof PithaPriceChart>;
    el.setAttribute('candles-url', '/api/v1/symbols/7203/candles');
    el.setAttribute('ws-url', '/ws/symbols/7203');
    document.body.appendChild(el);
    await el.updateComplete;

    expect(createChartOptions).toHaveLength(1);
    expect(createChartOptions[0]).toMatchObject({ autoSize: true });
  });

  // The first update cycle used to reload candles and reopen the socket (issue #170).
  test('loads candles and opens the WebSocket once on mount, again only on URL change', async () => {
    const el = document.createElement('pitha-price-chart') as InstanceType<typeof PithaPriceChart>;
    el.setAttribute('candles-url', '/api/v1/symbols/7203/candles');
    el.setAttribute('ws-url', '/ws/symbols/7203');
    document.body.appendChild(el);
    await el.updateComplete;

    expect(globalThis.fetch).toHaveBeenCalledTimes(1);
    expect(FakeWebSocket.instances).toHaveLength(1);

    el.setAttribute('candles-url', '/api/v1/symbols/6758/candles');
    el.setAttribute('ws-url', '/ws/symbols/6758');
    await el.updateComplete;

    expect(globalThis.fetch).toHaveBeenCalledTimes(2);
    expect(FakeWebSocket.instances).toHaveLength(2);
    expect(FakeWebSocket.instances[1].url).toContain('/ws/symbols/6758');
  });
});
