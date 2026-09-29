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
import { logger } from '../lib/logger';
import { resolveWsUrl, WsClient } from '../lib/ws';

// Mirrors docs/api/endpoints.md §5 `GET /api/v1/activity` shapes.
export interface QueueStatus {
  queue: string;
  pending: number;
  running: number;
  failed_recent: number;
}

export interface ActivityEvent {
  type: string;
  timestamp: string;
  queue?: string;
  symbol?: string;
  detail: string;
  latency_ms?: number;
}

interface ActivityAPIResponse {
  queues: QueueStatus[];
  events: ActivityEvent[];
  as_of: string;
}

// Mirrors docs/api/endpoints.md §6 `/ws/activity` message shapes.
type ActivityWsMessage =
  | { type: 'job_update'; queue: string; pending: number; running: number; failed_recent?: number }
  | { type: 'activity_event'; event: ActivityEvent };

const EVENT_TYPES = ['job', 'jev_scout', 'jev_trader', 'kill_switch'] as const;
const QUEUES = [
  'market-data',
  'feature-calc',
  'jev-scout',
  'jev-trader',
  'risk-check',
  'paper-execution',
  'outcome-labeling',
  'analytics',
] as const;

// Feed size bounds (functional.md FR-ACT-3): the displayed list never
// grows past the API's maximum, however long the page stays open.
const MAX_EVENTS = 500;
const KILL_SWITCH_LIMIT = 10;

@customElement('pitha-activity-feed')
export class PithaActivityFeed extends LitElement {
  // Light DOM, clearing the SSR children first - the same
  // upgrade-after-existing-children reasoning as pitha-scanner-table's
  // createRenderRoot (organisms.QueueStatusPanel /
  // ActivityFeedFallback are this element's server-rendered children).
  protected override createRenderRoot(): HTMLElement {
    this.innerHTML = '';
    return this;
  }

  @property({ type: String, attribute: 'api-url' }) apiUrl = '/api/v1/activity';
  @property({ type: String, attribute: 'ws-url' }) wsUrl = '/ws/activity';

  @state() private queues: QueueStatus[] = [];
  @state() private events: ActivityEvent[] = [];
  @state() private killSwitchEvents: ActivityEvent[] = [];
  @state() private typeFilter = '';
  @state() private queueFilter = '';
  @state() private error: string | null = null;

  private wsClient: WsClient<ActivityWsMessage> | null = null;

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

  private async loadSnapshot(): Promise<void> {
    try {
      const response = await get<ActivityAPIResponse>(this.feedUrl());
      this.queues = response.queues;
      this.events = response.events;
      this.error = null;
    } catch (err) {
      this.error = err instanceof Error ? err.message : String(err);
      logger.error('pitha-activity-feed: failed to load activity snapshot', { error: err });
    }
  }

  private async loadKillSwitchEvents(): Promise<void> {
    try {
      const response = await get<ActivityAPIResponse>(
        `${this.apiUrl}?type=kill_switch&limit=${KILL_SWITCH_LIMIT}`,
      );
      this.killSwitchEvents = response.events;
    } catch (err) {
      logger.error('pitha-activity-feed: failed to load kill switch events', { error: err });
    }
  }

  private subscribeWs(): void {
    this.wsClient = new WsClient<ActivityWsMessage>(resolveWsUrl(this.wsUrl), {
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

  protected override render() {
    return html`
      <section id="queue-status" data-testid="queue-status" class="mt-6">
        <h2 class="mb-2 text-lg font-semibold text-slate-900">Job Queues</h2>
        <table class="w-full border-collapse text-left text-sm">
          <thead>
            <tr class="border-b border-slate-200 text-xs font-semibold uppercase text-slate-500">
              <th class="px-3 py-2">Queue</th>
              <th class="px-3 py-2">Pending</th>
              <th class="px-3 py-2">Running</th>
              <th class="px-3 py-2">Failed (recent)</th>
            </tr>
          </thead>
          <tbody>
            ${this.queues.map(
              (q) => html`
                <tr class="border-b border-slate-100" data-queue=${q.queue}>
                  <td class="px-3 py-2 font-medium text-slate-900">${q.queue}</td>
                  <td class="px-3 py-2">${q.pending}</td>
                  <td class="px-3 py-2">${q.running}</td>
                  <td class="px-3 py-2">${q.failed_recent}</td>
                </tr>
              `,
            )}
          </tbody>
        </table>
      </section>

      <section id="kill-switch-events" data-testid="kill-switch-events" class="mt-6">
        <h2 class="mb-2 text-lg font-semibold text-slate-900">Recent Kill Switch Events</h2>
        ${
          this.killSwitchEvents.length === 0
            ? html`<p class="text-sm text-slate-500">No kill switch events.</p>`
            : html`<ul class="text-sm">
                ${this.killSwitchEvents.map(
                  (e) =>
                    html`<li class="py-1"><span class="text-slate-500">${e.timestamp}</span> ${e.detail}</li>`,
                )}
              </ul>`
        }
      </section>

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
        <table class="w-full border-collapse text-left text-sm">
          <thead>
            <tr class="border-b border-slate-200 text-xs font-semibold uppercase text-slate-500">
              <th class="px-3 py-2">Time</th>
              <th class="px-3 py-2">Type</th>
              <th class="px-3 py-2">Symbol</th>
              <th class="px-3 py-2">Detail</th>
              <th class="px-3 py-2">Latency (ms)</th>
            </tr>
          </thead>
          <tbody>
            ${this.events.map(
              (e) => html`
                <tr class="border-b border-slate-100" data-event-type=${e.type}>
                  <td class="px-3 py-2">${e.timestamp}</td>
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
      ${this.error ? html`<p class="pitha-activity-feed-error" role="alert">${this.error}</p>` : nothing}
    `;
  }

  protected override updated(changed: PropertyValues<this>): void {
    if (changed.has('wsUrl') && this.wsClient) {
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
