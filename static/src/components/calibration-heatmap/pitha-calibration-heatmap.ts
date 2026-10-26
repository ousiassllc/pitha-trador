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
import { helpMarkStyles, renderHelpMark } from '../lib/help-mark';
import { logger } from '../lib/logger';
import { buttonStyles, noticeStyles } from '../lib/styles';
import { type ABArm, renderABArms } from './ab-arms-view';
import { renderBarrierSummary, renderBucketBarrier } from './barrier-view';
import { heatmapStyles } from './calibration-styles';
import {
  type BarrierStats,
  bucketColors,
  bucketMidpointPct,
  type CalibrationAPIResponse,
  type CalibrationBucket,
  type CalibrationDirection,
  calibrationUrlFor,
  DEFAULT_HORIZON,
  formatYen,
  type HorizonOption,
  hasLabeledSamples,
} from './calibration-view';
import { renderDirectionTable } from './direction-view';
import { renderHorizonSelector } from './horizon-selector';
import { type LLMCutoffInfo, type MetaModel, renderMetaModel } from './meta-model/meta-model-view';
import { type RetrainSkip, renderRetrainSkips } from './meta-model/retrain-skips-view';

const ACCURACY_LINE_COLOR = '#2563eb';
const PERFECT_CALIBRATION_LINE_COLOR = '#9ca3af';

@customElement('pitha-calibration-heatmap')
export class PithaCalibrationHeatmap extends LitElement {
  // Shadow DOM: Tailwind does not reach in here, so style locally.
  static override styles = [buttonStyles, noticeStyles, heatmapStyles, helpMarkStyles];

  @property({ type: String, attribute: 'calibration-url' }) calibrationUrl = '';

  // The horizon selected on screen; `shownHorizon` is the one the rendered
  // figures were actually aggregated over (the response's echo).
  @state() private horizon: HorizonOption = DEFAULT_HORIZON;
  @state() private shownHorizon: HorizonOption | null = null;
  @state() private buckets: CalibrationBucket[] = [];
  @state() private byDirection: CalibrationDirection[] = [];
  @state() private hasSamples = false;
  @state() private brierScore: number | null = null;
  @state() private logLoss: number | null = null;
  @state() private expectedCalibrationError: number | null = null;
  @state() private tradeCount = 0;
  @state() private pnlBrierScore = 0;
  @state() private pnlLogLoss = 0;
  @state() private barrier: BarrierStats | null = null;
  @state() private metaModel: MetaModel | null | undefined;
  @state() private metaModelCutoff: LLMCutoffInfo | undefined;
  @state() private retrainSkips: RetrainSkip[] | undefined;
  @state() private abArms: ABArm[] | undefined;
  @state() private error: string | null = null;
  @state() private loading = false;

  private loadSeq = 0;
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
    const seq = ++this.loadSeq;
    this.loading = true;
    try {
      const response = await get<CalibrationAPIResponse>(
        calibrationUrlFor(this.calibrationUrl, this.horizon),
      );
      // A newer request (horizon switched meanwhile) supersedes this one.
      if (seq !== this.loadSeq) return;
      this.shownHorizon = response.horizon;
      this.buckets = response.buckets;
      this.byDirection = response.by_direction;
      this.hasSamples = hasLabeledSamples(response);
      this.brierScore = response.brier_score;
      this.logLoss = response.log_loss;
      this.expectedCalibrationError = response.expected_calibration_error;
      this.tradeCount = response.trade_count;
      this.pnlBrierScore = response.pnl_brier_score;
      this.pnlLogLoss = response.pnl_log_loss;
      this.barrier = response.triple_barrier;
      this.metaModel = response.meta_model ?? null;
      this.metaModelCutoff = response.meta_model_cutoff;
      this.retrainSkips = response.meta_model_skips;
      this.abArms = response.ab_arms;
      this.applyBuckets(response.buckets);
      this.error = null;
    } catch (err) {
      if (seq !== this.loadSeq) return;
      this.error = err instanceof Error ? err.message : String(err);
      logger.error('pitha-calibration-heatmap: failed to load calibration metrics', { error: err });
    } finally {
      if (seq === this.loadSeq) this.loading = false;
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

  private onHorizonChange(horizon: HorizonOption): void {
    if (horizon === this.horizon) return;
    this.horizon = horizon;
    void this.load();
  }

  protected override render() {
    return html`
      <div class="pitha-calibration-heatmap">
        <button type="button" @click=${this.onRefresh} ?disabled=${this.loading}>
          ${this.loading ? '更新中…' : '更新'}
        </button>
        ${renderHorizonSelector(this.horizon, this.shownHorizon, (option) => this.onHorizonChange(option))}
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
                <span class="sample-count">${b.sample_count} 件</span>
                ${
                  b.sample_count > 0
                    ? html`
                      <span class="accuracy">${(b.direction_accuracy * 100).toFixed(1)}%</span>
                      <span class="avg-return">${b.avg_future_return_pct.toFixed(2)}%</span>
                      <span class="avg-confidence">確信度 ${b.avg_confidence.toFixed(2)}</span>
                    `
                    : html`<span class="no-data">データなし</span>`
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
        ${renderDirectionTable(this.byDirection)}
        ${
          this.brierScore !== null && !this.hasSamples
            ? html`<p class="pitha-calibration-heatmap-no-samples" data-testid="calibration-no-samples">サンプルなし（ラベル付き判断がありません）</p>`
            : ''
        }
        ${renderBarrierSummary(this.barrier)}
        ${renderMetaModel(this.metaModel, this.metaModelCutoff)}
        ${renderRetrainSkips(this.retrainSkips)}
        ${renderABArms(this.abArms)}
        ${
          this.brierScore !== null && this.hasSamples
            ? html`
              <dl class="pitha-calibration-heatmap-summary">
                <dt>
                  ブライアスコア
                  ${renderHelpMark({
                    id: 'help-calibration-brier',
                    label: 'ブライアスコアの説明',
                    text: '予測した確率と結果（方向が当たったか）のずれを表すスコアです。小さいほど良い値です。',
                  })}
                </dt>
                <dd>${this.brierScore.toFixed(3)}</dd>
                <dt>
                  対数損失
                  ${renderHelpMark({
                    id: 'help-calibration-log-loss',
                    label: '対数損失の説明',
                    text: '予測した確率と結果のずれを、外れた自信の強さに厳しく表すスコアです。小さいほど良い値です。',
                  })}
                </dt>
                <dd>${this.logLoss?.toFixed(3)}</dd>
                <dt>
                  期待較正誤差
                  ${renderHelpMark({
                    id: 'help-calibration-ece',
                    label: '期待較正誤差の説明',
                    text: '予測した確信度と実際の的中率の差を、確信度の帯ごとに平均した値です。小さいほど、確信度どおりに当たっています。',
                  })}
                </dt>
                <dd>${this.expectedCalibrationError?.toFixed(3)}</dd>
                ${
                  this.tradeCount > 0
                    ? html`
                      <dt>
                        損益ブライアスコア（${this.tradeCount} 件）
                        ${renderHelpMark({
                          id: 'help-calibration-pnl-brier',
                          label: '損益ブライアスコアの説明',
                          text: '実際に取引した判断だけを対象に、損益がプラスだったかで測ったブライアスコアです。',
                        })}
                      </dt>
                      <dd>${this.pnlBrierScore.toFixed(3)}</dd>
                      <dt>
                        損益の対数損失
                        ${renderHelpMark({
                          id: 'help-calibration-pnl-log-loss',
                          label: '損益の対数損失の説明',
                          text: '実際に取引した判断だけを対象に、損益がプラスだったかで測った対数損失です。',
                        })}
                      </dt>
                      <dd>${this.pnlLogLoss.toFixed(3)}</dd>
                    `
                    : ''
                }
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
