import { describe, expect, mock, spyOn, test } from 'bun:test';
import {
  createFeed,
  emit,
  event,
  FakeWebSocket,
  type FeedElement,
  flush,
  installFakes,
  mount,
  queue,
} from './activity-feed-test-support';
import { PithaActivityFeed } from './pitha-activity-feed';

installFakes();

// Same upgrade-after-existing-children timing as
// pitha-scanner-table.hydration.test.ts: the SSR markup must already be
// in the DOM when the (subclass) element is defined, or this would be a
// fresh construction that never sees children to preserve.
function hydrate(tag: string): FeedElement {
  document.body.innerHTML = `<${tag} api-url="/api/v1/activity" ws-url="/ws/activity" kill-switch-events-url="/api/v1/activity?type=kill_switch&limit=10"><section id="queue-status"><table><tbody><tr data-queue="ssr"></tr></tbody></table></section><section id="activity-feed"><table><tbody><tr data-event-type="ssr"></tr></tbody></table></section></${tag}>`;
  // happy-dom runs connectedCallback before delivering the existing
  // attributes on upgrade, unlike browsers (attributeChangedCallback
  // first). Read them in the constructor to reproduce the browser
  // order, since the component has no URL defaults.
  class PithaActivityFeedUnderTest extends PithaActivityFeed {
    constructor() {
      super();
      this.apiUrl = this.getAttribute('api-url') ?? '';
      this.wsUrl = this.getAttribute('ws-url') ?? '';
      this.killSwitchEventsUrl = this.getAttribute('kill-switch-events-url') ?? '';
    }
  }
  customElements.define(tag, PithaActivityFeedUnderTest);
  return document.querySelector(tag) as FeedElement;
}

describe('pitha-activity-feed', () => {
  test('replaces the server-rendered fallback with the snapshot once it arrives', async () => {
    globalThis.fetch = mock(() =>
      Promise.resolve(
        new Response(JSON.stringify({ queues: [queue()], events: [event()], as_of: 'x' })),
      ),
    ) as unknown as typeof fetch;
    const el = hydrate('pitha-activity-feed-hydration-test');
    await flush(el);

    expect(el.querySelectorAll('#queue-status')).toHaveLength(1);
    expect(el.querySelectorAll('#activity-feed')).toHaveLength(1);
    expect(el.querySelector('[data-queue="ssr"]')).toBeNull();
    expect(el.querySelector('[data-event-type="ssr"]')).toBeNull();
    expect(el.querySelector('[data-queue="jev-scout"]')).not.toBeNull();
  });

  test('keeps the server-rendered fallback while loading and when the snapshot fetch fails', async () => {
    globalThis.fetch = mock(() =>
      Promise.resolve(new Response('boom', { status: 500 })),
    ) as unknown as typeof fetch;
    const el = hydrate('pitha-activity-feed-hydration-failure-test');
    await flush(el);

    expect(el.querySelector('[data-queue="ssr"]')).not.toBeNull();
    expect(el.querySelector('[data-event-type="ssr"]')).not.toBeNull();
    expect(el.querySelectorAll('#queue-status')).toHaveLength(1);
    expect(el.querySelectorAll('#activity-feed')).toHaveLength(1);
    expect(el.querySelector('[role="alert"]')).not.toBeNull();
    // Light DOM: Shadow `noticeStyles` do not apply, so Tailwind classes must (#355).
    expect(el.querySelector('[role="alert"]')?.classList.contains('text-red-700')).toBe(true);
  });

  test('renders queue counts and feed rows from the snapshot', async () => {
    const { el } = await mount(
      [queue(), queue({ queue: 'jev-trader', pending: 0, running: 0, failed_recent: 2 })],
      [
        event(),
        event({
          type: 'job',
          symbol: undefined,
          latency_ms: undefined,
          detail: 'queue=jev-scout status=running attempts=1',
        }),
      ],
    );

    const queueRows = el.querySelectorAll('#queue-status tbody tr');
    expect(queueRows).toHaveLength(2);
    expect(queueRows[1].textContent).toContain('jev-trader');
    expect(queueRows[1].querySelectorAll('td')[3].textContent).toBe('2');

    const rows = el.querySelectorAll('#activity-feed tbody tr');
    expect(rows).toHaveLength(2);
    expect(rows[0].textContent).toContain('direction=LONG');
    expect(rows[0].textContent).toContain('820');
    expect(rows[1].querySelectorAll('td')[2].textContent).toBe('—'); // no symbol
    expect(rows[1].querySelectorAll('td')[4].textContent).toBe('—'); // no latency
  });

  test('lists kill switch events in their own section', async () => {
    const ks = event({
      type: 'kill_switch',
      symbol: undefined,
      latency_ms: undefined,
      detail: 'reason=daily_loss_limit',
    });
    const { el } = await mount([queue()], [event()], [ks]);

    expect(el.querySelector('#kill-switch-events')?.textContent).toContain(
      'reason=daily_loss_limit',
    );
  });

  test('shows "No kill switch events." only after a successful empty load', async () => {
    let release: (res: Response) => void = () => {};
    const fetchMock = mock((url: string) =>
      url.includes('type=kill_switch')
        ? new Promise<Response>((resolve) => {
            release = resolve;
          })
        : Promise.resolve(new Response(JSON.stringify({ queues: [], events: [], as_of: 'x' }))),
    );
    globalThis.fetch = fetchMock as unknown as typeof fetch;
    const el = createFeed();
    document.body.appendChild(el);
    await flush(el);

    const section = () => el.querySelector('#kill-switch-events')?.textContent ?? '';
    expect(section()).not.toContain('No kill switch events.');
    expect(section()).toContain('Loading kill switch events');

    release(new Response(JSON.stringify({ queues: [], events: [], as_of: 'x' })));
    await flush(el);
    expect(section()).toContain('No kill switch events.');
  });

  test('shows an alert instead of "No kill switch events." when the fetch fails', async () => {
    const errorSpy = spyOn(console, 'error').mockImplementation(() => {});
    globalThis.fetch = mock((url: string) =>
      Promise.resolve(
        url.includes('type=kill_switch')
          ? new Response('boom', { status: 500 })
          : new Response(JSON.stringify({ queues: [], events: [], as_of: 'x' })),
      ),
    ) as unknown as typeof fetch;
    const el = createFeed();
    document.body.appendChild(el);
    await flush(el);

    const section = el.querySelector('#kill-switch-events');
    expect(section?.querySelector('[role="alert"]')).not.toBeNull();
    expect(section?.querySelector('[role="alert"]')?.classList.contains('text-red-700')).toBe(true);
    expect(section?.textContent).not.toContain('No kill switch events.');
    expect(section?.textContent).not.toContain('Loading kill switch events');
    errorSpy.mockRestore();
  });

  test('job_update pushes replace only that queue counts', async () => {
    const { el, fetchMock } = await mount(
      [queue(), queue({ queue: 'jev-trader', pending: 5 })],
      [],
    );
    const before = fetchMock.mock.calls.length;

    emit({ type: 'job_update', queue: 'jev-scout', pending: 0, running: 2, failed_recent: 1 });
    await el.updateComplete;

    const cells = (name: string) =>
      [...el.querySelectorAll(`[data-queue="${name}"] td`)].map((td) => td.textContent);
    expect(cells('jev-scout')).toEqual(['jev-scout', '0', '2', '1']);
    expect(cells('jev-trader')).toEqual(['jev-trader', '5', '1', '0']);
    expect(fetchMock.mock.calls.length).toBe(before); // no re-fetch
  });

  test('activity_event pushes are prepended newest first', async () => {
    const { el } = await mount([queue()], [event({ symbol: '7203' })]);

    emit({
      type: 'activity_event',
      event: event({ type: 'jev_scout', symbol: '9984', timestamp: '2026-09-29T01:16:00Z' }),
    });
    await el.updateComplete;

    const rows = el.querySelectorAll('#activity-feed tbody tr');
    expect(rows).toHaveLength(2);
    expect(rows[0].getAttribute('data-event-type')).toBe('jev_scout');
    expect(rows[0].textContent).toContain('9984');
  });

  // The first update cycle used to close and reopen the socket (issue #170).
  test('opens a single WebSocket on mount and reconnects once when ws-url changes', async () => {
    const { el } = await mount([queue()], []);
    expect(FakeWebSocket.instances).toHaveLength(1);

    el.setAttribute('ws-url', '/ws/activity-2');
    await el.updateComplete;

    expect(FakeWebSocket.instances).toHaveLength(2);
    expect(FakeWebSocket.instances[1].url).toContain('/ws/activity-2');
  });

  test('logs an error and makes no request when the injected URLs are missing', async () => {
    const errorSpy = spyOn(console, 'error').mockImplementation(() => {});
    const fetchMock = mock(() => Promise.resolve(new Response('{}')));
    globalThis.fetch = fetchMock as unknown as typeof fetch;
    const el = document.createElement('pitha-activity-feed') as FeedElement; // no URL attributes
    document.body.appendChild(el);
    await flush(el);

    expect(fetchMock).not.toHaveBeenCalled();
    expect(FakeWebSocket.instances).toHaveLength(0);
    for (const name of ['api-url', 'ws-url', 'kill-switch-events-url']) {
      expect(errorSpy).toHaveBeenCalledWith(
        expect.objectContaining({ message: `pitha-activity-feed: ${name} is not set` }),
      );
    }
    errorSpy.mockRestore();
  });
});
