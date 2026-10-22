import { afterEach, describe, expect, test } from 'bun:test';
import { backtestTabUrl, forward } from './pitha-performance-tabs';

afterEach(() => {
  location.hash = '';
});

describe('backtestTabUrl', () => {
  test('the old #backtest anchor of /performance maps to the backtest tab', () => {
    expect(backtestTabUrl('/performance', '', '#backtest')).toBe('/performance?tab=backtest');
  });

  test('other query parameters are kept', () => {
    expect(backtestTabUrl('/performance', '?x=1', '#backtest')).toBe(
      '/performance?x=1&tab=backtest',
    );
  });

  test('other hashes and other pages map to nothing', () => {
    expect(backtestTabUrl('/performance', '', '')).toBeNull();
    expect(backtestTabUrl('/performance', '', '#other')).toBeNull();
    expect(backtestTabUrl('/calibration', '', '#backtest')).toBeNull();
  });
});

describe('forward', () => {
  test('does nothing without the old anchor', () => {
    const seen: string[] = [];
    forward((url) => seen.push(url));
    expect(seen).toEqual([]);
  });
});
