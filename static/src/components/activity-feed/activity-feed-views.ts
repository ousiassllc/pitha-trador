// Stateless templates for pitha-activity-feed's ジョブの状況 table and 最近の
// Kill Switch section, split out of the element to keep it small.
import { html, nothing } from 'lit';
import { formatJstDateTime } from '../lib/jst-datetime';
import { activityDetail, jobQueueLabel } from '../lib/labels';
import { lightDomErrorClass } from '../lib/styles';
import type { ActivityEvent, QueueStatus } from './activity-feed-types';

export function renderQueueStatus(queues: QueueStatus[]) {
  return html`
    <section id="queue-status" data-testid="queue-status" class="mt-6">
      <h2 class="mb-6 text-xl font-semibold text-ink">ジョブの状況</h2>
      <table class="w-full border-collapse text-left text-sm" aria-label="キュー状況">
        <thead>
          <tr class="border-b border-border text-xs font-semibold uppercase text-mute">
            <th scope="col" class="px-3 py-2.5">キュー</th>
            <th scope="col" class="px-3 py-2.5 text-right">待機中</th>
            <th scope="col" class="px-3 py-2.5 text-right">実行中</th>
            <th scope="col" class="px-3 py-2.5 text-right">失敗（直近）</th>
          </tr>
        </thead>
        <tbody>
          ${queues.map(
            (q) => html`
              <tr class="border-b border-border" data-queue=${q.queue}>
                <td class="px-3 py-2.5 text-ink">${jobQueueLabel(q.queue)}</td>
                <td class="px-3 py-2.5 text-right font-mono tabular-nums">${q.pending}</td>
                <td class="px-3 py-2.5 text-right font-mono tabular-nums">${q.running}</td>
                <td class="px-3 py-2.5 text-right font-mono tabular-nums">${q.failed_recent}</td>
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
    <section id="kill-switch-events" data-testid="kill-switch-events" class="mt-12">
      <h2 class="mb-6 text-xl font-semibold text-ink">最近の Kill Switch</h2>
      ${
        error
          ? html`<p class="pitha-activity-feed-error ${lightDomErrorClass}" role="alert">Kill Switch の履歴を読み込めませんでした: ${error}</p>`
          : nothing
      }
      ${
        events.length > 0
          ? html`<ul class="text-sm">
              ${events.map(
                (e) =>
                  html`<li class="py-1"><span class="text-mute">${formatJstDateTime(e.timestamp)}</span> ${activityDetail(e.type, e.detail)}</li>`,
              )}
            </ul>`
          : error
            ? nothing
            : html`<p class="text-sm text-mute">${loaded ? 'Kill Switch の履歴はありません。' : 'Kill Switch の履歴を読み込んでいます…'}</p>`
      }
    </section>
  `;
}
