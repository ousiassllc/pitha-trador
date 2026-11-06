// Chart colors from the design-guide tokens (brand-guide §8 チャート: series in
// the state colors / --mute / --ink, no prism, axes and grid in --border only).
// lightweight-charts draws on a canvas, which cannot resolve `var(--x)`, so the
// tokens defined in static/src/css/app.css are read from the document once when
// a chart is created (a theme switch needs a new chart). The Light values are
// the fallback where the stylesheet is not loaded (tests, a failed CSS load).

import type { ColorType } from 'lightweight-charts';

const LIGHT_FALLBACK = {
  ink: '#050505',
  mute: '#6b6b6e',
  border: '#e7e7e5',
  surface: '#ffffff',
  success: '#3d5c4a',
  warning: '#7a6a3a',
  info: '#4a5a6a',
} as const;

export type ChartToken = keyof typeof LIGHT_FALLBACK;

/** The `--<token>` value currently in force on the document (Light or Dark). */
export function readToken(token: ChartToken): string {
  const value =
    typeof document === 'undefined'
      ? ''
      : getComputedStyle(document.documentElement).getPropertyValue(`--${token}`).trim();
  return value || LIGHT_FALLBACK[token];
}

/** `#rrggbb` at `alpha` as `rgba()`, which the canvas parser accepts; other formats pass through. */
export function withAlpha(color: string, alpha: number): string {
  const hex = /^#([0-9a-f]{6})$/i.exec(color);
  if (!hex) return color;
  const n = Number.parseInt(hex[1], 16);
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${alpha})`;
}

/** The numeric font of the guide (canvas text does not inherit CSS font-family). */
export const CHART_FONT = '"JetBrains Mono", ui-monospace, monospace';

/**
 * `createChart` options shared by every chart: the guide's font, --mute axis text, --border axes and grid.
 * The crosshair is --mute lines with --ink axis labels (lightweight-charts picks black or white label text
 * from the label's background, so the text flips with the theme); its defaults are off-palette blue-greys.
 */
export function chartBaseOptions() {
  const border = readToken('border');
  const mute = readToken('mute');
  const ink = readToken('ink');
  const crosshairLine = { color: mute, labelBackgroundColor: ink };
  return {
    layout: {
      fontFamily: CHART_FONT,
      textColor: readToken('mute'),
      background: { type: 'solid' as ColorType.Solid, color: 'transparent' },
    },
    grid: { vertLines: { color: border }, horzLines: { color: border } },
    crosshair: { vertLine: crosshairLine, horzLine: crosshairLine },
    rightPriceScale: { borderColor: border },
    timeScale: { borderColor: border },
  };
}
