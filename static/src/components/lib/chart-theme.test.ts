import { afterEach, describe, expect, test } from 'bun:test';
import type { ReactiveControllerHost } from 'lit';
import {
  ColorSchemeController,
  chartBaseOptions,
  clearPrintSnapshot,
  readToken,
  syncPrintSnapshot,
  withAlpha,
} from './chart-theme';
import { installColorSchemeSwitch, setDarkTokens } from './color-scheme-test-support';
import { applyTheme } from './theme';

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

describe('ColorSchemeController', () => {
  test('calls back on each OS theme switch while the host is connected', () => {
    const scheme = installColorSchemeSwitch();
    try {
      let calls = 0;
      const host = { addController: () => {} } as unknown as ReactiveControllerHost;
      const controller = new ColorSchemeController(host, () => calls++);

      controller.hostConnected();
      scheme.fire();
      scheme.fire();
      controller.hostDisconnected();
      scheme.fire();

      expect(calls).toBe(2);
      expect(scheme.listeners()).toBe(0);
    } finally {
      scheme.restore();
    }
  });

  test('calls back when the header switch changes the theme, until disconnected', () => {
    let calls = 0;
    const host = { addController: () => {} } as unknown as ReactiveControllerHost;
    const controller = new ColorSchemeController(host, () => calls++);

    controller.hostConnected();
    applyTheme('dark');
    controller.hostDisconnected();
    applyTheme('light');
    document.documentElement.removeAttribute('data-theme');

    expect(calls).toBe(1);
  });

  describe('print', () => {
    const connect = (onChange: () => void) => {
      const host = { addController: () => {} } as unknown as ReactiveControllerHost;
      const controller = new ColorSchemeController(host, onChange);
      controller.hostConnected();
      return controller;
    };

    test('reads the Light tokens between beforeprint and afterprint under a Dark theme', () => {
      setDarkTokens();
      const seen: string[] = [];
      const controller = connect(() => seen.push(readToken('ink')));

      window.dispatchEvent(new Event('beforeprint'));
      expect(readToken('ink')).toBe('#050505');
      window.dispatchEvent(new Event('afterprint'));
      expect(readToken('ink')).toBe('#ffffff');
      controller.hostDisconnected();

      expect(seen).toEqual(['#050505', '#ffffff']);
    });

    test('follows the print media query too', () => {
      const scheme = installColorSchemeSwitch();
      try {
        setDarkTokens();
        const seen: string[] = [];
        const controller = connect(() => seen.push(readToken('surface')));

        scheme.firePrint(true);
        scheme.firePrint(false);
        controller.hostDisconnected();

        expect(seen).toEqual(['#ffffff', '#141414']);
      } finally {
        scheme.restore();
      }
    });

    test('keeps the Light tokens through an OS theme switch while printing', () => {
      const scheme = installColorSchemeSwitch();
      try {
        setDarkTokens();
        const seen: string[] = [];
        const controller = connect(() => seen.push(readToken('mute')));

        scheme.firePrint(true);
        scheme.fire();
        scheme.firePrint(false);
        controller.hostDisconnected();

        expect(seen).toEqual(['#6b6b6e', '#6b6b6e', '#8b8b8e']);
      } finally {
        scheme.restore();
      }
    });

    // `printing` is module-global: a host disconnected across afterprint never saw it, so a stale
    // `true` must not survive into the next connection.
    test('resyncs from the print media query when connected', () => {
      const scheme = installColorSchemeSwitch();
      try {
        setDarkTokens();
        connect(() => {});
        scheme.firePrint(true);
        expect(readToken('ink')).toBe('#050505');

        // printing ended while no host was listening
        scheme.firePrint(false);
        window.dispatchEvent(new Event('beforeprint')); // stale: left over from a removed host
        const controller = connect(() => {});
        expect(readToken('ink')).toBe('#ffffff');
        controller.hostDisconnected();

        // printing began while no host was listening
        scheme.firePrint(true);
        const second = connect(() => {});
        expect(readToken('ink')).toBe('#050505');
        second.hostDisconnected();
        scheme.firePrint(false);
        connect(() => {}).hostDisconnected();
      } finally {
        scheme.restore();
      }
    });

    test('draws every chart color from the Light tokens while printing under a Dark theme', () => {
      const scheme = installColorSchemeSwitch();
      try {
        setDarkTokens();
        const controller = connect(() => {});
        scheme.firePrint(true);

        const options = chartBaseOptions();
        const colors = [
          options.layout.textColor,
          options.grid.vertLines.color,
          options.grid.horzLines.color,
          options.crosshair.vertLine.color,
          options.crosshair.vertLine.labelBackgroundColor,
          options.rightPriceScale.borderColor,
          options.timeScale.borderColor,
        ];
        const light = new Set(['#050505', '#6b6b6e', '#e7e7e5', '#ffffff']);
        for (const color of colors) expect(light.has(color)).toBe(true);
        expect(colors).not.toContain('#2a2a2a'); // Dark --border

        scheme.firePrint(false);
        controller.hostDisconnected();
      } finally {
        scheme.restore();
      }
    });

    test('stops listening for print once disconnected', () => {
      let calls = 0;
      const controller = connect(() => calls++);
      controller.hostDisconnected();

      window.dispatchEvent(new Event('beforeprint'));
      window.dispatchEvent(new Event('afterprint'));

      expect(calls).toBe(0);
    });
  });

  test('does nothing where matchMedia is unavailable', () => {
    const original = globalThis.matchMedia;
    globalThis.matchMedia = undefined as unknown as typeof matchMedia;
    try {
      const host = { addController: () => {} } as unknown as ReactiveControllerHost;
      const controller = new ColorSchemeController(host, () => {});
      expect(() => {
        controller.hostConnected();
        controller.hostDisconnected();
      }).not.toThrow();
    } finally {
      globalThis.matchMedia = original;
    }
  });
});

describe('print snapshot', () => {
  const setup = () => {
    const container = document.createElement('div');
    const live = document.createElement('div');
    container.append(live);
    const bitmap = document.createElement('canvas');
    const chart = { takeScreenshot: () => bitmap, chartElement: () => live };
    return { container, live, chart };
  };

  afterEach(() => {
    window.dispatchEvent(new Event('afterprint'));
  });

  const startPrint = () => {
    const host = { addController: () => {} } as unknown as ReactiveControllerHost;
    const controller = new ColorSchemeController(host, () => {});
    controller.hostConnected();
    window.dispatchEvent(new Event('beforeprint'));
    return controller;
  };

  // The bitmap is the on-screen size; the print layout has another container width.
  test('keeps the aspect ratio of the bitmap instead of stretching it to the container', () => {
    const { container, chart } = setup();
    const controller = startPrint();

    syncPrintSnapshot(chart);

    const snapshot = container.querySelector<HTMLElement>('[data-print-snapshot]');
    expect(snapshot?.style.objectFit).toBe('contain');
    expect(snapshot?.style.position).toBe('absolute');
    controller.hostDisconnected();
  });

  test('clearPrintSnapshot removes the image and the container position', () => {
    const { container, live, chart } = setup();
    const controller = startPrint();
    syncPrintSnapshot(chart);
    expect(container.style.position).toBe('relative');
    expect(live.style.visibility).toBe('hidden');

    clearPrintSnapshot(container);

    expect(container.querySelector('[data-print-snapshot]')).toBeNull();
    expect(container.style.position).toBe('');
    controller.hostDisconnected();
  });

  test('clearPrintSnapshot tolerates a missing container', () => {
    expect(() => clearPrintSnapshot(null)).not.toThrow();
    expect(() => clearPrintSnapshot(undefined)).not.toThrow();
  });
});
