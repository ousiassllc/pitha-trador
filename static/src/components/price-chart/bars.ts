// 1-minute bar helpers for `pitha-price-chart`.

import type { UTCTimestamp } from 'lightweight-charts';

const BAR_SECONDS = 60;

export interface Bar {
  time: UTCTimestamp;
  open: number;
  high: number;
  low: number;
  close: number;
}

export function toUTCTimestamp(iso: string): UTCTimestamp {
  return Math.floor(new Date(iso).getTime() / 1000) as UTCTimestamp;
}

// foldTick returns the bar to draw after a price tick, or null when the tick
// must be ignored. The tick goes into the 1-minute bar of its snapshot time
// (`snapshotTime`, RFC 3339 - not the client clock, so a stalled feed adds no
// fake bars), matching the candles endpoint's resolution. Ignored are a
// non-positive price (no snapshot yet; it would drag the chart's autoscale to
// 0), an unparsable time, and a snapshot older than the last bar
// (series.update rejects a bar older than the last).
export function foldTick(lastBar: Bar | null, price: number, snapshotTime: string): Bar | null {
  if (!(price > 0)) return null;
  const snapshot = toUTCTimestamp(snapshotTime);
  if (!Number.isFinite(snapshot)) return null;
  const time = (Math.floor(snapshot / BAR_SECONDS) * BAR_SECONDS) as UTCTimestamp;
  if (lastBar && time < lastBar.time) return null;
  if (lastBar && lastBar.time === time) {
    return {
      ...lastBar,
      high: Math.max(lastBar.high, price),
      low: Math.min(lastBar.low, price),
      close: price,
    };
  }
  return { time, open: price, high: price, low: price, close: price };
}
