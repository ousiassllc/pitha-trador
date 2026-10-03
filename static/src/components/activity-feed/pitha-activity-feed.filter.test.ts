import { describe, expect, test } from 'bun:test';
import { emit, event, flush, installFakes, mount, queue } from './activity-feed-test-support';

installFakes();

describe('pitha-activity-feed filters', () => {
  test('type filter re-fetches with ?type= and drops non-matching pushes', async () => {
    const { el, fetchMock } = await mount([queue()], [event()]);

    const select = el.querySelector('select[data-filter="type"]') as HTMLSelectElement;
    select.value = 'jev_scout';
    select.dispatchEvent(new Event('change'));
    await flush(el);
    const urls = fetchMock.mock.calls.map((c) => c[0] as string);
    expect(urls.some((u) => u.includes('?type=jev_scout') && !u.includes('kill_switch'))).toBe(
      true,
    );

    const rowsBefore = el.querySelectorAll('#activity-feed tbody tr').length;
    emit({ type: 'activity_event', event: event({ type: 'jev_trader', symbol: '1111' }) });
    await el.updateComplete;
    expect(el.querySelectorAll('#activity-feed tbody tr')).toHaveLength(rowsBefore);

    emit({ type: 'activity_event', event: event({ type: 'jev_scout', symbol: '2222' }) });
    await el.updateComplete;
    expect(el.querySelectorAll('#activity-feed tbody tr')).toHaveLength(rowsBefore + 1);
  });

  test('queue filter matches only job events on that queue', async () => {
    const { el, fetchMock } = await mount([queue()], []);

    const select = el.querySelector('select[data-filter="queue"]') as HTMLSelectElement;
    select.value = 'jev-scout';
    select.dispatchEvent(new Event('change'));
    await flush(el);
    expect((fetchMock.mock.calls.at(-1)?.[0] as string) ?? '').toContain('queue=jev-scout');

    emit({
      type: 'activity_event',
      event: event({ type: 'job', queue: 'jev-trader', symbol: undefined }),
    });
    emit({ type: 'activity_event', event: event({ type: 'jev_scout' }) });
    emit({
      type: 'activity_event',
      event: event({ type: 'job', queue: 'jev-scout', symbol: undefined }),
    });
    await el.updateComplete;

    const rows = el.querySelectorAll('#activity-feed tbody tr');
    expect(rows).toHaveLength(1);
    expect(rows[0].getAttribute('data-event-type')).toBe('job');
  });

  test('caps the live feed at 500 rows', async () => {
    const { el } = await mount([queue()], []);
    for (let i = 0; i < 510; i++) {
      emit({ type: 'activity_event', event: event({ symbol: String(i) }) });
    }
    await el.updateComplete;

    expect(el.querySelectorAll('#activity-feed tbody tr')).toHaveLength(500);
  });
});
