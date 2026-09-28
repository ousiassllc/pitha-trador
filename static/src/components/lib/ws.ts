// Shared WebSocket client with automatic reconnection used by every Lit
// component (docs/components/overview.md §6). Components MUST use this
// class instead of calling `new WebSocket()` directly, so reconnect/backoff
// behavior stays consistent across `pitha-*` components.

const MAX_RETRIES = 10;
const INITIAL_BACKOFF_MS = 500;
const MAX_BACKOFF_MS = 30_000;

// resolveWsUrl turns a possibly-relative WebSocket path (e.g. `/ws/scanner`)
// into an absolute `ws://`/`wss://` URL matching the current page's
// protocol/host, or returns url unchanged if it is already absolute.
// Shared by every `pitha-*` component that takes a `ws-url` attribute, so
// each one does not re-derive this independently.
export function resolveWsUrl(url: string): string {
  if (/^wss?:\/\//i.test(url)) {
    return url;
  }
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${protocol}//${location.host}${url}`;
}

export interface WsClientOptions<T> {
  onOpen?: () => void;
  onMessage?: (message: T) => void;
  onClose?: (event: CloseEvent) => void;
}

export class WsClient<T = unknown> {
  private socket: WebSocket | null = null;
  private retries = 0;
  private closedByUser = false;
  private reconnectTimer: number | null = null;

  constructor(
    private readonly url: string,
    private readonly options: WsClientOptions<T> = {},
  ) {
    this.connect();
  }

  private connect(): void {
    const socket = new WebSocket(this.url);

    socket.addEventListener('open', () => {
      this.retries = 0;
      this.options.onOpen?.();
    });

    socket.addEventListener('message', (event: MessageEvent<string>) => {
      try {
        this.options.onMessage?.(JSON.parse(event.data) as T);
      } catch {
        // Malformed payloads are ignored; the server is the sole source of
        // truth and is expected to always send valid JSON.
      }
    });

    socket.addEventListener('close', (event) => {
      this.options.onClose?.(event as CloseEvent);
      if (!this.closedByUser && this.retries < MAX_RETRIES) {
        const backoff = Math.min(INITIAL_BACKOFF_MS * 2 ** this.retries, MAX_BACKOFF_MS);
        this.retries += 1;
        this.reconnectTimer = window.setTimeout(() => this.connect(), backoff);
      }
    });

    this.socket = socket;
  }

  close(): void {
    this.closedByUser = true;
    if (this.reconnectTimer) {
      window.clearTimeout(this.reconnectTimer);
    }
    this.socket?.close();
  }
}
