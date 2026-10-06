// Pure helpers for `pitha-price-chart`: series data derivation, the pane
// layout and the text alternative of the canvas.

import type { LineData, WhitespaceData } from 'lightweight-charts';
import { toUTCTimestamp } from './bars';

// The candlestick/VWAP pane sits in the top 65% and the volume histogram in
// the bottom 20%. Without margins the overlay volume scale defaults to the
// 20%-90% band and its opaque bars cover the candles and the VWAP line.
export const MAIN_SCALE_MARGINS = { top: 0.1, bottom: 0.25 };
export const VOLUME_SCALE_MARGINS = { top: 0.8, bottom: 0 };
export const VOLUME_COLOR = 'rgba(156, 163, 175, 0.5)';

/**
 * VWAP line data. A `vwap` of 0 means "no usable value" (nothing traded yet;
 * the server cannot send null), and plotting it would stretch the price scale
 * down to 0, so such candles become whitespace: the time axis keeps its slot
 * and the line has a gap.
 */
export function vwapSeriesData(
  candles: readonly { time: string; vwap: number }[],
): (LineData | WhitespaceData)[] {
  return candles.map((c) => {
    const time = toUTCTimestamp(c.time);
    return c.vwap > 0 ? { time, value: c.vwap } : { time };
  });
}

/** What the chart currently shows, as plain values for the accessible name. */
export interface ChartSummary {
  symbol: string | null;
  close: number | null;
  vwap: number | null;
  direction: string | null;
}

export const EMPTY_SUMMARY: ChartSummary = {
  symbol: null,
  close: null,
  vwap: null,
  direction: null,
};

/** The accessible name of the chart canvas (role="img"); never empty. */
export function describeChart(summary: ChartSummary): string {
  const subject = summary.symbol ? `${summary.symbol} の` : '';
  const parts: string[] = [];
  if (summary.close !== null) parts.push(`最新終値 ${summary.close}`);
  if (summary.vwap !== null) parts.push(`VWAP ${summary.vwap}`);
  if (summary.direction) parts.push(`Jev方向 ${summary.direction}`);
  const title = `${subject}価格チャート（ローソク足・VWAP・出来高）`;
  return parts.length > 0 ? `${title}: ${parts.join('、')}` : `${title}: データなし`;
}
