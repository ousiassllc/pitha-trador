import { describe, expect, test } from 'bun:test';
import { calibrationUrlFor, horizonLabel } from './calibration-view';
import {
  bucket,
  flush,
  type HeatmapElement,
  installHeatmapHarness,
  mount,
  response,
} from './heatmap-test-support';

await import('./pitha-calibration-heatmap');

installHeatmapHarness();

const requestedUrl = (fetchMock: { mock: { calls: unknown[][] } }, call: number): string =>
  String(fetchMock.mock.calls[call]?.[0]);

const click = (el: HeatmapElement, testId: string) =>
  el.shadowRoot?.querySelector<HTMLButtonElement>(`[data-testid="${testId}"]`)?.click();

describe('calibration horizon helpers', () => {
  test('calibrationUrlFor appends the horizon query', () => {
    expect(calibrationUrlFor('/api/v1/calibration', '5')).toBe('/api/v1/calibration?horizon=5');
    expect(calibrationUrlFor('/api/v1/calibration?x=1', 'all')).toBe(
      '/api/v1/calibration?x=1&horizon=all',
    );
  });

  test('horizonLabel names minutes and the combined view', () => {
    expect(horizonLabel('10')).toBe('10分');
    expect(horizonLabel('all')).toBe('全体');
  });
});

describe('pitha-calibration-heatmap horizon switch', () => {
  test('loads the default horizon (all) and says so on screen', async () => {
    const { el, fetchMock } = await mount(response({ horizon: 'all' }));

    expect(requestedUrl(fetchMock, 0)).toBe('/api/v1/calibration?horizon=all');
    const current = el.shadowRoot?.querySelector('[data-testid="calibration-horizon-current"]');
    expect(current?.textContent).toContain('集計ホライズン');
    expect(current?.textContent).toContain('全体');
    expect(current?.textContent).toContain('実現PnL');
    expect(
      el.shadowRoot
        ?.querySelector('[data-testid="calibration-horizon-all"]')
        ?.getAttribute('aria-pressed'),
    ).toBe('true');
  });

  test('selecting a horizon re-fetches with it and re-renders that horizon only', async () => {
    const { el, fetchMock } = await mount(response({ horizon: 'all' }));
    fetchMock.mockImplementation(() =>
      Promise.resolve(
        new Response(
          JSON.stringify(
            response({
              horizon: '5',
              buckets: [bucket({ range: '0.90-1.00', sample_count: 3, direction_accuracy: 0.9 })],
              brier_score: 0.123,
            }),
          ),
        ),
      ),
    );

    click(el, 'calibration-horizon-5');
    await flush(el);

    expect(fetchMock.mock.calls.length).toBe(2);
    expect(requestedUrl(fetchMock, 1)).toBe('/api/v1/calibration?horizon=5');
    const text = el.shadowRoot?.textContent ?? '';
    expect(
      el.shadowRoot?.querySelector('[data-testid="calibration-horizon-current"]')?.textContent,
    ).toContain('5分');
    expect(text).toContain('n=3');
    expect(text).toContain('0.123');
    expect(
      el.shadowRoot
        ?.querySelector('[data-testid="calibration-horizon-5"]')
        ?.getAttribute('aria-pressed'),
    ).toBe('true');
    expect(
      el.shadowRoot
        ?.querySelector('[data-testid="calibration-horizon-all"]')
        ?.getAttribute('aria-pressed'),
    ).toBe('false');
  });

  test('selecting the active horizon does not re-fetch, and refresh keeps the selection', async () => {
    const { el, fetchMock } = await mount(response());
    click(el, 'calibration-horizon-all');
    await flush(el);
    expect(fetchMock.mock.calls.length).toBe(1);

    fetchMock.mockImplementation(() =>
      Promise.resolve(new Response(JSON.stringify(response({ horizon: '15' })))),
    );
    click(el, 'calibration-horizon-15');
    await flush(el);
    el.shadowRoot?.querySelector('button')?.click();
    await flush(el);

    expect(fetchMock.mock.calls.length).toBe(3);
    expect(requestedUrl(fetchMock, 2)).toBe('/api/v1/calibration?horizon=15');
  });

  test('a response that arrives after a newer selection is ignored', async () => {
    const { el, fetchMock } = await mount(response());
    const pending: ((r: Response) => void)[] = [];
    fetchMock.mockImplementation(() => new Promise<Response>((resolve) => pending.push(resolve)));

    click(el, 'calibration-horizon-5');
    click(el, 'calibration-horizon-10');
    // The newer (10m) request resolves first, then the stale 5m one.
    pending[1]?.(new Response(JSON.stringify(response({ horizon: '10' }))));
    await flush(el);
    pending[0]?.(new Response(JSON.stringify(response({ horizon: '5' }))));
    await flush(el);

    expect(
      el.shadowRoot?.querySelector('[data-testid="calibration-horizon-current"]')?.textContent,
    ).toContain('10分');
  });
});
