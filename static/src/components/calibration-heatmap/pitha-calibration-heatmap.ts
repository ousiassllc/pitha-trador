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

// Mirrors docs/api/endpoints.md §GET /api/v1/calibration's `buckets[]` item
// shape (internal/web/handler.calibrationBucketOutput).
export interface CalibrationBucket {
  range: string;
  direction_accuracy: number;
  avg_future_return_pct: number;
}

// Mirrors docs/api/endpoints.md §GET /api/v1/calibration's response body
// (internal/web/handler.CalibrationAPIOutput).
export interface CalibrationAPIResponse {
  buckets: CalibrationBucket[];
  brier_score: number;
  log_loss: number;
  expected_calibration_error: number;
}

const CHART_HEIGHT = 300;
const ACCURACY_LINE_COLOR = '#2563eb';
const PERFECT_CALIBRATION_LINE_COLOR = '#9ca3af';

// bucketMidpointPct parses e.g. "0.70-0.80" into 75. The reliability
// curve's x-axis is "predicted confidence" (a bucket's midpoint), not a
// real date, but lightweight-charts requires an increasing numeric `Time`
// for every series point - the midpoint, scaled to an integer percent,
// doubles as that x-axis value while the time-scale's date labels stay
// hidden (see initChart below).
function bucketMidpointPct(range: string): number {
  const [low, high] = range.split('-').map(Number);
  return Math.round(((low + high) / 2) * 100);
}

// heatmapColor maps a [0,1] direction_accuracy to a red(low)->green(high)
// background color for the confidence-band heatmap
// (docs/components/overview.md §5.3 "confidence帯別カラーヒートマップ").
function heatmapColor(directionAccuracy: number): string {
  const hue = Math.max(0, Math.min(1, directionAccuracy)) * 120;
  return `hsl(${hue}, 70%, 45%)`;
}

@customElement('pitha-calibration-heatmap')
export class PithaCalibrationHeatmap extends LitElement {
  @property({ type: String, attribute: 'calibration-url' }) calibrationUrl = '';

  @state() private buckets: CalibrationBucket[] = [];
  @state() private brierScore: number | null = null;
  @state() private logLoss: number | null = null;
  @state() private expectedCalibrationError: number | null = null;
  @state() private error: string | null = null;
  @state() private loading = false;

  private readonly containerRef = createRef<HTMLDivElement>();
  private chart: IChartApi | null = null;
  private accuracySeries: ISeriesApi<'Line'> | null = null;
  private perfectSeries: ISeriesApi<'Line'> | null = null;

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.chart?.remove();
    this.chart = null;
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

    this.chart = createChart(container, {
      width: container.clientWidth || 600,
      height: CHART_HEIGHT,
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
    this.loading = true;
    try {
      const response = await get<CalibrationAPIResponse>(this.calibrationUrl);
      this.buckets = response.buckets;
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
    this.accuracySeries.setData(
      buckets.map((b) => ({
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
                class="pitha-calibration-heatmap-cell"
                style="background-color: ${heatmapColor(b.direction_accuracy)}"
                data-testid="calibration-heatmap-cell"
              >
                <span class="range">${b.range}</span>
                <span class="accuracy">${(b.direction_accuracy * 100).toFixed(1)}%</span>
                <span class="avg-return">${b.avg_future_return_pct.toFixed(2)}%</span>
              </li>
            `,
          )}
        </ul>
        ${
          this.brierScore !== null
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
