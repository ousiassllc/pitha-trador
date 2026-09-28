import { afterEach, beforeEach, describe, expect, test, vi } from 'bun:test';
import { WsClient } from './ws';

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
});
