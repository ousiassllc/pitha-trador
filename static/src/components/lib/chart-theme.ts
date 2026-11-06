// Chart colors from the design-guide tokens (brand-guide §8 チャート: series in
// the state colors / --mute / --ink, no prism, axes and grid in --border only).
// lightweight-charts draws on a canvas, which cannot resolve `var(--x)`, so the
// tokens defined in static/src/css/app.css are read from the document when a
// chart is created and again, via ColorSchemeController, each time the OS or the
// header's theme switch changes between Light and Dark, and when the page goes to or
// comes back from print (brand-guide §8: print is always ink on bone, so a Dark screen
// prints with the Light tokens). The Light values are also the fallback where the
// stylesheet is not loaded (tests, a failed CSS load).

import type { ColorType } from 'lightweight-charts';
import type { ReactiveController, ReactiveControllerHost } from 'lit';
import { THEME_CHANGE_EVENT } from './theme';

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

// True from `beforeprint` (or the `print` media query matching) to `afterprint`. The browser
// fires `beforeprint` while the document still answers with its screen styles, so under a Dark
// theme getComputedStyle would hand back Dark tokens; while printing, the Light tokens are used.
let printing = false;

export function isPrinting(): boolean {
  return printing;
}

/** The `--<token>` value currently in force on the document: Light while printing, else Light or Dark. */
export function readToken(token: ChartToken): string {
  if (printing) return LIGHT_FALLBACK[token];
  const value =
    typeof document === 'undefined'
      ? ''
      : getComputedStyle(document.documentElement).getPropertyValue(`--${token}`).trim();
  return value || LIGHT_FALLBACK[token];
}

/**
 * Calls `onChange` while the host is connected each time the theme switches
 * between Light and Dark: the OS setting (app.css follows `prefers-color-scheme`
 * unless `<html data-theme>` forces a theme) or the header's switch
 * (THEME_CHANGE_EVENT, lib/theme.ts). It also fires on `beforeprint` / `afterprint` and on
 * the `print` media query changing, after switching readToken to the Light tokens for the
 * duration of the print. A canvas keeps the colors it was given,
 * so each chart re-applies its tokens then.
 */
export class ColorSchemeController implements ReactiveController {
  private query: MediaQueryList | null = null;
  private printQuery: MediaQueryList | null = null;

  constructor(
    host: ReactiveControllerHost,
    private readonly onChange: () => void,
  ) {
    host.addController(this);
  }

  private readonly setPrinting = (value: boolean): void => {
    printing = value;
    this.onChange();
  };
  private readonly beforePrint = (): void => this.setPrinting(true);
  private readonly afterPrint = (): void => this.setPrinting(false);
  private readonly printChange = (event: { matches: boolean }): void =>
    this.setPrinting(event.matches);

  hostConnected(): void {
    // `printing` is module-global and only the listeners below keep it current: a host that was
    // disconnected across `afterprint` (or `beforeprint`) missed it, so resync from the media query.
    if (typeof matchMedia === 'function') printing = matchMedia('print').matches;
    document.addEventListener(THEME_CHANGE_EVENT, this.onChange);
    window.addEventListener('beforeprint', this.beforePrint);
    window.addEventListener('afterprint', this.afterPrint);
    if (typeof matchMedia !== 'function') return;
    this.query = matchMedia('(prefers-color-scheme: dark)');
    this.query.addEventListener('change', this.onChange);
    this.printQuery = matchMedia('print');
    this.printQuery.addEventListener('change', this.printChange);
  }

  hostDisconnected(): void {
    document.removeEventListener(THEME_CHANGE_EVENT, this.onChange);
    window.removeEventListener('beforeprint', this.beforePrint);
    window.removeEventListener('afterprint', this.afterPrint);
    this.query?.removeEventListener('change', this.onChange);
    this.printQuery?.removeEventListener('change', this.printChange);
    this.query = null;
    this.printQuery = null;
  }
}

const PRINT_SNAPSHOT_ATTR = 'data-print-snapshot';

interface PrintableChart {
  takeScreenshot(): HTMLCanvasElement;
  chartElement(): HTMLElement;
}

/**
 * Makes `chart` print in the colors it holds now. Chrome prints a canvas that was drawn after
 * `beforeprint` from its recorded draw calls, and lightweight-charts starts every frame with a
 * clear (clearRect) that the PDF backend turns into a mask over the whole canvas: the repainted
 * chart would print blank. A canvas that only receives `drawImage` prints fine, so while
 * printing, the live chart is hidden and its `takeScreenshot()` (which also flushes the pending
 * repaint synchronously, rAF does not run before the print) is laid over it as a still image.
 * Outside of print the snapshot is removed and the live chart shown again. Call after the
 * chart's colors were re-applied.
 */
export function syncPrintSnapshot(chart: PrintableChart): void {
  const live = chart.chartElement();
  const container = live.parentElement;
  if (!container) return;
  if (!isPrinting()) {
    clearPrintSnapshot(container);
    live.style.visibility = '';
    return;
  }
  container.querySelector(`[${PRINT_SNAPSHOT_ATTR}]`)?.remove();
  const snapshot = chart.takeScreenshot();
  snapshot.setAttribute(PRINT_SNAPSHOT_ATTR, '');
  // The bitmap has the size the chart had on screen (beforeprint fires before the print layout), the
  // container may be narrower or wider in print: `contain` keeps the aspect ratio so text is not squashed.
  snapshot.style.cssText =
    'position:absolute;inset:0;width:100%;height:100%;object-fit:contain;object-position:left top';
  container.style.position = 'relative';
  container.append(snapshot);
  live.style.visibility = 'hidden';
}

/**
 * Undoes syncPrintSnapshot on `container` (the element the chart was created in): removes the still
 * image and the `position` it set. Call it when the chart is torn down while the snapshot may be
 * showing, e.g. a host disconnected during a print: the container is reused on reconnect, and an old
 * image would otherwise cover the new chart. The live chart element is shown again by the caller's
 * own teardown (chart.remove()) or by syncPrintSnapshot.
 */
export function clearPrintSnapshot(container: HTMLElement | null | undefined): void {
  if (!container) return;
  container.querySelector(`[${PRINT_SNAPSHOT_ATTR}]`)?.remove();
  container.style.position = '';
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
