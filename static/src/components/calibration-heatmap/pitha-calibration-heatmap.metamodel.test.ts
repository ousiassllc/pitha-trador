import { describe, expect, test } from 'bun:test';
import { installHeatmapHarness, mount, response } from './heatmap-test-support';
import type { MetaModel } from './meta-model-view';

// Dynamic: a static import would load the component before the mock exists.
await import('./pitha-calibration-heatmap');

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
  oos_calibration: [{ mean_predicted: 0.55, observed_rate: 0.5, count: 40 }],
  llm_cutoff: '2026-09-01',
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
});
