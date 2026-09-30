// `pitha-price-chart`: Symbol Detail's candlestick + VWAP + volume chart
// with Jev signal markers (docs/components/overview.md §5.1). It loads
// its initial series from `GET /api/v1/symbols/{symbol}/candles` (via
// lib/api.ts), then applies `/ws/symbols/{symbol}` `tick`/`jev_update`
// pushes (via lib/ws.ts) as they arrive.

import {
  createChart,
  type IChartApi,
  type ISeriesApi,
  type SeriesMarker,
  type Time,
  type UTCTimestamp,
} from 'lightweight-charts';
import { css, html, LitElement, type PropertyValues } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { createRef, ref } from 'lit/directives/ref.js';
import { get } from '../lib/api';
import { logger } from '../lib/logger';
import { noticeStyles } from '../lib/styles';
import { resolveWsUrl, WsClient } from '../lib/ws';

// Mirrors docs/api/endpoints.md §5 `GET /api/v1/symbols/{symbol}/candles`
// item shape (internal/web/handler/symbol.candleOutput).
export interface Candle {
  time: string;
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
  vwap: number;
}

interface CandlesAPIResponse {
  symbol: string;
  candles: Candle[];
}

// Mirrors docs/api/endpoints.md §6's `/ws/symbols/{symbol}` message
// shapes (internal/web/handler/symbol.symbolTickMessage/symbolJevUpdateMessage).
interface TickMessage {
  type: 'tick';
  price: number;
}

interface JevUpdateMessage {
  type: 'jev_update';
  direction: string | null;
  confidence: number | null;
}

type SymbolMessage = TickMessage | JevUpdateMessage;

const CHART_HEIGHT = 400;
const BAR_SECONDS = 60;

interface Bar {
  time: UTCTimestamp;
  open: number;
  high: number;
  low: number;
  close: number;
}
const LONG_MARKER_COLOR = '#16a34a';
const SHORT_MARKER_COLOR = '#dc2626';

function toUTCTimestamp(iso: string): UTCTimestamp {
  return Math.floor(new Date(iso).getTime() / 1000) as UTCTimestamp;
}

@customElement('pitha-price-chart')
export class PithaPriceChart extends LitElement {
  // Shadow DOM: Tailwind does not reach in here, so style locally.
  static override styles = [
    noticeStyles,
    css`
      :host {
        display: block;
      }
      .pitha-price-chart-container {
        width: 100%;
        height: ${CHART_HEIGHT}px;
      }
    `,
  ];

  @property({ type: String, attribute: 'symbol' }) symbol = '';
  @property({ type: String, attribute: 'candles-url' }) candlesUrl = '';
  @property({ type: String, attribute: 'ws-url' }) wsUrl = '';

  @state() private error: string | null = null;

  private readonly containerRef = createRef<HTMLDivElement>();
  private chart: IChartApi | null = null;
  private candleSeries: ISeriesApi<'Candlestick'> | null = null;
  private vwapSeries: ISeriesApi<'Line'> | null = null;
  private volumeSeries: ISeriesApi<'Histogram'> | null = null;
  private wsClient: WsClient<SymbolMessage> | null = null;
  private markers: SeriesMarker<Time>[] = [];
  private lastDirection: string | null = null;
  // The newest 1-minute bar on the chart (loaded or tick-built): ticks fold
  // into it instead of stacking new bars.
  private lastBar: Bar | null = null;

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.chart?.remove();
    this.chart = null;
    this.wsClient?.close();
    this.wsClient = null;
  }

  // firstUpdated (not connectedCallback) so this.containerRef.value is
  // guaranteed populated: createChart needs a real, already-rendered DOM
  // element to attach its canvas to.
  protected override firstUpdated(): void {
    this.initChart();
    this.loadInitial();
    this.subscribeWs();
  }

  private initChart(): void {
    const container = this.containerRef.value;
    if (!container) return;

    // autoSize makes the chart follow its container (the CSS below fixes the
    // container's height, and width is 100% of the host); it ignores
    // explicit width/height options.
    this.chart = createChart(container, { autoSize: true });
    this.candleSeries = this.chart.addCandlestickSeries();
    this.vwapSeries = this.chart.addLineSeries({ color: '#2962ff', lineWidth: 1 });
    this.volumeSeries = this.chart.addHistogramSeries({ priceScaleId: '', color: '#9ca3af' });
  }

  private async loadInitial(): Promise<void> {
    try {
      const response = await get<CandlesAPIResponse>(this.candlesUrl);
      this.applyCandles(response.candles);
      this.error = null;
    } catch (err) {
      this.error = err instanceof Error ? err.message : String(err);
      logger.error('pitha-price-chart: failed to load initial candles', { error: err });
    }
  }

  private applyCandles(candles: Candle[]): void {
    if (!this.candleSeries || !this.vwapSeries || !this.volumeSeries) return;
    const bars: Bar[] = candles.map((c) => ({
      time: toUTCTimestamp(c.time),
      open: c.open,
      high: c.high,
      low: c.low,
      close: c.close,
    }));
    this.candleSeries.setData(bars);
    this.lastBar = bars.at(-1) ?? null;
    this.vwapSeries.setData(candles.map((c) => ({ time: toUTCTimestamp(c.time), value: c.vwap })));
    this.volumeSeries.setData(
      candles.map((c) => ({ time: toUTCTimestamp(c.time), value: c.volume })),
    );
  }

  private subscribeWs(): void {
    this.wsClient = new WsClient<SymbolMessage>(resolveWsUrl(this.wsUrl), {
      onMessage: (message) => {
        if (message.type === 'tick') {
          this.applyTick(message);
        } else if (message.type === 'jev_update') {
          this.applyJevUpdate(message);
        }
      },
    });
  }

  // applyTick folds a price tick into the current 1-minute bar (a new bar
  // only when the minute changes), matching the candles endpoint's
  // resolution. A non-positive price means the server has no snapshot yet
  // and would drag the chart's autoscale to 0, so it is ignored.
  private applyTick(message: TickMessage): void {
    if (!this.candleSeries || !(message.price > 0)) return;
    const price = message.price;
    // Never go back in time: series.update rejects a bar older than the last.
    const minute = Math.floor(Date.now() / 1000 / BAR_SECONDS) * BAR_SECONDS;
    const time = Math.max(minute, this.lastBar?.time ?? 0) as UTCTimestamp;
    const bar: Bar =
      this.lastBar && this.lastBar.time === time
        ? {
            ...this.lastBar,
            high: Math.max(this.lastBar.high, price),
            low: Math.min(this.lastBar.low, price),
            close: price,
          }
        : { time, open: price, high: price, low: price, close: price };
    this.lastBar = bar;
    this.candleSeries.update(bar);
  }

  // applyJevUpdate draws an up/down arrow marker on direction changes
  // (docs/components/overview.md §5.1: "LONG転換で上向き矢印"). A repeat
  // of the same direction (or no direction yet) draws nothing.
  private applyJevUpdate(message: JevUpdateMessage): void {
    if (!this.candleSeries || !message.direction || message.direction === this.lastDirection) {
      this.lastDirection = message.direction;
      return;
    }
    this.lastDirection = message.direction;

    const isLong = message.direction === 'LONG';
    const marker: SeriesMarker<Time> = {
      time: Math.floor(Date.now() / 1000) as UTCTimestamp,
      position: isLong ? 'belowBar' : 'aboveBar',
      color: isLong ? LONG_MARKER_COLOR : SHORT_MARKER_COLOR,
      shape: isLong ? 'arrowUp' : 'arrowDown',
      text: message.direction,
    };
    this.markers = [...this.markers, marker];
    this.candleSeries.setMarkers(this.markers);
  }

  protected override render() {
    return html`
      <div ${ref(this.containerRef)} class="pitha-price-chart-container"></div>
      ${this.error ? html`<p class="pitha-price-chart-error" role="alert">${this.error}</p>` : ''}
    `;
  }

  // `changed.get(...) !== undefined` skips the first update cycle, whose
  // recorded old value is undefined: firstUpdated has already loaded the
  // candles and opened the socket, and redoing both would double them.
  protected override updated(changed: PropertyValues<this>): void {
    if (changed.get('candlesUrl') !== undefined && this.candleSeries) {
      this.loadInitial();
    }
    if (changed.get('wsUrl') !== undefined && this.wsClient) {
      this.wsClient.close();
      this.subscribeWs();
    }
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'pitha-price-chart': PithaPriceChart;
  }
}
