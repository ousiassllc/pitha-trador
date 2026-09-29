import { afterEach, beforeEach, describe, expect, test, vi } from 'bun:test';
import { isWsDisconnected, WsClient, type WsStatus } from './ws';

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
