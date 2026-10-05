import { afterEach, beforeEach, describe, expect, test, vi } from 'bun:test';
import { isWsDisconnected, resolveWsUrl, WsClient, type WsStatus } from './ws';

type Listener = (event: unknown) => void;

class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  private listeners: Record<string, Listener[]> = {};

  constructor(public readonly url: string) {
    FakeWebSocket.instances.push(this);
  }

  addEventListener(type: string, listener: Listener): void {
    if (!this.listeners[type]) {
      this.listeners[type] = [];
    }
    this.listeners[type].push(listener);
  }

  emit(type: string, event: unknown = {}): void {
    for (const listener of this.listeners[type] ?? []) {
      listener(event);
    }
  }

  close(): void {
    this.emit('close', { code: 1000 });
  }
}

let originalWebSocket: typeof WebSocket;

beforeEach(() => {
  originalWebSocket = globalThis.WebSocket;
  FakeWebSocket.instances = [];
  globalThis.WebSocket = FakeWebSocket as unknown as typeof WebSocket;
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  globalThis.WebSocket = originalWebSocket;
});

describe('WsClient', () => {
  test('parses JSON messages and forwards them to onMessage', () => {
    const received: unknown[] = [];
    new WsClient('/ws/scanner', { onMessage: (message) => received.push(message) });

    const socket = FakeWebSocket.instances[0];
    socket.emit('message', { data: JSON.stringify({ type: 'tick', price: 123 }) });

    expect(received).toEqual([{ type: 'tick', price: 123 }]);
  });

  test('reconnects with the initial backoff after the server closes the connection', () => {
    new WsClient('/ws/scanner', {});

    expect(FakeWebSocket.instances).toHaveLength(1);
    FakeWebSocket.instances[0].emit('close', { code: 1006 });

    vi.advanceTimersByTime(500);

    expect(FakeWebSocket.instances).toHaveLength(2);
  });

  test('stops reconnecting once close() has been called by the caller', () => {
    const client = new WsClient('/ws/scanner', {});
    client.close();

    vi.advanceTimersByTime(500);

    expect(FakeWebSocket.instances).toHaveLength(1);
  });

  test('reports connecting, open and reconnecting through onStatusChange', () => {
    const statuses: WsStatus[] = [];
    new WsClient('/ws/scanner', { onStatusChange: (status) => statuses.push(status) });

    expect(statuses).toEqual(['connecting']);
    FakeWebSocket.instances[0].emit('open');
    FakeWebSocket.instances[0].emit('close', { code: 1006 });
    vi.advanceTimersByTime(500);
    FakeWebSocket.instances[1].emit('open');

    expect(statuses).toEqual(['connecting', 'open', 'reconnecting', 'open']);
  });

  test('calls onReconnect only on the first open after a disconnect, never on the initial open', () => {
    const onReconnect = vi.fn();
    new WsClient('/ws/scanner', { onReconnect });

    FakeWebSocket.instances[0].emit('open');
    expect(onReconnect).not.toHaveBeenCalled();

    FakeWebSocket.instances[0].emit('close', { code: 1006 });
    expect(onReconnect).not.toHaveBeenCalled();
    vi.advanceTimersByTime(500);
    FakeWebSocket.instances[1].emit('open');
    expect(onReconnect).toHaveBeenCalledTimes(1);

    // A later drop re-arms it; a repeated open without a drop does not fire it.
    FakeWebSocket.instances[1].emit('open');
    expect(onReconnect).toHaveBeenCalledTimes(1);
    FakeWebSocket.instances[1].emit('close', { code: 1006 });
    vi.advanceTimersByTime(1000);
    FakeWebSocket.instances[2].emit('open');
    expect(onReconnect).toHaveBeenCalledTimes(2);
  });

  test('does not call onReconnect after close() by the caller', () => {
    const onReconnect = vi.fn();
    const client = new WsClient('/ws/scanner', { onReconnect });
    FakeWebSocket.instances[0].emit('open');
    client.close();
    vi.advanceTimersByTime(500);

    expect(FakeWebSocket.instances).toHaveLength(1);
    expect(onReconnect).not.toHaveBeenCalled();
  });

  test('reports failed after the retry limit but keeps retrying every 30s', () => {
    const statuses: WsStatus[] = [];
    new WsClient('/ws/scanner', { onStatusChange: (status) => statuses.push(status) });

    for (let attempt = 0; attempt < 10; attempt += 1) {
      FakeWebSocket.instances.at(-1)?.emit('close', { code: 1006 });
      expect(statuses.at(-1)).toBe('reconnecting');
      vi.advanceTimersByTime(30_000);
    }
    FakeWebSocket.instances.at(-1)?.emit('close', { code: 1006 });
    expect(statuses.at(-1)).toBe('failed');

    const before = FakeWebSocket.instances.length;
    vi.advanceTimersByTime(30_000);
    expect(FakeWebSocket.instances).toHaveLength(before + 1);
    FakeWebSocket.instances.at(-1)?.emit('open');
    expect(statuses.at(-1)).toBe('open');
  });

  // Issue #466: a server that accepts and then immediately closes must not
  // reset the backoff, or `failed` is never reached.
  test('grows the backoff and reaches failed when connections close right after open', () => {
    const statuses: WsStatus[] = [];
    new WsClient('/ws/scanner', { onStatusChange: (status) => statuses.push(status) });

    for (let attempt = 0; attempt < 10; attempt += 1) {
      const expectedBackoff = Math.min(500 * 2 ** attempt, 30_000);
      const count = FakeWebSocket.instances.length;
      FakeWebSocket.instances.at(-1)?.emit('open');
      FakeWebSocket.instances.at(-1)?.emit('close', { code: 1011 });
      expect(statuses.at(-1)).toBe('reconnecting');
      vi.advanceTimersByTime(expectedBackoff - 1);
      expect(FakeWebSocket.instances).toHaveLength(count);
      vi.advanceTimersByTime(1);
      expect(FakeWebSocket.instances).toHaveLength(count + 1);
    }
    FakeWebSocket.instances.at(-1)?.emit('open');
    FakeWebSocket.instances.at(-1)?.emit('close', { code: 1011 });
    expect(statuses.at(-1)).toBe('failed');
  });

  test('restarts from the initial backoff after a connection that received a message', () => {
    new WsClient('/ws/scanner', {});

    for (let attempt = 0; attempt < 3; attempt += 1) {
      FakeWebSocket.instances.at(-1)?.emit('open');
      FakeWebSocket.instances.at(-1)?.emit('close', { code: 1011 });
      vi.advanceTimersByTime(500 * 2 ** attempt);
    }
    const healthy = FakeWebSocket.instances.at(-1);
    healthy?.emit('open');
    healthy?.emit('message', { data: '{}' });
    healthy?.emit('close', { code: 1006 });

    const before = FakeWebSocket.instances.length;
    vi.advanceTimersByTime(500);
    expect(FakeWebSocket.instances).toHaveLength(before + 1);
  });

  test('restarts from the initial backoff after a connection that stayed up for 10s', () => {
    new WsClient('/ws/scanner', {});

    FakeWebSocket.instances.at(-1)?.emit('open');
    FakeWebSocket.instances.at(-1)?.emit('close', { code: 1011 });
    vi.advanceTimersByTime(500);
    const quiet = FakeWebSocket.instances.at(-1);
    quiet?.emit('open');
    vi.advanceTimersByTime(10_000);
    quiet?.emit('close', { code: 1006 });

    const before = FakeWebSocket.instances.length;
    vi.advanceTimersByTime(500);
    expect(FakeWebSocket.instances).toHaveLength(before + 1);
  });

  test('does not report a status change or reconnect after close() by the caller', () => {
    const statuses: WsStatus[] = [];
    const client = new WsClient('/ws/scanner', { onStatusChange: (s) => statuses.push(s) });
    client.close();

    expect(statuses).toEqual(['connecting']);
    vi.advanceTimersByTime(60_000);
    expect(FakeWebSocket.instances).toHaveLength(1);
  });

  test('drops malformed JSON with a warning instead of hiding it', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const received: unknown[] = [];
    try {
      new WsClient('/ws/scanner', { onMessage: (m) => received.push(m) });
      FakeWebSocket.instances[0].emit('message', { data: '{not json' });

      expect(received).toEqual([]);
      expect(warn).toHaveBeenCalledTimes(1);
    } finally {
      warn.mockRestore();
    }
  });

  test('isWsDisconnected is true only for reconnecting/failed', () => {
    expect(
      (['connecting', 'open', 'reconnecting', 'failed'] as const).map(isWsDisconnected),
    ).toEqual([false, false, true, true]);
  });
});

describe('resolveWsUrl', () => {
  afterEach(() => {
    document.head.innerHTML = '';
  });

  function setWsBase(content: string): void {
    const meta = document.createElement('meta');
    meta.name = 'ws-base';
    meta.content = content;
    document.head.appendChild(meta);
  }

  test('uses the page origin without a ws-base meta', () => {
    expect(resolveWsUrl('/ws/scanner')).toBe(`ws://${location.host}/ws/scanner`);
  });

  // Issue #266: in the desktop app the WebSocket listener is not the
  // page's origin; resolving against location.host never connects.
  test('uses the ws-base meta the server renders when WebSockets live elsewhere', () => {
    setWsBase('ws://wails.localhost:51234');
    expect(resolveWsUrl('/ws/scanner')).toBe('ws://wails.localhost:51234/ws/scanner');
  });

  test('returns an absolute URL unchanged', () => {
    setWsBase('ws://wails.localhost:51234');
    expect(resolveWsUrl('wss://example.test/ws/x')).toBe('wss://example.test/ws/x');
  });
});
