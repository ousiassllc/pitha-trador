// Wire shapes for `pitha-scanner-table`.
// Mirrors docs/api/endpoints.md §5 `GET /api/v1/scanner` item shape.
export interface ScannerItem {
  symbol: string;
  // Server-generated symbol detail link (Go organisms.SymbolHref); used as-is for the row's href.
  detail_url: string;
  price: number;
  // Percent (0.42 == +0.42%); the API converts the Feature Engine's decimal ratio.
  return_1m: number | null;
  return_5m: number | null;
  volume_ratio_5m: number | null;
  price_vs_vwap_bps: number;
  spread_bps: number | null;
  jev_direction: string | null;
  jev_confidence: number | null;
  entry_quality: string | null;
  current_position: number | null;
}

export interface ScannerAPIResponse {
  items: ScannerItem[];
  as_of: string;
}

// Mirrors docs/api/endpoints.md §6 `/ws/scanner` message shape.
export interface ScannerUpdateMessage {
  type: string;
  items: ScannerItem[];
  // Same RFC 3339 scan-cycle timestamp as ScannerAPIResponse.as_of.
  as_of: string;
}

export type SortDirection = 'asc' | 'desc';
