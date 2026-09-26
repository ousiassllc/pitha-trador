// `pitha-kill-switch-panel`: system state display + Kill Switch operate
// panel, embedded in Header on every page (docs/components/overview.md
// §5.4). It loads its initial state from `GET /api/v1/system/status`,
// applies `POST /api/v1/system/pause|resume|kill` for operator actions,
// and listens on `/ws/system` for a `kill_switch` event pushed when Risk
// Engine triggers a Kill Switch directly (rather than through this
// panel's own Kill button) - overview.md §8.3. Every state change
// dispatches `systemStateChanged` so Header's HTMX-driven StatusDot
// (organisms.Header) re-fetches and stays in sync (HTMX↔Lit boundary:
// Lit notifies via CustomEvent, HTMX reacts via hx-trigger).
import { html, LitElement } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { get, post } from '../lib/api';
import { logger } from '../lib/logger';
import { resolveWsUrl, WsClient } from '../lib/ws';

export type SystemStatus = 'running' | 'paused' | 'killed';

// Mirrors docs/api/endpoints.md §5's `{"state": "..."}` shape
// (internal/web/handler.SystemStateOutput), shared by
// `GET /api/v1/system/status` and every `POST /api/v1/system/*` action.
interface SystemStateResponse {
  state: SystemStatus;
}

// Mirrors docs/api/endpoints.md §6's `/ws/system`
// `{"type":"kill_switch","reason":"..."}` message
// (internal/web/handler.systemKillSwitchMessage).
interface KillSwitchMessage {
  type: string;
  reason: string;
}

const KILL_CONFIRM_MESSAGE = 'Kill Switchを発動しますか？新規エントリーが停止します。';

@customElement('pitha-kill-switch-panel')
export class PithaKillSwitchPanel extends LitElement {
  // HATEOAS: a caller that already knows the current state (e.g. a
  // future page render with real risk.Engine access) can inject it
  // directly via these attributes; loadInitial() below only fills them
  // in when no such caller exists (organisms.Header's current
  // placeholder embedding).
  @property({ type: String }) status: SystemStatus = 'running';
  @property({ type: Boolean, attribute: 'can-pause' }) canPause = false;
  @property({ type: Boolean, attribute: 'can-resume' }) canResume = false;
  @property({ type: Boolean, attribute: 'can-kill' }) canKill = false;

  @property({ type: String, attribute: 'status-url' }) statusUrl = '/api/v1/system/status';
  @property({ type: String, attribute: 'ws-url' }) wsUrl = '/ws/system';

  @state() private error: string | null = null;
  @state() private busy = false;

  private wsClient: WsClient<KillSwitchMessage> | null = null;

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
      const response = await get<SystemStateResponse>(this.statusUrl);
      this.applyStatus(response.state);
      this.error = null;
    } catch (err) {
      this.error = err instanceof Error ? err.message : String(err);
      logger.error('pitha-kill-switch-panel: failed to load initial status', { error: err });
    }
  }

  private subscribeWs(): void {
    this.wsClient = new WsClient<KillSwitchMessage>(resolveWsUrl(this.wsUrl), {
      onMessage: (message) => {
        if (message.type === 'kill_switch') {
          this.applyStatus('killed');
        }
      },
    });
  }

  // applyStatus updates status plus its derived can-pause/resume/kill
  // flags (docs/api/endpoints.md §4's state diagram: pause only from
  // running, resume from paused or killed, kill from running or
  // paused), then notifies Header regardless of what triggered the
  // change - this panel's own action or a Risk-Engine-triggered
  // `/ws/system` push (components/overview.md §5.4).
  private applyStatus(status: SystemStatus): void {
    this.status = status;
    this.canPause = status === 'running';
    this.canResume = status === 'paused' || status === 'killed';
    this.canKill = status === 'running' || status === 'paused';
    this.dispatchEvent(new CustomEvent('systemStateChanged', { bubbles: true, composed: true }));
  }

  private async performAction(path: string): Promise<void> {
    if (this.busy) return;
    this.busy = true;
    try {
      const response = await post<SystemStateResponse>(path);
      this.applyStatus(response.state);
      this.error = null;
    } catch (err) {
      this.error = err instanceof Error ? err.message : String(err);
      logger.error(`pitha-kill-switch-panel: ${path} failed`, { error: err });
    } finally {
      this.busy = false;
    }
  }

  private onPause(): void {
    void this.performAction('/api/v1/system/pause');
  }

  private onResume(): void {
    void this.performAction('/api/v1/system/resume');
  }

  private onKill(): void {
    if (!window.confirm(KILL_CONFIRM_MESSAGE)) return;
    void this.performAction('/api/v1/system/kill');
  }

  protected override render() {
    return html`
      <div class="pitha-kill-switch-panel" data-status=${this.status}>
        <span class="pitha-kill-switch-panel-status">${this.status}</span>
        ${this.canPause ? html`<button type="button" ?disabled=${this.busy} @click=${this.onPause}>Pause</button>` : ''}
        ${this.canResume ? html`<button type="button" ?disabled=${this.busy} @click=${this.onResume}>Resume</button>` : ''}
        ${this.canKill ? html`<button type="button" ?disabled=${this.busy} @click=${this.onKill}>Kill</button>` : ''}
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
