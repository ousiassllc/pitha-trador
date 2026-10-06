import { describe, expect, mock, spyOn, test } from 'bun:test';
import { heatmapColors, NO_DATA_COLOR } from './calibration-view';
import {
  bucket,
  flush,
  type HeatmapElement,
  installHeatmapHarness,
  mount,
  response,
} from './heatmap-test-support';

// Dynamic: a static import would load the component before the mock exists.
await import('./pitha-calibration-heatmap');

installHeatmapHarness();

describe('pitha-calibration-heatmap', () => {
  test('loads calibration metrics from calibration-url and renders a heatmap cell per bucket', async () => {
    const { el, fetchMock } = await mount(
      response({
        buckets: [
          bucket({ range: '0.50-0.60', direction_accuracy: 0.51, avg_future_return_pct: -0.05 }),
          bucket({ range: '0.90-1.00', direction_accuracy: 0.78, avg_future_return_pct: 0.39 }),
        ],
      }),
    );

    const cells = el.shadowRoot?.querySelectorAll('[data-testid="calibration-heatmap-cell"]');
    expect(cells?.length).toBe(2);
    expect(el.shadowRoot?.textContent).toContain('0.50-0.60');
    expect(el.shadowRoot?.textContent).toContain('0.90-1.00');
    expect(el.shadowRoot?.textContent).toContain('51.0%');
    expect(el.shadowRoot?.textContent).toContain('0.190'); // brier_score
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/calibration');
  });

  test('colors each cell by its direction_accuracy without a style attribute binding', async () => {
    const { el } = await mount(
      response({
        buckets: [
          bucket({ range: '0.50-0.60', direction_accuracy: 0.1 }),
          bucket({ range: '0.90-1.00', direction_accuracy: 0.9 }),
        ],
      }),
    );

    const cells = [
      ...(el.shadowRoot?.querySelectorAll<HTMLElement>(
        '[data-testid="calibration-heatmap-cell"]',
      ) ?? []),
    ];
    expect(cells.map((c) => c.style.backgroundColor)).toEqual([
      heatmapColors(0.1).background,
      heatmapColors(0.9).background,
    ]);
    expect(cells.map((c) => c.style.color)).toEqual([
      heatmapColors(0.1).color,
      heatmapColors(0.9).color,
    ]);
    expect(heatmapColors(0.1).background).not.toBe(heatmapColors(0.9).background);
  });

  test('renders per-bucket PnL and the per-direction average return', async () => {
    const { el } = await mount(
      response({
        buckets: [
          bucket({ trade_count: 3, total_pnl: 1200, avg_pnl_pct: 0.4, avg_confidence: 0.76 }),
        ],
        by_direction: [
          {
            direction: 'LONG',
            sample_count: 10,
            direction_accuracy: 0.6,
            avg_future_return_pct: 0.12,
          },
          {
            direction: 'SHORT',
            sample_count: 4,
            direction_accuracy: 0.5,
            avg_future_return_pct: -0.3,
          },
        ],
      }),
    );

    const text = el.shadowRoot?.textContent ?? '';
    expect(text).toContain('3 trades');
    expect(text).toContain('+1,200 JPY');
    expect(text).toContain('conf 0.76');
    const rows = el.shadowRoot?.querySelectorAll('[data-testid="calibration-direction-row"]');
    expect(rows?.length).toBe(2);
    expect(rows?.[1]?.textContent).toContain('SHORT');
    expect(rows?.[1]?.textContent).toContain('-0.30%');
  });

  test('renders a band without samples as neutral "データなし" instead of a 0% red cell', async () => {
    const empty = bucket({
      range: '0.50-0.60',
      sample_count: 0,
      direction_accuracy: 0,
      avg_future_return_pct: 0,
      avg_confidence: 0,
    });
    const { el } = await mount(
      response({ buckets: [empty, bucket({ range: '0.90-1.00', sample_count: 7 })] }),
    );

    const cells = [
      ...(el.shadowRoot?.querySelectorAll<HTMLElement>(
        '[data-testid="calibration-heatmap-cell"]',
      ) ?? []),
    ];
    const [emptyCell, filledCell] = cells;
    expect(emptyCell?.style.backgroundColor).toBe(NO_DATA_COLOR);
    expect(emptyCell?.style.backgroundColor).not.toBe(heatmapColors(0).background);
    expect(emptyCell?.textContent).toContain('データなし');
    expect(emptyCell?.textContent).toContain('n=0');
    expect(emptyCell?.textContent).not.toContain('0.0%');
    expect(emptyCell?.textContent).not.toContain('conf');
    expect(filledCell?.textContent).toContain('n=7');
    expect(filledCell?.textContent).toContain('63.0%');
    expect(filledCell?.textContent).not.toContain('データなし');
  });

  test('hides Brier/LogLoss/ECE and says "サンプルなし" when there are no labeled samples', async () => {
    const { el } = await mount(
      response({
        buckets: [bucket({ sample_count: 0, direction_accuracy: 0 })],
        by_direction: [
          { direction: 'LONG', sample_count: 0, direction_accuracy: 0, avg_future_return_pct: 0 },
          { direction: 'SHORT', sample_count: 0, direction_accuracy: 0, avg_future_return_pct: 0 },
        ],
        brier_score: 0,
        log_loss: 0,
        expected_calibration_error: 0,
      }),
    );

    const text = el.shadowRoot?.textContent ?? '';
    expect(el.shadowRoot?.querySelector('[data-testid="calibration-no-samples"]')).not.toBeNull();
    expect(text).not.toContain('Brier Score');
    expect(text).not.toContain('0.000');
    const rows = el.shadowRoot?.querySelectorAll('[data-testid="calibration-direction-row"]');
    expect(rows?.[0]?.textContent).toContain('0');
    expect(rows?.[0]?.textContent).not.toContain('0.0%');
    expect(rows?.[0]?.textContent).not.toContain('0.00%');
  });

  test('shows the summary scores and no "サンプルなし" notice when samples exist', async () => {
    const { el } = await mount(response());
    expect(el.shadowRoot?.querySelector('[data-testid="calibration-no-samples"]')).toBeNull();
    expect(el.shadowRoot?.textContent).toContain('Brier Score');
  });

  test('the refresh button re-fetches calibration-url without opening a WebSocket', async () => {
    const originalWebSocket = globalThis.WebSocket;
    // @ts-expect-error - deliberately undefined to prove no WebSocket is used
    globalThis.WebSocket = undefined;
    try {
      const { el, fetchMock } = await mount(response());
      expect(fetchMock.mock.calls.length).toBe(1);

      const button = el.shadowRoot?.querySelector('button');
      button?.click();
      await flush(el);

      expect(fetchMock.mock.calls.length).toBe(2);
    } finally {
      globalThis.WebSocket = originalWebSocket;
    }
  });

  test('renders an error message when the fetch fails', async () => {
    const fetchMock = mock(() => Promise.resolve(new Response('fail', { status: 500 })));
    globalThis.fetch = fetchMock as unknown as typeof fetch;

    const el = document.createElement('pitha-calibration-heatmap') as HeatmapElement;
    el.setAttribute('calibration-url', '/api/v1/calibration');
    document.body.appendChild(el);
    await flush(el);

    expect(el.shadowRoot?.querySelector('[role="alert"]')).not.toBeNull();
  });

  // fetch('') would GET the current page and fail with a JSON parse error.
  test('logs an error and makes no request when calibration-url is not injected', async () => {
    const errorSpy = spyOn(console, 'error').mockImplementation(() => {});
    try {
      const fetchMock = mock(() => Promise.resolve(new Response('{}')));
      globalThis.fetch = fetchMock as unknown as typeof fetch;

      const el = document.createElement('pitha-calibration-heatmap') as HeatmapElement; // no URL attribute
      document.body.appendChild(el);
      await flush(el);

      expect(fetchMock).not.toHaveBeenCalled();
      const unsetLogs = errorSpy.mock.calls.filter(
        ([entry]) =>
          (entry as { message?: string }).message ===
          'pitha-calibration-heatmap: calibration-url is not set',
      );
      expect(unsetLogs).toHaveLength(1);
      expect(el.shadowRoot?.querySelector('[role="alert"]')).toBeNull();
    } finally {
      errorSpy.mockRestore();
    }
  });
});
