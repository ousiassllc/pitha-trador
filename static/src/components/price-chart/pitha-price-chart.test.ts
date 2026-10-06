import { describe, expect, test } from 'bun:test';
import {
  createChartOptions,
  createdCharts,
  createdSeries,
  FakeWebSocket,
  installChartHarness,
} from './chart-test-support';

const { PithaPriceChart } = await import('./pitha-price-chart');

installChartHarness();

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

    type Fmt = (t: number, type?: number) => string | null;
    const options = createChartOptions[0] as {
      localization: { timeFormatter: Fmt };
      timeScale: { timeVisible: boolean; tickMarkFormatter: Fmt };
    };
    const tseOpen = Date.parse('2026-10-05T00:00:00Z') / 1000;
    expect(options.timeScale.timeVisible).toBe(true);
    expect(options.localization.timeFormatter(tseOpen)).toBe('2026-10-05 09:00');
    expect(options.timeScale.tickMarkFormatter(tseOpen, 3)).toBe('09:00');
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

  // Removing the element tore the chart and socket down, but firstUpdated
  // does not run again, so re-inserting it left a blank chart (issue #523).
  test('rebuilds the chart, candles and WebSocket when re-attached to the DOM', async () => {
    const el = document.createElement('pitha-price-chart') as InstanceType<typeof PithaPriceChart>;
    el.setAttribute('candles-url', '/api/v1/symbols/7203/candles');
    el.setAttribute('ws-url', '/ws/symbols/7203');
    document.body.appendChild(el);
    await el.updateComplete;

    el.remove();
    expect(createdCharts[0].remove).toHaveBeenCalledTimes(1);
    expect(FakeWebSocket.instances[0].closed).toBe(true);

    document.body.appendChild(el);
    await el.updateComplete;

    expect(createChartOptions).toHaveLength(2);
    expect(globalThis.fetch).toHaveBeenCalledTimes(2);
    expect(FakeWebSocket.instances).toHaveLength(2);
    expect(FakeWebSocket.instances[1].closed).toBe(false);
  });

  // Markers and lastDirection of the previous symbol used to survive a URL
  // change, so its arrows stayed on the new chart and the new symbol's first
  // jev_update in the same direction was swallowed (issue #562). The first
  // jev_update of a connection only seeds the direction (issue #624), so each
  // symbol needs a seed plus a flip to draw one marker.
  test('clears the previous symbol markers and last direction when the URLs change', async () => {
    const el = document.createElement('pitha-price-chart') as InstanceType<typeof PithaPriceChart>;
    el.setAttribute('candles-url', '/api/v1/symbols/7203/candles');
    el.setAttribute('ws-url', '/ws/symbols/7203');
    document.body.appendChild(el);
    await el.updateComplete;
    const candle = createdSeries[0];
    const jev = (direction: string) => ({
      data: JSON.stringify({ type: 'jev_update', direction, time: '2026-09-29T01:00:30Z' }),
    });

    FakeWebSocket.instances[0].emit('message', jev('SHORT'));
    FakeWebSocket.instances[0].emit('message', jev('LONG'));
    expect(candle.setMarkers.mock.calls.at(-1)?.[0]).toHaveLength(1);

    el.setAttribute('candles-url', '/api/v1/symbols/6758/candles');
    el.setAttribute('ws-url', '/ws/symbols/6758');
    await el.updateComplete;
    expect(candle.setMarkers.mock.calls.at(-1)?.[0]).toEqual([]);

    FakeWebSocket.instances[1].emit('message', jev('SHORT'));
    FakeWebSocket.instances[1].emit('message', jev('LONG'));
    expect(candle.setMarkers.mock.calls.at(-1)?.[0]).toHaveLength(1);
  });

  test('does not keep the closed WsClient when ws-url becomes empty', async () => {
    const el = document.createElement('pitha-price-chart') as InstanceType<typeof PithaPriceChart>;
    el.setAttribute('candles-url', '/api/v1/symbols/7203/candles');
    el.setAttribute('ws-url', '/ws/symbols/7203');
    document.body.appendChild(el);
    await el.updateComplete;

    el.setAttribute('ws-url', '');
    await el.updateComplete;

    expect(FakeWebSocket.instances[0].closed).toBe(true);
    expect(FakeWebSocket.instances).toHaveLength(1);
    expect((el as unknown as { wsClient: unknown }).wsClient).toBeNull();
  });
});
