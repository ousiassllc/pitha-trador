// `pitha-exit-alignment`: Calibration's "label basis versus actual exit" block
// (issue #774, parent #751/#752). It loads `GET /api/v1/calibration/exit-alignment`
// once on connect and on the refresh button: the table of the label's TP/SL/
// timeout/cost against the actual exit, the mismatch warnings, the per-decision
// label-versus-replay comparison and the model's OOS EV beside the Paper return.
import { css, html, LitElement } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { get } from '../lib/api';
import { logger } from '../lib/logger';
import { buttonStyles, noticeStyles } from '../lib/styles';
import {
  type AlignmentResponse,
  renderComparison,
  renderOOSPaper,
  renderTable,
  renderWarnings,
} from './exit-alignment-view';

@customElement('pitha-exit-alignment')
export class PithaExitAlignment extends LitElement {
  static override styles = [
    buttonStyles,
    noticeStyles,
    css`
      :host {
        display: block;
      }
      table {
        border-collapse: collapse;
        font-size: 0.875rem;
        font-variant-numeric: tabular-nums;
      }
      th,
      td {
        border: 1px solid #cbd5e1;
        padding: 0.25rem 0.5rem;
        text-align: left;
      }
      .warnings {
        margin: 0;
        padding: 0.5rem 1rem;
        border: 1px solid #f59e0b;
        border-radius: 0.375rem;
        background: #fffbeb;
        color: #92400e;
        font-size: 0.875rem;
      }
      .summary {
        display: grid;
        grid-template-columns: max-content 1fr;
        gap: 0.25rem 1rem;
        margin: 0;
        font-size: 0.875rem;
      }
      .summary dt {
        color: #475569;
      }
      .summary dd {
        margin: 0;
      }
      h2,
      h3 {
        margin: 0.75rem 0 0.25rem;
        font-size: 1rem;
      }
    `,
  ];

  @property({ type: String, attribute: 'alignment-url' }) alignmentUrl = '';

  @state() private data: AlignmentResponse | null = null;
  @state() private error: string | null = null;
  @state() private loading = false;

  private loadSeq = 0;

  protected override firstUpdated(): void {
    void this.load();
  }

  private async load(): Promise<void> {
    if (!this.alignmentUrl) {
      logger.error('pitha-exit-alignment: alignment-url is not set');
      return;
    }
    const seq = ++this.loadSeq;
    this.loading = true;
    try {
      const data = await get<AlignmentResponse>(this.alignmentUrl);
      if (seq !== this.loadSeq) return;
      this.data = data;
      this.error = null;
    } catch (err) {
      if (seq !== this.loadSeq) return;
      this.error = err instanceof Error ? err.message : String(err);
      logger.error('pitha-exit-alignment: failed to load the exit alignment', { error: err });
    } finally {
      if (seq === this.loadSeq) this.loading = false;
    }
  }

  protected override render() {
    return html`
      <section aria-label="ラベル基準と実出口の整合">
        <h2>ラベル基準と実出口の整合（#774）</h2>
        <button type="button" @click=${() => void this.load()} ?disabled=${this.loading}>
          ${this.loading ? '更新中…' : '更新'}
        </button>
        ${this.error ? html`<p role="alert">${this.error}</p>` : ''}
        ${
          this.data
            ? html`
              ${renderWarnings(this.data.warnings)}
              ${renderTable(this.data.rows)}
              ${renderComparison(this.data.comparison)}
              ${renderOOSPaper(this.data.oos_paper)}
            `
            : ''
        }
      </section>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'pitha-exit-alignment': PithaExitAlignment;
  }
}
