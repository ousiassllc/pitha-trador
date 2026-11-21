// Confidence-band cell grid of `pitha-calibration-heatmap`. Split out of the
// component to keep it within the linterly per-file limit.
import { html } from 'lit';
import { ref } from 'lit/directives/ref.js';
import { renderBucketBarrier } from './barrier-view';
import { applyCellColors, type CalibrationBucket, formatYen } from './calibration-view';

export function renderBucketGrid(buckets: CalibrationBucket[]) {
  return html`
    <ul class="pitha-calibration-heatmap-grid" data-testid="calibration-heatmap-grid">
      ${buckets.map(
        (b) => html`
          <li
            ${ref((el) => applyCellColors(el, b))}
            class="pitha-calibration-heatmap-cell"
            ?data-empty=${b.sample_count === 0}
            data-testid="calibration-heatmap-cell"
          >
            <span class="range">${b.range}</span>
            <span class="sample-count">${b.sample_count} 件</span>
            ${
              b.sample_count > 0
                ? html`
                  <span class="accuracy">${(b.direction_accuracy * 100).toFixed(1)}%</span>
                  <span class="avg-return">${b.avg_future_return_pct.toFixed(2)}%</span>
                  <span class="avg-confidence">確信度 ${b.avg_confidence.toFixed(2)}</span>
                `
                : html`<span class="no-data">データはありません</span>`
            }
            <span class="bucket-pnl">
              取引 ${b.trade_count} 件 / ${formatYen(b.total_pnl)}（${b.avg_pnl_pct.toFixed(2)}%）${
                b.trade_count > 0 ? html` / 勝率 ${(b.trade_win_rate * 100).toFixed(1)}%` : ''
              }
            </span>
            ${renderBucketBarrier(b.triple_barrier)}
          </li>
        `,
      )}
    </ul>
  `;
}
