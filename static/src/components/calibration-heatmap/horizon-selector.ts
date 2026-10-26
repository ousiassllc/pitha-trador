// Horizon switch + "which horizon is shown" line of `pitha-calibration-heatmap`,
// split out of the component to keep it within the linterly per-file limit.
import { html } from 'lit';
import { renderHelpMark } from '../lib/help-mark';
import { HORIZON_OPTIONS, type HorizonOption, horizonLabel } from './calibration-view';

// renderHorizonSelector renders the 5/10/15/全体 buttons (`selected` pressed)
// and, once a response arrived, the horizon its figures were aggregated over
// (`shown`, the response's echo); the notes on what the horizon covers sit in
// a help mark.
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
            ${renderHelpMark({
              id: 'help-calibration-horizon',
              label: '集計ホライズンの説明',
              text: `${shown === 'all' ? '全体は 5・10・15 分の合算です（旧ホライズンのラベルは除外）。' : ''}実現損益（取引件数・円）はホライズンに依存しません。`,
            })}
          </p>`
        : ''
    }
  `;
}
