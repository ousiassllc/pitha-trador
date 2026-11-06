import { afterEach, describe, expect, test } from 'bun:test';
import { chartBaseOptions, readToken, withAlpha } from './chart-theme';

afterEach(() => {
  document.documentElement.removeAttribute('style');
});

describe('readToken', () => {
  test('falls back to the Light token where the stylesheet is not loaded', () => {
    expect(readToken('success')).toBe('#3d5c4a');
    expect(readToken('border')).toBe('#e7e7e5');
  });

  test('reads the value in force on the document (e.g. Dark)', () => {
    document.documentElement.style.setProperty('--success', ' #8fafa0 ');
    expect(readToken('success')).toBe('#8fafa0');
  });
});

describe('withAlpha', () => {
  test('turns #rrggbb into rgba() for the canvas', () => {
    expect(withAlpha('#6b6b6e', 0.5)).toBe('rgba(107, 107, 110, 0.5)');
  });

  test('leaves other formats alone', () => {
    expect(withAlpha('rgb(1, 2, 3)', 0.5)).toBe('rgb(1, 2, 3)');
  });
});

describe('chartBaseOptions', () => {
  test('uses the numeric font, --mute text and --border axes and grid only', () => {
    const options = chartBaseOptions();
    expect(options.layout.fontFamily).toBe('"JetBrains Mono", ui-monospace, monospace');
    expect(options.layout.textColor).toBe(readToken('mute'));
    expect(options.grid.horzLines.color).toBe(readToken('border'));
    expect(options.grid.vertLines.color).toBe(readToken('border'));
    expect(options.rightPriceScale.borderColor).toBe(readToken('border'));
    expect(options.timeScale.borderColor).toBe(readToken('border'));
  });

  // lightweight-charts' defaults are #9598A1 lines and #131722 / #4c525e label
  // backgrounds, none of them a guide token.
  test('draws the crosshair in --mute with --ink axis labels, in Light and Dark', () => {
    const light = chartBaseOptions();
    expect(light.crosshair.vertLine).toEqual({ color: '#6b6b6e', labelBackgroundColor: '#050505' });
    expect(light.crosshair.horzLine).toEqual({ color: '#6b6b6e', labelBackgroundColor: '#050505' });

    document.documentElement.style.setProperty('--mute', '#8b8b8e');
    document.documentElement.style.setProperty('--ink', '#ffffff');
    const dark = chartBaseOptions();
    expect(dark.crosshair.vertLine).toEqual({ color: '#8b8b8e', labelBackgroundColor: '#ffffff' });
    expect(dark.crosshair.horzLine).toEqual({ color: '#8b8b8e', labelBackgroundColor: '#ffffff' });
  });
});
