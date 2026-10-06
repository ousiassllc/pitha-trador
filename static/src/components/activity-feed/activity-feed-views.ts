// Stateless templates for pitha-activity-feed's Job Queues table and Recent
// Kill Switch Events section, split out of the element to keep it small.
import { html, nothing } from 'lit';
import { formatJstDateTime } from '../lib/jst-datetime';
import { lightDomErrorClass } from '../lib/styles';
import type { ActivityEvent, QueueStatus } from './activity-feed-types';

export function renderQueueStatus(queues: QueueStatus[]) {
  return html`
    <section id="queue-status" data-testid="queue-status" class="mt-6">
      <h2 class="mb-2 text-lg font-semibold text-slate-900">Job Queues</h2>
      <table class="w-full border-collapse text-left text-sm" aria-label="キュー状況">
        <thead>
          <tr class="border-b border-slate-200 text-xs font-semibold uppercase text-slate-500">
            <th scope="col" class="px-3 py-2">Queue</th>
            <th scope="col" class="px-3 py-2">Pending</th>
            <th scope="col" class="px-3 py-2">Running</th>
            <th scope="col" class="px-3 py-2">Failed (recent)</th>
          </tr>
        </thead>
        <tbody>
          ${queues.map(
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
  `;
}

// `loaded` / `error` distinguish "not loaded yet" / "failed" / "zero events"
// so a missing Kill Switch history is never shown as "no events".
export function renderKillSwitchEvents(
  events: ActivityEvent[],
  loaded: boolean,
  error: string | null,
) {
  return html`
    <section id="kill-switch-events" data-testid="kill-switch-events" class="mt-6">
      <h2 class="mb-2 text-lg font-semibold text-slate-900">Recent Kill Switch Events</h2>
      ${
        error
          ? html`<p class="pitha-activity-feed-error ${lightDomErrorClass}" role="alert">Failed to load kill switch events: ${error}</p>`
          : nothing
      }
      ${
        events.length > 0
          ? html`<ul class="text-sm">
              ${events.map(
                (e) =>
                  html`<li class="py-1"><span class="text-slate-500">${formatJstDateTime(e.timestamp)}</span> ${e.detail}</li>`,
              )}
            </ul>`
          : error
            ? nothing
            : html`<p class="text-sm text-slate-500">${loaded ? 'No kill switch events.' : 'Loading kill switch events…'}</p>`
      }
    </section>
  `;
}
