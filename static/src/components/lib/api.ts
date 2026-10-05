// Shared JSON API client used by every Lit component
// (docs/components/overview.md §6). All fetches MUST go through this
// module instead of calling `fetch()` directly, so CSRF handling and error
// behavior stay consistent across `pitha-*` components.

// Marks a request the page fires by itself (not an operator action), so the
// server's Heartbeat middleware does not count it as operator activity
// (FR-RISK-6). Must match middleware.BackgroundHeader.
const BACKGROUND_HEADER = 'X-Pitha-Background';

export interface GetOptions {
  /** True for auto-fired requests (resync after a push / WS reconnect). */
  background?: boolean;
}

/** Response header the server sets on a 403 caused by a stale session cookie/CSRF token (middleware.CSRFRejectHeader). */
export const CSRF_REJECT_HEADER = 'X-CSRF-Reject';
export const CSRF_REJECT_STALE = 'stale';
/** Shown when the page was opened before an app restart: its tokens are gone, only a reload fetches new ones. */
export const STALE_SESSION_MESSAGE =
  'アプリが再起動されたためこのページの認証情報が失効しました。ページを再読み込みしてください。';

/** A request refused because the page's session cookie/CSRF token predates the running server. */
export class StaleSessionError extends Error {
  constructor() {
    super(STALE_SESSION_MESSAGE);
    this.name = 'StaleSessionError';
  }
}

/**
 * A non-2xx API response. `message` is operator-facing (Japanese) and carries
 * only the HTTP status and the server's problem+json `detail`; the request
 * method/path are kept as fields for logs, not shown in the UI.
 */
export class ApiError extends Error {
  constructor(
    readonly method: string,
    readonly path: string,
    readonly status: number,
    readonly detail: string,
  ) {
    super(`リクエストに失敗しました（HTTP ${status}）${detail ? `: ${detail}` : ''}`);
    this.name = 'ApiError';
  }
}

// readProblemDetail returns the RFC 7807 `detail` of an error response, or ''
// when the body is empty, not JSON, or carries no string `detail`.
async function readProblemDetail(response: Response): Promise<string> {
  try {
    const problem: unknown = await response.json();
    if (typeof problem === 'object' && problem !== null && 'detail' in problem) {
      return typeof problem.detail === 'string' ? problem.detail : '';
    }
  } catch {
    // Empty or non-JSON body: fall back to the status alone.
  }
  return '';
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  options: GetOptions = {},
): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (options.background) {
    headers[BACKGROUND_HEADER] = '1';
  }
  const token = document.querySelector<HTMLMetaElement>('meta[name="csrf-token"]')?.content ?? null;
  if (token) {
    headers['X-CSRF-Token'] = token;
  }

  const init: RequestInit = {
    method,
    headers,
    credentials: 'same-origin',
  };

  if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(body);
  }

  const response = await fetch(path, init);
  if (response.status === 403 && response.headers.get(CSRF_REJECT_HEADER) === CSRF_REJECT_STALE) {
    throw new StaleSessionError();
  }
  if (!response.ok) {
    throw new ApiError(method, path, response.status, await readProblemDetail(response));
  }

  return (await response.json()) as T;
}

export function get<T>(path: string, options?: GetOptions): Promise<T> {
  return request<T>('GET', path, undefined, options);
}

export function post<T>(path: string, body?: unknown): Promise<T> {
  return request<T>('POST', path, body);
}
