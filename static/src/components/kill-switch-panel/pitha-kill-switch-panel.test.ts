import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test';
import './pitha-kill-switch-panel';

type Listener = (event: unknown) => void;
type PanelElement = HTMLElement & { updateComplete: Promise<boolean> };

class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  private listeners: Record<string, Listener[]> = {};

  constructor(public readonly url: string) {
    FakeWebSocket.instances.push(this);
  }

  addEventListener(type: string, listener: Listener): void {
    const list = this.listeners[type] ?? [];
    list.push(listener);
    this.listeners[type] = list;
  }

  emit(type: string, event: unknown = {}): void {
    for (const listener of this.listeners[type] ?? []) listener(event);
  }

  close(): void {
    this.emit('close', { code: 1000 });
  }
}

let originalFetch: typeof fetch;
let originalWebSocket: typeof WebSocket;

beforeEach(() => {
  originalFetch = globalThis.fetch;
  originalWebSocket = globalThis.WebSocket;
  FakeWebSocket.instances = [];
  globalThis.WebSocket = FakeWebSocket as unknown as typeof WebSocket;
  document.body.innerHTML = '';
});

afterEach(() => {
  globalThis.fetch = originalFetch;
  globalThis.WebSocket = originalWebSocket;
  document.body.innerHTML = '';
});

// Mirrors pitha-scanner-table.test.ts's nextMacrotask/flush: loadInitial()
// is fire-and-forget from connectedCallback, so a single real macrotask
// tick is the deterministic way to let its promise chain settle before
// asserting (microtasks-before-macrotasks ordering, not a guessed delay).
async function nextMacrotask(): Promise<void> {
  const { promise, resolve } = Promise.withResolvers<void>();
  setTimeout(resolve, 0);
  await promise;
}

async function flush(el: PanelElement): Promise<void> {
  await nextMacrotask();
  await el.updateComplete;
}

const URLS = {
  'status-url': '/api/v1/system/status',
  'pause-url': '/api/v1/system/pause',
  'resume-url': '/api/v1/system/resume',
  'kill-url': '/api/v1/system/kill',
  'ws-url': '/ws/system',
};

// Server-side transition rules (domain.SystemState.CanPause/CanResume/
// CanKill); the component must take these from responses, not derive them.
function stateBody(state: string, flags: Partial<Record<string, boolean>> = {}) {
  const rules: Record<string, [boolean, boolean, boolean]> = {
    running: [true, false, true],
    paused: [false, true, true],
    killed: [false, true, false],
  };
  const [canPause, canResume, canKill] = rules[state] ?? [false, false, false];
  return {
    state,
    can_pause: canPause,
    can_resume: canResume,
    can_kill: canKill,
    ...flags,
  };
}

function buttonLabels(el: PanelElement): (string | undefined)[] {
  return Array.from(el.shadowRoot?.querySelectorAll('button') ?? []).map((b) =>
    b.textContent?.trim(),
  );
}

// mount stubs `fetch` to answer every request with bodies[url] (default:
// the `state` rules above), appends a <pitha-kill-switch-panel> carrying
// the server-injected URL attributes, and waits for its initial render.
async function mount(state: string, attrs: Record<string, string> = {}) {
  const fetchMock = mock((input: RequestInfo | URL) => {
    void input;
    return Promise.resolve(new Response(JSON.stringify(stateBody(state))));
  });
  globalThis.fetch = fetchMock as unknown as typeof fetch;

  const el = document.createElement('pitha-kill-switch-panel') as PanelElement;
  for (const [name, value] of Object.entries({ ...URLS, ...attrs })) {
    el.setAttribute(name, value);
  }
  document.body.appendChild(el);
  await flush(el);
  return { el, fetchMock };
}

describe('pitha-kill-switch-panel', () => {
  test('loads initial status and renders the action buttons the server allows', async () => {
    const { el } = await mount('running');

    expect(el.shadowRoot?.querySelector('[data-status]')?.getAttribute('data-status')).toBe(
      'running',
    );
    const buttons = buttonLabels(el);
    expect(buttons).toContain('Pause');
    expect(buttons).toContain('Kill');
    expect(buttons).not.toContain('Resume');
  });

  test('renders Resume instead of Pause/Kill when already paused', async () => {
    const { el } = await mount('paused');

    const buttons = buttonLabels(el);
    expect(buttons).toContain('Resume');
    expect(buttons).toContain('Kill');
    expect(buttons).not.toContain('Pause');
  });

  test('takes allowed actions from the server response, not from the state name', async () => {
    const fetchMock = mock(() =>
      Promise.resolve(
        new Response(JSON.stringify(stateBody('running', { can_pause: false, can_kill: false }))),
      ),
    );
    globalThis.fetch = fetchMock as unknown as typeof fetch;
    const el = document.createElement('pitha-kill-switch-panel') as PanelElement;
    el.setAttribute('status-url', URLS['status-url']);
    document.body.appendChild(el);
    await flush(el);

    expect(buttonLabels(el)).toEqual([]);
  });

  test('renders the SSR-injected state without waiting for a fetch', async () => {
    globalThis.fetch = mock(() => new Promise<Response>(() => {})) as unknown as typeof fetch;
    const el = document.createElement('pitha-kill-switch-panel') as PanelElement;
    el.setAttribute('status', 'paused');
    el.setAttribute('can-resume', '');
    el.setAttribute('can-kill', '');
    document.body.appendChild(el);
    await el.updateComplete;

    expect(buttonLabels(el)).toEqual(['Resume', 'Kill']);
  });

  test('performs actions against the injected URLs and applies the response', async () => {
    const { el, fetchMock } = await mount('running', {
      'pause-url': '/injected/pause',
    });
    const posted: string[] = [];
    fetchMock.mockImplementation(((input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === 'POST') posted.push(String(input));
      return Promise.resolve(new Response(JSON.stringify(stateBody('paused'))));
    }) as never);

    el.shadowRoot?.querySelector('button')?.click();
    await flush(el);

    expect(posted).toEqual(['/injected/pause']);
    expect(buttonLabels(el)).toEqual(['Resume', 'Kill']);
  });

  test('has no URL of its own: without injected attributes it issues no request and opens no socket', async () => {
    const fetchMock = mock(() => Promise.resolve(new Response('{}')));
    globalThis.fetch = fetchMock as unknown as typeof fetch;
    const el = document.createElement('pitha-kill-switch-panel') as PanelElement;
    document.body.appendChild(el);
    await flush(el);

    expect(fetchMock.mock.calls.length).toBe(0);
    expect(FakeWebSocket.instances.length).toBe(0);
  });

  test('dispatches systemStateChanged and resyncs allowed actions on a kill_switch WebSocket push', async () => {
    const { el, fetchMock } = await mount('running');
    let changedCount = 0;
    el.addEventListener('systemStateChanged', () => {
      changedCount += 1;
    });
    const countBeforePush = changedCount;
    fetchMock.mockImplementation((() =>
      Promise.resolve(new Response(JSON.stringify(stateBody('killed'))))) as never);

    const socket = FakeWebSocket.instances[0];
    socket.emit('open');
    socket.emit('message', {
      data: JSON.stringify({ type: 'kill_switch', reason: 'daily_loss_limit' }),
    });
    await flush(el);

    expect(changedCount).toBeGreaterThan(countBeforePush);
    expect(el.shadowRoot?.querySelector('[data-status]')?.getAttribute('data-status')).toBe(
      'killed',
    );
    expect(buttonLabels(el)).toEqual(['Resume']);
  });

  test('offers no actions after a kill_switch push when the resync fails', async () => {
    const { el, fetchMock } = await mount('running');
    fetchMock.mockImplementation((() => Promise.reject(new Error('offline'))) as never);

    FakeWebSocket.instances[0].emit('message', {
      data: JSON.stringify({ type: 'kill_switch', reason: 'daily_loss_limit' }),
    });
    await flush(el);

    expect(el.shadowRoot?.querySelector('[data-status]')?.getAttribute('data-status')).toBe(
      'killed',
    );
    expect(buttonLabels(el)).toEqual([]);
  });

  test('shows a disconnect notice while the WebSocket is down and resyncs once it reconnects', async () => {
    const { el, fetchMock } = await mount('running');
    expect(el.shadowRoot?.querySelector('[data-ws-status]')).toBeNull();

    FakeWebSocket.instances[0].emit('open');
    FakeWebSocket.instances[0].emit('close', { code: 1006 });
    await el.updateComplete;
    expect(el.shadowRoot?.querySelector('[data-ws-status]')?.getAttribute('data-ws-status')).toBe(
      'reconnecting',
    );

    const callsBefore = fetchMock.mock.calls.length;
    await new Promise((resolve) => setTimeout(resolve, 600));
    FakeWebSocket.instances[1].emit('open');
    await flush(el);

    expect(el.shadowRoot?.querySelector('[data-ws-status]')).toBeNull();
    expect(fetchMock.mock.calls.length).toBeGreaterThan(callsBefore);
  });
});
