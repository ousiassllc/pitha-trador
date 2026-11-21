import { describe, expect, mock, test } from 'bun:test';
import { createdSeries, FakeWebSocket, installChartHarness } from './chart-test-support';
import type { Candle } from './chart-types';

const { PithaPriceChart } = await import('./pitha-price-chart');

installChartHarness();

type ChartElement = InstanceType<typeof PithaPriceChart>;

const candle = (close: number): Candle => ({
  time: '2026-09-29T01:00:00Z',
  open: close,
  high: close,
  low: close,
  close,
  volume: 100,
  vwap: close,
});

const body = (symbol: string, close: number) =>
  new Response(JSON.stringify({ symbol, candles: [candle(close)] }));

const label = (el: ChartElement): string =>
  el.shadowRoot?.querySelector('.pitha-price-chart-container')?.getAttribute('aria-label') ?? '';

// A fetch whose responses the test releases itself, in any order.
function deferredFetch() {
  const pending: ((response: Response) => void)[] = [];
  globalThis.fetch = mock(
    () => new Promise<Response>((resolve) => pending.push(resolve)),
  ) as unknown as typeof fetch;
  return pending;
}

function create(): ChartElement {
  const el = document.createElement('pitha-price-chart') as ChartElement;
  el.setAttribute('candles-url', '/api/v1/symbols/7203/candles');
  el.setAttribute('ws-url', '/ws/symbols/7203');
  return el;
}

// Settles the fire-and-forget fetch chain (one macrotask) and Lit's update.
async function settle(el: ChartElement): Promise<void> {
  await new Promise<void>((resolve) => setImmediate(resolve));
  await el.updateComplete;
}

describe('pitha-price-chart stale initial loads', () => {
  // The slow 7203 response used to land after 6758's and put the previous
  // symbol's candles and name back on the chart (issue #1165).
  test('drops a slow response of the previous candles-url once the URL changed', async () => {
    const pending = deferredFetch();
    const el = create();
    document.body.appendChild(el);
    await el.updateComplete;

    el.setAttribute('candles-url', '/api/v1/symbols/6758/candles');
    await el.updateComplete;
    expect(pending).toHaveLength(2);

    pending[1](body('6758', 2000));
    await settle(el);
    expect(label(el)).toContain('6758');

    pending[0](body('7203', 3000));
    await settle(el);
    expect(label(el)).toContain('6758');
    expect(label(el)).not.toContain('7203');
    const candles = createdSeries[0];
    expect(candles.setData).toHaveBeenCalledTimes(1);
  });

  test('drops the error of a superseded load', async () => {
    const pending = deferredFetch();
    const el = create();
    document.body.appendChild(el);
    await el.updateComplete;

    el.setAttribute('candles-url', '/api/v1/symbols/6758/candles');
    await el.updateComplete;
    pending[1](body('6758', 2000));
    await settle(el);

    pending[0](new Response('{}', { status: 500 }));
    await settle(el);
    expect(el.shadowRoot?.querySelector('.pitha-price-chart-error')).toBeNull();
  });

  test('drops a response that arrives after the element was detached', async () => {
    const pending = deferredFetch();
    const el = create();
    document.body.appendChild(el);
    await el.updateComplete;

    el.remove();
    pending[0](new Response('{}', { status: 500 }));
    await settle(el);

    expect(el.shadowRoot?.querySelector('.pitha-price-chart-error')).toBeNull();
  });

  // The ws-url change closes the socket, but the candles still belong to the same URL.
  test('keeps the load in flight when only the ws-url changes', async () => {
    const pending = deferredFetch();
    const el = create();
    document.body.appendChild(el);
    await el.updateComplete;

    el.setAttribute('ws-url', '/ws/symbols/7203-b');
    await el.updateComplete;
    expect(FakeWebSocket.instances).toHaveLength(2);

    pending[0](body('7203', 3000));
    await settle(el);
    expect(label(el)).toContain('7203');
  });
});
