import { describe, expect, test } from 'bun:test';
import { heatmapStyles } from './calibration-styles';
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
    // The horizon's notes sit in a help mark, readable through aria-describedby.
    const button = current?.querySelector('[data-help-mark-button]');
    const help = current?.querySelector(`#${button?.getAttribute('aria-describedby')}`);
    expect(help?.getAttribute('role')).toBe('tooltip');
    expect(help?.textContent).toContain('実現損益');
    expect(help?.textContent).toContain('5・10・15 分の合算');
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
    expect(text).toContain('3 件');
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

describe('horizon switch is the guide sub-nav (brand-guide #app-ui)', () => {
  // No track; the selected segment is a --surface-sel pill; --accent fill is for primary actions only.
  test('the selected segment is a --surface-sel pill, the track has no ground, nothing is --accent', () => {
    const css = heatmapStyles.cssText;
    expect(css).not.toMatch(/\.pitha-calibration-heatmap-horizons\s*\{[^}]*background:/);
    expect(css).toMatch(
      /button\[aria-pressed='true'\][^{]*\{[^}]*background:\s*var\(--surface-sel\)/,
    );
    expect(css).not.toMatch(/background:\s*var\(--accent\)\s*;/);
  });

  // WCAG 1.4.1: selection is not told by the face colour alone; SemiBold is allowed on buttons.
  test('the selected segment is also semibold', () => {
    expect(heatmapStyles.cssText).toMatch(
      /button\[aria-pressed='true'\][^{]*\{[^}]*font-weight:\s*600/,
    );
  });
});

describe('heatmap cell typography (brand-guide #app-ui, three steps)', () => {
  // Value rows are read, not annotations: 15. Only the band label is the 13px note.
  test('the cell value rows are 0.9375rem and only the range label is 0.8125rem', () => {
    const css = heatmapStyles.cssText;
    expect(css).toMatch(/\.pitha-calibration-heatmap-cell\s*\{[^}]*font-size:\s*0\.9375rem/);
    expect(css).toMatch(
      /\.pitha-calibration-heatmap-cell \.range\s*\{[^}]*font-size:\s*0\.8125rem/,
    );
    expect(css).not.toMatch(/font-size:\s*(0\.75|0\.875|1)rem/);
  });

  // The 15px step is 1.6 — spelled out so it does not inherit body's 1.8.
  test('the 0.9375rem horizon label and summary carry line-height 1.6', () => {
    const css = heatmapStyles.cssText;
    expect(css).toMatch(
      /\.pitha-calibration-heatmap-horizon\s*\{[^}]*font-size:\s*0\.9375rem;\s*line-height:\s*1\.6/,
    );
    expect(css).toMatch(
      /\.pitha-calibration-heatmap-summary\s*\{[^}]*font-size:\s*0\.9375rem;\s*line-height:\s*1\.6/,
    );
  });

  // Cards are 16px and have no 1px frame, an empty band included (flat --surface-sel ground).
  test('the cell is a --r-card card without a border', () => {
    const css = heatmapStyles.cssText;
    expect(css).toMatch(
      /\.pitha-calibration-heatmap-cell\s*\{[^}]*border-radius:\s*var\(--r-card\)/,
    );
    expect(css).not.toMatch(/\.pitha-calibration-heatmap-cell\s*\{[^}]*border:/);
  });
});
