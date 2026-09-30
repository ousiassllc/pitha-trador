import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test';
import contract from './scanner-contract.json';
import './pitha-scanner-table';
import type { ScannerItem } from './pitha-scanner-table';
import { COLUMNS } from './scanner-view';

// Contract test shared with internal/web/organisms/scanner_table_contract_test.go:
// both render scanner-contract.json's items and must produce exactly its
// column definitions and cells, so the SSR fallback and this Lit component
// cannot drift apart (the SSR view is what users see before hydration).

type ScannerTableElement = HTMLElement & { updateComplete: Promise<boolean> };

class FakeWebSocket {
  constructor(public readonly url: string) {}
  addEventListener(): void {}
  close(): void {}
}

let originalFetch: typeof fetch;
let originalWebSocket: typeof WebSocket;

beforeEach(() => {
  originalFetch = globalThis.fetch;
  originalWebSocket = globalThis.WebSocket;
  globalThis.WebSocket = FakeWebSocket as unknown as typeof WebSocket;
  document.body.innerHTML = '';
});

afterEach(() => {
  globalThis.fetch = originalFetch;
  globalThis.WebSocket = originalWebSocket;
  document.body.innerHTML = '';
});

async function render(items: ScannerItem[]): Promise<ScannerTableElement> {
  globalThis.fetch = mock(() =>
    Promise.resolve(new Response(JSON.stringify({ items, as_of: '2026-09-26T10:15:00+09:00' }))),
  ) as unknown as typeof fetch;
  const el = document.createElement('pitha-scanner-table') as ScannerTableElement;
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

  for (const row of contract.rows) {
    test(`row renders the golden cells: ${row.name}`, async () => {
      const el = await render([row.item as ScannerItem]);

      const tds = [...el.querySelectorAll('tbody tr td')];
      expect(tds.map((td) => td.textContent?.trim())).toEqual(row.cells);
      expect(tds[0].querySelector('a')?.getAttribute('href')).toBe(row.href);
      expect(classSet(tds[2])).toEqual(sorted(row.returnClasses[0]));
      expect(classSet(tds[3])).toEqual(sorted(row.returnClasses[1]));
      expect(classSet(tds[7].querySelector('span'))).toEqual(sorted(row.directionClass));
      expect(classSet(tds[9].querySelector('span'))).toEqual(sorted(row.qualityClass));
    });
  }

  test('empty state text matches the golden message', async () => {
    const el = await render([]);

    const empty = el.querySelector('[data-testid="scanner-empty"]');
    expect(empty?.textContent?.replace(/\s+/g, ' ').trim()).toBe(contract.emptyMessage);
  });
});
