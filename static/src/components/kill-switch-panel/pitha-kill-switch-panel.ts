// `pitha-kill-switch-panel`: system state display + Kill Switch operate
// panel, embedded in Header on every page (docs/components/overview.md
// §5.4). HATEOAS (issue #106): organisms.KillSwitchPanel server-renders
// `status`, `can-pause`/`can-resume`/`can-kill` and every URL this
// component calls (`pause-url`/`resume-url`/`kill-url`/`status-url`/
// `ws-url`); the component has no URL of its own and does not derive which
// actions are allowed from `status` - after each action or resync it takes
// `can_pause`/`can_resume`/`can_kill` from the server's response. It
// listens on `/ws/system` for a `kill_switch` event pushed when Risk
// Engine triggers a Kill Switch directly (rather than through this
// panel's own Kill button) - overview.md §10.3 - and resyncs from
// `status-url` then, and after a WebSocket reconnect (a push may have been
// missed while disconnected). Every state change dispatches
// `systemStateChanged` so Header's HTMX-driven StatusDot
// (organisms.Header) re-fetches and stays in sync (HTMX↔Lit boundary:
// Lit notifies via CustomEvent, HTMX reacts via hx-trigger).
import { css, html, LitElement } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { get, post } from '../lib/api';
import { logger } from '../lib/logger';
import { buttonStyles, noticeStyles } from '../lib/styles';
import { isWsDisconnected, resolveWsUrl, WsClient, type WsStatus } from '../lib/ws';
import { renderWsDisconnected } from '../lib/ws-status';

export type SystemStatus = 'running' | 'paused' | 'killed';

// Mirrors docs/api/endpoints.md §5's response shape
// (internal/web/handler.SystemStateOutput), shared by
// `GET /api/v1/system/status` and every `POST /api/v1/system/*` action.
interface SystemStateResponse {
  state: SystemStatus;
  can_pause: boolean;
  can_resume: boolean;
  can_kill: boolean;
}

// Mirrors docs/api/endpoints.md §6's `/ws/system`
// `{"type":"kill_switch","reason":"..."}` message
// (internal/web/handler.systemKillSwitchMessage).
interface KillSwitchMessage {
  type: string;
  reason: string;
}

// Kill force-closes every open position (Engine.Kill → closer.CloseAll,
// FR-RISK-3 / UC-11), so the only safety net must say so (issue #194).
const KILL_CONFIRM_MESSAGE =
  'Kill Switchを発動しますか？新規エントリーが停止し、保有中の全ポジションが強制決済されます。';

@customElement('pitha-kill-switch-panel')
export class PithaKillSwitchPanel extends LitElement {
  // Shadow DOM: Tailwind does not reach in here, so style locally. Kill is
  // red so the dangerous action stands apart from Pause / Resume.
  static override styles = [
    buttonStyles,
    noticeStyles,
    css`
      .pitha-kill-switch-panel {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: 0.5rem;
      }
      .pitha-kill-switch-panel-status {
        border-radius: 9999px;
        background: #e2e8f0;
        padding: 0.125rem 0.625rem;
        font-size: 0.75rem;
        font-weight: 600;
        text-transform: uppercase;
      }
      [data-status='running'] .pitha-kill-switch-panel-status {
        background: #dcfce7;
        color: #166534;
      }
      [data-status='paused'] .pitha-kill-switch-panel-status {
        background: #fef3c7;
        color: #92400e;
      }
      [data-status='killed'] .pitha-kill-switch-panel-status {
        background: #fee2e2;
        color: #991b1b;
      }
      button.pitha-kill-switch-panel-kill {
        border-color: #b91c1c;
        background: #dc2626;
        color: #ffffff;
        font-weight: 600;
      }
      button.pitha-kill-switch-panel-kill:hover:not(:disabled) {
        background: #b91c1c;
      }
    `,
  ];

  // Injected by organisms.KillSwitchPanel ('' while the server could not
  // read the state); refreshed from the server's responses afterwards.
  @property({ type: String }) status: SystemStatus | '' = '';
  @property({ type: Boolean, attribute: 'can-pause' }) canPause = false;
  @property({ type: Boolean, attribute: 'can-resume' }) canResume = false;
  @property({ type: Boolean, attribute: 'can-kill' }) canKill = false;

  @property({ type: String, attribute: 'pause-url' }) pauseUrl = '';
  @property({ type: String, attribute: 'resume-url' }) resumeUrl = '';
  @property({ type: String, attribute: 'kill-url' }) killUrl = '';
  @property({ type: String, attribute: 'status-url' }) statusUrl = '';
  @property({ type: String, attribute: 'ws-url' }) wsUrl = '';

  @state() private error: string | null = null;
  @state() private busy = false;
  @state() private wsStatus: WsStatus = 'connecting';

  private wsClient: WsClient<KillSwitchMessage> | null = null;

  override connectedCallback(): void {
    super.connectedCallback();
    void this.resync();
    this.subscribeWs();
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.wsClient?.close();
    this.wsClient = null;
  }

  // resync re-reads the state and allowed actions from status-url. It is
  // always auto-fired (initial load, Kill Switch push, WS reconnect), never
  // an operator action, so it is sent as a background request that must not
  // refresh the operator heartbeat (FR-RISK-6 dead-man's switch).
  private async resync(): Promise<void> {
    if (!this.statusUrl) return;
    try {
      this.applyState(await get<SystemStateResponse>(this.statusUrl, { background: true }));
      this.error = null;
    } catch (err) {
      this.error = err instanceof Error ? err.message : String(err);
      logger.error('pitha-kill-switch-panel: failed to load status', { error: err });
    }
  }

  private subscribeWs(): void {
    if (!this.wsUrl) return;
    let wasDisconnected = false;
    this.wsClient = new WsClient<KillSwitchMessage>(resolveWsUrl(this.wsUrl), {
      onStatusChange: (status) => {
        this.wsStatus = status;
        if (isWsDisconnected(status)) {
          wasDisconnected = true;
        } else if (status === 'open' && wasDisconnected) {
          wasDisconnected = false;
          void this.resync();
        }
      },
      onMessage: (message) => {
        if (message.type === 'kill_switch') {
          this.onKillSwitchPush();
        }
      },
    });
  }

  // The push only says "killed"; which actions are allowed from there is
  // the server's call, so drop every action until resync() returns them.
  private onKillSwitchPush(): void {
    this.status = 'killed';
    this.canPause = false;
    this.canResume = false;
    this.canKill = false;
    this.dispatchEvent(new CustomEvent('systemStateChanged', { bubbles: true, composed: true }));
    void this.resync();
  }

  // applyState takes the state and allowed actions from a server response,
  // then notifies Header regardless of what triggered the change - this
  // panel's own action or a resync (components/overview.md §5.4).
  private applyState(response: SystemStateResponse): void {
    this.status = response.state;
    this.canPause = response.can_pause;
    this.canResume = response.can_resume;
    this.canKill = response.can_kill;
    this.dispatchEvent(new CustomEvent('systemStateChanged', { bubbles: true, composed: true }));
  }

  private async performAction(url: string): Promise<void> {
    if (this.busy || !url) return;
    this.busy = true;
    try {
      this.applyState(await post<SystemStateResponse>(url));
      this.error = null;
    } catch (err) {
      logger.error(`pitha-kill-switch-panel: ${url} failed`, { error: err });
      // The server may have changed state despite the failure (Kill sets
      // killed before it liquidates, so a failed liquidation still leaves
      // the system killed - issue #195): resync, then keep the action's
      // own error visible over whatever resync() left in `error`.
      await this.resync();
      this.error = err instanceof Error ? err.message : String(err);
    } finally {
      this.busy = false;
    }
  }

  private onPause(): void {
    void this.performAction(this.pauseUrl);
  }

  private onResume(): void {
    void this.performAction(this.resumeUrl);
  }

  private onKill(): void {
    if (!window.confirm(KILL_CONFIRM_MESSAGE)) return;
    void this.performAction(this.killUrl);
  }

  protected override render() {
    return html`
      <div class="pitha-kill-switch-panel" data-status=${this.status}>
        <span class="pitha-kill-switch-panel-status">${this.status}</span>
        ${this.canPause ? html`<button type="button" ?disabled=${this.busy} @click=${this.onPause}>Pause</button>` : ''}
        ${this.canResume ? html`<button type="button" ?disabled=${this.busy} @click=${this.onResume}>Resume</button>` : ''}
        ${this.canKill ? html`<button type="button" class="pitha-kill-switch-panel-kill" ?disabled=${this.busy} @click=${this.onKill}>Kill</button>` : ''}
        ${renderWsDisconnected(this.wsStatus)}
        ${this.error ? html`<p class="pitha-kill-switch-panel-error" role="alert">${this.error}</p>` : ''}
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'pitha-kill-switch-panel': PithaKillSwitchPanel;
  }
}
