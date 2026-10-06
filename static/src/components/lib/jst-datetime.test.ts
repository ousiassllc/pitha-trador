import { describe, expect, test } from 'bun:test';
import { formatJstDateTime, isZeroTime } from './jst-datetime';

describe('formatJstDateTime', () => {
  test('UTC 00:30Z is 09:30 JST', () => {
    expect(formatJstDateTime('2026-10-05T00:30:00Z')).toBe('2026-10-05 09:30:00 JST');
  });

  test('UTC evening rolls over to the next JST day', () => {
    expect(formatJstDateTime('2026-10-05T15:30:00Z')).toBe('2026-10-06 00:30:00 JST');
  });

  test('offset input and nanosecond fractions', () => {
    expect(formatJstDateTime('2026-09-26T10:15:30.123456789+09:00')).toBe(
      '2026-09-26 10:15:30 JST',
    );
    expect(formatJstDateTime('2026-09-26T01:15:30-08:00')).toBe('2026-09-26 18:15:30 JST');
  });

  test('unparseable input is shown unchanged', () => {
    expect(formatJstDateTime('not a time')).toBe('not a time');
  });
});

describe('isZeroTime', () => {
  test('detects the Go zero time in any offset form', () => {
    expect(isZeroTime('0001-01-01T00:00:00Z')).toBe(true);
    expect(isZeroTime('0001-01-01T09:00:00+09:00')).toBe(true);
  });

  test('real timestamps are not the zero time', () => {
    expect(isZeroTime('2026-10-05T00:30:00Z')).toBe(false);
  });
});
