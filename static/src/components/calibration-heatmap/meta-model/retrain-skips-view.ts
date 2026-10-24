// Skipped nightly retrainings of `pitha-calibration-heatmap`: a retraining that stored no model for lack of usable labeled history
// is listed with its fixed reason code and how far short the data was
// ("必要130件中 87件").
import { html } from 'lit';
import { formatJstDateTime } from '../../lib/jst-datetime';

// Mirrors docs/api/endpoints.md §GET /api/v1/calibration's `meta_model_skips[]`
// item (internal/web/handler/calibration.retrainSkipOutput).
export interface RetrainSkip {
  skipped_at: string;
  reason_code: string;
  usable_rows: number;
  required_rows: number;
}

// SKIP_REASON_LABELS names the fixed codes of internal/repository/metalabel
// (Skip*); an unknown code is shown raw.
const SKIP_REASON_LABELS: Record<string, string> = {
  too_few_rows_before_oos: '検証期間より前のラベル付き判断が不足',
  too_few_rows_after_purge: '重なり除去（purge/embargo）後の学習用データが不足',
  training_data_unusable: '件数はあるが学習できない（ラベルが片側のみ等）',
};

const skipReasonLabel = (code: string): string => SKIP_REASON_LABELS[code] ?? code;

// renderRetrainSkips lists the newest skips; nothing when there is none.
export function renderRetrainSkips(skips: RetrainSkip[] | undefined) {
  if (skips === undefined || skips.length === 0) return '';
  return html`
    <table
      class="pitha-calibration-heatmap-directions"
      data-testid="calibration-retrain-skips"
      aria-label="メタモデル再学習の見送り履歴"
    >
      <caption>メタモデル再学習の見送り（データ不足）</caption>
      <thead>
        <tr>
          <th scope="col">日時</th>
          <th scope="col">理由</th>
          <th scope="col">件数</th>
        </tr>
      </thead>
      <tbody>
        ${skips.map(
          (s) => html`
            <tr data-testid="calibration-retrain-skip-row">
              <td>${formatJstDateTime(s.skipped_at)}</td>
              <td data-testid="calibration-retrain-skip-reason" data-code=${s.reason_code}>${skipReasonLabel(s.reason_code)}</td>
              <td data-testid="calibration-retrain-skip-rows">必要${s.required_rows}件中 ${s.usable_rows}件</td>
            </tr>
          `,
        )}
      </tbody>
    </table>
  `;
}
