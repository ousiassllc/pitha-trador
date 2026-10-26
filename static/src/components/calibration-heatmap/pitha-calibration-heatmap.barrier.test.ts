import { describe, expect, test } from 'bun:test';
import { barrier, bucket, installHeatmapHarness, mount, response } from './heatmap-test-support';

// Dynamic: a static import would load the component before the mock exists.
await import('./pitha-calibration-heatmap');

installHeatmapHarness();

const text = (el: HTMLElement, testid: string): string | undefined =>
  el.shadowRoot?.querySelector(`[data-testid="${testid}"]`)?.textContent?.trim();

// Issue #767: the screen shows the EV (net) and the tp/sl/timeout reach rates.
describe('pitha-calibration-heatmap triple barrier', () => {
  test('renders the overall EV (net) and the per-barrier reach rates', async () => {
    const { el } = await mount(
      response({
        triple_barrier: barrier({
          sample_count: 10,
          tp_count: 4,
          sl_count: 3,
          timeout_count: 3,
          tp_rate: 0.4,
          sl_rate: 0.3,
          timeout_rate: 0.3,
          ev_net_bps: 12.5,
        }),
      }),
    );

    expect(text(el, 'calibration-barrier-ev')).toBe('+12.5 bps');
    expect(text(el, 'calibration-barrier-tp')).toBe('40.0% (4)');
    expect(text(el, 'calibration-barrier-sl')).toBe('30.0% (3)');
    expect(text(el, 'calibration-barrier-timeout')).toBe('30.0% (3)');
    expect(el.shadowRoot?.querySelector('[data-testid="calibration-barrier-none"]')).toBeNull();
  });

  test('renders a negative EV with its sign', async () => {
    const { el } = await mount(
      response({
        triple_barrier: barrier({ sample_count: 2, sl_count: 2, sl_rate: 1, ev_net_bps: -30 }),
      }),
    );

    expect(text(el, 'calibration-barrier-ev')).toBe('-30.0 bps');
  });

  test('says there are no triple-barrier labels instead of showing a 0 EV', async () => {
    const { el } = await mount(response());

    expect(el.shadowRoot?.querySelector('[data-testid="calibration-barrier-summary"]')).toBeNull();
    expect(text(el, 'calibration-barrier-none')).toContain('トリプルバリアのラベルなし');
  });

  test('shows each band its own EV and rates, and nothing for a band without labels', async () => {
    const { el } = await mount(
      response({
        buckets: [
          bucket({
            range: '0.50-0.60',
            triple_barrier: barrier({
              sample_count: 4,
              tp_rate: 0.25,
              sl_rate: 0.5,
              timeout_rate: 0.25,
              ev_net_bps: -7.5,
            }),
          }),
          bucket({ range: '0.60-0.70' }),
        ],
      }),
    );

    const cells = el.shadowRoot?.querySelectorAll('[data-testid="calibration-heatmap-cell"]');
    const first = cells?.[0]?.querySelector('[data-testid="calibration-bucket-barrier"]');
    expect(first?.textContent).toContain('期待値 -7.5 bps');
    expect(first?.textContent).toContain('利確 25.0%');
    expect(first?.textContent).toContain('損切り 50.0%');
    expect(first?.textContent).toContain('時間切れ 25.0%');
    expect(cells?.[1]?.querySelector('[data-testid="calibration-bucket-barrier"]')).toBeNull();
  });
});
