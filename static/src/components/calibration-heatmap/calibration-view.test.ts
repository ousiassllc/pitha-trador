import { describe, expect, test } from 'bun:test';
import {
  bucketColors,
  type CalibrationBucket,
  contrastRatio,
  DARK_TEXT,
  heatmapColors,
  LIGHT_TEXT,
  MIN_CONTRAST,
  NO_DATA_COLOR,
} from './calibration-view';

// An independent WCAG 2.x implementation (hsl -> sRGB -> luminance) so the
// assertions do not reuse the production color math.
function luminanceOf(css: string): number {
  const hsl = /^hsl\(([\d.]+), (\d+)%, (\d+)%\)$/.exec(css);
  let rgb: number[];
  if (hsl) {
    const h = Number(hsl[1]);
    const s = Number(hsl[2]) / 100;
    const l = Number(hsl[3]) / 100;
    const chroma = (1 - Math.abs(2 * l - 1)) * s;
    const x = chroma * (1 - Math.abs(((h / 60) % 2) - 1));
    const m = l - chroma / 2;
    const sector = Math.min(5, Math.floor(h / 60));
    const [r, g, b] = [
      [chroma, x, 0],
      [x, chroma, 0],
      [0, chroma, x],
      [0, x, chroma],
      [x, 0, chroma],
      [chroma, 0, x],
    ][sector];
    rgb = [r + m, g + m, b + m];
  } else {
    rgb = [1, 3, 5].map((i) => Number.parseInt(css.slice(i, i + 2), 16) / 255);
  }
  const [r, g, b] = rgb.map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

describe('heatmapColors', () => {
  test('keeps text/background contrast >= 4.5:1 over the whole direction_accuracy range', () => {
    for (let i = 0; i <= 100; i++) {
      const { background, color } = heatmapColors(i / 100);
      const ratio = contrastRatio(luminanceOf(background), luminanceOf(color));
      expect(ratio).toBeGreaterThanOrEqual(MIN_CONTRAST);
    }
  });

  test('switches to white text on the low-accuracy (red) end and keeps dark text on green', () => {
    expect(heatmapColors(0).color).toBe(LIGHT_TEXT);
    expect(heatmapColors(0.1).color).toBe(LIGHT_TEXT);
    expect(heatmapColors(0.5).color).toBe(DARK_TEXT);
    expect(heatmapColors(1).color).toBe(DARK_TEXT);
  });

  test('keeps the red -> green hue ramp and the unchanged mid/high background', () => {
    expect(heatmapColors(0).background).toMatch(/^hsl\(0, 70%, \d+%\)$/);
    expect(heatmapColors(1).background).toBe('hsl(120, 70%, 45%)');
  });

  test('clamps out-of-range accuracy', () => {
    expect(heatmapColors(-1)).toEqual(heatmapColors(0));
    expect(heatmapColors(2)).toEqual(heatmapColors(1));
  });
});

describe('bucketColors', () => {
  const emptyBand = { sample_count: 0, direction_accuracy: 0 } as CalibrationBucket;

  test('uses the neutral grey with readable dark text for a band without samples', () => {
    const colors = bucketColors(emptyBand);
    expect(colors.background).toBe(NO_DATA_COLOR);
    expect(
      contrastRatio(luminanceOf(colors.background), luminanceOf(colors.color)),
    ).toBeGreaterThanOrEqual(MIN_CONTRAST);
  });
});
