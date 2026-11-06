import { afterEach, beforeEach, describe, expect, test } from 'bun:test';
import { installColorSchemeSwitch, setDarkTokens } from '../lib/color-scheme-test-support';
import {
  bucket,
  createdCharts,
  createdSeries,
  installHeatmapHarness,
  mount,
  response,
} from './heatmap-test-support';

await import('./pitha-calibration-heatmap');

installHeatmapHarness();

describe('pitha-calibration-heatmap chart', () => {
  // Series are created perfect-first, then accuracy (initChart order).
  test('plots only non-empty bands on the accuracy curve and every band on the perfect line', async () => {
    await mount(
      response({
        buckets: [
          bucket({ range: '0.50-0.60', sample_count: 0, direction_accuracy: 0 }),
          bucket({ range: '0.70-0.80', sample_count: 12, direction_accuracy: 0.7 }),
        ],
      }),
    );

    const [perfect, accuracy] = createdSeries;
    expect(accuracy.setData).toHaveBeenLastCalledWith([{ time: 75, value: 0.7 }]);
    expect(perfect.setData).toHaveBeenLastCalledWith([
      { time: 55, value: 0.55 },
      { time: 75, value: 0.75 },
    ]);
  });

  // Removing the element removed the chart, but firstUpdated does not run
  // again, so re-inserting it left the curve blank (issue #523).
  test('rebuilds the chart and redraws the curve when re-attached to the DOM', async () => {
    const { el } = await mount(response());

    el.remove();
    expect(createdCharts[0].remove).toHaveBeenCalledTimes(1);

    document.body.appendChild(el);
    await el.updateComplete;

    expect(createdCharts).toHaveLength(2);
    const [, , perfect, accuracy] = createdSeries;
    expect(accuracy.setData).toHaveBeenLastCalledWith([{ time: 75, value: 0.63 }]);
    expect(perfect.setData).toHaveBeenLastCalledWith([{ time: 75, value: 0.75 }]);
  });
});

// canvas colors are read once, so an OS Light/Dark switch must re-apply them (issue #964).
describe('pitha-calibration-heatmap OS theme switch (issue #964)', () => {
  let scheme: ReturnType<typeof installColorSchemeSwitch>;
  beforeEach(() => {
    scheme = installColorSchemeSwitch();
  });
  afterEach(() => {
    scheme.restore();
    document.documentElement.removeAttribute('style');
  });

  test('re-applies the chart and both curve colors from the tokens now in force', async () => {
    await mount(response());

    setDarkTokens();
    scheme.fire();

    const [perfect, accuracy] = createdSeries;
    expect(createdCharts[0].applyOptions).toHaveBeenCalledWith(
      expect.objectContaining({ layout: expect.objectContaining({ textColor: '#8b8b8e' }) }),
    );
    expect(perfect.applyOptions).toHaveBeenCalledWith({ color: '#8b8b8e' });
    expect(accuracy.applyOptions).toHaveBeenCalledWith({ color: '#ffffff' });
  });

  test('re-applies the Light tokens while printing and the Dark ones afterwards', async () => {
    const { el } = await mount(response());
    setDarkTokens();
    const [perfect, accuracy] = createdSeries;

    scheme.firePrint(true);
    expect(perfect.applyOptions).toHaveBeenLastCalledWith({ color: '#6b6b6e' });
    expect(accuracy.applyOptions).toHaveBeenLastCalledWith({ color: '#050505' });

    expect(createdCharts[0].takeScreenshot).toHaveBeenCalled();
    expect(el.shadowRoot?.querySelector('[data-print-snapshot]')).not.toBeNull();
    expect(createdCharts[0].live.style.visibility).toBe('hidden');

    scheme.firePrint(false);
    expect(el.shadowRoot?.querySelector('[data-print-snapshot]')).toBeNull();
    expect(createdCharts[0].live.style.visibility).toBe('');
    expect(perfect.applyOptions).toHaveBeenLastCalledWith({ color: '#8b8b8e' });
    expect(accuracy.applyOptions).toHaveBeenLastCalledWith({ color: '#ffffff' });
  });

  test('stops listening once the element is removed', async () => {
    const { el } = await mount(response());
    expect(scheme.listeners()).toBe(2);

    el.remove();

    expect(scheme.listeners()).toBe(0);
  });
});
