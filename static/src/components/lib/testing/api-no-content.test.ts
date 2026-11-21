import { afterEach, beforeEach, expect, mock, test } from 'bun:test';
import * as api from '../api';

// Lives under testing/ because lib/ is already at its line limit; the rest of api.test.ts stays in lib/.
let originalFetch: typeof fetch;

beforeEach(() => {
  originalFetch = globalThis.fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

// A huma handler without a Body answers 204 and an empty body; reading that as
// JSON would throw "Unexpected end of JSON input" after the server succeeded.
test.each([
  ['post', () => api.post<void>('/api/v1/migration/cancel')],
  ['get', () => api.get<void>('/api/v1/anything')],
])('%s resolves a 204 with no body to undefined', async (_name, call) => {
  globalThis.fetch = mock(() =>
    Promise.resolve(new Response(null, { status: 204 })),
  ) as unknown as typeof fetch;

  expect(await call()).toBeUndefined();
});

test('a 200 JSON body is still parsed', async () => {
  globalThis.fetch = mock(() =>
    Promise.resolve(new Response('{"ok":true}', { status: 200 })),
  ) as unknown as typeof fetch;

  expect(await api.post<{ ok: boolean }>('/api/v1/x')).toEqual({ ok: true });
});
