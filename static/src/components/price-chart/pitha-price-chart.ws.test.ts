import { afterEach, describe, expect, mock, spyOn, test } from 'bun:test';
import { FakeWebSocket, installChartHarness } from './chart-test-support';

const { PithaPriceChart } = await import('./pitha-price-chart');

installChartHarness();

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
