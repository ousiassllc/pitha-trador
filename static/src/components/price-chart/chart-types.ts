// Response and WebSocket message shapes of `pitha-price-chart` (split out of
// the component to keep it within the linterly per-file limit).

// Mirrors docs/api/endpoints.md §5 `GET /api/v1/symbols/{symbol}/candles`
// item shape (internal/web/handler/symbol.candleOutput).
export interface Candle {
  time: string;
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
  vwap: number;
}

export interface CandlesAPIResponse {
  symbol: string;
  candles: Candle[];
}

// Mirrors docs/api/endpoints.md §6's `/ws/symbols/{symbol}` message
// shapes (internal/web/handler/symbol.symbolTickMessage/symbolJevUpdateMessage).
export interface TickMessage {
  type: 'tick';
  price: number;
  // RFC 3339 time of the snapshot the price comes from.
  time: string;
}

export interface JevUpdateMessage {
  type: 'jev_update';
  direction: string | null;
  confidence: number | null;
  // RFC 3339 time of the Jev decision; the marker goes on the bar it falls in.
  time: string;
}

export type SymbolMessage = TickMessage | JevUpdateMessage;
