// `pitha-calibration-heatmap`: Calibration's reliability curve + confidence-
// band heatmap (docs/components/overview.md §5.3, functional.md §5.4). It
// loads `GET /api/v1/calibration` (via lib/api.ts) once on connect and
// again whenever the refresh button is pressed - real-time updates are not
// needed, so unlike pitha-price-chart/pitha-scanner-table this component
// never opens a WebSocket; a page revisit or the refresh button are the
// only re-fetch triggers.
import {
  createChart,
  type IChartApi,
  type ISeriesApi,
  LineStyle,
  type UTCTimestamp,
} from 'lightweight-charts';
import { html, LitElement } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { createRef, ref } from 'lit/directives/ref.js';
import { get } from '../lib/api';
import { logger } from '../lib/logger';
import { buttonStyles, noticeStyles } from '../lib/styles';
import { heatmapStyles } from './calibration-styles';
import {
  bucketColors,
  bucketMidpointPct,
  type CalibrationAPIResponse,
  type CalibrationBucket,
  type CalibrationDirection,
  formatYen,
  hasLabeledSamples,
} from './calibration-view';

const ACCURACY_LINE_COLOR = '#2563eb';
const PERFECT_CALIBRATION_LINE_COLOR = '#9ca3af';

@customElement('pitha-calibration-heatmap')
export class PithaCalibrationHeatmap extends LitElement {
  // Shadow DOM: Tailwind does not reach in here, so style locally.
  static override styles = [buttonStyles, noticeStyles, heatmapStyles];

  @property({ type: String, attribute: 'calibration-url' }) calibrationUrl = '';

  @state() private buckets: CalibrationBucket[] = [];
  @state() private byDirection: CalibrationDirection[] = [];
  @state() private hasSamples = false;
  @state() private brierScore: number | null = null;
  @state() private logLoss: number | null = null;
  @state() private expectedCalibrationError: number | null = null;
  @state() private error: string | null = null;
  @state() private loading = false;

  private readonly containerRef = createRef<HTMLDivElement>();
  private chart: IChartApi | null = null;
  private accuracySeries: ISeriesApi<'Line'> | null = null;
  private perfectSeries: ISeriesApi<'Line'> | null = null;

  override connectedCallback(): void {
    super.connectedCallback();
    // Re-attached after a removal: firstUpdated does not run again, but the
    // rendered container and the loaded buckets survive, so rebuild the chart
    // disconnectedCallback tore down and redraw the curve from them.
    if (this.hasUpdated) {
      this.initChart();
      this.applyBuckets(this.buckets);
    }
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.chart?.remove();
    this.chart = null;
    this.accuracySeries = null;
    this.perfectSeries = null;
  }

  // firstUpdated (not connectedCallback) so this.containerRef.value is
  // guaranteed populated: createChart needs a real, already-rendered DOM
  // element to attach its canvas to (mirrors pitha-price-chart).
  protected override firstUpdated(): void {
    this.initChart();
    void this.load();
  }

  private initChart(): void {
    const container = this.containerRef.value;
    if (!container) return;

    // autoSize: follow the container (height is fixed in `static styles`).
    this.chart = createChart(container, {
      autoSize: true,
      timeScale: { visible: false },
    });
    this.perfectSeries = this.chart.addLineSeries({
      color: PERFECT_CALIBRATION_LINE_COLOR,
      lineWidth: 1,
      lineStyle: LineStyle.Dashed,
      title: 'Perfect calibration',
    });
    this.accuracySeries = this.chart.addLineSeries({
      color: ACCURACY_LINE_COLOR,
      lineWidth: 2,
      title: 'Observed accuracy',
    });
  }

  private async load(): Promise<void> {
    if (!this.calibrationUrl) {
      logger.error('pitha-calibration-heatmap: calibration-url is not set');
      return;
    }
    this.loading = true;
    try {
      const response = await get<CalibrationAPIResponse>(this.calibrationUrl);
      this.buckets = response.buckets;
      this.byDirection = response.by_direction;
      this.hasSamples = hasLabeledSamples(response);
      this.brierScore = response.brier_score;
      this.logLoss = response.log_loss;
      this.expectedCalibrationError = response.expected_calibration_error;
      this.applyBuckets(response.buckets);
      this.error = null;
    } catch (err) {
      this.error = err instanceof Error ? err.message : String(err);
      logger.error('pitha-calibration-heatmap: failed to load calibration metrics', { error: err });
    } finally {
      this.loading = false;
    }
  }

  private applyBuckets(buckets: CalibrationBucket[]): void {
    if (!this.accuracySeries || !this.perfectSeries) return;
    // Empty bands carry direction_accuracy 0 ("no data"): leave them off the
    // curve rather than plotting a drop to 0.
    this.accuracySeries.setData(
      buckets
        .filter((b) => b.sample_count > 0)
        .map((b) => ({
          time: bucketMidpointPct(b.range) as UTCTimestamp,
          value: b.direction_accuracy,
        })),
    );
    this.perfectSeries.setData(
      buckets.map((b) => ({
        time: bucketMidpointPct(b.range) as UTCTimestamp,
        value: bucketMidpointPct(b.range) / 100,
      })),
    );
  }

  private onRefresh(): void {
    void this.load();
  }

  protected override render() {
    return html`
      <div class="pitha-calibration-heatmap">
        <button type="button" @click=${this.onRefresh} ?disabled=${this.loading}>
          ${this.loading ? '更新中…' : '更新'}
        </button>
        ${
          this.error
            ? html`<p class="pitha-calibration-heatmap-error" role="alert">${this.error}</p>`
            : ''
        }
        <div ${ref(this.containerRef)} class="pitha-calibration-heatmap-chart"></div>
        <ul class="pitha-calibration-heatmap-grid" data-testid="calibration-heatmap-grid">
          ${this.buckets.map(
            (b) => html`
              <li
                ${ref((el) => {
                  // CSSOM, not a `style` attribute binding: the CSP (no
                  // `style-src 'unsafe-inline'`) blocks inline style attributes,
                  // and Lit's styleMap renders one on its first pass.
                  if (el) {
                    const { background, color } = bucketColors(b);
                    (el as HTMLElement).style.backgroundColor = background;
                    (el as HTMLElement).style.color = color;
                  }
                })}
                class="pitha-calibration-heatmap-cell"
                data-testid="calibration-heatmap-cell"
              >
                <span class="range">${b.range}</span>
                <span class="sample-count">n=${b.sample_count}</span>
                ${
                  b.sample_count > 0
                    ? html`
                      <span class="accuracy">${(b.direction_accuracy * 100).toFixed(1)}%</span>
                      <span class="avg-return">${b.avg_future_return_pct.toFixed(2)}%</span>
                      <span class="avg-confidence">conf ${b.avg_confidence.toFixed(2)}</span>
                    `
                    : html`<span class="no-data">データなし</span>`
                }
                <span class="bucket-pnl">
                  ${b.trade_count} trades / ${formatYen(b.total_pnl)} (${b.avg_pnl_pct.toFixed(2)}%)
                </span>
              </li>
            `,
          )}
        </ul>
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
            ${this.byDirection.map(
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
        ${
          this.brierScore !== null && !this.hasSamples
            ? html`<p class="pitha-calibration-heatmap-no-samples" data-testid="calibration-no-samples">サンプルなし（ラベル付き判断がまだありません）</p>`
            : ''
        }
        ${
          this.brierScore !== null && this.hasSamples
            ? html`
              <dl class="pitha-calibration-heatmap-summary">
                <dt>Brier Score</dt>
                <dd>${this.brierScore.toFixed(3)}</dd>
                <dt>Log Loss</dt>
                <dd>${this.logLoss?.toFixed(3)}</dd>
                <dt>Expected Calibration Error</dt>
                <dd>${this.expectedCalibrationError?.toFixed(3)}</dd>
              </dl>
            `
            : ''
        }
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'pitha-calibration-heatmap': PithaCalibrationHeatmap;
  }
}
