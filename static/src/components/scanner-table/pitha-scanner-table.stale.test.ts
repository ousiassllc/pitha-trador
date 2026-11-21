import { describe, expect, mock, test } from 'bun:test';
import { FakeWebSocket } from '../lib/ws-test-support';
import {
  createScannerTable,
  flush,
  installFakes,
  item,
  type ScannerTableElement,
} from './scanner-test-support';
import type { ScannerItem } from './scanner-types';

installFakes();

const response = (items: ScannerItem[], asOf: string) =>
  new Response(JSON.stringify({ items, as_of: asOf }));

// A fetch whose responses the test releases itself, in any order.
function deferredFetch() {
  const pending: ((response: Response) => void)[] = [];
  globalThis.fetch = mock(
    () => new Promise<Response>((resolve) => pending.push(resolve)),
  ) as unknown as typeof fetch;
  return pending;
}

const symbols = (el: ScannerTableElement): (string | null)[] =>
  Array.from(el.querySelectorAll('tbody tr')).map((row) => row.getAttribute('data-symbol'));

describe('pitha-scanner-table stale initial loads', () => {
  // The first GET used to overwrite a newer WebSocket push when it arrived late (issue #1165).
  test('drops a slow initial response that arrives after a scanner_update push', async () => {
    const pending = deferredFetch();
    const el = createScannerTable();
    document.body.appendChild(el);
    await flush(el);

    FakeWebSocket.instances[0].emit('message', {
      data: JSON.stringify({
        type: 'scanner_update',
        items: [item({ symbol: '6758' })],
        as_of: '2026-09-26T10:16:00+09:00',
      }),
    });
    await el.updateComplete;
    expect(symbols(el)).toEqual(['6758']);

    pending[0](response([item({ symbol: '7203' })], '2026-09-26T10:15:00+09:00'));
    await flush(el);

    expect(symbols(el)).toEqual(['6758']);
  });

  test('drops the error of an initial load superseded by a push', async () => {
    const pending = deferredFetch();
    const el = createScannerTable();
    document.body.appendChild(el);
    await flush(el);

    FakeWebSocket.instances[0].emit('message', {
      data: JSON.stringify({ type: 'scanner_update', items: [item()], as_of: null }),
    });
    await el.updateComplete;

    pending[0](new Response('{}', { status: 500 }));
    await flush(el);

    expect(el.querySelector('[role="alert"]')).toBeNull();
  });

  // Detaching and re-inserting starts a second GET; the first one is stale.
  test('keeps the newest response when the element is re-attached while a GET is in flight', async () => {
    const pending = deferredFetch();
    const el = createScannerTable();
    document.body.appendChild(el);
    await flush(el);
    el.remove();
    document.body.appendChild(el);
    await flush(el);
    expect(pending).toHaveLength(2);

    pending[1](response([item({ symbol: '6758' })], '2026-09-26T10:16:00+09:00'));
    await flush(el);
    pending[0](response([item({ symbol: '7203' })], '2026-09-26T10:15:00+09:00'));
    await flush(el);

    expect(symbols(el)).toEqual(['6758']);
  });
});
