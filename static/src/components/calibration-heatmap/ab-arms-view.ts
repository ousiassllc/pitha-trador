// A/B comparison block of `pitha-calibration-heatmap` (issue #755, parent #751):
// the paper comparison of the fixed momentum arm and the regime-switching arm,
// each arm's win rate and EV after costs over its closed positions, overall and
// per market regime. Split out of the component to keep it within the linterly
// per-file limit.
import { html } from 'lit';
import { formatBps, formatRate } from './barrier-view';
import { formatYen } from './calibration-view';

// Mirrors docs/api/endpoints.md §GET /api/v1/calibration's `ab_arms[].by_regime[]`
// item shape (internal/web/handler/calibration.abRegimeOutput).
export interface ABRegimeStats {
  regime: 'trend' | 'mean_revert' | 'unknown';
  trade_count: number;
  win_rate: number;
  ev_net_bps: number;
  total_pnl: number;
}

// Mirrors docs/api/endpoints.md §GET /api/v1/calibration's `ab_arms[]` item
// shape (internal/web/handler/calibration.abArmOutput).
export interface ABArm {
  arm: 'momentum' | 'regime';
  trade_count: number;
  win_rate: number;
  ev_net_bps: number;
  total_pnl: number;
  by_regime: ABRegimeStats[];
}

const ARM_LABELS: Record<ABArm['arm'], string> = {
  momentum: 'モメンタム固定',
  regime: 'レジーム切替',
};

const REGIME_LABELS: Record<ABRegimeStats['regime'], string> = {
  trend: 'トレンド',
  mean_revert: '平均回帰',
  unknown: '不明',
};

// figures are the cells shared by an arm row and its regime rows; a group
// without closed positions has no win rate / EV to show.
function figures(s: {
  trade_count: number;
  win_rate: number;
  ev_net_bps: number;
  total_pnl: number;
}) {
  if (s.trade_count === 0) return html`<td>0</td><td>—</td><td>—</td><td>—</td>`;
  return html`
    <td>${s.trade_count}</td>
    <td data-testid="calibration-ab-win">${formatRate(s.win_rate)}</td>
    <td data-testid="calibration-ab-ev">${formatBps(s.ev_net_bps)}</td>
    <td>${formatYen(s.total_pnl)}</td>
  `;
}

// renderABArms is the A/B comparison table: one row per arm followed by its
// per-regime rows. `arms` is undefined until a response arrived or when the
// source reports no comparison (nothing shown).
export function renderABArms(arms: ABArm[] | undefined) {
  if (arms === undefined || arms.length === 0) return '';
  return html`
    <table
      class="pitha-calibration-heatmap-directions"
      data-testid="calibration-ab-table"
      aria-label="A/B比較（モメンタム固定 vs レジーム切替、コスト控除後）"
    >
      <thead>
        <tr>
          <th scope="col">アーム / レジーム</th>
          <th scope="col">決済済み件数</th>
          <th scope="col">勝率</th>
          <th scope="col">EV（net, 1件あたり）</th>
          <th scope="col">損益合計</th>
        </tr>
      </thead>
      <tbody>
        ${arms.map(
          (a) => html`
            <tr data-testid="calibration-ab-arm" data-arm=${a.arm}>
              <th scope="row">${ARM_LABELS[a.arm]}</th>
              ${figures(a)}
            </tr>
            ${a.by_regime.map(
              (r) => html`
                <tr data-testid="calibration-ab-regime" data-arm=${a.arm} data-regime=${r.regime}>
                  <th scope="row">└ ${REGIME_LABELS[r.regime]}</th>
                  ${figures(r)}
                </tr>
              `,
            )}
          `,
        )}
      </tbody>
    </table>
  `;
}
