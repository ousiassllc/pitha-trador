// JST date-time label shared with the SSR fallbacks
// (internal/web/atoms.FormatJST): `2026-10-05 09:30:00 JST`. The zone is
// pinned to Asia/Tokyo (not the host/Wails zone) so the same instant reads
// the same everywhere, and the Lit render matches what the server rendered
// before hydration.

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

// RFC 3339 / RFC 3339 Nano -> JST label. Fractional seconds are dropped. An
// unparseable value is returned as-is rather than shown as "Invalid Date".
export function formatJstDateTime(rfc3339: string): string {
  const at = new Date(rfc3339);
  if (Number.isNaN(at.getTime())) return rfc3339;
  const p: Record<string, string> = {};
  for (const part of jstFormat.formatToParts(at)) p[part.type] = part.value;
  return `${p.year}-${p.month}-${p.day} ${p.hour}:${p.minute}:${p.second} JST`;
}
