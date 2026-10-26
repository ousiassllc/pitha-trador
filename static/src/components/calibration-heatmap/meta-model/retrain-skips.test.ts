import { describe, expect, test } from 'bun:test';
import { installHeatmapHarness, mount, response } from '../heatmap-test-support';

// Dynamic: a static import would load the component before the mock exists.
await import('../pitha-calibration-heatmap');

installHeatmapHarness();

const rows = (el: HTMLElement, testid: string): string[] =>
  Array.from(el.shadowRoot?.querySelectorAll(`[data-testid="${testid}"]`) ?? []).map(
    (n) => n.textContent?.replace(/\s+/g, ' ').trim() ?? '',
  );

// Issue #799: a retraining skipped for lack of data is on the screen with the
// shortfall, even though no model was ever trained.
describe('pitha-calibration-heatmap retrain skips', () => {
  test('lists each skip as "必要N件中 x件" with the fixed code, without a model', async () => {
    const { el } = await mount(
      response({
        meta_model_skips: [
          {
            skipped_at: '2026-10-19T18:00:00Z',
            reason_code: 'too_few_rows_before_oos',
            usable_rows: 87,
            required_rows: 130,
          },
          {
            skipped_at: '2026-10-18T18:00:00Z',
            reason_code: 'something_new',
            usable_rows: 0,
            required_rows: 130,
          },
        ],
      }),
    );

    expect(rows(el, 'calibration-retrain-skip-rows')).toEqual([
      '必要130件中 87件',
      '必要130件中 0件',
    ]);
    const reasons = Array.from(
      el.shadowRoot?.querySelectorAll('[data-testid="calibration-retrain-skip-reason"]') ?? [],
    );
    expect(reasons.map((r) => r.getAttribute('data-code'))).toEqual([
      'too_few_rows_before_oos',
      'something_new',
    ]);
    expect(reasons[0]?.textContent).toContain('検証期間より前のラベル付き判断が不足');
    // an unknown code is never printed: the generic name shows, the code stays in data-code
    expect(reasons[1]?.textContent?.trim()).toBe('その他（理由コードあり）');
  });

  test('shows nothing when no retraining was skipped', async () => {
    const { el } = await mount(response({ meta_model_skips: [] }));
    expect(el.shadowRoot?.querySelector('[data-testid="calibration-retrain-skips"]')).toBeNull();
  });
});
