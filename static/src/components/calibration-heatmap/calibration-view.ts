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

// Heatmap cell text colors: dark slate on light backgrounds, white on dark.
export const DARK_TEXT = '#0f172a';
export const LIGHT_TEXT = '#ffffff';
// WCAG 2.x AA contrast for normal text; the cells use 0.75rem text.
export const MIN_CONTRAST = 4.5;

export interface CellColors {
  background: string;
  color: string;
}

// hslLuminance is the WCAG 2.x relative luminance of an hsl() color
// (hue in degrees, saturation and lightness in [0,1]).
function hslLuminance(hue: number, saturation: number, lightness: number): number {
  const a = saturation * Math.min(lightness, 1 - lightness);
  const channel = (n: number): number => {
    const k = (n + hue / 30) % 12;
    const c = lightness - a * Math.max(-1, Math.min(k - 3, 9 - k, 1));
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * channel(0) + 0.7152 * channel(8) + 0.0722 * channel(4);
}

const DARK_TEXT_LUMINANCE = 0.0088; // relative luminance of #0f172a

// contrastRatio is the WCAG contrast of two relative luminances.
export function contrastRatio(a: number, b: number): number {
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
}

// heatmapColors maps a [0,1] direction_accuracy to a red(low)->green(high)
// background and a text color for the confidence-band heatmap
// (docs/components/overview.md §5.3 "confidence帯別カラーヒートマップ").
// The text is dark slate while that reaches MIN_CONTRAST on hsl(h, 70%, 45%);
// on the red/orange end it switches to white and the background is darkened
// just enough for white to reach MIN_CONTRAST (neither text color does at the
// crossover with the 45% lightness).
export function heatmapColors(directionAccuracy: number): CellColors {
  const hue = Math.max(0, Math.min(1, directionAccuracy)) * 120;
  const luminanceAt = (percent: number): number => hslLuminance(hue, 0.7, percent / 100);
  if (contrastRatio(luminanceAt(45), DARK_TEXT_LUMINANCE) >= MIN_CONTRAST) {
    return { background: `hsl(${hue}, 70%, 45%)`, color: DARK_TEXT };
  }
  let percent = 45;
  while (percent > 0 && contrastRatio(luminanceAt(percent), 1) < MIN_CONTRAST) percent--;
  return { background: `hsl(${hue}, 70%, ${percent}%)`, color: LIGHT_TEXT };
}

// NO_DATA_COLOR is the neutral (grey) heatmap background of a band with no
// labeled samples, visibly distinct from heatmapColors' red(0% accuracy).
export const NO_DATA_COLOR = 'hsl(0, 0%, 75%)';

// bucketColors is a band's heatmap colors: heatmapColors of its observed
// accuracy, or the neutral grey with dark text when it has no samples (the
// API returns 0 for every metric of an empty band, which is "no data", not
// "0% accurate").
export function bucketColors(bucket: CalibrationBucket): CellColors {
  return bucket.sample_count > 0
    ? heatmapColors(bucket.direction_accuracy)
    : { background: NO_DATA_COLOR, color: DARK_TEXT };
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
