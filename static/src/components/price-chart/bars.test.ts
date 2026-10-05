import { describe, expect, test } from 'bun:test';
import { type Bar, foldTick, toUTCTimestamp } from './bars';

describe('foldTick', () => {
  const at = (iso: string) => toUTCTimestamp(iso);
  const bar: Bar = {
    time: at('2026-09-29T01:00:00Z'),
    open: 100,
    high: 105,
    low: 99,
    close: 102,
  };

  // Snapshot time, not the client clock, picks the bar (issue #557).
  test('starts a new bar at the snapshot minute', () => {
    expect(foldTick(bar, 110, '2026-09-29T01:01:59Z')).toEqual({
      time: at('2026-09-29T01:01:00Z'),
      open: 110,
      high: 110,
      low: 110,
      close: 110,
    });
  });

  test('folds a tick in the same minute into the last bar', () => {
    expect(foldTick(bar, 98, '2026-09-29T01:00:30Z')).toEqual({ ...bar, low: 98, close: 98 });
  });

  test('ignores a snapshot older than the last bar', () => {
    expect(foldTick(bar, 101, '2026-09-29T00:59:59Z')).toBeNull();
  });

  test.each([0, -1, Number.NaN])('ignores price %p', (price) => {
    expect(foldTick(bar, price, '2026-09-29T01:00:30Z')).toBeNull();
  });

  test('ignores an unparsable time', () => {
    expect(foldTick(bar, 101, 'not-a-time')).toBeNull();
  });
});
