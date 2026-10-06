// Shared WebSocket client with automatic reconnection used by every Lit
// component (docs/components/overview.md §6). Components MUST use this
// class instead of calling `new WebSocket()` directly, so reconnect/backoff
// behavior stays consistent across `pitha-*` components.

import { logger } from './logger';

// MAX_RETRIES is the number of consecutive failed reconnects (with
// exponential backoff up to MAX_BACKOFF_MS) after which the client reports
// `failed` to its owner. It keeps retrying at MAX_BACKOFF_MS afterwards:
// giving up silently would leave e.g. the Kill Switch panel deaf to
// `/ws/system` notifications for good while the UI still looks connected.
const MAX_RETRIES = 10;
const INITIAL_BACKOFF_MS = 500;
const MAX_BACKOFF_MS = 30_000;
// STABLE_CONNECTION_MS is how long an opened connection must stay up (without
// receiving any message) before it counts as healthy and resets the backoff.
const STABLE_CONNECTION_MS = 10_000;

// resolveWsUrl turns a possibly-relative WebSocket path (e.g. `/ws/scanner`)
// into an absolute `ws://`/`wss://` URL, or returns url unchanged if it is
// already absolute. The base is the `<meta name="ws-base">` the server
// renders when WebSockets are served from another origin than the page
// (the desktop app: the Wails AssetServer cannot carry WebSockets, issue
// #266), else the current page's protocol/host.
// Shared by every `pitha-*` component that takes a `ws-url` attribute, so
// each one does not re-derive this independently.
export function resolveWsUrl(url: string): string {
  if (/^wss?:\/\//i.test(url)) {
    return url;
  }
  const base = document.querySelector<HTMLMetaElement>('meta[name="ws-base"]')?.content;
  if (base) {
    return `${base.replace(/\/$/, '')}${url}`;
  }
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${protocol}//${location.host}${url}`;
}

// WsStatus is the connection state reported through onStatusChange:
//   - connecting:   first connection attempt in flight
//   - open:         connected
//   - reconnecting: connection lost, retrying with backoff
//   - failed:       MAX_RETRIES consecutive reconnects failed (still
//                   retrying every MAX_BACKOFF_MS)
export type WsStatus = 'connecting' | 'open' | 'reconnecting' | 'failed';

// isWsDisconnected reports whether the UI should tell the user that live
// updates are currently not arriving.
export function isWsDisconnected(status: WsStatus): boolean {
  return status === 'reconnecting' || status === 'failed';
}

export interface WsClientOptions<T> {
  onOpen?: () => void;
  // Fires once on the first open after the connection was lost (never on the
  // initial open). Events pushed while the socket was down are gone, so
  // components re-fetch their state here instead of tracking it themselves.
  // Use onStatusChange only to display the connection state.
  onReconnect?: () => void;
  onMessage?: (message: T) => void;
  onClose?: (event: CloseEvent) => void;
  onStatusChange?: (status: WsStatus) => void;
}

export class WsClient<T = unknown> {
  private socket: WebSocket | null = null;
  private retries = 0;
  private closedByUser = false;
  private reconnectTimer: number | null = null;
  // True from the first lost connection until the next successful open,
  // which is what onReconnect fires on.
  private wasDisconnected = false;

  constructor(
    private readonly url: string,
    private readonly options: WsClientOptions<T> = {},
  ) {
    this.options.onStatusChange?.('connecting');
    this.connect();
  }

  private connect(): void {
    const socket = new WebSocket(this.url);

    // The backoff is reset only once the connection proves healthy (first
    // message or STABLE_CONNECTION_MS of uptime), not on `open`: a server that
    // accepts and then immediately closes would otherwise pin the backoff at
    // INITIAL_BACKOFF_MS and never reach `failed` (issue #466).
    let stableTimer: number | null = null;
    const clearStableTimer = () => {
      if (stableTimer !== null) {
        window.clearTimeout(stableTimer);
        stableTimer = null;
      }
    };
    const markHealthy = () => {
      clearStableTimer();
      this.retries = 0;
    };

    socket.addEventListener('open', () => {
      stableTimer = window.setTimeout(markHealthy, STABLE_CONNECTION_MS);
      this.options.onStatusChange?.('open');
      this.options.onOpen?.();
      if (this.wasDisconnected) {
        this.wasDisconnected = false;
        this.options.onReconnect?.();
      }
    });

    socket.addEventListener('message', (event: MessageEvent<string>) => {
      markHealthy();
      let message: T;
      try {
        message = JSON.parse(event.data) as T;
      } catch (err) {
        // A malformed payload is a server-side contract violation: drop it
        // but leave a trace instead of hiding it.
        logger.warn('ws: dropped malformed message', { url: this.url, error: err });
        return;
      }
      this.options.onMessage?.(message);
    });

    socket.addEventListener('close', (event) => {
      clearStableTimer();
      this.options.onClose?.(event as CloseEvent);
      if (this.closedByUser) return;
      const backoff = Math.min(INITIAL_BACKOFF_MS * 2 ** this.retries, MAX_BACKOFF_MS);
      this.retries += 1;
      this.wasDisconnected = true;
      this.options.onStatusChange?.(this.retries > MAX_RETRIES ? 'failed' : 'reconnecting');
      this.reconnectTimer = window.setTimeout(() => this.connect(), backoff);
    });

    this.socket = socket;
  }

  close(): void {
    this.closedByUser = true;
    if (this.reconnectTimer !== null) {
      window.clearTimeout(this.reconnectTimer);
    }
    this.socket?.close();
  }
}
