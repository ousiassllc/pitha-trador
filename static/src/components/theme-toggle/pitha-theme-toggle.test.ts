import { afterEach, beforeEach, describe, expect, test } from 'bun:test';
import { THEME_STORAGE_KEY } from '../lib/theme';
import { init, onClick, sync } from './pitha-theme-toggle';

const segments = `
  <div role="group" aria-label="テーマ">
    <button type="button" data-theme-choice="auto" aria-pressed="true">自動</button>
    <button type="button" data-theme-choice="light" aria-pressed="false">ライト</button>
    <button type="button" data-theme-choice="dark" aria-pressed="false">ダーク</button>
  </div>`;

const button = (choice: string) =>
  document.querySelector(`[data-theme-choice="${choice}"]`) as HTMLElement;
const pressed = () =>
  [...document.querySelectorAll('[data-theme-choice]')]
    .filter((b) => b.getAttribute('aria-pressed') === 'true')
    .map((b) => (b as HTMLElement).dataset.themeChoice);

beforeEach(() => {
  document.body.innerHTML = segments;
});
afterEach(() => {
  document.body.innerHTML = '';
  localStorage.clear();
  document.documentElement.removeAttribute('data-theme');
});

describe('click', () => {
  test('dark forces the theme, is saved and is the only pressed segment', () => {
    onClick({ target: button('dark') } as unknown as Event);
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark');
    expect(pressed()).toEqual(['dark']);
  });

  test('auto goes back to following the OS', () => {
    onClick({ target: button('light') } as unknown as Event);
    onClick({ target: button('auto') } as unknown as Event);
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false);
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBeNull();
    expect(pressed()).toEqual(['auto']);
  });

  test('a click elsewhere changes nothing', () => {
    onClick({ target: document.body } as unknown as Event);
    onClick({ target: null } as unknown as Event);
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false);
    expect(pressed()).toEqual(['auto']);
  });
});

describe('sync', () => {
  test('presses the saved choice (a reload keeps it)', () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'light');
    sync();
    expect(pressed()).toEqual(['light']);
  });
});

describe('init', () => {
  test('listens for clicks on the document', () => {
    init();
    button('dark').click();
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
    expect(pressed()).toEqual(['dark']);
  });
});
