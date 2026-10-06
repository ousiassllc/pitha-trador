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
    border-radius: 0.375rem;
    padding: 0.5rem;
    font-size: 0.75rem;
    /* The text color is set per cell (bucketColors) for contrast with its background. */
    color: #0f172a;
  }
  .pitha-calibration-heatmap-cell .accuracy {
    font-size: 1rem;
    font-weight: 600;
  }
  .pitha-calibration-heatmap-summary {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 0.25rem 1rem;
    margin: 0;
    font-size: 0.875rem;
  }
  .pitha-calibration-heatmap-summary dt {
    color: #475569;
  }
  .pitha-calibration-heatmap-summary dd {
    margin: 0;
    font-variant-numeric: tabular-nums;
  }
`;
