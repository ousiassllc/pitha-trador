import { describe, expect, mock, spyOn, test } from 'bun:test';
import {
  FakeWebSocket,
  flush,
  installFakes,
  item,
  mount,
  type ScannerTableElement,
} from './scanner-test-support';

installFakes();

describe('pitha-scanner-table', () => {
  test('loads the initial candidate list from api-url and renders a row per item', async () => {
    const { el } = await mount([
      item(),
      item({ symbol: '9984', price: 7000, jev_direction: null }),
    ]);

    const rows = el.querySelectorAll('tbody tr');
    expect(rows).toHaveLength(2);
    expect(rows[0].getAttribute('data-symbol')).toBe('7203');
    expect(rows[0].textContent).toContain('LONG');
    expect(rows[1].getAttribute('data-symbol')).toBe('9984');
    expect(rows[1].textContent).toContain('—'); // no Jev evaluation yet
  });

  test('renders symbol cells as plain <a href> links to the server-provided detail_url (real navigation, not HTMX)', async () => {
    const { el } = await mount([item({ detail_url: '/from-server?q=%26' })]);

    const link = el.querySelector('td a') as HTMLAnchorElement;
    expect(link.getAttribute('href')).toBe('/from-server?q=%26');
    expect(link.getAttribute('hx-get')).toBeNull();
    expect(link.hasAttribute('hx-boost')).toBe(false);
  });

  test('applies /ws/scanner scanner_update pushes without re-fetching api-url', async () => {
    const { el, fetchMock } = await mount([item()]);
    expect(fetchMock).toHaveBeenCalledTimes(1);

    const socket = FakeWebSocket.instances[0];
    socket.emit('message', {
      data: JSON.stringify({
        type: 'scanner_update',
        items: [item({ symbol: '6758', price: 1500 })],
      }),
    });
    await el.updateComplete;

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const rows = el.querySelectorAll('tbody tr');
    expect(rows).toHaveLength(1);
    expect(rows[0].getAttribute('data-symbol')).toBe('6758');
  });

  test('shows the candidate count and as-of caption, and colors returns by sign', async () => {
    const { el } = await mount([
      item({ symbol: 'UP', return_1m: 0.12, return_5m: -0.3 }),
      item({ symbol: 'FLAT', return_1m: 0, return_5m: null, price_vs_vwap_bps: 0 }),
    ]);

    expect(el.querySelector('[data-testid="scanner-count"]')?.textContent).toContain('2');
    expect(el.querySelector('caption')?.textContent?.trim()).toBe(
      'Scanner Dashboard — as of 2026-09-26T10:15:00+09:00',
    );

    const cells = (symbol: string) => el.querySelectorAll(`tr[data-symbol="${symbol}"] td`);
    expect(cells('UP')[2].textContent?.trim()).toBe('+0.12');
    expect(cells('UP')[2].className).toContain('text-green-700');
    expect(cells('UP')[3].textContent?.trim()).toBe('-0.30');
    expect(cells('UP')[3].className).toContain('text-red-700');
    expect(cells('FLAT')[2].textContent?.trim()).toBe('0.00'); // zero carries no "+" (same as SSR)
    expect(cells('FLAT')[2].className).toContain('text-slate-500');
    expect(cells('FLAT')[5].textContent?.trim()).toBe('0');
    expect(cells('FLAT')[3].textContent?.trim()).toBe('—');
  });

  test('renders Jev direction and entry quality as badges, pending when not evaluated', async () => {
    const { el } = await mount([
      item({ symbol: 'AAAA', jev_direction: 'SHORT', entry_quality: 'exceptional' }),
      item({ symbol: 'BBBB', jev_direction: null, jev_confidence: null, entry_quality: null }),
    ]);

    const evaluated = el.querySelectorAll('tr[data-symbol="AAAA"] td');
    expect(evaluated[7].querySelector('span')?.className).toContain('bg-red-100');
    expect(evaluated[8].textContent?.trim()).toBe('74%');
    expect(evaluated[9].querySelector('span')?.className).toContain('bg-emerald-600');

    const pending = el.querySelectorAll('tr[data-symbol="BBBB"] td');
    expect(pending[7].textContent?.trim()).toBe('pending');
    expect(pending[8].textContent?.trim()).toBe('—');
    expect(pending[9].textContent?.trim()).toBe('—');
  });

  test('gives every column header a tooltip explaining it', async () => {
    const { el } = await mount([item()]);

    const headers = el.querySelectorAll('thead th');
    expect(headers).toHaveLength(11);
    for (const th of headers) {
      expect(th.getAttribute('title')?.length).toBeGreaterThan(0);
    }
  });

  test('shows an empty-state message with a zero count when there are no candidates', async () => {
    const { el } = await mount([]);

    expect(el.querySelector('[data-testid="scanner-empty"]')).not.toBeNull();
    expect(el.querySelector('[data-testid="scanner-count"]')?.textContent).toContain('0');

    FakeWebSocket.instances[0].emit('message', {
      data: JSON.stringify({ type: 'scanner_update', items: [item()] }),
    });
    await el.updateComplete;

    expect(el.querySelector('[data-testid="scanner-empty"]')).toBeNull();
    expect(el.querySelector('[data-testid="scanner-count"]')?.textContent).toContain('1');
  });

  // The first update cycle used to close and reopen the socket (issue #170).
  test('opens a single WebSocket on mount and reconnects once when ws-url changes', async () => {
    const { el } = await mount([item()]);
    expect(FakeWebSocket.instances).toHaveLength(1);

    el.setAttribute('ws-url', '/ws/scanner-2');
    await el.updateComplete;

    expect(FakeWebSocket.instances).toHaveLength(2);
    expect(FakeWebSocket.instances[1].url).toContain('/ws/scanner-2');
  });

  test('logs an error and makes no request when api-url and ws-url are not injected', async () => {
    const errorSpy = spyOn(console, 'error').mockImplementation(() => {});
    const fetchMock = mock(() => Promise.resolve(new Response('{}')));
    globalThis.fetch = fetchMock as unknown as typeof fetch;
    const el = document.createElement('pitha-scanner-table') as ScannerTableElement; // no URL attributes
    document.body.appendChild(el);
    await flush(el);

    expect(fetchMock).not.toHaveBeenCalled();
    expect(FakeWebSocket.instances).toHaveLength(0);
    for (const name of ['api-url', 'ws-url']) {
      expect(errorSpy).toHaveBeenCalledWith(
        expect.objectContaining({ message: `pitha-scanner-table: ${name} is not set` }),
      );
    }
    errorSpy.mockRestore();
  });
});
