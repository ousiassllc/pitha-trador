// `pitha-scanner-table`: the Scanner Dashboard's sortable, live-updating
// candidate table (docs/components/overview.md §5.2). It loads its
// initial rows from `GET /api/v1/scanner` (via lib/api.ts), then applies
// `/ws/scanner` PUSH updates (via lib/ws.ts) as they arrive. Column
// headers sort client-side (no server round trip); symbol rows are plain
// `<a href="/symbols/{symbol}">` links so clicking one is a normal browser
// navigation, not an HTMX request (HTMX↔Lit boundary rule: "Lit does not
// trigger HTMX requests").
import { html, LitElement, type PropertyValues } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { get } from '../lib/api';
import { logger } from '../lib/logger';
import { WsClient } from '../lib/ws';

// Mirrors docs/api/endpoints.md §5 `GET /api/v1/scanner` item shape.
export interface ScannerItem {
  symbol: string;
  price: number;
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

interface ScannerAPIResponse {
  items: ScannerItem[];
  as_of: string;
}

// Mirrors docs/api/endpoints.md §6 `/ws/scanner` message shape.
interface ScannerUpdateMessage {
  type: string;
  items: ScannerItem[];
}

type SortKey = keyof Pick<
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
type SortDirection = 'asc' | 'desc';

interface Column {
  key: SortKey;
  label: string;
}

const COLUMNS: Column[] = [
  { key: 'symbol', label: 'Symbol' },
  { key: 'price', label: 'Price' },
  { key: 'return_1m', label: '1m Return' },
  { key: 'return_5m', label: '5m Return' },
  { key: 'volume_ratio_5m', label: 'Volume Ratio' },
  { key: 'price_vs_vwap_bps', label: 'VWAP Distance (bps)' },
  { key: 'spread_bps', label: 'Spread (bps)' },
  { key: 'jev_direction', label: 'Jev Direction' },
  { key: 'jev_confidence', label: 'Jev Confidence' },
  { key: 'entry_quality', label: 'Entry Quality' },
  { key: 'current_position', label: 'Position' },
];

@customElement('pitha-scanner-table')
export class PithaScannerTable extends LitElement {
  // Render into light DOM rather than a shadow root: the server already
  // renders this exact table as `pitha-scanner-table`'s children
  // (organisms.ScannerTableFallback), so on first render this replaces
  // that SSR fallback markup in place instead of duplicating it inside a
  // separate shadow tree.
  protected override createRenderRoot(): HTMLElement {
    return this;
  }

  @property({ type: String, attribute: 'api-url' }) apiUrl = '/api/v1/scanner';
  @property({ type: String, attribute: 'ws-url' }) wsUrl = '/ws/scanner';

  @state() private items: ScannerItem[] = [];
  @state() private sortKey: SortKey = 'symbol';
  @state() private sortDirection: SortDirection = 'asc';
  @state() private error: string | null = null;

  private wsClient: WsClient<ScannerUpdateMessage> | null = null;

  override connectedCallback(): void {
    super.connectedCallback();
    this.loadInitial();
    this.subscribeWs();
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.wsClient?.close();
    this.wsClient = null;
  }

  private async loadInitial(): Promise<void> {
    try {
      const response = await get<ScannerAPIResponse>(this.apiUrl);
      this.items = response.items;
      this.error = null;
    } catch (err) {
      this.error = err instanceof Error ? err.message : String(err);
      logger.error('pitha-scanner-table: failed to load initial candidates', { error: err });
    }
  }

  private subscribeWs(): void {
    this.wsClient = new WsClient<ScannerUpdateMessage>(resolveWsUrl(this.wsUrl), {
      onMessage: (message) => {
        if (message.type === 'scanner_update') {
          this.items = message.items;
        }
      },
    });
  }

  private onHeaderClick(key: SortKey): void {
    if (this.sortKey === key) {
      this.sortDirection = this.sortDirection === 'asc' ? 'desc' : 'asc';
    } else {
      this.sortKey = key;
      this.sortDirection = 'asc';
    }
  }

  private sortedItems(): ScannerItem[] {
    const { sortKey, sortDirection } = this;
    const factor = sortDirection === 'asc' ? 1 : -1;
    return [...this.items].sort((a, b) => factor * compareValues(a[sortKey], b[sortKey]));
  }

  protected override render() {
    const items = this.sortedItems();
    return html`
      <table>
        <thead>
          <tr>
            ${COLUMNS.map(
              (column) => html`
                <th
                  scope="col"
                  aria-sort=${this.sortKey === column.key ? (this.sortDirection === 'asc' ? 'ascending' : 'descending') : 'none'}
                  @click=${() => this.onHeaderClick(column.key)}
                >
                  ${column.label}
                </th>
              `,
            )}
          </tr>
        </thead>
        <tbody>
          ${items.map(
            (item) => html`
              <tr data-symbol=${item.symbol}>
                <td><a href=${`/symbols/${item.symbol}`}>${item.symbol}</a></td>
                <td>${item.price.toFixed(2)}</td>
                <td>${formatNullableNumber(item.return_1m)}</td>
                <td>${formatNullableNumber(item.return_5m)}</td>
                <td>${formatNullableNumber(item.volume_ratio_5m)}</td>
                <td>${item.price_vs_vwap_bps.toFixed(2)}</td>
                <td>${formatNullableNumber(item.spread_bps)}</td>
                <td>${item.jev_direction ?? '—'}</td>
                <td>${formatNullableNumber(item.jev_confidence)}</td>
                <td>${item.entry_quality ?? '—'}</td>
                <td>${formatNullableNumber(item.current_position)}</td>
              </tr>
            `,
          )}
        </tbody>
      </table>
      ${this.error ? html`<p class="pitha-scanner-table-error" role="alert">${this.error}</p>` : ''}
    `;
  }

  protected override updated(changed: PropertyValues<this>): void {
    if (changed.has('wsUrl') && this.wsClient) {
      this.wsClient.close();
      this.subscribeWs();
    }
  }
}

function resolveWsUrl(url: string): string {
  if (/^wss?:\/\//i.test(url)) {
    return url;
  }
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${protocol}//${location.host}${url}`;
}

function formatNullableNumber(value: number | null): string {
  return value === null ? '—' : value.toFixed(2);
}

// compareValues orders nulls first (regardless of column direction, they
// sort as the smallest value), then strings lexicographically and numbers
// numerically.
function compareValues(a: ScannerItem[SortKey], b: ScannerItem[SortKey]): number {
  if (a === null || b === null) {
    if (a === b) return 0;
    return a === null ? -1 : 1;
  }
  if (typeof a === 'string' && typeof b === 'string') {
    return a.localeCompare(b);
  }
  return (a as number) - (b as number);
}

declare global {
  interface HTMLElementTagNameMap {
    'pitha-scanner-table': PithaScannerTable;
  }
}
