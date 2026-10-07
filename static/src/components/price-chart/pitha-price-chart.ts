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
} from 'lightweight-charts';
import { css, html, LitElement, type PropertyValues } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { createRef, ref } from 'lit/directives/ref.js';
import { get } from '../lib/api';
import { logger } from '../lib/logger';
import { noticeStyles } from '../lib/styles';
import { resolveWsUrl, WsClient, type WsStatus } from '../lib/ws';
import { renderWsDisconnected } from '../lib/ws-status';
import { type Bar, foldTick, toBarTime, toUTCTimestamp } from './bars';
import {
  type ChartSummary,
  describeChart,
  EMPTY_SUMMARY,
  MAIN_SCALE_MARGINS,
  VOLUME_COLOR,
  VOLUME_SCALE_MARGINS,
  vwapSeriesData,
} from './chart-data';
import type {
  Candle,
  CandlesAPIResponse,
  JevUpdateMessage,
  SymbolMessage,
  TickMessage,
} from './chart-types';
import { formatCrosshairTime, formatTickMark } from './jst-time';

const CHART_HEIGHT = 400;
const LONG_MARKER_COLOR = '#16a34a';
const SHORT_MARKER_COLOR = '#dc2626';

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
      .pitha-price-chart-empty {
        margin: 0;
        color: #475569;
        font-size: 0.875rem;
      }
    `,
  ];

  @property({ type: String, attribute: 'candles-url' }) candlesUrl = '';
  @property({ type: String, attribute: 'ws-url' }) wsUrl = '';

  @state() private error: string | null = null;
  @state() private wsStatus: WsStatus = 'connecting';
  // Feeds the canvas's accessible name (role="img"); updated as data arrives.
  @state() private summary: ChartSummary = EMPTY_SUMMARY;
  // True once a load returned no candles (off-hours: the default window is the
  // last 6 hours) and no tick has built a bar since; shows the empty-state text.
  @state() private empty = false;

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

  override connectedCallback(): void {
    super.connectedCallback();
    // Re-attached after a removal: firstUpdated does not run again, but the
    // rendered container survives, so rebuild what disconnectedCallback tore down.
    if (this.hasUpdated) this.start();
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.chart?.remove();
    this.chart = null;
    this.candleSeries = null;
    this.vwapSeries = null;
    this.volumeSeries = null;
    this.lastBar = null;
    this.markers = [];
    this.lastDirection = null;
    this.summary = EMPTY_SUMMARY;
    this.empty = false;
    this.stopWs();
  }

  // firstUpdated (not connectedCallback) for the first start so
  // this.containerRef.value is guaranteed populated: createChart needs a
  // real, already-rendered DOM element to attach its canvas to.
  protected override firstUpdated(): void {
    this.start();
  }

  private start(): void {
    this.initChart();
    this.loadInitial();
    this.subscribeWs();
  }

  private stopWs(): void {
    this.wsClient?.close();
    this.wsClient = null;
  }

  private resetMarkers(): void {
    this.markers = [];
    this.lastDirection = null;
    this.summary = EMPTY_SUMMARY; // the previous symbol's values
    this.empty = false;
    this.candleSeries?.setMarkers([]);
  }

  private initChart(): void {
    const container = this.containerRef.value;
    if (!container) return;

    // autoSize makes the chart follow its container (the CSS below fixes the
    // container's height, and width is 100% of the host); it ignores
    // explicit width/height options.
    // timeVisible/secondsVisible: bars are 1 minute, so ticks show HH:mm
    // (lightweight-charts shows dates only by default). The formatters render
    // every time in Asia/Tokyo; the library would otherwise print UTC.
    this.chart = createChart(container, {
      autoSize: true,
      localization: { timeFormatter: formatCrosshairTime },
      timeScale: { timeVisible: true, secondsVisible: false, tickMarkFormatter: formatTickMark },
    });
    this.candleSeries = this.chart.addCandlestickSeries();
    this.vwapSeries = this.chart.addLineSeries({ color: '#2962ff', lineWidth: 1 });
    this.volumeSeries = this.chart.addHistogramSeries({ priceScaleId: '', color: VOLUME_COLOR });
    // Keep the volume bars in their own bottom band, clear of the candles/VWAP.
    this.volumeSeries.priceScale().applyOptions({ scaleMargins: VOLUME_SCALE_MARGINS });
    this.chart.priceScale('right').applyOptions({ scaleMargins: MAIN_SCALE_MARGINS });
  }

  // `background` marks a resync the page fires by itself (WS reconnect), so
  // it is not counted as operator activity (FR-RISK-6, flows.md §10.4).
  private async loadInitial(background = false): Promise<void> {
    if (!this.candlesUrl) {
      logger.error('pitha-price-chart: candles-url is not set');
      return;
    }
    try {
      const response = await get<CandlesAPIResponse>(this.candlesUrl, { background });
      this.applyCandles(response.symbol, response.candles);
      this.error = null;
    } catch (err) {
      this.error = err instanceof Error ? err.message : String(err);
      logger.error('pitha-price-chart: failed to load initial candles', { error: err });
    }
  }

  private applyCandles(symbol: string, candles: Candle[]): void {
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
    this.empty = this.lastBar === null;
    this.vwapSeries.setData(vwapSeriesData(candles));
    this.volumeSeries.setData(
      candles.map((c) => ({ time: toUTCTimestamp(c.time), value: c.volume })),
    );
    const lastVwap = candles.filter((c) => c.vwap > 0).at(-1)?.vwap ?? null;
    this.summary = { ...this.summary, symbol, close: this.lastBar?.close ?? null, vwap: lastVwap };
  }

  private subscribeWs(): void {
    this.wsStatus = 'connecting';
    if (!this.wsUrl) {
      logger.error('pitha-price-chart: ws-url is not set');
      return;
    }
    // Ticks build bars from snapshot times, so bars of the minutes the
    // socket was down are missing until the candles are re-fetched (#336).
    this.wsClient = new WsClient<SymbolMessage>(resolveWsUrl(this.wsUrl), {
      onStatusChange: (status) => {
        this.wsStatus = status;
      },
      onReconnect: () => void this.loadInitial(true),
      onMessage: (message) => {
        if (message.type === 'tick') {
          this.applyTick(message);
        } else if (message.type === 'jev_update') {
          this.applyJevUpdate(message);
        }
      },
    });
  }

  // applyTick draws a price tick folded into the bar of its snapshot time
  // (see foldTick).
  private applyTick(message: TickMessage): void {
    if (!this.candleSeries) return;
    const bar = foldTick(this.lastBar, message.price, message.time);
    if (!bar) return;
    this.lastBar = bar;
    this.empty = false;
    this.candleSeries.update(bar);
    this.summary = { ...this.summary, close: bar.close };
  }

  // applyJevUpdate draws an up/down arrow marker on direction changes
  // (docs/components/overview.md §5.1: "LONG転換で上向き矢印"), on the bar
  // of the decision's own time. The first message after (re)connecting is the
  // server's current state, not a change, so it only seeds lastDirection; a
  // repeat of the same direction (or no direction) draws nothing.
  private applyJevUpdate(message: JevUpdateMessage): void {
    const previous = this.lastDirection;
    this.lastDirection = message.direction;
    this.summary = { ...this.summary, direction: message.direction };
    if (!this.candleSeries || !message.direction) return;
    if (previous === null || previous === message.direction) return;
    const time = toBarTime(message.time);
    if (time === null) return;

    const isLong = message.direction === 'LONG';
    const marker: SeriesMarker<Time> = {
      time,
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
      <div
        ${ref(this.containerRef)}
        class="pitha-price-chart-container"
        role="img"
        aria-label=${describeChart(this.summary)}
      ></div>
      ${
        this.empty
          ? html`<p class="pitha-price-chart-empty" role="status">表示できる足がありません（立会時間外、またはまだ市況データが保存されていません）</p>`
          : ''
      }
      ${renderWsDisconnected(this.wsStatus)}
      ${this.error ? html`<p class="pitha-price-chart-error" role="alert">${this.error}</p>` : ''}
    `;
  }

  // `changed.get(...) !== undefined` skips the first update cycle, whose
  // recorded old value is undefined: firstUpdated has already loaded the
  // candles and opened the socket, and redoing both would double them.
  protected override updated(changed: PropertyValues<this>): void {
    if (!this.chart) return;
    const candlesChanged = changed.get('candlesUrl') !== undefined;
    const wsChanged = changed.get('wsUrl') !== undefined;
    // Markers and the last direction belong to the previous symbol.
    if (candlesChanged || wsChanged) this.resetMarkers();
    if (candlesChanged) this.loadInitial();
    if (wsChanged) {
      this.stopWs();
      this.subscribeWs();
    }
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'pitha-price-chart': PithaPriceChart;
  }
}
