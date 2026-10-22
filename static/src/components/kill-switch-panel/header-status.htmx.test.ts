// Header's `#header-status` follows `systemStateChanged` from the real
// `pitha-kill-switch-panel` on every page (issue #896): the panel sits in the
// `<header>` on most pages but inside the 取引の状態 card on Home, so the
// trigger listens on `body` (`from:body`) and not on `closest header`.
// The markup below is what the server renders (internal/web/organisms/header.templ,
// pinned by internal/router/systemheader's tests); the real vendored htmx
// runs on it, so this proves the event reaches htmx's trigger, not just that
// the event bubbles.
import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { installFakeWebSocket } from '../lib/ws-test-support';
import { buttonLabels, flush, type PanelElement, URLS } from './kill-switch-test-support';

type Htmx = { process(el: Element): void };

// htmx.min.js is a classic script (`var htmx = ...`), so evaluate it as one.
// happy-dom has no XPath; htmx only uses it to find `hx-on:*` attributes,
// which this markup does not have, so an expression that matches nothing will do.
(globalThis as { XPathEvaluator?: unknown }).XPathEvaluator ??= class {
  createExpression() {
    return { evaluate: () => ({ iterateNext: () => null }) };
  }
};
const htmx = new Function(
  `${readFileSync(new URL('../../vendor/htmx.min.js', import.meta.url), 'utf8')}; return htmx;`,
)() as Htmx;

const PANEL = `<pitha-kill-switch-panel status="running" can-pause can-kill
  status-url="${URLS['status-url']}" pause-url="${URLS['pause-url']}" resume-url="${URLS['resume-url']}"
  kill-url="${URLS['kill-url']}" ws-url="${URLS['ws-url']}"></pitha-kill-switch-panel>`;

const STATUS_DOT = (trigger: string) =>
  `<div id="header-status" hx-get="/system/status" hx-headers='{"X-Pitha-Background":"1"}' hx-trigger="${trigger}" hx-target="this" hx-swap="innerHTML"></div>`;

// A page whose Kill Switch panel is in the header (every page but Home).
const headerPage = (trigger: string) =>
  `<header>${STATUS_DOT(trigger)}${PANEL}</header><main><p>page</p></main>`;
// Home: the header holds only the status; the panel is inside a card.
const homePage = (trigger: string) =>
  `<header>${STATUS_DOT(trigger)}</header><main><section data-testid="home-trade-status-card">${PANEL}</section></main>`;

let restoreWebSocket: () => void;
let originalFetch: typeof fetch;
let statusRequests: string[];

beforeEach(() => {
  // htmx keeps its `from:body` listeners on the body element for good, so each
  // test gets a fresh one.
  document.documentElement.replaceChild(document.createElement('body'), document.body);
  // htmx resolves request paths against the page URL.
  (window as unknown as { happyDOM: { setURL(url: string): void } }).happyDOM.setURL(
    'http://localhost/scanner',
  );
  originalFetch = globalThis.fetch;
  restoreWebSocket = installFakeWebSocket();
  statusRequests = [];
  // Count what htmx is about to request for the status dot, and cancel it:
  // the response is not under test.
  document.body.addEventListener('htmx:beforeRequest', (e) => {
    const detail = (e as CustomEvent<{ elt: Element; pathInfo: { requestPath: string } }>).detail;
    if (detail.elt.id === 'header-status') statusRequests.push(detail.pathInfo.requestPath);
    e.preventDefault();
  });
  globalThis.fetch = mock((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (init?.method === 'POST' && url === URLS['pause-url']) {
      return Promise.resolve(
        new Response(
          JSON.stringify({ state: 'paused', can_pause: false, can_resume: true, can_kill: true }),
        ),
      );
    }
    return Promise.resolve(
      new Response(
        JSON.stringify({ state: 'running', can_pause: true, can_resume: false, can_kill: true }),
      ),
    );
  }) as never;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
  restoreWebSocket();
  document.body.innerHTML = '';
});

// mountPage renders html, lets htmx process it and the panel's first status
// read settle, then forgets the requests that read caused.
async function mountPage(html: string): Promise<PanelElement> {
  document.body.innerHTML = html;
  htmx.process(document.body);
  const panel = document.querySelector('pitha-kill-switch-panel') as PanelElement;
  await flush(panel);
  statusRequests.length = 0;
  return panel;
}

function clickPause(panel: PanelElement): void {
  const pause = Array.from(panel.shadowRoot?.querySelectorAll('button') ?? []).find(
    (b) => b.textContent?.trim() === '新規エントリーを停止',
  );
  pause?.click();
}

describe('#header-status follows systemStateChanged (from:body)', () => {
  test('panel in the header (Scanner, Performance, AI proposals, Settings): a pause refreshes the status dot', async () => {
    const panel = await mountPage(headerPage('systemStateChanged from:body'));
    expect(buttonLabels(panel)).toEqual(['新規エントリーを停止', '全決済して停止']);

    clickPause(panel);
    await flush(panel);

    expect(panel.shadowRoot?.querySelector('[data-status]')?.getAttribute('data-status')).toBe(
      'paused',
    );
    expect(statusRequests).toEqual(['/system/status']);
  });

  test('panel inside a Home card (outside the header): a pause refreshes the status dot', async () => {
    const panel = await mountPage(homePage('systemStateChanged from:body'));

    clickPause(panel);
    await flush(panel);

    expect(statusRequests).toEqual(['/system/status']);
  });

  test('the Kill Switch push (the event the panel raises on its own) refreshes it too', async () => {
    await mountPage(headerPage('systemStateChanged from:body'));
    document
      .querySelector('pitha-kill-switch-panel')
      ?.dispatchEvent(new CustomEvent('systemStateChanged', { bubbles: true, composed: true }));
    await Promise.resolve();
    expect(statusRequests).toEqual(['/system/status']);
  });

  test('why from:body: the old `from:closest header` misses a panel that is not in the header', async () => {
    const panel = await mountPage(homePage('systemStateChanged from:closest header'));
    clickPause(panel);
    await flush(panel);
    expect(statusRequests).toEqual([]);
  });
});
