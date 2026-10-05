import { describe, expect, mock, test } from 'bun:test';
import {
  createScannerTable,
  FakeWebSocket,
  flush,
  installFakes,
  item,
  mount,
} from './scanner-test-support';
import { COLUMNS } from './scanner-view';

installFakes();

// Header sorting/accessibility and /ws/scanner push behavior.
describe('pitha-scanner-table interaction', () => {
  test('sorts client-side on header click without an additional fetch', async () => {
    const { el, fetchMock } = await mount([item({ symbol: 'BBBB' }), item({ symbol: 'AAAA' })]);

    // Default sort key is 'symbol' ascending.
    let rows = el.querySelectorAll('tbody tr');
    expect(rows[0].getAttribute('data-symbol')).toBe('AAAA');

    const symbolHeader = el.querySelector('thead th') as HTMLElement;
    expect(symbolHeader.getAttribute('aria-sort')).toBe('ascending');
    expect(symbolHeader.textContent).toContain('銘柄 ▲');

    (symbolHeader.querySelector('button') as HTMLElement).click(); // same column again -> toggles to descending
    await el.updateComplete;

    expect(fetchMock).toHaveBeenCalledTimes(1);
    rows = el.querySelectorAll('tbody tr');
    expect(rows[0].getAttribute('data-symbol')).toBe('BBBB');
    expect(symbolHeader.getAttribute('aria-sort')).toBe('descending');
    expect(symbolHeader.textContent).toContain('銘柄 ▼');
  });

  // Lexicographic order would put "exceptional" first and "poor" after
  // "good" (issue #493); the column hint promises poor < ... < exceptional.
  test('sorts the entry-quality column by quality rank, nulls first, and reverses on a second click', async () => {
    const qualities = ['strong', 'poor', null, 'exceptional', 'good', 'fair'];
    const { el } = await mount(
      qualities.map((entry_quality, i) => item({ symbol: `S${i}`, entry_quality })),
    );
    const header =
      el.querySelectorAll('thead th')[COLUMNS.findIndex((c) => c.key === 'entry_quality')];
    const order = () =>
      [...el.querySelectorAll('tbody tr')].map(
        (row) => qualities[Number(row.getAttribute('data-symbol')?.slice(1))],
      );

    (header.querySelector('button') as HTMLElement).click();
    await el.updateComplete;
    expect(order()).toEqual([null, 'poor', 'fair', 'good', 'strong', 'exceptional']);

    (header.querySelector('button') as HTMLElement).click();
    await el.updateComplete;
    expect(order()).toEqual(['exceptional', 'strong', 'good', 'fair', 'poor', null]);
  });

  // The header <button> is what makes sorting reachable with Tab and
  // Enter/Space (native button behavior). The mouse-only `title` tooltip
  // is complemented by a visible <details> listing every column's hint,
  // which keyboard/touch/screen-reader users can open.
  test('sortable headers are focusable buttons and every hint is listed in the column help', async () => {
    const { el } = await mount([item()]);

    const headers = [...el.querySelectorAll('thead th')];
    for (const th of headers) {
      const button = th.querySelector('button');
      expect(button).not.toBeNull();
      expect(button?.getAttribute('type')).toBe('button');
      // One announcement per hint: no aria-describedby duplicating `title`.
      expect(button?.hasAttribute('aria-describedby')).toBe(false);
      expect(th.querySelector('.sr-only')).toBeNull();
    }

    const help = el.querySelector('details[data-testid="scanner-column-help"]');
    expect(help?.querySelector('summary')?.textContent).toBe('列の意味');
    expect(help?.hasAttribute('open')).toBe(false);
    const terms = [...(help?.querySelectorAll('dt') ?? [])].map((dt) => dt.textContent);
    const descriptions = [...(help?.querySelectorAll('dd') ?? [])].map((dd) => dd.textContent);
    expect(terms).toEqual(COLUMNS.map((c) => c.label));
    expect(descriptions).toEqual(COLUMNS.map((c) => c.hint));
    expect(headers.map((th) => th.getAttribute('title'))).toEqual(COLUMNS.map((c) => c.hint));

    const priceButton = el.querySelectorAll('thead th')[1].querySelector('button') as HTMLElement;
    priceButton.click();
    await el.updateComplete;
    expect(el.querySelectorAll('thead th')[1].getAttribute('aria-sort')).toBe('ascending');
    expect(el.querySelectorAll('thead th')[0].getAttribute('aria-sort')).toBe('none');
  });

  test('keeps the as-of caption in the JST SSR format after a /ws/scanner push', async () => {
    const { el } = await mount([item()]);

    FakeWebSocket.instances[0].emit('message', {
      data: JSON.stringify({
        type: 'scanner_update',
        items: [item()],
        as_of: '2026-09-26T10:15:30.123456789+09:00',
      }),
    });
    await el.updateComplete;

    // Same JST label as the SSR caption; fractional seconds dropped.
    expect(el.querySelector('caption')?.textContent?.trim()).toBe(
      'Scanner Dashboard — as of 2026-09-26 10:15:30 JST',
    );
  });

  test('a push can be the first data when the initial fetch failed, and clears the error', async () => {
    globalThis.fetch = mock(() =>
      Promise.resolve(new Response('', { status: 500 })),
    ) as unknown as typeof fetch;
    const el = createScannerTable();
    document.body.appendChild(el);
    await flush(el);
    expect(el.querySelector('[role="alert"]')).not.toBeNull();

    FakeWebSocket.instances[0].emit('message', {
      data: JSON.stringify({
        type: 'scanner_update',
        items: [item()],
        as_of: '2026-09-26T10:15:00+09:00',
      }),
    });
    await el.updateComplete;

    expect(el.querySelector('[role="alert"]')).toBeNull();
    expect(el.querySelectorAll('tbody tr')).toHaveLength(1);
  });

  test('renders nothing but notices before the first data arrives', async () => {
    globalThis.fetch = mock(() => new Promise<Response>(() => {})) as unknown as typeof fetch;
    const el = createScannerTable();
    document.body.appendChild(el);
    await el.updateComplete;

    expect(el.querySelector('[data-testid="scanner-count"]')).toBeNull();
    expect(el.querySelector('caption')).toBeNull();
    expect(el.querySelector('table')).toBeNull();
    expect(el.querySelector('[data-testid="scanner-empty"]')).toBeNull();
  });

  test('renders a fetch error message instead of throwing', async () => {
    globalThis.fetch = mock(() =>
      Promise.resolve(new Response('', { status: 500 })),
    ) as unknown as typeof fetch;

    const el = createScannerTable();
    document.body.appendChild(el);
    await flush(el);

    const alert = el.querySelector('[role="alert"]');
    expect(alert).not.toBeNull();
    expect(alert?.textContent).toContain('500');
    // Light DOM: Shadow `noticeStyles` do not apply, so Tailwind classes must (#355).
    expect(alert?.classList.contains('text-red-700')).toBe(true);
    // Nothing has loaded: no count, caption or empty-state claim.
    expect(el.querySelector('[data-testid="scanner-count"]')).toBeNull();
    expect(el.querySelector('caption')).toBeNull();
    expect(el.querySelector('[data-testid="scanner-empty"]')).toBeNull();
  });

  test('styles the disconnected notice with Tailwind classes while the socket is down', async () => {
    const { el } = await mount([item()]);
    FakeWebSocket.instances[0].emit('open', {});
    await el.updateComplete;
    expect(el.querySelector('.pitha-ws-disconnected')).toBeNull();

    FakeWebSocket.instances[0].emit('close', { code: 1006 });
    await el.updateComplete;
    const notice = el.querySelector('.pitha-ws-disconnected');
    expect(notice?.classList.contains('text-amber-700')).toBe(true);
    expect(notice?.classList.contains('text-xs')).toBe(true);
  });
});
