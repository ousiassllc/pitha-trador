// Meta-labeling model block of `pitha-calibration-heatmap` (issue #766, parent
// #752): the newest model's out-of-sample AUC, reliability curve and EV, and
// the Jev evaluation period. Split out of the component to keep it within the
// linterly per-file limit.
import { html } from 'lit';
import { formatJstDateTime } from '../lib/jst-datetime';
import { formatBps } from './barrier-view';

// Mirrors docs/api/endpoints.md §GET /api/v1/calibration's `meta_model.oos_calibration[]`
// item shape (internal/web/handler/calibration.metaModelCalibrationBinOutput).
export interface MetaModelCalibrationBin {
  mean_predicted: number;
  observed_rate: number;
  count: number;
}

// Mirrors docs/api/endpoints.md §GET /api/v1/calibration's `meta_model` object
// (internal/web/handler/calibration.metaModelOutput). Absent from the
// response until a model was trained.
export interface MetaModel {
  version: string;
  algorithm: string;
  status: 'active' | 'rejected';
  reject_reason: string;
  active_version: string;
  trained_at: string;
  train_start: string;
  train_end: string;
  oos_start: string;
  oos_end: string;
  train_samples: number;
  oos_samples: number;
  oos_auc: number | null;
  oos_ev_bps: number | null;
  oos_calibration: MetaModelCalibrationBin[];
  llm_cutoff: string;
}

const formatPercent = (rate: number): string => `${(rate * 100).toFixed(1)}%`;

// periodLabel renders a window as "start 〜 end" in JST.
const periodLabel = (start: string, end: string): string =>
  `${formatJstDateTime(start)} 〜 ${formatJstDateTime(end)}`;

function renderStatus(model: MetaModel) {
  if (model.status === 'active') return html`採用中`;
  const inForce = model.active_version
    ? `採用中のモデルは ${model.active_version} のまま`
    : '採用中のモデルなし（エントリーは見送り）';
  return html`不採用（${model.reject_reason}）— ${inForce}`;
}

function renderCalibrationCurve(bins: MetaModelCalibrationBin[]) {
  if (bins.length === 0) return '';
  return html`
    <table
      class="pitha-calibration-heatmap-directions"
      data-testid="calibration-metamodel-curve"
      aria-label="メタモデルのOOS較正曲線"
    >
      <thead>
        <tr>
          <th scope="col">予測確率（平均）</th>
          <th scope="col">実際の勝率</th>
          <th scope="col">サンプル数</th>
        </tr>
      </thead>
      <tbody>
        ${bins.map(
          (b) => html`
            <tr data-testid="calibration-metamodel-curve-row">
              <td>${formatPercent(b.mean_predicted)}</td>
              <td>${formatPercent(b.observed_rate)}</td>
              <td>${b.count}</td>
            </tr>
          `,
        )}
      </tbody>
    </table>
  `;
}

// renderMetaModel is the OOS block: the model, its OOS AUC / EV and the Jev
// evaluation period (the OOS window, never before the LLM cutoff), then the
// reliability curve of its calibrated probability. `model` is undefined until
// a response arrived (nothing shown) and null when no model was trained yet.
export function renderMetaModel(model: MetaModel | null | undefined) {
  if (model === undefined) return '';
  if (model === null) {
    return html`<p class="pitha-calibration-heatmap-no-samples" data-testid="calibration-metamodel-none">
      メタラベリングモデルは未学習です（ラベルが貯まると夜間に学習されます）
    </p>`;
  }
  return html`
    <dl class="pitha-calibration-heatmap-summary" data-testid="calibration-metamodel" aria-label="メタラベリングモデル（OOS）">
      <dt>モデル</dt>
      <dd data-testid="calibration-metamodel-version">${model.version}（${model.algorithm}）</dd>
      <dt>状態</dt>
      <dd data-testid="calibration-metamodel-status">${renderStatus(model)}</dd>
      <dt>学習日時</dt>
      <dd>${formatJstDateTime(model.trained_at)}</dd>
      <dt>Jev 評価期間（OOS）</dt>
      <dd data-testid="calibration-metamodel-period">${periodLabel(model.oos_start, model.oos_end)}</dd>
      <dt>LLM 学習データ終了日</dt>
      <dd data-testid="calibration-metamodel-cutoff">${model.llm_cutoff}</dd>
      <dt>学習期間</dt>
      <dd>${periodLabel(model.train_start, model.train_end)}</dd>
      <dt>サンプル数（学習 / OOS）</dt>
      <dd data-testid="calibration-metamodel-samples">${model.train_samples} / ${model.oos_samples}</dd>
      <dt>OOS AUC</dt>
      <dd data-testid="calibration-metamodel-auc">${model.oos_auc === null ? '—' : model.oos_auc.toFixed(3)}</dd>
      <dt>OOS EV（net, エントリーした判断あたり）</dt>
      <dd data-testid="calibration-metamodel-ev">${model.oos_ev_bps === null ? '—' : formatBps(model.oos_ev_bps)}</dd>
    </dl>
    ${renderCalibrationCurve(model.oos_calibration)}
  `;
}
