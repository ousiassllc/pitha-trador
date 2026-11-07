// Response shape and pure presentation helpers for `pitha-calibration-heatmap`
// (split out of the component to keep it within the linterly per-file limit).
import type { ABArm } from './ab-arms-view';
import type { LLMCutoffInfo, MetaModel } from './meta-model/meta-model-view';
import type { RetrainSkip } from './meta-model/retrain-skips-view';

// Mirrors docs/api/endpoints.md §GET /api/v1/calibration's `triple_barrier`
// object (internal/web/handler/calibration.calibrationBarrierOutput): the EV
// (net, bps) and tp/sl/timeout reach rates of the triple-barrier labels
// (issue #767). All 0 when sample_count is 0.
export interface BarrierStats {
  sample_count: number;
  tp_count: number;
  sl_count: number;
  timeout_count: number;
  tp_rate: number;
  sl_rate: number;
  timeout_rate: number;
  ev_net_bps: number;
}

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
  trade_win_rate: number;
  triple_barrier: BarrierStats;
}

// Mirrors docs/api/endpoints.md §GET /api/v1/calibration's `by_direction[]`
// item shape (internal/web/handler/calibration.calibrationDirectionOutput).
export interface CalibrationDirection {
  direction: 'LONG' | 'SHORT';
  sample_count: number;
  direction_accuracy: number;
  avg_future_return_pct: number;
}

// The `horizon` query values of GET /api/v1/calibration (judgment horizon in
// minutes, or "all" = every current horizon together; legacy horizons such as
// 20 minutes are never aggregated; "all" also includes every triple-barrier
// label, whose horizon is the Settings timeout). Mirrors
// calibration.HorizonAll and service/calibration.DefaultHorizonsMinutes.
export const HORIZON_OPTIONS = ['5', '10', '15', 'all'] as const;
export type HorizonOption = (typeof HORIZON_OPTIONS)[number];
export const DEFAULT_HORIZON: HorizonOption = 'all';

// horizonLabel is the operator-facing name of a horizon, e.g. "5分" / "全体".
export function horizonLabel(horizon: HorizonOption): string {
  return horizon === 'all' ? '全体' : `${horizon}分`;
}

// calibrationUrlFor appends the horizon selection to the injected
// calibration-url.
export function calibrationUrlFor(baseUrl: string, horizon: HorizonOption): string {
  return `${baseUrl}${baseUrl.includes('?') ? '&' : '?'}horizon=${horizon}`;
}

// Mirrors docs/api/endpoints.md §GET /api/v1/calibration's response body
// (internal/web/handler/calibration.CalibrationAPIOutput).
export interface CalibrationAPIResponse {
  horizon: HorizonOption;
  buckets: CalibrationBucket[];
  by_direction: CalibrationDirection[];
  brier_score: number;
  log_loss: number;
  expected_calibration_error: number;
  trade_count: number;
  pnl_brier_score: number;
  pnl_log_loss: number;
  triple_barrier: BarrierStats;
  // The newest meta-labeling model's OOS metrics (issue #766); omitted until
  // a model was trained.
  meta_model?: MetaModel;
  // The Jev model's training-data cutoff setting with its status and warning
  // (issue #776); shown even before a model was trained.
  meta_model_cutoff?: LLMCutoffInfo;
  // The newest skipped nightly retrainings (issue #799), newest first; shown
  // even before a model was trained; omitted when none was skipped.
  meta_model_skips?: RetrainSkip[];
  // The paper A/B comparison of the momentum and regime-switching arms
  // (issue #755); omitted when no source is wired.
  ab_arms?: ABArm[];
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

export interface CellColors {
  background: string;
  color: string;
}

// The heatmap is a density ramp in the guide's neutrals, not a spectrum
// (brand-guide §1/§8: no off-token hues, no prism; the exact accuracy is also
// printed in every cell, so color is never the only carrier). A cell's
// background mixes --ink into --surface: MIN_INK_PERCENT at 0% accuracy to
// MAX_INK_PERCENT at 100%, with --ink text on top. At MAX_INK_PERCENT the ink
// text still has >= MIN_CONTRAST (WCAG AA, normal text; the cells use 0.875rem
// text) in both Light and Dark, which calibration-view.test.ts verifies from
// the token values, so a theme switch needs no per-cell recomputation.
export const MIN_INK_PERCENT = 4;
export const MAX_INK_PERCENT = 30;
export const MIN_CONTRAST = 4.5;
export const CELL_TEXT_COLOR = 'var(--ink)';

// heatmapColors maps a [0,1] direction_accuracy to the cell colors of the
// confidence-band heatmap (docs/components/overview.md §5.3 "confidence帯別
// カラーヒートマップ"): the more accurate the band, the denser the cell.
export function heatmapColors(directionAccuracy: number): CellColors {
  const accuracy = Math.max(0, Math.min(1, directionAccuracy));
  const percent = Math.round(MIN_INK_PERCENT + accuracy * (MAX_INK_PERCENT - MIN_INK_PERCENT));
  return {
    background: `color-mix(in srgb, var(--ink) ${percent}%, var(--surface))`,
    color: CELL_TEXT_COLOR,
  };
}

// NO_DATA_COLOR is the flat --bone background of a band with no labeled
// samples (its cell also gets a dashed border from calibration-styles), visibly
// distinct from every ramp step.
export const NO_DATA_COLOR = 'var(--bone)';

// bucketColors is a band's heatmap colors: heatmapColors of its observed
// accuracy, or the flat no-data background with --mute text when it has no
// samples (the API returns 0 for every metric of an empty band, which is "no
// data", not "0% accurate").
export function bucketColors(bucket: CalibrationBucket): CellColors {
  return bucket.sample_count > 0
    ? heatmapColors(bucket.direction_accuracy)
    : { background: NO_DATA_COLOR, color: 'var(--mute)' };
}

// applyCellColors sets a band's cell colors as CSSOM custom properties that the
// cell's CSS reads (--cell-bg / --cell-fg), not as a `style` attribute binding:
// the CSP (no `style-src 'unsafe-inline'`) blocks inline style attributes, and
// Lit's styleMap renders one on its first pass.
export function applyCellColors(el: Element | undefined, bucket: CalibrationBucket): void {
  if (!(el instanceof HTMLElement)) return;
  const { background, color } = bucketColors(bucket);
  el.style.setProperty('--cell-bg', background);
  el.style.setProperty('--cell-fg', color);
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

// formatYen renders a signed yen amount, e.g. "+1,200 円" / "-400 円".
export function formatYen(amount: number): string {
  const sign = amount > 0 ? '+' : '';
  return `${sign}${Math.round(amount).toLocaleString('en-US')} 円`;
}
