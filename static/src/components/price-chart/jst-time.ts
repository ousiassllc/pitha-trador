// JST formatters for `pitha-price-chart`'s time axis and crosshair.
// lightweight-charts v4 formats every UTCTimestamp with getUTCHours, so
// without these the TSE session (09:00-15:30 JST) would read 00:00-06:30.
// The zone is pinned to Asia/Tokyo (not the host/Wails zone) because the
// whole app is JST-based.

import type { TickMarkType, Time } from 'lightweight-charts';

const jstFormat = new Intl.DateTimeFormat('ja-JP', {
  timeZone: 'Asia/Tokyo',
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hourCycle: 'h23',
});

interface JstParts {
  year: string;
  month: string;
  day: string;
  hour: string;
  minute: string;
  second: string;
}

function jstParts(epochSeconds: number): JstParts {
  const parts: Record<string, string> = {};
  for (const part of jstFormat.formatToParts(new Date(epochSeconds * 1000))) {
    parts[part.type] = part.value;
  }
  return parts as unknown as JstParts;
}

// Crosshair label (`localization.timeFormatter`): "2026-10-05 09:00". The
// chart only ever receives UTCTimestamp (epoch seconds); anything else
// (BusinessDay, date string) falls back to its string form.
export function formatCrosshairTime(time: Time): string {
  if (typeof time !== 'number') return String(time);
  const p = jstParts(time);
  return `${p.year}-${p.month}-${p.day} ${p.hour}:${p.minute}`;
}

// Mirrors lightweight-charts' `TickMarkType` enum values. Numeric literals are
// used (with a type-only import) so this module has no runtime dependency on
// the library.
const TICK_YEAR: TickMarkType = 0;
const TICK_MONTH: TickMarkType = 1;
const TICK_DAY_OF_MONTH: TickMarkType = 2;
const TICK_TIME: TickMarkType = 3;
const TICK_TIME_WITH_SECONDS: TickMarkType = 4;

// Axis tick label (`timeScale.tickMarkFormatter`).
export function formatTickMark(time: Time, tickMarkType: TickMarkType): string | null {
  if (typeof time !== 'number') return null;
  const p = jstParts(time);
  switch (tickMarkType) {
    case TICK_YEAR:
      return p.year;
    case TICK_MONTH:
      return `${Number(p.month)}月`;
    case TICK_DAY_OF_MONTH:
      return String(Number(p.day));
    case TICK_TIME:
      return `${p.hour}:${p.minute}`;
    case TICK_TIME_WITH_SECONDS:
      return `${p.hour}:${p.minute}:${p.second}`;
    default:
      return null;
  }
}
