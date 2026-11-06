import { describe, expect, test } from 'bun:test';
import {
  bucketColors,
  type CalibrationBucket,
  heatmapColors,
  MAX_INK_PERCENT,
  MIN_CONTRAST,
  MIN_INK_PERCENT,
  NO_DATA_COLOR,
} from './calibration-view';

// The guide's tokens (static/src/css/app.css): ink / surface per theme.
const THEMES = {
  light: { ink: '#050505', surface: '#ffffff' },
  dark: { ink: '#ffffff', surface: '#141414' },
};

// An independent WCAG 2.x implementation (sRGB mix -> luminance) so the
// assertions do not reuse the production code.
const channels = (hex: string) =>
  [1, 3, 5].map((i) => Number.parseInt(hex.slice(i, i + 2), 16) / 255);
const luminance = (rgb: number[]) => {
  const [r, g, b] = rgb.map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
};
const contrast = (a: number, b: number) => (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
const inkPercent = (css: string) => Number(/ink\) (\d+)%/.exec(css)?.[1]);

describe('heatmapColors', () => {
  test('is a --ink density ramp on --surface with --ink text: no hue, no hex', () => {
    for (const accuracy of [0, 0.25, 0.5, 1]) {
      const { background, color } = heatmapColors(accuracy);
      expect(background).toMatch(/^color-mix\(in srgb, var\(--ink\) \d+%, var\(--surface\)\)$/);
      expect(color).toBe('var(--ink)');
      expect(background).not.toMatch(/hsl|#[0-9a-f]{3,6}/i);
    }
  });

  test('gets denser as accuracy rises, from MIN to MAX ink', () => {
    expect(inkPercent(heatmapColors(0).background)).toBe(MIN_INK_PERCENT);
    expect(inkPercent(heatmapColors(1).background)).toBe(MAX_INK_PERCENT);
    let previous = -1;
    for (let i = 0; i <= 100; i++) {
      const percent = inkPercent(heatmapColors(i / 100).background);
      expect(percent).toBeGreaterThanOrEqual(previous);
      previous = percent;
    }
  });

  test('keeps --ink text >= 4.5:1 on the densest cell in both Light and Dark', () => {
    for (const { ink, surface } of Object.values(THEMES)) {
      const share = MAX_INK_PERCENT / 100;
      const mixed = channels(ink).map((c, i) => c * share + channels(surface)[i] * (1 - share));
      expect(contrast(luminance(mixed), luminance(channels(ink)))).toBeGreaterThanOrEqual(
        MIN_CONTRAST,
      );
    }
  });

  test('clamps out-of-range accuracy', () => {
    expect(heatmapColors(-1)).toEqual(heatmapColors(0));
    expect(heatmapColors(2)).toEqual(heatmapColors(1));
  });
});

describe('bucketColors', () => {
  const emptyBand = { sample_count: 0, direction_accuracy: 0 } as CalibrationBucket;

  test('uses the flat --bone background and --mute text for a band without samples', () => {
    const colors = bucketColors(emptyBand);
    expect(colors.background).toBe(NO_DATA_COLOR);
    expect(colors.color).toBe('var(--mute)');
    expect(colors.background).not.toBe(heatmapColors(0).background);
  });

  test('uses the ramp for a band that has samples', () => {
    expect(bucketColors({ sample_count: 5, direction_accuracy: 1 } as CalibrationBucket)).toEqual(
      heatmapColors(1),
    );
  });
});
