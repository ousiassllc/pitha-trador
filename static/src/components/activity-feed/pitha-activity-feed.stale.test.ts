import { describe, expect, mock, test } from 'bun:test';
import {
  createFeed,
  emit,
  event,
  type FeedElement,
  flush,
  installFakes,
  queue,
} from './activity-feed-test-support';
import type { ActivityEvent, QueueStatus } from './activity-feed-types';

installFakes();

const KILL_SWITCH_URL = 'type=kill_switch';

const killSwitch = (reason: string, timestamp: string): ActivityEvent =>
  event({
    type: 'kill_switch',
    symbol: undefined,
    latency_ms: undefined,
    detail: `reason=${reason}`,
    timestamp,
  });

const body = (queues: QueueStatus[], events: ActivityEvent[]) =>
  new Response(JSON.stringify({ queues, events, as_of: 'x' }));

// A fetch whose responses the test releases itself, in any order.
function deferredFetch() {
  const feed: ((response: Response) => void)[] = [];
  const killSwitchHistory: ((response: Response) => void)[] = [];
  globalThis.fetch = mock(
    (url: string) =>
      new Promise<Response>((resolve) =>
        (url.includes(KILL_SWITCH_URL) ? killSwitchHistory : feed).push(resolve),
      ),
  ) as unknown as typeof fetch;
  return { feed, killSwitchHistory };
}

async function mountPending(): Promise<FeedElement> {
  const el = createFeed();
  document.body.appendChild(el);
  await flush(el);
  return el;
}

const killSwitchText = (el: FeedElement) =>
  el.querySelector('#kill-switch-events')?.textContent ?? '';
const feedRows = (el: FeedElement) => el.querySelectorAll('#activity-feed tbody tr');
const queueCells = (el: FeedElement, name: string) =>
  Array.from(el.querySelectorAll(`#queue-status tr[data-queue="${name}"] td`)).map((td) =>
    td.textContent?.trim(),
  );

describe('pitha-activity-feed Kill Switch history vs WS pushes', () => {
  // The late initial GET used to replace the list and erase a push (issue #1197).
  test('keeps a kill_switch event pushed while the history GET is in flight', async () => {
    const { killSwitchHistory } = deferredFetch();
    const el = await mountPending();

    emit({
      type: 'activity_event',
      event: killSwitch('operator_manual', '2026-09-29T02:00:00Z'),
    });
    killSwitchHistory[0](body([], [killSwitch('daily_loss_limit', '2026-09-29T01:00:00Z')]));
    await flush(el);

    const text = killSwitchText(el);
    expect(text).toContain('手動で発動した');
    expect(text).toContain('日次の損失上限に達した');
    expect(text.indexOf('手動で発動した')).toBeLessThan(text.indexOf('日次の損失上限に達した'));
  });

  test('does not list a pushed event twice when the response already has it', async () => {
    const { killSwitchHistory } = deferredFetch();
    const el = await mountPending();
    const pushed = killSwitch('operator_manual', '2026-09-29T02:00:00Z');

    emit({ type: 'activity_event', event: pushed });
    killSwitchHistory[0](
      body([], [pushed, killSwitch('daily_loss_limit', '2026-09-29T01:00:00Z')]),
    );
    await flush(el);

    expect(el.querySelectorAll('#kill-switch-events li')).toHaveLength(2);
  });

  test('drops an older history response that arrives after a newer one', async () => {
    const { killSwitchHistory } = deferredFetch();
    const el = await mountPending();
    emit({ type: 'resync' });
    await flush(el);
    expect(killSwitchHistory).toHaveLength(2);

    killSwitchHistory[1](body([], [killSwitch('operator_manual', '2026-09-29T02:00:00Z')]));
    await flush(el);
    killSwitchHistory[0](body([], [killSwitch('daily_loss_limit', '2026-09-29T01:00:00Z')]));
    await flush(el);

    expect(killSwitchText(el)).toContain('手動で発動した');
    expect(killSwitchText(el)).not.toContain('日次の損失上限に達した');
  });

  test('drops the error of a history GET superseded by a newer one', async () => {
    const { killSwitchHistory } = deferredFetch();
    const el = await mountPending();
    emit({ type: 'resync' });
    await flush(el);

    killSwitchHistory[1](body([], []));
    await flush(el);
    killSwitchHistory[0](new Response('{}', { status: 500 }));
    await flush(el);

    expect(el.querySelector('#kill-switch-events [role="alert"]')).toBeNull();
    expect(killSwitchText(el)).toContain('Kill Switch の履歴はありません。');
  });
});

describe('pitha-activity-feed snapshot vs WS pushes', () => {
  test('keeps an activity_event and a job_update pushed while the snapshot GET is in flight', async () => {
    const { feed } = deferredFetch();
    const el = await mountPending();

    emit({ type: 'activity_event', event: event({ symbol: '6758' }) });
    emit({ type: 'job_update', queue: 'jev-scout', pending: 9, running: 2 });
    feed[0](body([queue({ pending: 3, running: 1 })], [event({ symbol: '7203' })]));
    await flush(el);

    const symbols = Array.from(feedRows(el)).map((row) => row.children[2].textContent?.trim());
    expect(symbols).toEqual(['6758', '7203']);
    expect(queueCells(el, 'jev-scout')[1]).toBe('9');
    expect(queueCells(el, 'jev-scout')[2]).toBe('2');
  });

  test('does not list a pushed activity_event twice when the response already has it', async () => {
    const { feed } = deferredFetch();
    const el = await mountPending();
    const pushed = event({ symbol: '6758' });

    emit({ type: 'activity_event', event: pushed });
    feed[0](body([queue()], [pushed, event({ symbol: '7203' })]));
    await flush(el);

    expect(feedRows(el)).toHaveLength(2);
  });

  // A filter change that a push overtakes still has to land: the pushes are
  // deltas, so dropping the GET would leave the old filter's rows on screen.
  test('still applies a filter-change response after a push, minus non-matching pushes', async () => {
    const { feed } = deferredFetch();
    const el = await mountPending();
    feed[0](body([queue()], [event({ symbol: '7203' })]));
    await flush(el);

    const select = el.querySelector('select[data-filter="type"]') as HTMLSelectElement;
    select.value = 'jev_scout';
    select.dispatchEvent(new Event('change'));
    await flush(el);

    emit({ type: 'activity_event', event: event({ type: 'jev_trader', symbol: '1111' }) });
    emit({ type: 'activity_event', event: event({ type: 'jev_scout', symbol: '2222' }) });
    feed[1](body([queue()], [event({ type: 'jev_scout', symbol: '3333' })]));
    await flush(el);

    const symbols = Array.from(feedRows(el)).map((row) => row.children[2].textContent?.trim());
    expect(symbols).toEqual(['2222', '3333']);
  });

  test('drops an older snapshot response that arrives after a newer one', async () => {
    const { feed } = deferredFetch();
    const el = await mountPending();
    emit({ type: 'resync' });
    await flush(el);
    expect(feed).toHaveLength(2);

    feed[1](body([queue()], [event({ symbol: '6758' })]));
    await flush(el);
    feed[0](body([queue()], [event({ symbol: '7203' })]));
    await flush(el);

    expect(feedRows(el)).toHaveLength(1);
    expect(feedRows(el)[0].textContent).toContain('6758');
  });
});
