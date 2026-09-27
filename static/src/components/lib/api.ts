// Shared JSON API client used by every Lit component
// (docs/components/overview.md §6). All fetches MUST go through this
// module instead of calling `fetch()` directly, so CSRF handling and error
// behavior stay consistent across `pitha-*` components.

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' };
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
  if (!response.ok) {
    throw new Error(`${method} ${path} failed with status ${response.status}`);
  }

  return (await response.json()) as T;
}

export function get<T>(path: string): Promise<T> {
  return request<T>('GET', path);
}

export function post<T>(path: string, body?: unknown): Promise<T> {
  return request<T>('POST', path, body);
}

export function put<T>(path: string, body?: unknown): Promise<T> {
  return request<T>('PUT', path, body);
}

export function patch<T>(path: string, body?: unknown): Promise<T> {
  return request<T>('PATCH', path, body);
}

export function del<T>(path: string): Promise<T> {
  return request<T>('DELETE', path);
}
