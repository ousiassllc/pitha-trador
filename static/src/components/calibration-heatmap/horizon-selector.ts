// Horizon switch + "which horizon is shown" line of `pitha-calibration-heatmap`
// (issue #719), split out of the component to keep it within the linterly
// per-file limit.
import { html } from 'lit';
import { HORIZON_OPTIONS, type HorizonOption, horizonLabel } from './calibration-view';

// renderHorizonSelector renders the 5/10/15/全体 buttons (`selected` pressed)
// and, once a response arrived, the horizon its figures were aggregated over
// (`shown`, the response's echo) with the note that realized PnL is
// horizon-independent.
export function renderHorizonSelector(
  selected: HorizonOption,
  shown: HorizonOption | null,
  onSelect: (horizon: HorizonOption) => void,
) {
  return html`
    <div class="pitha-calibration-heatmap-horizons" role="group" aria-label="集計ホライズン">
      ${HORIZON_OPTIONS.map(
        (option) => html`
          <button
            type="button"
            data-testid="calibration-horizon-${option}"
            aria-pressed=${option === selected ? 'true' : 'false'}
            @click=${() => onSelect(option)}
          >
            ${horizonLabel(option)}
          </button>
        `,
      )}
    </div>
    ${
      shown !== null
        ? html`<p class="pitha-calibration-heatmap-horizon" data-testid="calibration-horizon-current">
            集計ホライズン: <strong>${horizonLabel(shown)}</strong>
            ${shown === 'all' ? '（5/10/15分の合算。旧ホライズンのラベルは除外）' : ''}
            <span class="note">実現PnL（trades / JPY）はホライズンに依存しません</span>
          </p>`
        : ''
    }
  `;
}
