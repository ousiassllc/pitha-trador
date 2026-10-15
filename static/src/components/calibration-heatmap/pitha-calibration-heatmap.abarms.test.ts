import { describe, expect, test } from 'bun:test';
import type { ABArm } from './ab-arms-view';
import { installHeatmapHarness, mount, response } from './heatmap-test-support';

// Dynamic: a static import would load the component before the mock exists.
await import('./pitha-calibration-heatmap');

installHeatmapHarness();

const normalize = (s: string | null | undefined): string | undefined =>
  s?.replace(/\s+/g, ' ').trim();

const arms = (): ABArm[] => [
  {
    arm: 'momentum',
    trade_count: 10,
    win_rate: 0.4,
    ev_net_bps: -1.5,
    total_pnl: -3200,
    by_regime: [
      { regime: 'trend', trade_count: 6, win_rate: 0.5, ev_net_bps: 2, total_pnl: 1000 },
      { regime: 'mean_revert', trade_count: 4, win_rate: 0.25, ev_net_bps: -6, total_pnl: -4200 },
    ],
  },
  { arm: 'regime', trade_count: 0, win_rate: 0, ev_net_bps: 0, total_pnl: 0, by_regime: [] },
];

// Issue #755: the screen shows each A/B arm's cost-adjusted EV and win rate.
describe('pitha-calibration-heatmap A/B arms', () => {
  test('renders one row per arm with its win rate, EV and total PnL, then its regime rows', async () => {
    const { el } = await mount(response({ ab_arms: arms() }));

    const rows = [...(el.shadowRoot?.querySelectorAll('[data-testid="calibration-ab-arm"]') ?? [])];
    expect(rows.map((r) => r.getAttribute('data-arm'))).toEqual(['momentum', 'regime']);
    expect(normalize(rows[0]?.textContent)).toBe('モメンタム固定 10 40.0% -1.5 bps -3,200 JPY');
    // An arm without closed positions has no win rate / EV to show.
    expect(normalize(rows[1]?.textContent)).toBe('レジーム切替 0———');

    const regimeRows = [
      ...(el.shadowRoot?.querySelectorAll('[data-testid="calibration-ab-regime"]') ?? []),
    ];
    expect(regimeRows.map((r) => r.getAttribute('data-regime'))).toEqual(['trend', 'mean_revert']);
    expect(normalize(regimeRows[1]?.textContent)).toBe('└ 平均回帰 4 25.0% -6.0 bps -4,200 JPY');
  });

  test('renders nothing when the response has no ab_arms', async () => {
    const { el } = await mount(response());

    expect(el.shadowRoot?.querySelector('[data-testid="calibration-ab-table"]')).toBeNull();
  });
});
