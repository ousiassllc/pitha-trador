// Series colors of pitha-price-chart: guide tokens (lib/chart-theme.ts), read when the chart
// is created and again on an OS Light/Dark switch. Candles --ink (down hollow on --surface),
// VWAP --info, volume --mute at 50%, markers --success (LONG) / --warning (SHORT).
import { readToken, withAlpha } from '../lib/chart-theme';

// Up is a filled --ink candle, down a hollow one (--surface fill, --ink
// border): the guide's chart series are --success/--warning/--info/--mute/
// --ink only, and a price move is not an error, so --danger is not used.
export const candleColors = () => {
  const ink = readToken('ink');
  return {
    upColor: ink,
    downColor: readToken('surface'),
    borderUpColor: ink,
    borderDownColor: ink,
    wickUpColor: ink,
    wickDownColor: ink,
  };
};
export const vwapColor = () => ({ color: readToken('info') });
export const volumeColor = () => ({ color: withAlpha(readToken('mute'), 0.5) });
export const markerColor = (direction: string) =>
  readToken(direction === 'LONG' ? 'success' : 'warning');
