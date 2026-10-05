import { describe, expect, mock, spyOn, test } from 'bun:test';
import {
  buttonLabels,
  FakeWebSocket,
  flush,
  installFakes,
  mount,
  type PanelElement,
  stateBody,
  URLS,
} from './kill-switch-test-support';

installFakes();

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

  // Tailwind does not cross the shadow boundary (issue #145): the panel's
  // own styles must reach the shadow root and make Kill look dangerous.
  test('styles the Kill button distinctly from Pause inside the shadow root', async () => {
    const { el } = await mount('running');

    const [pause, kill] = ['Pause', 'Kill'].map((label) =>
      Array.from(el.shadowRoot?.querySelectorAll('button') ?? []).find(
        (b) => b.textContent?.trim() === label,
      ),
    );
    expect(pause && kill).toBeTruthy();
    const killBg = getComputedStyle(kill as Element).backgroundColor;
    expect(killBg).not.toBe('');
    expect(killBg).not.toBe(getComputedStyle(pause as Element).backgroundColor);
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

  // Issue #363: AutoResume / another window's Resume leaves Killed without
  // any panel action, so the push must pull the panel and Header back.
  test('resyncs, restores the allowed actions and dispatches systemStateChanged on a state_changed push', async () => {
    const { el, fetchMock } = await mount('killed');
    expect(buttonLabels(el)).toEqual(['Resume']);
    let changedCount = 0;
    el.addEventListener('systemStateChanged', () => {
      changedCount += 1;
    });
    fetchMock.mockImplementation((() =>
      Promise.resolve(new Response(JSON.stringify(stateBody('running'))))) as never);

    FakeWebSocket.instances[0].emit('message', {
      data: JSON.stringify({ type: 'state_changed', state: 'running' }),
    });
    await flush(el);

    expect(changedCount).toBe(1);
    expect(el.shadowRoot?.querySelector('[data-status]')?.getAttribute('data-status')).toBe(
      'running',
    );
    expect(buttonLabels(el)).toEqual(['Pause', 'Kill']);
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

  test('sends every resync (initial, push, reconnect) as a background request, never the operator heartbeat', async () => {
    const { el, fetchMock } = await mount('running');
    const isBackground = (call: unknown): boolean => {
      const init = (call as [unknown, RequestInit])[1];
      return (init.headers as Record<string, string>)['X-Pitha-Background'] === '1';
    };
    expect(fetchMock.mock.calls.length).toBeGreaterThan(0);
    expect(fetchMock.mock.calls.every(isBackground)).toBe(true);

    fetchMock.mockClear();
    FakeWebSocket.instances[0].emit('message', {
      data: JSON.stringify({ type: 'kill_switch', reason: 'operator_heartbeat_timeout' }),
    });
    await flush(el);
    expect(fetchMock.mock.calls.length).toBe(1);
    expect(isBackground(fetchMock.mock.calls[0])).toBe(true);
  });

  // HATEOAS: URL attributes are injected by Templ only. A missing one is a
  // wiring mistake, so it must be logged rather than silently ignored.
  test('logs an error and does no request or subscription when status-url and ws-url are not injected', async () => {
    const errorSpy = spyOn(console, 'error').mockImplementation(() => {});
    try {
      const { fetchMock } = await mount('running', { 'status-url': '', 'ws-url': '' });

      expect(fetchMock).not.toHaveBeenCalled();
      expect(FakeWebSocket.instances).toHaveLength(0);
      for (const name of ['status-url', 'ws-url']) {
        expect(errorSpy).toHaveBeenCalledWith(
          expect.objectContaining({ message: `pitha-kill-switch-panel: ${name} is not set` }),
        );
      }
    } finally {
      errorSpy.mockRestore();
    }
  });

  test('logs an error and sends no POST when the clicked action URL is not injected', async () => {
    const errorSpy = spyOn(console, 'error').mockImplementation(() => {});
    try {
      const { el, fetchMock } = await mount('running', { 'pause-url': '' });
      fetchMock.mockClear();

      const pause = Array.from(el.shadowRoot?.querySelectorAll('button') ?? []).find(
        (b) => b.textContent?.trim() === 'Pause',
      );
      pause?.click();
      await flush(el);

      expect(fetchMock).not.toHaveBeenCalled();
      expect(errorSpy).toHaveBeenCalledWith(
        expect.objectContaining({ message: 'pitha-kill-switch-panel: pause-url is not set' }),
      );
    } finally {
      errorSpy.mockRestore();
    }
  });
});
