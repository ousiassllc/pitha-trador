// Skipped nightly retrainings of `pitha-calibration-heatmap`: a retraining that stored no model for lack of usable labeled history
// is listed with its fixed reason code and how far short the data was
// ("必要130件中 87件").
import { html } from 'lit';
import { formatJstDateTime } from '../../lib/jst-datetime';
import { retrainSkipLabel } from '../../lib/labels';

// Mirrors docs/api/endpoints.md §GET /api/v1/calibration's `meta_model_skips[]`
// item (internal/web/handler/calibration.retrainSkipOutput).
export interface RetrainSkip {
  skipped_at: string;
  reason_code: string;
  usable_rows: number;
  required_rows: number;
}

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
              <td data-testid="calibration-retrain-skip-reason" data-code=${s.reason_code}>${retrainSkipLabel(s.reason_code)}</td>
              <td data-testid="calibration-retrain-skip-rows">必要${s.required_rows}件中 ${s.usable_rows}件</td>
            </tr>
          `,
        )}
      </tbody>
    </table>
  `;
}
