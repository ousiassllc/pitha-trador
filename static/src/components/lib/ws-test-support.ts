// The one fake `WebSocket` of the component tests (esbuild bundles only the
// `pitha-*.ts` entries, so this never reaches the production bundle). It
// mirrors what lib/ws.ts relies on: `addEventListener` for open/message/close,
// `emit` to fire them from a test, and a `close()` that fires `close`
// immediately. Keep it in step with lib/ws.ts here, not in per-test copies.

type Listener = (event: unknown) => void;

export class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  closed = false;
  private readonly listeners: Record<string, Listener[]> = {};

  constructor(public readonly url: string) {
    FakeWebSocket.instances.push(this);
  }

  addEventListener(type: string, listener: Listener): void {
    this.listeners[type] = [...(this.listeners[type] ?? []), listener];
  }

  emit(type: string, event: unknown = {}): void {
    for (const listener of this.listeners[type] ?? []) listener(event);
  }

  close(): void {
    this.closed = true;
    this.emit('close', { code: 1000 });
  }
}

/** Swaps `globalThis.WebSocket` for FakeWebSocket (instances reset); returns the function that restores the original. */
export function installFakeWebSocket(): () => void {
  const original = globalThis.WebSocket;
  FakeWebSocket.instances = [];
  globalThis.WebSocket = FakeWebSocket as unknown as typeof WebSocket;
  return () => {
    globalThis.WebSocket = original;
  };
}
