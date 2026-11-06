// `pitha-scanner-table`: the Scanner Dashboard's sortable, live-updating
// candidate table (docs/components/overview.md §5.2). It loads its
// initial rows from `GET /api/v1/scanner` (via lib/api.ts), then applies
// `/ws/scanner` PUSH updates (via lib/ws.ts) as they arrive. Column
// headers sort client-side (no server round trip); symbol rows are plain
// `<a href>` links to the server-provided `detail_url` (HATEOAS: the URL is
// never built here), so clicking one is a normal browser
// navigation, not an HTMX request (HTMX↔Lit boundary rule: "Lit does not
// trigger HTMX requests").
import { html, LitElement, type PropertyValues } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { get } from '../lib/api';
import { navArrowDown, navArrowUp } from '../lib/icons';
import { formatJstDateTime, isZeroTime } from '../lib/jst-datetime';
import { logger } from '../lib/logger';
import { lightDomErrorClass, lightDomWsNoticeClass } from '../lib/styles';
import { resolveWsUrl, WsClient, type WsStatus } from '../lib/ws';
import { renderWsDisconnected } from '../lib/ws-status';
import type {
  ScannerAPIResponse,
  ScannerItem,
  ScannerUpdateMessage,
  SortDirection,
} from './scanner-types';
import {
  COLUMNS,
  COUNT_HELP_TEXT,
  type Column,
  directionBadge,
  entryQualityBadge,
  formatConfidence,
  formatNullable,
  formatSigned,
  formatSignedNullable,
  renderLightHelpMark,
  returnClass,
  type SortKey,
  sortValue,
} from './scanner-view';

@customElement('pitha-scanner-table')
export class PithaScannerTable extends LitElement {
  // Render into light DOM rather than a shadow root: the server already
  // renders this exact table as `pitha-scanner-table`'s children
  // (organisms.ScannerTableFallback), and `scanner_page.templ`'s
  // `<script type="module">` loads after (deferred by default), so this
  // element is upgraded - not freshly constructed - with that SSR
  // markup already attached as children by the time createRenderRoot
  // (called from connectedCallback) runs. lit-html's render() only
  // manages content from a marker comment it inserts onward and leaves
  // pre-existing children alone, so the SSR nodes are remembered here
  // and removed once the first data arrives (willUpdate) - otherwise
  // Lit's own first render() would append a second <table> beside the
  // SSR one. Until then (and if the initial fetch fails) they stay
  // visible, so hydration never blanks the page. A naive repro that
  // imports this module before inserting the SSR markup does NOT
  // reproduce the duplicate: that constructs a fresh element before the
  // parser has appended any children.
  protected override createRenderRoot(): HTMLElement {
    this.ssrNodes = Array.from(this.childNodes);
    return this;
  }

  protected override willUpdate(): void {
    if (this.loaded && this.ssrNodes.length > 0) {
      for (const node of this.ssrNodes) node.remove();
      this.ssrNodes = [];
    }
  }

  private ssrNodes: ChildNode[] = [];

  // Injected by organisms.ScannerTableFallback; the component owns no URL
  // (docs/components/lit.md §5.2).
  @property({ type: String, attribute: 'api-url' }) apiUrl = '';
  @property({ type: String, attribute: 'ws-url' }) wsUrl = '';

  @state() private items: ScannerItem[] = [];
  // False until the first data (initial fetch or WS push) arrives, so an
  // empty list before loading is not mistaken for "no candidates".
  @state() private loaded = false;
  @state() private asOf: string | null = null;
  // null = server order (ScreenScore descending, same as the SSR fallback);
  // set only once a header is clicked (docs/components/lit.md §5.2, issue #674).
  @state() private sortKey: SortKey | null = null;
  @state() private sortDirection: SortDirection = 'asc';
  @state() private error: string | null = null;
  @state() private wsStatus: WsStatus = 'connecting';

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
    if (!this.apiUrl) {
      logger.error('pitha-scanner-table: api-url is not set');
      return;
    }
    try {
      const response = await get<ScannerAPIResponse>(this.apiUrl);
      this.items = response.items;
      this.asOf = response.as_of;
      this.loaded = true;
      this.error = null;
    } catch (err) {
      this.error = err instanceof Error ? err.message : String(err);
      logger.error('pitha-scanner-table: failed to load initial candidates', { error: err });
    }
  }

  private subscribeWs(): void {
    if (!this.wsUrl) {
      logger.error('pitha-scanner-table: ws-url is not set');
      return;
    }
    this.wsClient = new WsClient<ScannerUpdateMessage>(resolveWsUrl(this.wsUrl), {
      onStatusChange: (status) => {
        this.wsStatus = status;
      },
      onMessage: (message) => {
        if (message.type === 'scanner_update') {
          this.items = message.items;
          this.asOf = message.as_of;
          this.loaded = true;
          this.error = null;
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
    if (sortKey === null) return this.items;
    const factor = sortDirection === 'asc' ? 1 : -1;
    return [...this.items].sort(
      (a, b) => factor * compareValues(sortValue(a, sortKey), sortValue(b, sortKey)),
    );
  }

  protected override render() {
    const notices = html`
      ${renderWsDisconnected(this.wsStatus, lightDomWsNoticeClass)}
      ${this.error ? html`<p class="pitha-scanner-table-error ${lightDomErrorClass}" role="alert">${this.error}</p>` : ''}
    `;
    // Until the first data arrives the server-rendered table stays in
    // place (see createRenderRoot); only the notices are added beside it.
    if (!this.loaded) return notices;

    const items = this.sortedItems();
    return html`
      <p class="mb-3 text-sm text-mute" data-testid="scanner-count">
        候補
        <span class="text-lg font-mono tabular-nums text-ink">${items.length}</span>
        件
        ${renderLightHelpMark('help-scanner-count', '候補銘柄の説明', COUNT_HELP_TEXT)}
      </p>
      <div class="overflow-x-auto rounded-lg border border-border bg-surface">
        <table class="w-full border-collapse text-left text-sm" aria-label="スキャナー候補">
          ${
            this.asOf
              ? html`<caption class="border-b border-border px-3 py-2 text-left text-xs text-mute">候補銘柄 — ${isZeroTime(this.asOf) ? 'データ未取得' : `${formatJstDateTime(this.asOf)} 時点`}</caption>`
              : ''
          }
          <thead>
            <tr class="border-b border-border bg-bone text-xs font-semibold text-mute">
              ${COLUMNS.map((column) => this.renderHeader(column))}
            </tr>
          </thead>
          <tbody>
            ${items.map((item) => this.renderRow(item))}
          </tbody>
        </table>
      </div>
      ${
        items.length === 0
          ? html`<p class="mt-3 rounded-lg border border-dashed border-border bg-bone px-6 py-12 text-center text-sm text-mute" role="status" data-testid="scanner-empty">
              現在、条件を満たす候補銘柄はありません。
            </p>`
          : ''
      }
      ${notices}
    `;
  }

  // Each header is a real <button> so sorting works with Tab + Enter/Space.
  // The column's explanation is a 「?」 help mark beside the button (not
  // inside it), so keyboard and touch users can open it too.
  private renderHeader(column: Column) {
    const active = this.sortKey === column.key;
    const indicator = active ? (this.sortDirection === 'asc' ? navArrowUp() : navArrowDown()) : '';
    return html`
      <th scope="col"
        class="whitespace-nowrap px-3 py-2.5 ${column.numeric ? 'text-right' : ''}"
        aria-sort=${active ? (this.sortDirection === 'asc' ? 'ascending' : 'descending') : 'none'}
      >
        <button
          type="button"
          class="cursor-pointer select-none hover:text-ink"
          @click=${() => this.onHeaderClick(column.key)}
        >${column.label}${indicator}</button>
        ${
          column.hint
            ? renderLightHelpMark(
                `help-scanner-col-${column.key}`,
                `${column.label}の説明`,
                column.hint,
              )
            : ''
        }
      </th>
    `;
  }

  private renderRow(item: ScannerItem) {
    const numeric = 'px-3 py-2.5 text-right font-mono tabular-nums';
    return html`
      <tr class="border-b border-border last:border-b-0 hover:bg-accent-soft" data-symbol=${item.symbol}>
        <td class="px-3 py-2.5">
          <a class="text-accent underline-offset-2 hover:underline" href=${item.detail_url}>${item.symbol}</a>
        </td>
        <td class=${numeric}>${item.price.toFixed(1)}</td>
        <td class=${returnClass(item.return_1m)}>${formatSignedNullable(item.return_1m, 2)}</td>
        <td class=${returnClass(item.return_5m)}>${formatSignedNullable(item.return_5m, 2)}</td>
        <td class=${numeric}>${formatNullable(item.volume_ratio_5m, 2)}</td>
        <td class=${numeric}>${formatSigned(item.price_vs_vwap_bps, 0)}</td>
        <td class=${numeric}>${formatNullable(item.spread_bps, 0)}</td>
        <td class="px-3 py-2.5">${directionBadge(item.jev_direction)}</td>
        <td class=${numeric}>${formatConfidence(item.jev_confidence)}</td>
        <td class="px-3 py-2.5">${entryQualityBadge(item.entry_quality)}</td>
        <td class=${numeric}>${formatNullable(item.current_position, 0)}</td>
      </tr>
    `;
  }

  // Skip the first update cycle (old value undefined): connectedCallback
  // already subscribed, and reconnecting would open a second socket.
  protected override updated(changed: PropertyValues<this>): void {
    if (changed.get('wsUrl') !== undefined && this.wsClient) {
      this.wsClient.close();
      this.subscribeWs();
    }
  }
}

// compareValues orders nulls first (regardless of column direction, they
// sort as the smallest value), then strings lexicographically and numbers
// numerically.
function compareValues(a: string | number | null, b: string | number | null): number {
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
