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

  // The library prints UTC (09:00 JST as 00:00) unless given formatters (issue #478).
  test('renders the time axis and crosshair in JST', async () => {
    const el = document.createElement('pitha-price-chart') as InstanceType<typeof PithaPriceChart>;
    el.setAttribute('candles-url', '/api/v1/symbols/7203/candles');
    el.setAttribute('ws-url', '/ws/symbols/7203');
    document.body.appendChild(el);
    await el.updateComplete;

    const options = createChartOptions[0] as {
      localization: { timeFormatter: (t: number) => string };
      timeScale: {
        timeVisible: boolean;
        tickMarkFormatter: (t: number, type: number, locale: string) => string | null;
      };
    };
    const tseOpen = Date.parse('2026-10-05T00:00:00Z') / 1000;
    expect(options.timeScale.timeVisible).toBe(true);
    expect(options.localization.timeFormatter(tseOpen)).toBe('2026-10-05 09:00');
    expect(options.timeScale.tickMarkFormatter(tseOpen, 3, 'ja-JP')).toBe('09:00');
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

describe('pitha-price-chart WebSocket disconnection', () => {
  type ChartElement = InstanceType<typeof PithaPriceChart>;

  // One setImmediate boundary lets the fire-and-forget fetch chain settle.
  async function flush(el: ChartElement): Promise<void> {
    await new Promise<void>((resolve) => setImmediate(resolve));
    await el.updateComplete;
  }

  async function mount(): Promise<ChartElement> {
    const el = document.createElement('pitha-price-chart') as ChartElement;
    el.setAttribute('candles-url', '/api/v1/symbols/7203/candles');
    el.setAttribute('ws-url', '/ws/symbols/7203');
    document.body.appendChild(el);
    await flush(el);
    return el;
  }

  const isBackground = (call: unknown): boolean => {
    const init = (call as [unknown, RequestInit])[1];
    return (init.headers as Record<string, string>)['X-Pitha-Background'] === '1';
  };

  const notice = (el: ChartElement) => el.shadowRoot?.querySelector('.pitha-ws-disconnected');

  // WsClient schedules its backoff reconnect through window.setTimeout;
  // capture it so the test fires the reconnect without a real delay.
  function captureReconnect(): () => void {
    let reconnect: (() => void) | null = null;
    spyOn(window, 'setTimeout').mockImplementation(((fn: () => void) => {
      reconnect = fn;
      return 1;
    }) as never);
    return () => reconnect?.();
  }

  afterEach(() => {
    (window.setTimeout as unknown as { mockRestore?: () => void }).mockRestore?.();
  });

  // Without a notice a dropped socket left the chart frozen on a stale price (#336).
  test('shows the disconnected notice only while the socket is down', async () => {
    const el = await mount();
    const fireReconnect = captureReconnect();
    FakeWebSocket.instances[0].emit('open', {});
    await el.updateComplete;
    expect(notice(el)).toBeNull();

    FakeWebSocket.instances[0].emit('close', { code: 1006 });
    await el.updateComplete;
    expect(notice(el)).not.toBeNull();

    fireReconnect();
    FakeWebSocket.instances[1].emit('open', {});
    await flush(el);
    expect(notice(el)).toBeNull();
  });

  // Bars of the minutes the socket was down only return via re-fetch (#336).
  test('re-fetches the candles as a background request once the socket reconnects', async () => {
    const el = await mount();
    const fetchMock = mock(() =>
      Promise.resolve(new Response(JSON.stringify({ symbol: '7203', candles: [] }))),
    );
    globalThis.fetch = fetchMock as unknown as typeof fetch;
    const fireReconnect = captureReconnect();

    FakeWebSocket.instances[0].emit('open', {}); // first connect: no re-fetch
    await flush(el);
    expect(fetchMock).not.toHaveBeenCalled();

    FakeWebSocket.instances[0].emit('close', { code: 1006 });
    fireReconnect();
    FakeWebSocket.instances[1].emit('open', {});
    await flush(el);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(isBackground(fetchMock.mock.calls[0])).toBe(true);
  });

  test('neither fetches nor opens a WebSocket when the URLs are not set', async () => {
    const el = document.createElement('pitha-price-chart') as ChartElement;
    document.body.appendChild(el);
    await flush(el);

    expect(globalThis.fetch).not.toHaveBeenCalled();
    expect(FakeWebSocket.instances).toHaveLength(0);
  });
});
