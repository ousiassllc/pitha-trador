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
  test('throws with the method, path, and status when the response is not ok', async () => {
    const fetchMock = mock(() => Promise.resolve(new Response('', { status: 500 })));
    globalThis.fetch = fetchMock as unknown as typeof fetch;

    await expect(api.del('/api/v1/positions/1')).rejects.toThrow(
      'DELETE /api/v1/positions/1 failed with status 500',
    );
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

    await expect(api.post('/api/v1/system/kill')).rejects.toThrow(
      'POST /api/v1/system/kill failed with status 403',
    );
  });
});
