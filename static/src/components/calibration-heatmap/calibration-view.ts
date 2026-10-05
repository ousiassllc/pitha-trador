// Response shape and pure presentation helpers for `pitha-calibration-heatmap`
// (split out of the component to keep it within the linterly per-file limit).

// Mirrors docs/api/endpoints.md §GET /api/v1/calibration's `buckets[]` item
// shape (internal/web/handler/calibration.calibrationBucketOutput).
export interface CalibrationBucket {
  range: string;
  avg_confidence: number;
  direction_accuracy: number;
  avg_future_return_pct: number;
  sample_count: number;
  trade_count: number;
  total_pnl: number;
  avg_pnl_pct: number;
}

// Mirrors docs/api/endpoints.md §GET /api/v1/calibration's `by_direction[]`
// item shape (internal/web/handler/calibration.calibrationDirectionOutput).
export interface CalibrationDirection {
  direction: 'LONG' | 'SHORT';
  sample_count: number;
  direction_accuracy: number;
  avg_future_return_pct: number;
}

// Mirrors docs/api/endpoints.md §GET /api/v1/calibration's response body
// (internal/web/handler/calibration.CalibrationAPIOutput).
export interface CalibrationAPIResponse {
  buckets: CalibrationBucket[];
  by_direction: CalibrationDirection[];
  brier_score: number;
  log_loss: number;
  expected_calibration_error: number;
}

// bucketMidpointPct parses e.g. "0.70-0.80" into 75. The reliability
// curve's x-axis is "predicted confidence" (a bucket's midpoint), not a
// real date, but lightweight-charts requires an increasing numeric `Time`
// for every series point - the midpoint, scaled to an integer percent,
// doubles as that x-axis value while the time-scale's date labels stay
// hidden (see initChart in pitha-calibration-heatmap.ts).
export function bucketMidpointPct(range: string): number {
  const [low, high] = range.split('-').map(Number);
  return Math.round(((low + high) / 2) * 100);
}

// heatmapColor maps a [0,1] direction_accuracy to a red(low)->green(high)
// background color for the confidence-band heatmap
// (docs/components/overview.md §5.3 "confidence帯別カラーヒートマップ").
export function heatmapColor(directionAccuracy: number): string {
  const hue = Math.max(0, Math.min(1, directionAccuracy)) * 120;
  return `hsl(${hue}, 70%, 45%)`;
}

// NO_DATA_COLOR is the neutral (grey) heatmap background of a band with no
// labeled samples, visibly distinct from heatmapColor's red(0% accuracy).
export const NO_DATA_COLOR = 'hsl(0, 0%, 75%)';

// bucketColor is a band's heatmap background: heatmapColor of its observed
// accuracy, or NO_DATA_COLOR when it has no samples (the API returns 0 for
// every metric of an empty band, which is "no data", not "0% accurate").
export function bucketColor(bucket: CalibrationBucket): string {
  return bucket.sample_count > 0 ? heatmapColor(bucket.direction_accuracy) : NO_DATA_COLOR;
}

// hasLabeledSamples reports whether the response is based on any labeled
// sample. brier_score/log_loss/expected_calibration_error are 0 (the "best"
// score) when there is none, so they must not be shown then. by_direction
// counts every labeled sample (including any whose confidence falls outside
// all bands), so it is checked alongside the bands.
export function hasLabeledSamples(response: CalibrationAPIResponse): boolean {
  return (
    response.buckets.some((b) => b.sample_count > 0) ||
    response.by_direction.some((d) => d.sample_count > 0)
  );
}

// formatYen renders a signed JPY amount, e.g. "+1,200 JPY" / "-400 JPY".
export function formatYen(amount: number): string {
  const sign = amount > 0 ? '+' : '';
  return `${sign}${Math.round(amount).toLocaleString('en-US')} JPY`;
}
