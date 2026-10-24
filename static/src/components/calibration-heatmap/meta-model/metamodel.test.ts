import { describe, expect, test } from 'bun:test';
import { installHeatmapHarness, mount, response } from '../heatmap-test-support';
import type { LLMCutoffInfo, MetaModel } from './meta-model-view';

// Dynamic: a static import would load the component before the mock exists.
await import('../pitha-calibration-heatmap');

installHeatmapHarness();

const text = (el: HTMLElement, testid: string): string | undefined =>
  el.shadowRoot
    ?.querySelector(`[data-testid="${testid}"]`)
    ?.textContent?.replace(/\s+/g, ' ')
    .trim();

const metaModel = (overrides: Partial<MetaModel> = {}): MetaModel => ({
  version: 'v3',
  algorithm: 'logistic',
  status: 'active',
  reject_reason: '',
  active_version: 'v3',
  trained_at: '2026-10-14T09:00:00Z',
  train_start: '2026-09-01T00:00:00Z',
  train_end: '2026-10-01T00:00:00Z',
  oos_start: '2026-10-01T00:00:00Z',
  oos_end: '2026-10-14T00:00:00Z',
  train_samples: 300,
  oos_samples: 120,
  oos_auc: 0.612,
  oos_ev_bps: 3.5,
  oos_takes: 0,
  oos_days: 0,
  oos_ev_verdict: 'no_data',
  min_detectable_edge_bps: 11,
  oos_calibration: [{ mean_predicted: 0.55, observed_rate: 0.5, count: 40 }],
  llm_cutoff: '2026-09-01',
  ...overrides,
});

const cutoffInfo = (overrides: Partial<LLMCutoffInfo> = {}): LLMCutoffInfo => ({
  llm_cutoff: '2026-09-01',
  status: 'confirmed',
  warning: '',
  model: '',
  confirmed_on: '',
  source: '',
  ...overrides,
});

// Issue #766: the screen shows the model's OOS AUC / calibration curve / EV
// and the Jev evaluation period.
describe('pitha-calibration-heatmap meta-labeling model', () => {
  test('renders the OOS AUC, EV, sample counts and the Jev evaluation period', async () => {
    const { el } = await mount(response({ meta_model: metaModel() }));

    expect(text(el, 'calibration-metamodel-version')).toBe('v3（logistic）');
    expect(text(el, 'calibration-metamodel-status')).toBe('採用中');
    expect(text(el, 'calibration-metamodel-auc')).toBe('0.612');
    expect(text(el, 'calibration-metamodel-ev')).toBe('+3.5 bps');
    expect(text(el, 'calibration-metamodel-samples')).toBe('300 / 120');
    expect(text(el, 'calibration-metamodel-period')).toBe(
      '2026-10-01 09:00:00 JST 〜 2026-10-14 09:00:00 JST',
    );
    expect(text(el, 'calibration-metamodel-cutoff')).toBe('2026-09-01');
    expect(el.shadowRoot?.querySelector('[data-testid="calibration-metamodel-none"]')).toBeNull();
  });

  test('renders the OOS reliability curve rows', async () => {
    const { el } = await mount(
      response({
        meta_model: metaModel({
          oos_calibration: [
            { mean_predicted: 0.55, observed_rate: 0.5, count: 40 },
            { mean_predicted: 0.65, observed_rate: 0.7, count: 25 },
          ],
        }),
      }),
    );

    const rows = el.shadowRoot?.querySelectorAll('[data-testid="calibration-metamodel-curve-row"]');
    expect(rows?.length).toBe(2);
    expect(rows?.[0]?.textContent?.replace(/\s+/g, ' ').trim()).toBe('55.0% 50.0% 40');
    expect(rows?.[1]?.textContent?.replace(/\s+/g, ' ').trim()).toBe('65.0% 70.0% 25');
  });

  test('shows a rejected newest model with its reason and the model still in force', async () => {
    const { el } = await mount(
      response({
        meta_model: metaModel({
          status: 'rejected',
          reject_reason: 'OOS AUC が基準未満',
          active_version: 'v2',
        }),
      }),
    );

    expect(text(el, 'calibration-metamodel-status')).toBe(
      '不採用（OOS AUC が基準未満）— 採用中のモデルは v2 のまま',
    );
  });

  test('says no model is in force when the rejected model has no predecessor', async () => {
    const { el } = await mount(
      response({
        meta_model: metaModel({ status: 'rejected', reject_reason: 'x', active_version: '' }),
      }),
    );

    expect(text(el, 'calibration-metamodel-status')).toContain('採用中のモデルなし');
  });

  test('shows a dash for an undefined AUC / EV and no curve table without bins', async () => {
    const { el } = await mount(
      response({ meta_model: metaModel({ oos_auc: null, oos_ev_bps: null, oos_calibration: [] }) }),
    );

    expect(text(el, 'calibration-metamodel-auc')).toBe('—');
    expect(text(el, 'calibration-metamodel-ev')).toBe('—');
    expect(el.shadowRoot?.querySelector('[data-testid="calibration-metamodel-curve"]')).toBeNull();
  });

  test('says no model was trained yet when the response has no meta_model', async () => {
    const { el } = await mount(response());

    expect(el.shadowRoot?.querySelector('[data-testid="calibration-metamodel"]')).toBeNull();
    expect(text(el, 'calibration-metamodel-none')).toContain('未学習');
  });

  // Issue #776: a placeholder cutoff is flagged on the screen, even before any
  // model exists, and the rejection reason is readable.
  test('warns while the LLM cutoff is the placeholder, with or without a model', async () => {
    const placeholder = cutoffInfo({
      llm_cutoff: '2026-01-01',
      status: 'placeholder',
      warning: '仮置きのままです',
    });
    const withModel = await mount(
      response({
        meta_model: metaModel({
          status: 'rejected',
          reject_reason: 'llm_cutoff_placeholder',
          active_version: 'v2',
        }),
        meta_model_cutoff: placeholder,
      }),
    );
    expect(text(withModel.el, 'calibration-metamodel-cutoff-warning')).toBe('仮置きのままです');
    expect(text(withModel.el, 'calibration-metamodel-status')).toContain(
      'LLM 学習データの終わりが仮置きのまま',
    );

    document.body.innerHTML = '';
    const noModel = await mount(response({ meta_model_cutoff: placeholder }));
    expect(text(noModel.el, 'calibration-metamodel-cutoff-warning')).toBe('仮置きのままです');
    expect(text(noModel.el, 'calibration-metamodel-none')).toContain('未学習');
  });

  test('shows no warning but the recorded provenance once the cutoff is confirmed', async () => {
    const { el } = await mount(
      response({
        meta_model: metaModel(),
        meta_model_cutoff: cutoffInfo({
          model: 'jev-x',
          confirmed_on: '2026-10-17',
          source: 'vendor docs',
        }),
      }),
    );

    expect(text(el, 'calibration-metamodel-cutoff-warning')).toBeUndefined();
    expect(text(el, 'calibration-metamodel-cutoff-provenance')).toContain(
      'jev-x / 2026-10-17 / vendor docs',
    );
  });

  // Issue #780: the criteria, the intervals, the comparison, a rate-limit gate
  // and the forward evaluation are shown, and the new reject reasons are readable.
  test('shows how the model was judged: counts, intervals, criteria, comparison, gate, forward', async () => {
    const { el } = await mount(
      response({
        meta_model: metaModel({
          status: 'rejected',
          reject_reason: 'model_swap_rate_limited',
          oos_takes: 600,
          oos_days: 6,
          oos_ev_ci: { low: 0.5, high: 7 },
          oos_auc_ci: { low: 0.512, high: 0.571 },
          criteria: {
            min_oos_samples: 1084,
            min_takes: 542,
            min_oos_days: 5,
            min_ev_lower_bps: 0,
            min_auc_lower: 0.5,
            alpha: 0.01,
            resamples: 2000,
          },
          comparison: {
            active_version: 'v2',
            comparable: true,
            note: '',
            paired_samples: 1200,
            candidate_bps: 2,
            active_bps: 1,
            diff_bps: 1,
            diff_ci: { low: 0.2, high: 1.8 },
          },
          gate: {
            reason: 'model_swap_rate_limited',
            detail: '2 of 5 trading days',
            until: '2026-10-21',
          },
          forward: {
            evaluated_at: '2026-10-20T09:00:00Z',
            from: '2026-10-14T09:00:00Z',
            samples: 300,
            takes: 100,
            days: 3,
            ev_bps: -4,
            ev_ci: { low: -7, high: -1 },
            degraded: true,
          },
        }),
      }),
    );

    expect(text(el, 'calibration-metamodel-status')).toContain('モデル入れ替えの頻度制限中');
    expect(text(el, 'calibration-metamodel-takes')).toBe('600 / 6');
    expect(text(el, 'calibration-metamodel-ev-ci')).toContain('〜');
    expect(text(el, 'calibration-metamodel-auc-ci')).toBe('0.512 〜 0.571');
    expect(text(el, 'calibration-metamodel-criteria')).toContain('98%信頼区間');
    expect(text(el, 'calibration-metamodel-criteria')).toContain('542');
    expect(text(el, 'calibration-metamodel-comparison')).toContain('v2');
    expect(text(el, 'calibration-metamodel-gate')).toContain('2026-10-21');
    expect(text(el, 'calibration-metamodel-forward')).toContain('損失が続いています');
  });

  test('omits the adoption rows a legacy model does not have', async () => {
    const { el } = await mount(response({ meta_model: metaModel() }));

    expect(text(el, 'calibration-metamodel-criteria')).toBeUndefined();
    expect(text(el, 'calibration-metamodel-comparison')).toBeUndefined();
    expect(text(el, 'calibration-metamodel-gate')).toBeUndefined();
    expect(text(el, 'calibration-metamodel-forward')).toBeUndefined();
  });

  // Issue #780: an EV below the detectable edge is 判定不能, no takes stays
  // データ無し, and the cut-off is never presented as proof of safety.
  describe('OOS EV verdict', () => {
    test('shows データ無し while the model took no OOS signal', async () => {
      const { el } = await mount(response({ meta_model: metaModel() }));

      expect(text(el, 'calibration-metamodel-ev-verdict')).toContain('データ無し');
    });

    test('shows 判定不能 with the detectable-edge line for an edge below it', async () => {
      const { el } = await mount(
        response({
          meta_model: metaModel({
            oos_takes: 600,
            oos_ev_bps: 4,
            oos_ev_ci: { low: -3, high: 11 },
            oos_ev_verdict: 'undetermined',
            min_detectable_edge_bps: 11,
          }),
        }),
      );

      const verdict = text(el, 'calibration-metamodel-ev-verdict');
      expect(verdict).toContain('判定不能');
      expect(verdict).toContain('約11 bps 未満のエッジは検出力不足で見分けられません');
      expect(verdict).toContain('リターンの保証ではありません');
      expect(verdict).toContain('足切りであり、安全の証明ではありません');
    });

    test('shows 有意なエッジなし, not 判定不能, for a large EV whose interval straddles 0', async () => {
      const { el } = await mount(
        response({
          meta_model: metaModel({
            oos_takes: 600,
            oos_ev_bps: 15,
            oos_ev_ci: { low: -2, high: 32 },
            oos_ev_verdict: 'not_significant',
          }),
        }),
      );

      const verdict = text(el, 'calibration-metamodel-ev-verdict');
      expect(verdict).toContain('有意なエッジなし');
      expect(verdict).not.toContain('判定不能');
    });

    test('shows a positive verdict only with the cut-off wording', async () => {
      const { el } = await mount(
        response({
          meta_model: metaModel({
            oos_takes: 600,
            oos_ev_bps: 15,
            oos_ev_ci: { low: 3, high: 27 },
            oos_ev_verdict: 'positive',
          }),
        }),
      );

      const verdict = text(el, 'calibration-metamodel-ev-verdict');
      expect(verdict).toContain('正のエッジ');
      expect(verdict).not.toContain('判定不能');
      expect(verdict).toContain('安全の証明ではありません');
    });
  });
});
