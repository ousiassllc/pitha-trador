import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test';
import { installColorSchemeSwitch, setDarkTokens } from '../lib/color-scheme-test-support';
import {
  createdCharts,
  createdSeries,
  FakeWebSocket,
  installChartHarness,
  priceScaleOptions,
} from './chart-test-support';

const { PithaPriceChart } = await import('./pitha-price-chart');

installChartHarness();

type ChartElement = InstanceType<typeof PithaPriceChart>;

const MINUTE_MS = 60_000;
const baseMs = Math.floor(Date.parse('2026-09-29T01:00:00Z') / MINUTE_MS) * MINUTE_MS;
const iso = (ms: number) => new Date(ms).toISOString();

const candle = (ms: number, vwap: number, close = 101) => ({
  time: iso(ms),
  open: 100,
  high: 110,
  low: 95,
  close,
  volume: 10,
  vwap,
});

async function mount(candles: unknown[] = []): Promise<ChartElement> {
  globalThis.fetch = mock(() =>
    Promise.resolve(new Response(JSON.stringify({ symbol: '7203', candles }))),
  ) as unknown as typeof fetch;
  const el = document.createElement('pitha-price-chart') as ChartElement;
  el.setAttribute('candles-url', '/api/v1/symbols/7203/candles');
  el.setAttribute('ws-url', '/ws/symbols/7203');
  document.body.appendChild(el);
  await el.updateComplete;
  await new Promise((resolve) => setTimeout(resolve, 0));
  await el.updateComplete;
  return el;
}

const send = (message: unknown) =>
  FakeWebSocket.instances[0].emit('message', { data: JSON.stringify(message) });

const jevUpdate = (direction: string | null, ms: number) =>
  send({ type: 'jev_update', direction, confidence: 0.7, time: iso(ms) });

// createdSeries order follows initChart: candlestick, VWAP line, volume histogram.
const candleSeries = () => createdSeries[0];
const vwapSeriesMock = () => createdSeries[1];

describe('pitha-price-chart pane layout (issue #634)', () => {
  test('keeps the volume bars in the bottom band and the candles above it', async () => {
    await mount();

    expect(priceScaleOptions).toContainEqual({
      scale: 'series',
      options: { scaleMargins: { top: 0.8, bottom: 0 } },
    });
    expect(priceScaleOptions).toContainEqual({
      scale: 'right',
      options: { scaleMargins: { top: 0.1, bottom: 0.25 } },
    });
  });
});

describe('pitha-price-chart VWAP line (issue #635)', () => {
  test('turns vwap=0 candles into whitespace so the scale is not stretched to 0', async () => {
    await mount([candle(baseMs, 0), candle(baseMs + MINUTE_MS, 101.5)]);

    const data = vwapSeriesMock().setData.mock.calls[0][0] as { time: number; value?: number }[];
    expect(data).toEqual([{ time: baseMs / 1000 }, { time: baseMs / 1000 + 60, value: 101.5 }]);
    expect(data.some((p) => p.value === 0)).toBe(false);
  });

  test('plots every candle whose vwap is positive as before', async () => {
    await mount([candle(baseMs, 100), candle(baseMs + MINUTE_MS, 101)]);

    expect(vwapSeriesMock().setData.mock.calls[0][0]).toEqual([
      { time: baseMs / 1000, value: 100 },
      { time: baseMs / 1000 + 60, value: 101 },
    ]);
  });
});

describe('pitha-price-chart Jev markers (issue #624)', () => {
  test('does not draw a marker for the first jev_update of a connection', async () => {
    await mount();

    jevUpdate('LONG', baseMs - 3 * 60 * MINUTE_MS);

    expect(candleSeries().setMarkers).not.toHaveBeenCalled();
  });

  test('draws a direction change on the 1-minute bar of the decision time, not the client clock', async () => {
    await mount();

    jevUpdate('SHORT', baseMs - 60 * MINUTE_MS);
    jevUpdate('LONG', baseMs + 30_000);

    const markers = candleSeries().setMarkers.mock.calls.at(-1)?.[0] as unknown as Record<
      string,
      unknown
    >[];
    expect(markers).toHaveLength(1);
    expect(markers[0]).toMatchObject({
      time: baseMs / 1000,
      position: 'belowBar',
      shape: 'arrowUp',
      text: 'LONG',
    });
  });

  test('ignores a repeat of the same direction and a null direction', async () => {
    await mount();

    jevUpdate('LONG', baseMs);
    jevUpdate('LONG', baseMs + MINUTE_MS);
    jevUpdate(null, baseMs + 2 * MINUTE_MS);

    expect(candleSeries().setMarkers).not.toHaveBeenCalled();
  });
});

describe('pitha-price-chart text alternative (issue #637)', () => {
  const label = (el: ChartElement) => {
    const container = el.shadowRoot?.querySelector('.pitha-price-chart-container');
    expect(container?.getAttribute('role')).toBe('img');
    return container?.getAttribute('aria-label') ?? '';
  };

  test('has role="img" and a non-empty name before any data arrives', async () => {
    const el = await mount();

    expect(label(el)).not.toBe('');
  });

  test('names the symbol, latest close and VWAP after the initial load', async () => {
    const el = await mount([candle(baseMs, 100, 101), candle(baseMs + MINUTE_MS, 0, 103)]);

    const name = label(el);
    expect(name).toContain('7203');
    expect(name).toContain('最新終値 103');
    expect(name).toContain('VWAP 100'); // the latest usable (non-zero) VWAP
  });

  test('follows ticks and the Jev direction', async () => {
    const el = await mount([candle(baseMs, 100, 101)]);

    send({ type: 'tick', price: 104, time: iso(baseMs + 10_000) });
    jevUpdate('SHORT', baseMs);
    await el.updateComplete;

    const name = label(el);
    expect(name).toContain('最新終値 104');
    expect(name).toContain('Jev方向 SHORT');
  });
});

describe('pitha-price-chart empty state (issue #687)', () => {
  const emptyNotice = (el: ChartElement) =>
    el.shadowRoot?.querySelector('.pitha-price-chart-empty') ?? null;

  test('shows a visible role="status" message when no candles come back', async () => {
    const el = await mount([]);

    const notice = emptyNotice(el);
    expect(notice?.getAttribute('role')).toBe('status');
    expect(notice?.textContent).toContain('表示できる足がありません');
  });

  test('shows no empty-state message while candles exist', async () => {
    const el = await mount([candle(baseMs, 100)]);

    expect(emptyNotice(el)).toBeNull();
  });

  test('clears the message once a tick builds the first bar', async () => {
    const el = await mount([]);
    expect(emptyNotice(el)).not.toBeNull();

    send({ type: 'tick', price: 104, time: iso(baseMs + 10_000) });
    await el.updateComplete;

    expect(emptyNotice(el)).toBeNull();
  });

  // Issue #999: same dashed-frame recipe as scanner-empty (design guide S4/§4).
  test('uses the dashed --border frame, --r-lg and 48/24 padding', async () => {
    const el = await mount([]);
    const css = (
      el.constructor as unknown as { elementStyles: { cssText: string }[] }
    ).elementStyles
      .map((s) => s.cssText)
      .join('');
    expect(css).toMatch(
      /\.pitha-price-chart-empty\s*\{[^}]*border:\s*1px dashed var\(--border\);[^}]*border-radius:\s*var\(--r-lg\);[^}]*padding:\s*3rem 1\.5rem;[^}]*text-align:\s*center;/,
    );
  });
});

describe('pitha-price-chart series colors (design guide §1/§8)', () => {
  test('candles are --ink (up filled, down hollow on --surface), VWAP --info and volume --mute at 50%: no --danger', async () => {
    await mount();

    expect(candleSeries().options).toMatchObject({
      upColor: '#050505',
      downColor: '#ffffff',
      borderUpColor: '#050505',
      borderDownColor: '#050505',
      wickUpColor: '#050505',
      wickDownColor: '#050505',
    });
    expect(vwapSeriesMock().options).toMatchObject({ color: '#4a5a6a' });
    expect(createdSeries[2].options).toMatchObject({ color: 'rgba(107, 107, 110, 0.5)' });
  });

  test('draws axis text in the numeric font and the grid in --border', async () => {
    const { createChartOptions } = await import('./chart-test-support');
    await mount();

    expect(createChartOptions[0]).toMatchObject({
      layout: { fontFamily: '"JetBrains Mono", ui-monospace, monospace' },
      grid: { horzLines: { color: '#e7e7e5' } },
    });
  });
});

// canvas colors are read once, so an OS Light/Dark switch must re-apply them (issue #964).
describe('pitha-price-chart OS theme switch (issue #964)', () => {
  let scheme: ReturnType<typeof installColorSchemeSwitch>;
  beforeEach(() => {
    scheme = installColorSchemeSwitch();
  });
  afterEach(() => {
    scheme.restore();
    document.documentElement.removeAttribute('style');
  });

  test('re-applies the chart, series and marker colors from the tokens now in force', async () => {
    await mount();
    jevUpdate('SHORT', baseMs - 60 * MINUTE_MS);
    jevUpdate('LONG', baseMs + 30_000);

    setDarkTokens();
    scheme.fire();

    expect(createdCharts[0].applyOptions).toHaveBeenCalledWith(
      expect.objectContaining({
        layout: expect.objectContaining({ textColor: '#8b8b8e' }),
        grid: { vertLines: { color: '#2a2a2a' }, horzLines: { color: '#2a2a2a' } },
      }),
    );
    expect(candleSeries().applyOptions).toHaveBeenCalledWith({
      upColor: '#ffffff',
      downColor: '#141414',
      borderUpColor: '#ffffff',
      borderDownColor: '#ffffff',
      wickUpColor: '#ffffff',
      wickDownColor: '#ffffff',
    });
    expect(vwapSeriesMock().applyOptions).toHaveBeenCalledWith({ color: '#9aabbc' });
    expect(createdSeries[2].applyOptions).toHaveBeenCalledWith({
      color: 'rgba(139, 139, 142, 0.5)',
    });
    const markers = candleSeries().setMarkers.mock.calls.at(-1)?.[0] as unknown as Record<
      string,
      unknown
    >[];
    expect(markers).toHaveLength(1);
    expect(markers[0]).toMatchObject({ text: 'LONG', color: '#8fafa0' });
  });

  test('stops listening once the element is removed', async () => {
    const el = await mount();
    expect(scheme.listeners()).toBe(1);

    el.remove();

    expect(scheme.listeners()).toBe(0);
  });
});
