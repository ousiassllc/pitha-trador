// View helpers for `pitha-scanner-table`: column definitions (labels and
// tooltips), number formats, sign coloring and badge markup. All of it
// mirrors the SSR fallback (internal/web/organisms/scanner_table_fallback.templ
// and internal/web/atoms/badge.templ) so the hydrated table looks the same
// as the server-rendered one (issue #239).
import { html } from 'lit';
import type { ScannerItem } from './scanner-types';

export type SortKey = keyof Pick<
  ScannerItem,
  | 'symbol'
  | 'price'
  | 'return_1m'
  | 'return_5m'
  | 'volume_ratio_5m'
  | 'price_vs_vwap_bps'
  | 'spread_bps'
  | 'jev_direction'
  | 'jev_confidence'
  | 'entry_quality'
  | 'current_position'
>;
export interface Column {
  key: SortKey;
  label: string;
  // Tooltip (`title`) explaining what the column means.
  hint: string;
  // Numeric columns are right-aligned.
  numeric: boolean;
}

// Must stay in the same order/wording as `scannerColumns` in
// internal/web/organisms/scanner_table_fallback.templ so the hydrated
// table matches the SSR one (issue #239).
export const COLUMNS: Column[] = [
  {
    key: 'symbol',
    label: '銘柄',
    hint: '銘柄コード。行のコードをクリックすると銘柄詳細へ移動します',
    numeric: false,
  },
  { key: 'price', label: '現在値', hint: '直近の価格', numeric: true },
  {
    key: 'return_1m',
    label: '1分騰落率(%)',
    hint: '直近1分間の騰落率。プラス(緑)は上昇、マイナス(赤)は下落',
    numeric: true,
  },
  {
    key: 'return_5m',
    label: '5分騰落率(%)',
    hint: '直近5分間の騰落率。プラス(緑)は上昇、マイナス(赤)は下落',
    numeric: true,
  },
  {
    key: 'volume_ratio_5m',
    label: '出来高倍率',
    hint: '直近5分の出来高が平常時の何倍か。大きいほど売買が活発',
    numeric: true,
  },
  {
    key: 'price_vs_vwap_bps',
    label: 'VWAP乖離(bps)',
    hint: '現在値とVWAP(出来高加重平均価格)の差。1bps=0.01%。プラスはVWAPより高い',
    numeric: true,
  },
  {
    key: 'spread_bps',
    label: 'スプレッド(bps)',
    hint: '売り気配と買い気配の差。1bps=0.01%。小さいほど売買コストが低い',
    numeric: true,
  },
  {
    key: 'jev_direction',
    label: 'Jev方向',
    hint: 'Jev Traderの判定方向。LONG=買い、SHORT=売り、NONE=見送り、pending=判定待ち',
    numeric: false,
  },
  {
    key: 'jev_confidence',
    label: '確信度',
    hint: 'Jev判定の確信度。高いほど判定に自信がある',
    numeric: true,
  },
  {
    key: 'entry_quality',
    label: 'エントリー品質',
    hint: 'エントリー好適度。poor < fair < good < strong < exceptional の順に良い',
    numeric: false,
  },
  {
    key: 'current_position',
    label: '保有',
    hint: '現在の保有ポジション(株数)。未保有は—',
    numeric: true,
  },
];

// Quality order for sorting the entry_quality column, worst to best. Keep in
// step with the `entry_quality` hint above and the domain's source of truth,
// `JevEntryQuality*` in internal/domain/jevdecision.go ("Ordered from worst
// to best"; the Policy Engine's `entry_quality >= strong` depends on it). It
// only orders values for display; nothing here decides behavior (HATEOAS).
const ENTRY_QUALITY_RANK: Readonly<Record<string, number>> = {
  poor: 0,
  fair: 1,
  good: 2,
  strong: 3,
  exceptional: 4,
};

// sortValue is what a column's header sorts by: the entry_quality rank (an
// unknown value sorts like null, i.e. first) or the raw cell value.
export function sortValue(item: ScannerItem, key: SortKey): string | number | null {
  if (key === 'entry_quality') {
    return item.entry_quality === null ? null : (ENTRY_QUALITY_RANK[item.entry_quality] ?? null);
  }
  return item[key];
}

const BADGE_BASE = 'inline-flex items-center rounded-full px-2 py-1 text-xs font-semibold';

// Mirrors atoms.Badge / atoms.EntryQualityBadge (internal/web/atoms/badge.templ).
const DIRECTION_BADGE_CLASS: Record<string, string> = {
  LONG: 'bg-green-100 text-green-800',
  SHORT: 'bg-red-100 text-red-800',
};
const ENTRY_QUALITY_BADGE_CLASS: Record<string, string> = {
  exceptional: 'bg-emerald-600 text-white',
  strong: 'bg-green-100 text-green-800',
  good: 'bg-sky-100 text-sky-800',
  fair: 'bg-yellow-100 text-yellow-800',
};
const NEUTRAL_BADGE_CLASS = 'bg-gray-100 text-gray-600';

export function directionBadge(direction: string | null) {
  if (direction === null) {
    return html`<span class="inline-flex items-center rounded-full bg-gray-100 px-2 py-1 text-xs font-medium text-gray-500">pending</span>`;
  }
  const label = direction === 'LONG' || direction === 'SHORT' ? direction : 'NONE';
  return html`<span class="${BADGE_BASE} ${DIRECTION_BADGE_CLASS[label] ?? NEUTRAL_BADGE_CLASS}">${label}</span>`;
}

export function entryQualityBadge(quality: string | null) {
  if (quality === null) {
    return html`<span class="text-slate-400">—</span>`;
  }
  return html`<span class="${BADGE_BASE} ${ENTRY_QUALITY_BADGE_CLASS[quality] ?? NEUTRAL_BADGE_CLASS}">${quality}</span>`;
}

// Green when positive, red when negative, neutral for zero / missing.
export function returnClass(value: number | null): string {
  const base = 'px-3 py-2 text-right font-medium tabular-nums';
  if (value === null || value === 0) return `${base} text-slate-500`;
  return value > 0 ? `${base} text-green-700` : `${base} text-red-700`;
}

export function formatNullable(value: number | null, decimals: number): string {
  return value === null ? '—' : value.toFixed(decimals);
}

// Explicit "+" for positive values (negative values already carry "-").
// Zero carries no sign, like formatSignedFloat in
// internal/web/organisms/scanner_table_fallback.templ.
export function formatSigned(value: number, decimals: number): string {
  return `${value > 0 ? '+' : ''}${value.toFixed(decimals)}`;
}

// 0..1 confidence as a whole percent. Math.round and Go's math.Round both
// round halves up for non-negative values (Go's %.0f would round to even).
export function formatConfidence(value: number | null): string {
  return value === null ? '—' : `${Math.round(value * 100)}%`;
}

// The server's RFC 3339 timestamp without fractional seconds, matching the
// SSR caption (Go's time.RFC3339); `as_of` values from the API/WebSocket
// carry nanoseconds (RFC3339Nano) but the same offset.
export function formatAsOf(asOf: string): string {
  return asOf.replace(/\.\d+/, '');
}

export function formatSignedNullable(value: number | null, decimals: number): string {
  return value === null ? '—' : formatSigned(value, decimals);
}
