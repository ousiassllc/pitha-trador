import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test';
import { buttonLabels, flush, type PanelElement } from './kill-switch-test-support';

let originalFetch: typeof fetch;
let originalConfirm: typeof window.confirm;

beforeEach(() => {
  originalFetch = globalThis.fetch;
  originalConfirm = window.confirm;
  document.body.innerHTML = '';
});

afterEach(() => {
  globalThis.fetch = originalFetch;
  window.confirm = originalConfirm;
  document.body.innerHTML = '';
});

function state(name: string, canPause: boolean, canResume: boolean, canKill: boolean): Response {
  return new Response(
    JSON.stringify({
      state: name,
      can_pause: canPause,
      can_resume: canResume,
      can_kill: canKill,
    }),
  );
}

// Server-rendered as running; the server is already killed by the time the
// panel POSTs Kill (Engine.Kill sets killed before it liquidates).
function mountRunning(): PanelElement {
  const el = document.createElement('pitha-kill-switch-panel') as PanelElement;
  el.setAttribute('status', 'running');
  el.setAttribute('can-pause', '');
  el.setAttribute('can-kill', '');
  el.setAttribute('kill-url', '/api/v1/system/kill');
  el.setAttribute('status-url', '/api/v1/system/status');
  document.body.appendChild(el);
  return el;
}

function clickKill(el: PanelElement): void {
  const kill = Array.from(el.shadowRoot?.querySelectorAll('button') ?? []).find(
    (b) => b.textContent?.trim() === 'Kill',
  );
  kill?.click();
}

describe('pitha-kill-switch-panel actions', () => {
  test('the Kill confirmation warns that every open position is force-liquidated', async () => {
    globalThis.fetch = mock(() => Promise.resolve(state('running', true, false, true))) as never;
    const confirmMock = mock((_message?: string) => false);
    window.confirm = confirmMock as unknown as typeof window.confirm;
    const el = mountRunning();
    await flush(el);

    clickKill(el);

    expect(confirmMock).toHaveBeenCalledTimes(1);
    const message = confirmMock.mock.calls[0]?.[0] ?? '';
    expect(message).toContain('強制決済');
    expect(message).toContain('新規エントリー');
  });

  test('resyncs the state after a failed Kill (500) so a killed server is not shown as running', async () => {
    window.confirm = (() => true) as typeof window.confirm;
    // status-url reports running until Kill is POSTed, then killed (the
    // server flips to killed before liquidating, so the POST still 500s).
    let killAttempted = false;
    globalThis.fetch = mock((_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === 'POST') {
        killAttempted = true;
        return Promise.resolve(new Response('liquidation failed', { status: 500 }));
      }
      return Promise.resolve(
        killAttempted ? state('killed', false, true, false) : state('running', true, false, true),
      );
    }) as never;

    const el = mountRunning();
    await flush(el);
    expect(buttonLabels(el)).toEqual(['Pause', 'Kill']);
    let changed = 0;
    el.addEventListener('systemStateChanged', () => {
      changed += 1;
    });

    clickKill(el);
    await flush(el);

    expect(el.shadowRoot?.querySelector('[data-status]')?.getAttribute('data-status')).toBe(
      'killed',
    );
    expect(buttonLabels(el)).toEqual(['Resume']);
    // Header's StatusDot is told about the resynced state, and the failed
    // action's own error stays visible.
    expect(changed).toBeGreaterThan(0);
    expect(el.shadowRoot?.querySelector('[role="alert"]')?.textContent).toContain(
      'POST /api/v1/system/kill failed with status 500',
    );
  });
});
