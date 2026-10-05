import { describe, expect, test } from 'bun:test';
import type { TickMarkType, Time } from 'lightweight-charts';
import { formatCrosshairTime, formatTickMark } from './jst-time';

// lightweight-charts' TickMarkType enum values (Year..TimeWithSeconds).
const YEAR = 0 as TickMarkType;
const MONTH = 1 as TickMarkType;
const DAY_OF_MONTH = 2 as TickMarkType;
const TIME = 3 as TickMarkType;
const TIME_WITH_SECONDS = 4 as TickMarkType;

// 09:00 JST on the TSE open = 00:00 UTC. The formatter must not depend on the
// host timezone (the library itself would print 00:00 here).
const tseOpen = (Date.parse('2026-10-05T00:00:00Z') / 1000) as Time;

describe('jst-time', () => {
  test('crosshair shows the TSE open as 09:00 JST', () => {
    expect(formatCrosshairTime(tseOpen)).toBe('2026-10-05 09:00');
  });

  test('crosshair rolls over to the next JST day at 15:00 UTC', () => {
    const time = (Date.parse('2026-10-05T15:00:00Z') / 1000) as Time;
    expect(formatCrosshairTime(time)).toBe('2026-10-06 00:00');
  });

  test('time ticks show JST HH:mm (24h, midnight is 00 not 24)', () => {
    expect(formatTickMark(tseOpen, TIME)).toBe('09:00');
    const midnight = (Date.parse('2026-10-05T15:00:00Z') / 1000) as Time;
    expect(formatTickMark(midnight, TIME)).toBe('00:00');
  });

  test('seconds, day, month and year ticks use JST calendar fields', () => {
    const time = (Date.parse('2026-12-31T15:30:45Z') / 1000) as Time; // 2027-01-01 00:30:45 JST
    expect(formatTickMark(time, TIME_WITH_SECONDS)).toBe('00:30:45');
    expect(formatTickMark(time, DAY_OF_MONTH)).toBe('1');
    expect(formatTickMark(time, MONTH)).toBe('1月');
    expect(formatTickMark(time, YEAR)).toBe('2027');
  });

  test('non-timestamp times fall back to the library default', () => {
    expect(formatTickMark('2026-10-05', TIME)).toBeNull();
    expect(formatCrosshairTime('2026-10-05')).toBe('2026-10-05');
  });
});
