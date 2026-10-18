// Per-direction (LONG / SHORT) table of `pitha-calibration-heatmap`. Split out
// of the component to keep it within the linterly per-file limit.
import { html } from 'lit';
import type { CalibrationDirection } from './calibration-view';

export function renderDirectionTable(byDirection: CalibrationDirection[]) {
  return html`
    <table
      class="pitha-calibration-heatmap-directions"
      data-testid="calibration-direction-table"
      aria-label="方向別キャリブレーション"
    >
      <thead>
        <tr>
          <th scope="col">Direction</th>
          <th scope="col">Samples</th>
          <th scope="col">Accuracy</th>
          <th scope="col">Avg return</th>
        </tr>
      </thead>
      <tbody>
        ${byDirection.map(
          (d) => html`
            <tr data-testid="calibration-direction-row">
              <td>${d.direction}</td>
              <td>${d.sample_count}</td>
              <td>${d.sample_count > 0 ? `${(d.direction_accuracy * 100).toFixed(1)}%` : '—'}</td>
              <td>${d.sample_count > 0 ? `${d.avg_future_return_pct.toFixed(2)}%` : '—'}</td>
            </tr>
          `,
        )}
      </tbody>
    </table>
  `;
}
