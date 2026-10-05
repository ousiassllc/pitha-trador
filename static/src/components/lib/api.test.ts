import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test';
import * as api from './api';

let originalFetch: typeof fetch;

beforeEach(() => {
  originalFetch = globalThis.fetch;
  document.head.innerHTML = '';
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

describe('get', () => {
  test('sends a GET request without a body and parses the JSON response', async () => {
    const fetchMock = mock(() =>
      Promise.resolve(new Response(JSON.stringify({ ok: true }), { status: 200 })),
    );
    globalThis.fetch = fetchMock as unknown as typeof fetch;

    const result = await api.get<{ ok: boolean }>('/api/v1/scanner');

    expect(result).toEqual({ ok: true });
    const [path, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(path).toBe('/api/v1/scanner');
    expect(init.method).toBe('GET');
    expect(init.body).toBeUndefined();
    expect(init.credentials).toBe('same-origin');
  });
});

describe('get background', () => {
  test('marks auto-fired requests with X-Pitha-Background only when asked', async () => {
    const fetchMock = mock(() => Promise.resolve(new Response('{}', { status: 200 })));
    globalThis.fetch = fetchMock as unknown as typeof fetch;

    await api.get('/api/v1/system/status');
    await api.get('/api/v1/system/status', { background: true });

    const headersOf = (n: number) =>
      (fetchMock.mock.calls[n] as unknown as [string, RequestInit])[1].headers as Record<
        string,
        string
      >;
    expect(headersOf(0)['X-Pitha-Background']).toBeUndefined();
    expect(headersOf(1)['X-Pitha-Background']).toBe('1');
  });
});

describe('post', () => {
  test('sends the CSRF token from the meta tag and a JSON body', async () => {
    const meta = document.createElement('meta');
    meta.name = 'csrf-token';
    meta.content = 'test-token';
    document.head.appendChild(meta);

    const fetchMock = mock(() => Promise.resolve(new Response('{}', { status: 200 })));
    globalThis.fetch = fetchMock as unknown as typeof fetch;

    await api.post('/api/v1/system/kill', { reason: 'manual' });

    const [, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    const headers = init.headers as Record<string, string>;
    expect(headers['X-CSRF-Token']).toBe('test-token');
    expect(headers['Content-Type']).toBe('application/json');
    expect(init.body).toBe(JSON.stringify({ reason: 'manual' }));
  });
});

describe('error handling', () => {
  async function failure(response: Response): Promise<api.ApiError> {
    globalThis.fetch = mock(() => Promise.resolve(response)) as unknown as typeof fetch;
    const err = await api.post('/api/v1/system/kill').catch((e: unknown) => e);
    expect(err).toBeInstanceOf(api.ApiError);
    return err as api.ApiError;
  }

  // The server's problem+json detail explains why (e.g. liquidation failed);
  // the internal path must not reach the operator-facing message (#559).
  test('carries the problem+json detail and status, keeping the path out of the message', async () => {
    const err = await failure(
      new Response(
        JSON.stringify({ title: 'Internal Server Error', status: 500, detail: 'kill failed' }),
        {
          status: 500,
          headers: { 'Content-Type': 'application/problem+json' },
        },
      ),
    );

    expect(err.status).toBe(500);
    expect(err.detail).toBe('kill failed');
    expect(err.message).toBe('リクエストに失敗しました（HTTP 500）: kill failed');
    expect(err.message).not.toContain('/api/v1/system/kill');
    expect(err.method).toBe('POST');
    expect(err.path).toBe('/api/v1/system/kill');
  });

  test.each([
    ['an empty body', ''],
    ['a non-JSON body', '<html>Bad Gateway</html>'],
    ['a JSON body without detail', '{"status":502}'],
    ['a non-string detail', '{"detail":{"x":1}}'],
  ])('falls back to the status alone for %s', async (_name, body) => {
    const err = await failure(new Response(body, { status: 502 }));

    expect(err.status).toBe(502);
    expect(err.detail).toBe('');
    expect(err.message).toBe('リクエストに失敗しました（HTTP 502）');
  });
});

describe('stale session', () => {
  test('a 403 marked stale by the server throws StaleSessionError asking for a reload', async () => {
    const fetchMock = mock(() =>
      Promise.resolve(
        new Response('forbidden', { status: 403, headers: { 'X-CSRF-Reject': 'stale' } }),
      ),
    );
    globalThis.fetch = fetchMock as unknown as typeof fetch;

    const error = await api.post('/api/v1/system/kill').catch((e: unknown) => e);

    expect(error).toBeInstanceOf(api.StaleSessionError);
    expect((error as Error).message).toContain('再読み込み');
  });

  test('a plain 403 stays a generic status error', async () => {
    globalThis.fetch = mock(() =>
      Promise.resolve(new Response('', { status: 403 })),
    ) as unknown as typeof fetch;

    const error = await api.post('/api/v1/system/kill').catch((e: unknown) => e);

    expect(error).toBeInstanceOf(api.ApiError);
    expect((error as api.ApiError).status).toBe(403);
  });
});
