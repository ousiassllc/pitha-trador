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
