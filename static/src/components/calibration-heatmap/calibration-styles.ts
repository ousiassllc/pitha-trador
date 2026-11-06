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
  .pitha-calibration-heatmap-horizons {
    display: flex;
    gap: 0.25rem;
  }
  .pitha-calibration-heatmap-horizons button[aria-pressed='true'] {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--bone);
  }
  .pitha-calibration-heatmap-horizon {
    margin: 0;
    font-size: 0.875rem;
  }
  .pitha-calibration-heatmap-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(7rem, 1fr));
    gap: 0.5rem;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .pitha-calibration-heatmap-cell {
    display: flex;
    flex-direction: column;
    border-radius: 0.5rem;
    border: 1px solid var(--border);
    padding: 0.5rem;
    font-size: 0.75rem;
    /* --cell-bg / --cell-fg are set per cell (bucketColors): a density ramp of --ink on --surface with --ink text. */
    background: var(--cell-bg, var(--bone));
    color: var(--cell-fg, var(--ink));
  }
  /* A band without labeled samples: flat and dashed (the guide's empty-state border). */
  .pitha-calibration-heatmap-cell[data-empty] {
    border-style: dashed;
  }
  .pitha-calibration-heatmap-cell .accuracy {
    font-size: 1rem;
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
    font-size: 0.875rem;
  }
  .pitha-calibration-heatmap-summary dt {
    color: var(--mute);
  }
  .pitha-calibration-heatmap-summary dd {
    margin: 0;
    font-family: "JetBrains Mono", ui-monospace, monospace;
    font-variant-numeric: tabular-nums;
  }
`;
