import { describe, expect, test } from 'bun:test';
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
