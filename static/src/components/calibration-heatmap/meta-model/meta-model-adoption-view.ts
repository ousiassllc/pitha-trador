// Adoption block of the meta-labeling model view (issue #780): how the newest
// model was judged - OOS counts, the confidence intervals of its AUC and EV,
// the bar it was held to, the comparison with the model in force, a blocking
// change-rate limit and the forward (holdout) evaluation. Split out of
// meta-model-view.ts to keep it within the linterly per-file limit.
import { html } from 'lit';
import { formatBps } from '../barrier-view';

export interface Interval {
  low: number;
  high: number;
}

// Mirrors docs/api/endpoints.md §GET /api/v1/calibration's `meta_model`
// adoption fields (internal/web/handler/calibration/metamodel_adoption.go).
export interface MetaModelAdoption {
  oos_takes: number;
  oos_days: number;
  oos_ev_ci?: Interval;
  // The verdict on the OOS EV; min_detectable_edge_bps is the 判定不能 line.
  oos_ev_verdict: 'no_data' | 'undetermined' | 'positive' | 'negative' | 'not_significant';
  min_detectable_edge_bps: number;
  oos_auc_ci?: Interval;
  criteria?: {
    min_oos_samples: number;
    min_takes: number;
    min_oos_days: number;
    min_ev_lower_bps: number;
    min_auc_lower: number;
    alpha: number;
    resamples: number;
  };
  comparison?: {
    active_version: string;
    comparable: boolean;
    note: string;
    paired_samples: number;
    candidate_bps: number;
    active_bps: number;
    diff_bps: number;
    diff_ci?: Interval;
  };
  gate?: { reason: string; detail: string; until: string };
  forward?: {
    evaluated_at: string;
    from: string;
    samples: number;
    takes: number;
    days: number;
    ev_bps: number | null;
    ev_ci?: Interval;
    degraded: boolean;
  };
}

const bpsInterval = (iv: Interval | undefined): string =>
  iv === undefined ? '—' : `${formatBps(iv.low)} 〜 ${formatBps(iv.high)}`;

const aucInterval = (iv: Interval | undefined): string =>
  iv === undefined ? '—' : `${iv.low.toFixed(3)} 〜 ${iv.high.toFixed(3)}`;

// evVerdictText is the verdict on the OOS EV (issue #780): データ無し without
// takes, 判定不能 for an edge below what the sample can detect that the
// interval cannot separate from 0. The cut-off is never presented as proof of
// safety, and the figures are not a promise of any return.
function evVerdictText(a: MetaModelAdoption): string {
  const cutoff =
    '足切りであり、安全の証明ではありません。数字は仮定つきで、リターンの保証ではありません。';
  switch (a.oos_ev_verdict) {
    case 'no_data':
      return 'データ無し';
    case 'positive':
      return `正のエッジ（信頼区間の下限が 0 を超過）。${cutoff}`;
    case 'negative':
      return `負のエッジ（信頼区間の上限が 0 未満）。${cutoff}`;
    case 'not_significant':
      return `有意なエッジなし（信頼区間が 0 をまたぐ）。${cutoff}`;
    default:
      return `判定不能（約${a.min_detectable_edge_bps} bps 未満のエッジは検出力不足で見分けられません）。${cutoff}`;
  }
}

// confidenceLabel names the interval level: alpha is the one-sided tail.
const confidenceLabel = (alpha: number): string => `${((1 - 2 * alpha) * 100).toFixed(0)}%信頼区間`;

function renderCriteria(a: MetaModelAdoption) {
  const c = a.criteria;
  if (c === undefined) return '';
  return html`
    <dt>採用基準</dt>
    <dd data-testid="calibration-metamodel-criteria">
      OOS ${c.min_oos_samples} 件以上・take ${c.min_takes} 件以上・${c.min_oos_days} 営業日以上、
      EV の${confidenceLabel(c.alpha)}の下限 &gt; ${formatBps(c.min_ev_lower_bps)}、AUC の下限 &gt;
      ${c.min_auc_lower.toFixed(2)}（クラスタ・ブートストラップ ${c.resamples} 回）。足切りであり、安全の証明ではありません
    </dd>
  `;
}

function renderComparison(a: MetaModelAdoption) {
  const c = a.comparison;
  if (c === undefined) return '';
  const body = c.comparable
    ? html`採用中 ${c.active_version} と同一 OOS（${c.paired_samples} 件）で比較:
        候補 ${formatBps(c.candidate_bps)} / 現行 ${formatBps(c.active_bps)} /
        差 ${formatBps(c.diff_bps)}（${bpsInterval(c.diff_ci)}）`
    : html`採用中 ${c.active_version} と比較できないため現行を維持（${c.note}）`;
  return html`<dt>現行モデルとの比較</dt><dd data-testid="calibration-metamodel-comparison">${body}</dd>`;
}

function renderForward(a: MetaModelAdoption) {
  const f = a.forward;
  if (f === undefined) return '';
  const ev = f.ev_bps === null ? '—' : `${formatBps(f.ev_bps)}（${bpsInterval(f.ev_ci)}）`;
  return html`
    <dt>前向き評価（採用判断に不使用）</dt>
    <dd data-testid="calibration-metamodel-forward">
      学習後の判断 ${f.samples} 件・take ${f.takes} 件・${f.days} 営業日: EV ${ev}
      ${f.degraded ? html`<strong role="alert">— 損失が続いています</strong>` : ''}
    </dd>
  `;
}

// renderAdoption is the rows appended to the meta-model summary list.
export function renderAdoption(model: MetaModelAdoption) {
  return html`
    <dt>OOS take 数 / 営業日数</dt>
    <dd data-testid="calibration-metamodel-takes">${model.oos_takes} / ${model.oos_days}</dd>
    <dt>OOS EV の信頼区間</dt>
    <dd data-testid="calibration-metamodel-ev-ci">${bpsInterval(model.oos_ev_ci)}</dd>
    <dt>OOS EV の判定</dt>
    <dd data-testid="calibration-metamodel-ev-verdict">${evVerdictText(model)}</dd>
    <dt>OOS AUC の信頼区間</dt>
    <dd data-testid="calibration-metamodel-auc-ci">${aucInterval(model.oos_auc_ci)}</dd>
    ${renderCriteria(model)} ${renderComparison(model)}
    ${
      model.gate
        ? html`<dt>変更頻度の制限</dt>
            <dd data-testid="calibration-metamodel-gate">${model.gate.detail}（${model.gate.until} まで採用しません）</dd>`
        : ''
    }
    ${renderForward(model)}
  `;
}
