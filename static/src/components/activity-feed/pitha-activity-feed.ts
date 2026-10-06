// `pitha-activity-feed`: System Activity Log's live queue-status table and
// activity feed (docs/components/overview.md §5.5, functional.md §4.15).
// It loads the snapshot from `GET /api/v1/activity` (via lib/api.ts) - once
// unfiltered for the Kill Switch list and once with the current type/queue
// filters for the feed - then applies `/ws/activity` `job_update` /
// `activity_event` messages (via lib/ws.ts) as they arrive. Changing a
// filter re-fetches the feed from the server, so the server's per-type
// limit applies to the filtered view rather than to a client-side slice
// of a mixed list.
import { html, LitElement, nothing, type PropertyValues } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { get } from '../lib/api';
import { formatJstDateTime } from '../lib/jst-datetime';
import { logger } from '../lib/logger';
import { lightDomErrorClass, lightDomWsNoticeClass } from '../lib/styles';
import { resolveWsUrl, WsClient, type WsStatus } from '../lib/ws';
import { renderWsDisconnected } from '../lib/ws-status';
import {
  type ActivityAPIResponse,
  type ActivityEvent,
  type ActivityWsMessage,
  EVENT_TYPES,
  KILL_SWITCH_LIMIT,
  MAX_EVENTS,
  QUEUES,
  type QueueStatus,
} from './activity-feed-types';
import { renderKillSwitchEvents, renderQueueStatus } from './activity-feed-views';

@customElement('pitha-activity-feed')
export class PithaActivityFeed extends LitElement {
  // Light DOM. organisms.QueueStatusPanel / ActivityFeedFallback are this
  // element's server-rendered children, and the module script is deferred,
  // so the element is upgraded with them already attached (the same
  // reasoning as pitha-scanner-table's createRenderRoot). lit-html leaves
  // pre-existing children alone, so they are remembered here and removed
  // by willUpdate once the first snapshot arrives - otherwise Lit's first
  // render would append a second set of tables. Until then (and if the
  // initial fetch fails) they stay visible, so hydration never blanks the
  // page or discards data the server already rendered.
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

  // URLs are injected by Templ (pages.ActivityLogPage); the component
  // owns none (docs/components/lit.md §5.5). `kill-switch-events-url` is
  // the complete URL of the recent Kill Switch events query, so the
  // query string is never assembled client-side. Its server-side limit
  // must equal KILL_SWITCH_LIMIT, which trims pushed events.
  @property({ type: String, attribute: 'api-url' }) apiUrl = '';
  @property({ type: String, attribute: 'ws-url' }) wsUrl = '';
  @property({ type: String, attribute: 'kill-switch-events-url' }) killSwitchEventsUrl = '';

  @state() private queues: QueueStatus[] = [];
  @state() private events: ActivityEvent[] = [];
  @state() private killSwitchEvents: ActivityEvent[] = [];
  // Distinguishes "not loaded yet" / "failed" / "zero events" so a missing
  // Kill Switch history is never shown as "no events" (safety information).
  @state() private killSwitchLoaded = false;
  @state() private killSwitchError: string | null = null;
  // True once a snapshot has arrived. Until then the SSR tables stay in
  // place and the empty queues/events are never rendered over them.
  @state() private loaded = false;
  @state() private typeFilter = '';
  @state() private queueFilter = '';
  @state() private error: string | null = null;
  @state() private wsStatus: WsStatus = 'connecting';
  // RFC 3339 time of the latest snapshot; the table caption mirrors the SSR "as of".
  @state() private asOf = '';

  private wsClient: WsClient<ActivityWsMessage> | null = null;
  private snapshotGeneration = 0;

  override connectedCallback(): void {
    super.connectedCallback();
    this.loadSnapshot();
    this.loadKillSwitchEvents();
    this.subscribeWs();
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.wsClient?.close();
    this.wsClient = null;
  }

  private feedUrl(): string {
    const params = new URLSearchParams();
    if (this.typeFilter) params.set('type', this.typeFilter);
    if (this.queueFilter) params.set('queue', this.queueFilter);
    const query = params.toString();
    return query ? `${this.apiUrl}?${query}` : this.apiUrl;
  }

  // `background` marks a resync the page fires by itself (WS reconnect), so
  // it is not counted as operator activity (FR-RISK-6, flows.md §10.4).
  private async loadSnapshot(background = false): Promise<void> {
    if (!this.apiUrl) {
      logger.error('pitha-activity-feed: api-url is not set');
      return;
    }
    // Only the latest request may touch state: a slower, older response
    // (previous filter, or a pre-reconnect fetch) must not overwrite it.
    const generation = ++this.snapshotGeneration;
    try {
      const response = await get<ActivityAPIResponse>(this.feedUrl(), { background });
      if (generation !== this.snapshotGeneration) return;
      this.queues = response.queues;
      this.events = response.events;
      this.asOf = response.as_of;
      this.loaded = true;
      this.error = null;
    } catch (err) {
      if (generation !== this.snapshotGeneration) return;
      this.error = err instanceof Error ? err.message : String(err);
      logger.error('pitha-activity-feed: failed to load activity snapshot', { error: err });
    }
  }

  private async loadKillSwitchEvents(background = false): Promise<void> {
    if (!this.killSwitchEventsUrl) {
      logger.error('pitha-activity-feed: kill-switch-events-url is not set');
      return;
    }
    try {
      const response = await get<ActivityAPIResponse>(this.killSwitchEventsUrl, { background });
      this.killSwitchEvents = response.events;
      this.killSwitchLoaded = true;
      this.killSwitchError = null;
    } catch (err) {
      this.killSwitchError = err instanceof Error ? err.message : String(err);
      logger.error('pitha-activity-feed: failed to load kill switch events', { error: err });
    }
  }

  private subscribeWs(): void {
    if (!this.wsUrl) {
      logger.error('pitha-activity-feed: ws-url is not set');
      return;
    }
    // The server sends nothing on connect, so events emitted while the
    // socket was down are lost unless the snapshots are re-fetched (#221).
    this.wsClient = new WsClient<ActivityWsMessage>(resolveWsUrl(this.wsUrl), {
      onStatusChange: (status) => {
        this.wsStatus = status;
      },
      onReconnect: () => {
        void this.loadSnapshot(true);
        void this.loadKillSwitchEvents(true);
      },
      onMessage: (message) => this.onWsMessage(message),
    });
  }

  private onWsMessage(message: ActivityWsMessage): void {
    if (message.type === 'job_update') {
      this.queues = this.queues.map((q) =>
        q.queue === message.queue
          ? {
              ...q,
              pending: message.pending,
              running: message.running,
              failed_recent: message.failed_recent ?? q.failed_recent,
            }
          : q,
      );
    } else if (message.type === 'resync') {
      // The server dropped messages for this slow client (#536).
      void this.loadSnapshot(true);
      void this.loadKillSwitchEvents(true);
    } else if (message.type === 'activity_event') {
      const { event } = message;
      if (this.matchesFilter(event)) {
        this.events = [event, ...this.events].slice(0, MAX_EVENTS);
      }
      if (event.type === 'kill_switch') {
        this.killSwitchEvents = [event, ...this.killSwitchEvents].slice(0, KILL_SWITCH_LIMIT);
      }
    }
  }

  // Mirrors the server's filter semantics (activityfeed.Service): a queue
  // filter matches only job events on that queue.
  private matchesFilter(event: ActivityEvent): boolean {
    if (this.typeFilter && event.type !== this.typeFilter) return false;
    if (this.queueFilter && (event.type !== 'job' || event.queue !== this.queueFilter))
      return false;
    return true;
  }

  private onTypeChange(event: Event): void {
    this.typeFilter = (event.target as HTMLSelectElement).value;
    this.loadSnapshot();
  }

  private onQueueChange(event: Event): void {
    this.queueFilter = (event.target as HTMLSelectElement).value;
    this.loadSnapshot();
  }

  // Kill Switch history and the WS / error notices are not part of the SSR
  // fallback, so they render right away. The Job Queues and Recent Activity
  // tables are SSR'd too: until the first snapshot arrives (see
  // createRenderRoot) the SSR copies stay and only these are added beside them.
  protected override render() {
    if (!this.loaded) {
      return html`${this.renderKillSwitchEvents()}${this.renderNotices()}`;
    }
    return html`
      ${renderQueueStatus(this.queues)}

      ${this.renderKillSwitchEvents()}

      <section id="activity-feed" data-testid="activity-feed" class="mt-6">
        <h2 class="mb-2 text-lg font-semibold text-slate-900">Recent Activity</h2>
        <div class="mb-2 flex gap-4 text-sm">
          <label>
            Type
            <select data-filter="type" .value=${this.typeFilter} @change=${this.onTypeChange}>
              <option value="">all</option>
              ${EVENT_TYPES.map((t) => html`<option value=${t} ?selected=${this.typeFilter === t}>${t}</option>`)}
            </select>
          </label>
          <label>
            Queue
            <select data-filter="queue" .value=${this.queueFilter} @change=${this.onQueueChange}>
              <option value="">all</option>
              ${QUEUES.map((q) => html`<option value=${q} ?selected=${this.queueFilter === q}>${q}</option>`)}
            </select>
          </label>
        </div>
        <table class="w-full border-collapse text-left text-sm" aria-label="アクティビティフィード">
          ${this.asOf ? html`<caption class="mb-2 text-left text-sm text-slate-500">as of ${formatJstDateTime(this.asOf)}</caption>` : nothing}
          <thead>
            <tr class="border-b border-slate-200 text-xs font-semibold uppercase text-slate-500">
              <th scope="col" class="px-3 py-2">Time</th>
              <th scope="col" class="px-3 py-2">Type</th>
              <th scope="col" class="px-3 py-2">Symbol</th>
              <th scope="col" class="px-3 py-2">Detail</th>
              <th scope="col" class="px-3 py-2">Latency (ms)</th>
            </tr>
          </thead>
          <tbody>
            ${this.events.map(
              (e) => html`
                <tr class="border-b border-slate-100" data-event-type=${e.type}>
                  <td class="px-3 py-2">${formatJstDateTime(e.timestamp)}</td>
                  <td class="px-3 py-2 font-medium text-slate-900">${e.type}</td>
                  <td class="px-3 py-2">${e.symbol ?? '—'}</td>
                  <td class="px-3 py-2">${e.detail}</td>
                  <td class="px-3 py-2">${e.latency_ms ?? '—'}</td>
                </tr>
              `,
            )}
          </tbody>
        </table>
      </section>
      ${this.renderNotices()}
    `;
  }

  private renderKillSwitchEvents() {
    return renderKillSwitchEvents(
      this.killSwitchEvents,
      this.killSwitchLoaded,
      this.killSwitchError,
    );
  }

  private renderNotices() {
    return html`${renderWsDisconnected(this.wsStatus, lightDomWsNoticeClass)}${this.error ? html`<p class="pitha-activity-feed-error ${lightDomErrorClass}" role="alert">${this.error}</p>` : nothing}`;
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

declare global {
  interface HTMLElementTagNameMap {
    'pitha-activity-feed': PithaActivityFeed;
  }
}
