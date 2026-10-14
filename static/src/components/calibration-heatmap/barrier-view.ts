// Triple-barrier EV (net) and tp/sl/timeout reach-rate rendering of
// `pitha-calibration-heatmap` (issue #767), split out of the component to
// keep it within the linterly per-file limit.
import { html } from 'lit';
import type { BarrierStats } from './calibration-view';

// formatBps renders a signed bps amount, e.g. "+12.5 bps" / "-7.0 bps".
export function formatBps(bps: number): string {
  return `${bps > 0 ? '+' : ''}${bps.toFixed(1)} bps`;
}

export const formatRate = (rate: number): string => `${(rate * 100).toFixed(1)}%`;

// renderBucketBarrier is the one-line barrier summary inside a band's heatmap
// cell: the band's EV (net) and its tp/sl/timeout reach rates; nothing for a
// band without triple-barrier labels.
export function renderBucketBarrier(stats: BarrierStats) {
  if (stats.sample_count === 0) return '';
  return html`<span class="bucket-barrier" data-testid="calibration-bucket-barrier">
    EV ${formatBps(stats.ev_net_bps)} / TP ${formatRate(stats.tp_rate)} SL ${formatRate(stats.sl_rate)} TO ${formatRate(stats.timeout_rate)}
  </span>`;
}

// renderBarrierSummary is the overall triple-barrier block: the EV (net, after
// trading costs) and the share of labels that reached each barrier first.
// Shown only once a response arrived (`stats` non-null).
export function renderBarrierSummary(stats: BarrierStats | null) {
  if (stats === null) return '';
  if (stats.sample_count === 0) {
    return html`<p class="pitha-calibration-heatmap-no-samples" data-testid="calibration-barrier-none">
      トリプルバリアのラベルなし（EV・到達率は未集計）
    </p>`;
  }
  return html`
    <dl class="pitha-calibration-heatmap-summary" data-testid="calibration-barrier-summary" aria-label="トリプルバリア（コスト控除後）">
      <dt>EV（net, 1判断あたり）</dt>
      <dd data-testid="calibration-barrier-ev">${formatBps(stats.ev_net_bps)}</dd>
      <dt>TP（利確）到達率</dt>
      <dd data-testid="calibration-barrier-tp">${formatRate(stats.tp_rate)} (${stats.tp_count})</dd>
      <dt>SL（損切り）到達率</dt>
      <dd data-testid="calibration-barrier-sl">${formatRate(stats.sl_rate)} (${stats.sl_count})</dd>
      <dt>timeout 率</dt>
      <dd data-testid="calibration-barrier-timeout">${formatRate(stats.timeout_rate)} (${stats.timeout_count})</dd>
      <dt>ラベル数</dt>
      <dd>${stats.sample_count}</dd>
    </dl>
  `;
}
