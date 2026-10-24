// Rendering of `pitha-exit-alignment`: the label
// basis versus the actual exit table, its mismatch warnings, the per-decision
// label-versus-replay comparison, and the model's OOS EV beside the paper
// return. Split out of the component to keep both within the linterly limit.
import { html } from 'lit';
import { formatJstDateTime } from '../lib/jst-datetime';

// Mirrors docs/api/endpoints/huma-api-insights.md §GET /api/v1/calibration/exit-alignment
// (internal/web/handler/calibration.AlignmentAPIOutput).
export interface AlignmentRow {
  key: 'take_profit' | 'stop_loss' | 'horizon' | 'cost' | 'trailing_stop';
  item: string;
  label_text: string;
  exit_text: string;
  comparable: boolean;
  aligned: boolean;
}

export interface AlignmentWarning {
  code: string;
  message: string;
}

export interface Distribution {
  count: number;
  mean: number;
  std_dev: number;
  min: number;
  p5: number;
  p25: number;
  median: number;
  p75: number;
  p95: number;
  max: number;
}

export interface ReasonCount {
  reason: string;
  count: number;
}

export interface Comparison {
  from: string;
  to: string;
  horizon_minutes: number;
  replayed_count: number;
  skipped_count: number;
  label_net_bps: Distribution;
  replay_net_bps: Distribution;
  diff_bps: Distribution;
  label_tp_count: number;
  label_sl_count: number;
  label_timeout_count: number;
  replay_exits: ReasonCount[];
  paper_exits: ReasonCount[];
  not_evaluated_exits: string[];
  cost_long_bps: number;
  cost_short_bps: number;
}

export interface OOSPaper {
  model_version: string;
  oos_start: string;
  oos_end: string;
  oos_ev_bps: number | null;
  paper_trades: number;
  paper_mean_bps: number | null;
  diff_bps: number | null;
}

export interface AlignmentResponse {
  rows: AlignmentRow[];
  warnings: AlignmentWarning[];
  tolerance_bps: number;
  tolerance_minutes: number;
  comparison?: Comparison;
  oos_paper?: OOSPaper;
}

const NO_DATA = 'データ無し';

export const formatBps = (bps: number | null): string =>
  bps === null ? NO_DATA : `${bps >= 0 ? '+' : ''}${bps.toFixed(1)} bps`;

const reasonList = (counts: ReasonCount[]): string =>
  counts.length === 0 ? NO_DATA : counts.map((c) => `${c.reason} ${c.count}`).join(' / ');

export function renderWarnings(warnings: AlignmentWarning[]) {
  if (warnings.length === 0) {
    return html`<p data-testid="exit-alignment-ok">ラベル基準と実出口は許容範囲内で一致しています。</p>`;
  }
  return html`
    <ul class="warnings" data-testid="exit-alignment-warnings" role="alert">
      ${warnings.map((w) => html`<li data-code=${w.code}>${w.message}</li>`)}
    </ul>
  `;
}

export function renderTable(rows: AlignmentRow[]) {
  return html`
    <table data-testid="exit-alignment-table" aria-label="ラベル基準と実出口の対応">
      <thead>
        <tr><th scope="col">項目</th><th scope="col">ラベル基準</th><th scope="col">実出口</th><th scope="col">整合</th></tr>
      </thead>
      <tbody>
        ${rows.map(
          (r) => html`
            <tr data-testid="exit-alignment-row" data-key=${r.key}>
              <td>${r.item}</td>
              <td>${r.label_text}</td>
              <td>${r.exit_text}</td>
              <td>${r.comparable ? (r.aligned ? '一致' : '不一致') : '比較不可'}</td>
            </tr>
          `,
        )}
      </tbody>
    </table>
  `;
}

const distributionRow = (name: string, d: Distribution) => html`
  <tr>
    <td>${name}</td>
    <td>${formatBps(d.mean)}</td>
    <td>${d.std_dev.toFixed(1)}</td>
    <td>${formatBps(d.p5)}</td>
    <td>${formatBps(d.median)}</td>
    <td>${formatBps(d.p95)}</td>
  </tr>
`;

export function renderComparison(c: Comparison | undefined) {
  if (c === undefined) return '';
  if (c.replayed_count === 0) {
    return html`<p data-testid="exit-alignment-comparison-none">
      比較できる判断がありません（${NO_DATA}。再生不能 ${c.skipped_count} 件）
    </p>`;
  }
  return html`
    <h3>ラベル対実出口の再生（${formatJstDateTime(c.from)} 〜 ${formatJstDateTime(c.to)}、${c.replayed_count} 判断）</h3>
    <table data-testid="exit-alignment-comparison" aria-label="ラベルと実出口再生のnetリターン">
      <thead>
        <tr><th scope="col"></th><th scope="col">平均</th><th scope="col">標準偏差</th><th scope="col">5%</th><th scope="col">中央値</th><th scope="col">95%</th></tr>
      </thead>
      <tbody>
        ${distributionRow('ラベル net', c.label_net_bps)}
        ${distributionRow('実出口の再生 net', c.replay_net_bps)}
        ${distributionRow('差（再生−ラベル）', c.diff_bps)}
      </tbody>
    </table>
    <dl class="summary">
      <dt>ラベルの到達（TP / SL / timeout）</dt>
      <dd data-testid="exit-alignment-label-hits">${c.label_tp_count} / ${c.label_sl_count} / ${c.label_timeout_count}</dd>
      <dt>再生の決済理由</dt>
      <dd data-testid="exit-alignment-replay-exits">${reasonList(c.replay_exits)}</dd>
      <dt>Paper の決済理由</dt>
      <dd data-testid="exit-alignment-paper-exits">${reasonList(c.paper_exits)}</dd>
      <dt>再生できない出口</dt>
      <dd>${c.not_evaluated_exits.join(' / ')}</dd>
      <dt>再生時のコスト（LONG / SHORT）</dt>
      <dd>${c.cost_long_bps} / ${c.cost_short_bps} bps</dd>
    </dl>
  `;
}

export function renderOOSPaper(o: OOSPaper | undefined) {
  if (o === undefined) return '';
  return html`
    <h3>メタモデル OOS EV と Paper 実績（同じ OOS 期間）</h3>
    <dl class="summary" data-testid="exit-alignment-oos-paper">
      <dt>モデル / OOS 期間</dt>
      <dd>${o.model_version}（${formatJstDateTime(o.oos_start)} 〜 ${formatJstDateTime(o.oos_end)}）</dd>
      <dt>OOS EV（net）</dt>
      <dd data-testid="exit-alignment-oos-ev">${formatBps(o.oos_ev_bps)}</dd>
      <dt>Paper 実現リターン平均（コスト込み, ${o.paper_trades} 件）</dt>
      <dd data-testid="exit-alignment-paper-mean">${formatBps(o.paper_mean_bps)}</dd>
      <dt>差（Paper−OOS EV）</dt>
      <dd data-testid="exit-alignment-oos-diff">${formatBps(o.diff_bps)}</dd>
    </dl>
  `;
}
