// Shadow-DOM styles of `pitha-price-chart` (Tailwind does not reach into the
// shadow root), split out of the component to keep it within the linterly
// per-file limit.
import { css } from 'lit';

const CHART_HEIGHT = 400;

export const priceChartStyles = css`
  :host {
    display: block;
  }
  .pitha-price-chart-container {
    width: 100%;
    height: ${CHART_HEIGHT}px;
  }
  .pitha-price-chart-empty {
    margin: 0;
    border-radius: var(--r-card);
    background: var(--surface-sel);
    padding: 3rem 1.5rem;
    text-align: center;
    color: var(--mute-on-soft);
    font-size: 0.9375rem;
    line-height: 1.6;
  }
`;
