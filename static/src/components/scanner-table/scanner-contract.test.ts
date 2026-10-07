import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test';
import { installFakeWebSocket } from '../lib/ws-test-support';
import contract from './scanner-contract.json';
import './pitha-scanner-table';
import { createScannerTable } from './scanner-test-support';
import type { ScannerItem } from './scanner-types';
import { COLUMNS } from './scanner-view';

// Contract test shared with internal/web/organisms/scanner_table_contract_test.go:
// both render scanner-contract.json's items and must produce exactly its
// column definitions and cells, so the SSR fallback and this Lit component
// cannot drift apart (the SSR view is what users see before hydration).

type ScannerTableElement = HTMLElement & { updateComplete: Promise<boolean> };

let originalFetch: typeof fetch;
let restoreWebSocket: () => void;

beforeEach(() => {
  originalFetch = globalThis.fetch;
  restoreWebSocket = installFakeWebSocket();
  document.body.innerHTML = '';
});

afterEach(() => {
  globalThis.fetch = originalFetch;
  restoreWebSocket();
  document.body.innerHTML = '';
});

async function render(items: ScannerItem[]): Promise<ScannerTableElement> {
  globalThis.fetch = mock(() =>
    Promise.resolve(new Response(JSON.stringify({ items, as_of: '2026-09-26T10:15:00+09:00' }))),
  ) as unknown as typeof fetch;
  const el = createScannerTable();
  document.body.appendChild(el);
  const { promise, resolve } = Promise.withResolvers<void>();
  setTimeout(resolve, 0);
  await promise;
  await el.updateComplete;
  return el;
}

const classSet = (el: Element | null | undefined) =>
  (el?.getAttribute('class') ?? '').split(/\s+/).filter(Boolean).sort();
const sorted = (classes: string) => classes.split(/\s+/).filter(Boolean).sort();

describe('SSR/Lit scanner contract', () => {
  test('column labels, hints and alignment match the golden definitions in order', async () => {
    const el = await render([]);

    const ths = [...el.querySelectorAll('thead th')];
    expect(ths).toHaveLength(contract.columns.length);
    expect(COLUMNS).toHaveLength(contract.columns.length);
    ths.forEach((th, i) => {
      const want = contract.columns[i];
      const label = (th.querySelector('button')?.textContent ?? '').replace(/\s*[▲▼]$/, '').trim();
      expect(label).toBe(want.label);
      expect(th.getAttribute('title')).toBe(want.hint);
      expect(th.classList.contains('text-right')).toBe(want.numeric);
    });
  });

  test('column help lists the golden labels and hints in order', async () => {
    const el = await render([]);

    const help = el.querySelector('[data-testid="scanner-column-help"]');
    expect([...(help?.querySelectorAll('dt') ?? [])].map((dt) => dt.textContent)).toEqual(
      contract.columns.map((c) => c.label),
    );
    expect([...(help?.querySelectorAll('dd') ?? [])].map((dd) => dd.textContent)).toEqual(
      contract.columns.map((c) => c.hint),
    );
  });

  for (const row of contract.rows) {
    test(`row renders the golden cells: ${row.name}`, async () => {
      const el = await render([row.item as ScannerItem]);

      const tds = [...el.querySelectorAll('tbody tr td')];
      expect(tds.map((td) => td.textContent?.trim())).toEqual(row.cells);
      expect(tds[0].querySelector('a')?.getAttribute('href')).toBe(row.item.detail_url);
      expect(classSet(tds[2])).toEqual(sorted(row.returnClasses[0]));
      expect(classSet(tds[3])).toEqual(sorted(row.returnClasses[1]));
      expect(classSet(tds[7].querySelector('span'))).toEqual(sorted(row.directionClass));
      expect(classSet(tds[9].querySelector('span'))).toEqual(sorted(row.qualityClass));
    });
  }

  // Both sides keep the server order (ScreenScore descending) until a header is
  // clicked, so the rows must not reshuffle at hydration (issue #674).
  test('rows keep the given (server) order, like the SSR fallback', async () => {
    const el = await render(contract.rows.map((row) => row.item as ScannerItem));

    const symbols = [...el.querySelectorAll('tbody tr')].map((tr) =>
      tr.getAttribute('data-symbol'),
    );
    expect(symbols).toEqual(contract.rows.map((row) => row.item.symbol));
    for (const th of el.querySelectorAll('thead th')) {
      expect(th.getAttribute('aria-sort')).toBe('none');
    }
  });

  test('empty state text matches the golden message', async () => {
    const el = await render([]);

    const empty = el.querySelector('[data-testid="scanner-empty"]');
    expect(empty?.textContent?.replace(/\s+/g, ' ').trim()).toBe(contract.emptyMessage);
  });
});
