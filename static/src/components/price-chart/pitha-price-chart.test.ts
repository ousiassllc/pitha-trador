import { afterEach, beforeEach, describe, expect, mock, spyOn, test } from 'bun:test';

// createChart needs a real canvas; capture the options it is given instead.
const createChartOptions: unknown[] = [];
const createdSeries: { setData: ReturnType<typeof mock>; update: ReturnType<typeof mock> }[] = [];
const series = () => {
  const s = { setData: mock(), update: mock(), setMarkers: mock() };
  createdSeries.push(s);
  return s;
};
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
  close(): void {}
}

let originalFetch: typeof fetch;
let originalWebSocket: typeof WebSocket;

beforeEach(() => {
  originalFetch = globalThis.fetch;
  originalWebSocket = globalThis.WebSocket;
  createChartOptions.length = 0;
  createdSeries.length = 0;
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

describe('pitha-price-chart tick handling', () => {
  const MINUTE_MS = 60_000;
  // Fixed to the start of a minute so "same minute" is deterministic.
  const baseMs = Math.floor(Date.parse('2026-09-29T01:00:00Z') / MINUTE_MS) * MINUTE_MS;

  async function mount(candles: unknown[]) {
    globalThis.fetch = mock(() =>
      Promise.resolve(new Response(JSON.stringify({ symbol: '7203', candles }))),
    ) as unknown as typeof fetch;
    const el = document.createElement('pitha-price-chart') as InstanceType<typeof PithaPriceChart>;
    el.setAttribute('candles-url', '/api/v1/symbols/7203/candles');
    el.setAttribute('ws-url', '/ws/symbols/7203');
    document.body.appendChild(el);
    await el.updateComplete;
    await new Promise((resolve) => setTimeout(resolve, 0));
    return el;
  }

  function tick(price: number, nowMs: number) {
    spyOn(Date, 'now').mockReturnValue(nowMs);
    FakeWebSocket.instances[0].emit('message', { data: JSON.stringify({ type: 'tick', price }) });
  }

  afterEach(() => {
    (Date.now as unknown as { mockRestore?: () => void }).mockRestore?.();
  });

  // Ticks used to add one per-second pseudo bar each (issue #183).
  test('folds ticks within the same minute into one bar', async () => {
    await mount([]);
    const candle = createdSeries[0];

    tick(100, baseMs + 1_000);
    tick(105, baseMs + 3_000);
    tick(98, baseMs + 5_000);

    const bars = candle.update.mock.calls.map((call) => call[0]);
    expect(new Set(bars.map((b) => b.time)).size).toBe(1);
    expect(bars.at(-1)).toEqual({
      time: baseMs / 1000,
      open: 100,
      high: 105,
      low: 98,
      close: 98,
    });
  });

  test('continues the last loaded candle when it is the current minute', async () => {
    await mount([
      {
        time: new Date(baseMs).toISOString(),
        open: 100,
        high: 110,
        low: 95,
        close: 102,
        volume: 1,
        vwap: 101,
      },
    ]);
    const candle = createdSeries[0];

    tick(104, baseMs + 10_000);

    expect(candle.update.mock.calls.at(-1)?.[0]).toEqual({
      time: baseMs / 1000,
      open: 100,
      high: 110,
      low: 95,
      close: 104,
    });
  });

  test('starts a new bar only when the minute changes', async () => {
    await mount([]);
    const candle = createdSeries[0];

    tick(100, baseMs + 1_000);
    tick(101, baseMs + MINUTE_MS + 1_000);

    const bars = candle.update.mock.calls.map((call) => call[0]);
    expect(bars.map((b) => b.time)).toEqual([baseMs / 1000, baseMs / 1000 + 60]);
    expect(bars[1]).toMatchObject({ open: 101, high: 101, low: 101, close: 101 });
  });

  // A price of 0 (no snapshot yet) collapsed the chart's autoscale (issue #183).
  test('ignores ticks whose price is not positive', async () => {
    await mount([]);
    const candle = createdSeries[0];

    tick(0, baseMs + 1_000);
    tick(-1, baseMs + 2_000);

    expect(candle.update).not.toHaveBeenCalled();
  });
});
