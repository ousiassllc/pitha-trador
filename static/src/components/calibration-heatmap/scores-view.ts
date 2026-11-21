// Brier / log-loss / ECE summary list of `pitha-calibration-heatmap`. Split out
// of the component to keep it within the linterly per-file limit.
import { html } from 'lit';
import { renderHelpMark } from '../lib/help-mark';

export interface CalibrationScores {
  brierScore: number;
  logLoss: number | null;
  expectedCalibrationError: number | null;
  tradeCount: number;
  pnlBrierScore: number;
  pnlLogLoss: number;
}

export function renderCalibrationScores(s: CalibrationScores) {
  return html`
    <dl class="pitha-calibration-heatmap-summary">
      <dt>
        ブライアスコア
        ${renderHelpMark({
          id: 'help-calibration-brier',
          label: 'ブライアスコアの説明',
          text: '予測した確率と結果（方向が当たったか）のずれを表すスコアです。小さいほど良い値です。',
        })}
      </dt>
      <dd>${s.brierScore.toFixed(3)}</dd>
      <dt>
        対数損失
        ${renderHelpMark({
          id: 'help-calibration-log-loss',
          label: '対数損失の説明',
          text: '予測した確率と結果のずれを、外れた自信の強さに厳しく表すスコアです。小さいほど良い値です。',
        })}
      </dt>
      <dd>${s.logLoss?.toFixed(3)}</dd>
      <dt>
        期待較正誤差
        ${renderHelpMark({
          id: 'help-calibration-ece',
          label: '期待較正誤差の説明',
          text: '予測した確信度と実際の的中率の差を、確信度の帯ごとに平均した値です。小さいほど、確信度どおりに当たっています。',
        })}
      </dt>
      <dd>${s.expectedCalibrationError?.toFixed(3)}</dd>
      ${
        s.tradeCount > 0
          ? html`
            <dt>
              損益ブライアスコア（${s.tradeCount} 件）
              ${renderHelpMark({
                id: 'help-calibration-pnl-brier',
                label: '損益ブライアスコアの説明',
                text: '実際に取引した判断だけを対象に、損益がプラスだったかで測ったブライアスコアです。',
              })}
            </dt>
            <dd>${s.pnlBrierScore.toFixed(3)}</dd>
            <dt>
              損益の対数損失
              ${renderHelpMark({
                id: 'help-calibration-pnl-log-loss',
                label: '損益の対数損失の説明',
                text: '実際に取引した判断だけを対象に、損益がプラスだったかで測った対数損失です。',
              })}
            </dt>
            <dd>${s.pnlLogLoss.toFixed(3)}</dd>
          `
          : ''
      }
    </dl>
  `;
}
