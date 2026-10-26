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
  // Help text of the 「?」 mark beside the column name; '' when the name
  // already says everything (no mark is drawn).
  hint: string;
  // Numeric columns are right-aligned.
  numeric: boolean;
}

// Must stay in the same order/wording as `scannerColumns` in
// internal/web/organisms/scanner_table_fallback.templ so the hydrated
// table matches the SSR one (issue #239).
export const COLUMNS: Column[] = [
  { key: 'symbol', label: '銘柄', hint: '', numeric: false },
  { key: 'price', label: '現在値', hint: '', numeric: true },
  {
    key: 'return_1m',
    label: '1分騰落率(%)',
    hint: '直近1分間の騰落率。プラス(緑)は上昇、マイナス(赤)は下落を表します。',
    numeric: true,
  },
  {
    key: 'return_5m',
    label: '5分騰落率(%)',
    hint: '直近5分間の騰落率。プラス(緑)は上昇、マイナス(赤)は下落を表します。',
    numeric: true,
  },
  {
    key: 'volume_ratio_5m',
    label: '出来高倍率',
    hint: '直近5分の出来高が平常時の何倍か。大きいほど売買が活発です。',
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
    hint: 'Jev Traderの判定方向。LONG=買い、SHORT=売り、NONE=見送り。「判定待ち」はまだ判定していない候補です',
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

// Help beside 「候補 N 件」: what the list is, how often it refreshes and what
// the colors mean. Same wording as `scannerCountHelp` in
// internal/web/organisms/scanner_table_fallback.templ.
export const COUNT_HELP_TEXT =
  'Fast Screenerが選んだ売買候補の一覧です。15〜30秒ごとに自動更新されます。緑はプラス・LONG、赤はマイナス・SHORT を表します。';

// The 「?」 help mark in the light DOM: the markup and Tailwind classes of
// `atoms.HelpMark` (internal/web/atoms/help_mark.templ), which
// components/help-mark/pitha-help-mark.ts opens by document-level
// delegation. (lib/help-mark.ts' renderHelpMark is for shadow roots.)
export function renderLightHelpMark(id: string, label: string, text: string) {
  return html`<span class="relative inline-flex align-middle" data-help-mark><button type="button" class="inline-flex h-4 w-4 items-center justify-center rounded-full border border-slate-400 bg-white text-[10px] font-semibold leading-none text-slate-600 hover:border-slate-600 hover:text-slate-900 cursor-help focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-600" aria-label=${label} aria-describedby=${id} data-help-mark-button>?</button><span id=${id} role="tooltip" class="invisible fixed left-0 top-0 z-50 w-72 max-w-[80vw] rounded-md border border-slate-300 bg-white p-2 text-left text-xs font-normal normal-case leading-relaxed tracking-normal text-slate-700 shadow-lg data-[open]:visible" data-help-mark-text>${text}</span></span>`;
}

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
    return html`<span class="inline-flex items-center rounded-full bg-gray-100 px-2 py-1 text-xs font-medium text-gray-500">判定待ち</span>`;
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
// The SSR side is the single shared atoms.FormatConfidence (issue #677).
export function formatConfidence(value: number | null): string {
  return value === null ? '—' : `${Math.round(value * 100)}%`;
}

export function formatSignedNullable(value: number | null, decimals: number): string {
  return value === null ? '—' : formatSigned(value, decimals);
}
