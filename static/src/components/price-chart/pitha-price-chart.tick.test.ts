import { describe, expect, mock, test } from 'bun:test';
import { createdSeries, FakeWebSocket, installChartHarness } from './chart-test-support';

const { PithaPriceChart } = await import('./pitha-price-chart');

installChartHarness();

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

  // The bar is placed by the snapshot time carried in the message, not the
  // client clock (issue #557).
  function tick(price: number, snapshotMs: number) {
    const time = new Date(snapshotMs).toISOString();
    FakeWebSocket.instances[0].emit('message', {
      data: JSON.stringify({ type: 'tick', price, time }),
    });
  }

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
