import { afterEach, describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import {
  applyTheme,
  isThemeChoice,
  setTheme,
  storedTheme,
  THEME_CHANGE_EVENT,
  THEME_STORAGE_KEY,
} from './theme';

const html = () => document.documentElement;

afterEach(() => {
  localStorage.clear();
  html().removeAttribute('data-theme');
});

describe('storedTheme', () => {
  test('is auto when nothing, or something unknown, is saved', () => {
    expect(storedTheme()).toBe('auto');
    localStorage.setItem(THEME_STORAGE_KEY, 'sepia');
    expect(storedTheme()).toBe('auto');
  });

  test('returns a saved light or dark', () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'dark');
    expect(storedTheme()).toBe('dark');
    localStorage.setItem(THEME_STORAGE_KEY, 'light');
    expect(storedTheme()).toBe('light');
  });

  test('is auto where storage throws', () => {
    const original = Object.getOwnPropertyDescriptor(globalThis, 'localStorage');
    Object.defineProperty(globalThis, 'localStorage', {
      configurable: true,
      get: () => {
        throw new Error('blocked');
      },
    });
    try {
      expect(storedTheme()).toBe('auto');
      expect(() => setTheme('dark')).not.toThrow();
      expect(html().getAttribute('data-theme')).toBe('dark');
    } finally {
      if (original) Object.defineProperty(globalThis, 'localStorage', original);
    }
  });
});

describe('setTheme', () => {
  test('forces light or dark on <html> and saves it', () => {
    setTheme('dark');
    expect(html().getAttribute('data-theme')).toBe('dark');
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark');

    setTheme('light');
    expect(html().getAttribute('data-theme')).toBe('light');
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('light');
  });

  test('auto drops the attribute and the saved choice so the OS decides again', () => {
    setTheme('dark');
    setTheme('auto');
    expect(html().hasAttribute('data-theme')).toBe(false);
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBeNull();
  });
});

describe('applyTheme', () => {
  test('announces the change on document without saving', () => {
    let calls = 0;
    const listener = () => calls++;
    document.addEventListener(THEME_CHANGE_EVENT, listener);
    try {
      applyTheme('dark');
      expect(calls).toBe(1);
      expect(localStorage.getItem(THEME_STORAGE_KEY)).toBeNull();
    } finally {
      document.removeEventListener(THEME_CHANGE_EVENT, listener);
    }
  });
});

test('isThemeChoice accepts exactly auto, light and dark', () => {
  expect(['auto', 'light', 'dark'].every(isThemeChoice)).toBe(true);
  expect([undefined, '', 'sepia', 'Dark'].some(isThemeChoice)).toBe(false);
});

// The pre-paint script is plain JS (no imports), so it repeats the key.
describe('vendor/theme-init.js', () => {
  const script = readFileSync(new URL('../../vendor/theme-init.js', import.meta.url), 'utf8');
  const run = () => new Function(script)();

  test('applies the choice saved under the same key as theme.ts', () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'dark');
    run();
    expect(html().getAttribute('data-theme')).toBe('dark');
  });

  test('leaves <html> alone when nothing or garbage is saved', () => {
    run();
    expect(html().hasAttribute('data-theme')).toBe(false);
    localStorage.setItem(THEME_STORAGE_KEY, 'auto');
    run();
    expect(html().hasAttribute('data-theme')).toBe(false);
  });
});

// A selector cannot be shared between the media query and the attribute rule,
// so dark.css repeats the Dark values; they must not drift apart.
describe('dark.css Dark tokens', () => {
  const css = readFileSync(new URL('../../css/dark.css', import.meta.url), 'utf8');
  const declarations = (selector: string) => {
    const start = css.indexOf(`${selector} {`);
    expect(start).toBeGreaterThan(-1);
    const body = css.slice(css.indexOf('{', start) + 1, css.indexOf('}', start));
    return body
      .split(';')
      .map((d) => d.trim())
      .filter(Boolean);
  };

  test('data-theme="dark" sets the same values as the OS Dark rule', () => {
    const forced = declarations(':root[data-theme="dark"]');
    expect(forced).toContain('color-scheme: dark');
    expect(forced.length).toBeGreaterThan(10);
    expect(declarations(':root:not([data-theme="light"])')).toEqual(forced);
  });

  // Print is always ink on bone (brand-guide §8, issue #1002): both Dark rules sit
  // in `@media screen`, so the :root Light values apply under `@media print`.
  test('both Dark rules are screen-only, so print keeps the Light values', () => {
    const osRule = css.indexOf('@media screen and (prefers-color-scheme: dark) {');
    const forcedRule = css.indexOf('@media screen {');
    expect(osRule).toBeGreaterThan(-1);
    expect(css.indexOf(':root:not([data-theme="light"]) {')).toBeGreaterThan(osRule);
    expect(forcedRule).toBeGreaterThan(-1);
    expect(css.indexOf(':root[data-theme="dark"] {')).toBeGreaterThan(forcedRule);
    expect(css.replace(/\/\*[\s\S]*?\*\//g, '')).not.toMatch(/@media\s+\(prefers-color-scheme/);
  });
});
