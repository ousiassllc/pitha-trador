// Shadow-DOM styles of `pitha-calibration-heatmap` (Tailwind does not reach
// into the shadow root), split out of the component to keep it within the
// linterly per-file limit.
import { css } from 'lit';

export const CHART_HEIGHT = 300;

export const heatmapStyles = css`
  :host {
    display: block;
  }
  .pitha-calibration-heatmap {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }
  .pitha-calibration-heatmap-chart {
    width: 100%;
    height: ${CHART_HEIGHT}px;
  }
  .pitha-calibration-heatmap button {
    align-self: flex-start;
  }
  /* Segmented control (brand-guide app UI, sub-nav): no container ground; the
     selected segment is a --surface-sel pill and SemiBold (selection is not
     colour alone, WCAG 1.4.1), the others are transparent and take --surface-sel
     on hover. --accent fill is for primary actions only. */
  .pitha-calibration-heatmap-horizons {
    display: inline-flex;
    align-self: flex-start;
    gap: 0.25rem;
  }
  .pitha-calibration-heatmap-horizons button {
    background: transparent;
  }
  .pitha-calibration-heatmap-horizons button:hover:not(:disabled) {
    background: var(--surface-sel);
  }
  .pitha-calibration-heatmap-horizons button[aria-pressed='true'],
  .pitha-calibration-heatmap-horizons button[aria-pressed='true']:hover:not(:disabled) {
    background: var(--surface-sel);
    font-weight: 600;
  }
  .pitha-calibration-heatmap-horizon {
    margin: 0;
    font-size: 0.9375rem;
    line-height: 1.6;
  }
  .pitha-calibration-heatmap-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(9rem, 1fr));
    gap: 0.5rem;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .pitha-calibration-heatmap-cell {
    display: flex;
    flex-direction: column;
    border-radius: var(--r-card);
    padding: 0.5rem 0.75rem;
    font-size: 0.9375rem;
    line-height: 1.6;
    /* --cell-bg / --cell-fg are set per cell (bucketColors): a density ramp of --ink on --surface with --ink text. */
    background: var(--cell-bg, var(--surface-sel));
    color: var(--cell-fg, var(--ink));
  }
  /* The band label is a caption (13px, the guide's note step); the value rows keep 15px. */
  .pitha-calibration-heatmap-cell .range {
    font-size: 0.8125rem;
  }
  .pitha-calibration-heatmap-cell .accuracy {
    font-size: 0.9375rem;
    font-weight: 400;
    font-family: "JetBrains Mono", ui-monospace, monospace;
    font-variant-numeric: tabular-nums;
  }
  /* In-sentence emphasis is the --ink color, not a heavier weight. */
  .pitha-calibration-heatmap-emphasis {
    color: var(--ink);
  }
  .pitha-calibration-heatmap-cell .bucket-barrier {
    margin-top: 0.25rem;
  }
  .pitha-calibration-heatmap-summary {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 0.25rem 1rem;
    margin: 0;
    font-size: 0.9375rem;
    line-height: 1.6;
  }
  .pitha-calibration-heatmap-summary dt {
    color: var(--mute);
  }
  .pitha-calibration-heatmap-summary dd {
    margin: 0;
    font-family: "JetBrains Mono", ui-monospace, monospace;
    font-variant-numeric: tabular-nums;
  }
  /* A value that is wording (a verdict, a reason) is set in the text font, not Mono. */
  .pitha-calibration-heatmap-summary dd.text {
    font-family: inherit;
    font-variant-numeric: normal;
  }
`;
