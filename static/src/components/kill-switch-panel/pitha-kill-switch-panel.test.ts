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

// mount stubs `fetch` to resolve GET /api/v1/system/status with state,
// appends a fresh <pitha-kill-switch-panel>, and waits for its initial
// render to complete.
async function mount(state: string) {
  const fetchMock = mock(() => Promise.resolve(new Response(JSON.stringify({ state }))));
  globalThis.fetch = fetchMock as unknown as typeof fetch;

  const el = document.createElement('pitha-kill-switch-panel') as PanelElement;
  document.body.appendChild(el);
  await flush(el);
  return { el, fetchMock };
}

describe('pitha-kill-switch-panel', () => {
  test('loads initial status and renders derived action buttons', async () => {
    const { el } = await mount('running');

    expect(el.shadowRoot?.querySelector('[data-status]')?.getAttribute('data-status')).toBe(
      'running',
    );
    const buttons = Array.from(el.shadowRoot?.querySelectorAll('button') ?? []).map((b) =>
      b.textContent?.trim(),
    );
    expect(buttons).toContain('Pause');
    expect(buttons).toContain('Kill');
    expect(buttons).not.toContain('Resume');
  });

  test('renders Resume instead of Pause/Kill when already paused', async () => {
    const { el } = await mount('paused');

    const buttons = Array.from(el.shadowRoot?.querySelectorAll('button') ?? []).map((b) =>
      b.textContent?.trim(),
    );
    expect(buttons).toContain('Resume');
    expect(buttons).toContain('Kill');
    expect(buttons).not.toContain('Pause');
  });

  test('dispatches systemStateChanged and updates status on a kill_switch WebSocket push', async () => {
    const { el } = await mount('running');
    let changedCount = 0;
    el.addEventListener('systemStateChanged', () => {
      changedCount += 1;
    });
    const countBeforePush = changedCount;

    const socket = FakeWebSocket.instances[0];
    socket.emit('open');
    socket.emit('message', {
      data: JSON.stringify({ type: 'kill_switch', reason: 'daily_loss_limit' }),
    });
    await el.updateComplete;

    expect(changedCount).toBeGreaterThan(countBeforePush);
    expect(el.shadowRoot?.querySelector('[data-status]')?.getAttribute('data-status')).toBe(
      'killed',
    );
  });
});
