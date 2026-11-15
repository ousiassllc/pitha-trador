import { afterEach, beforeEach, describe, expect, test } from 'bun:test';
import { onClick } from './pitha-migration';

// A trimmed copy of organisms.MigrationPanel's markup: the hooks the module reads.
const PANEL = `
<dialog id="confirm-dialog" data-modal>
  <p data-confirm-message></p>
  <button type="button" data-confirm-cancel>キャンセル</button>
  <button type="button" data-confirm-accept>実行する</button>
</dialog>
<section data-migration-panel>
  <input type="file" data-migration-file>
  <button type="button" id="preview" data-migration-preview>内容を確認</button>
  <p data-migration-message></p>
  <div data-migration-result></div>
  <p data-migration-status></p>
  <button type="button" data-migration-cancel-pending hidden>やめる</button>
  <template data-migration-error-template><div data-inline-alert><p data-inline-alert-message></p></div></template>
  <template data-migration-row-template><table><tbody><tr><td data-field="name"></td><td data-field="current"></td><td data-field="bundle"></td></tr></tbody></table></template>
  <template data-migration-preview-template>
    <div>
      <dd data-field="created_at"></dd><dd data-field="app_version"></dd><dd data-field="schema"></dd>
      <table><tbody></tbody></table>
      <label data-migration-needs-replace hidden><input type="checkbox" data-migration-replace></label>
      <button type="button" id="confirm" data-migration-confirm>予約</button>
      <button type="button" id="cancel" data-migration-cancel>やめる</button>
    </div>
  </template>
</section>`;

interface Call {
  method: string;
  path: string;
  body: unknown;
}
let calls: Call[];
let originalFetch: typeof fetch;
let responses: Record<string, { status: number; body: unknown }>;

const preview = (hasData: boolean) => ({
  token: 'a'.repeat(32),
  manifest: {
    app_version: 'v1.2.3',
    created_at: '2026-11-14T01:02:03Z',
    schema_version: 54,
    excluded_settings: [],
  },
  tables: [{ name: 'orders', current: 1, bundle: 5 }],
  current_schema_version: 55,
  target_has_data: hasData,
});

beforeEach(() => {
  document.head.innerHTML = '<meta name="csrf-token" content="t">';
  document.body.innerHTML = PANEL;
  const dialog = document.getElementById('confirm-dialog') as HTMLDialogElement;
  dialog.showModal = () => dialog.setAttribute('open', '');
  dialog.close = () => {
    dialog.removeAttribute('open');
    dialog.dispatchEvent(new Event('close'));
  };
  calls = [];
  responses = {};
  originalFetch = globalThis.fetch;
  globalThis.fetch = ((path: string, init: RequestInit) => {
    calls.push({ method: init.method ?? 'GET', path, body: init.body });
    const r = responses[`${init.method} ${path}`] ?? { status: 200, body: {} };
    return Promise.resolve(new Response(JSON.stringify(r.body), { status: r.status }));
  }) as unknown as typeof fetch;
});
afterEach(() => {
  globalThis.fetch = originalFetch;
  document.body.innerHTML = '';
});

const text = (selector: string) => document.querySelector(selector)?.textContent ?? '';
const press = (id: string) => onClick({ target: document.getElementById(id) } as unknown as Event);

function chooseFile(): void {
  const input = document.querySelector<HTMLInputElement>(
    '[data-migration-file]',
  ) as HTMLInputElement;
  Object.defineProperty(input, 'files', { value: [new File(['x'], 'a.pitha-bundle')] });
}

describe('preview', () => {
  test('without a file nothing is sent', async () => {
    await press('preview');
    expect(calls).toHaveLength(0);
    expect(text('[data-migration-message]')).toContain('ファイルを選んでください');
  });

  test('uploads the file raw, then shows bundle info and both row counts without changing anything', async () => {
    chooseFile();
    responses['POST /api/v1/migration/preview'] = { status: 200, body: preview(true) };
    await press('preview');
    expect(calls.map((c) => `${c.method} ${c.path}`)).toEqual(['POST /api/v1/migration/preview']);
    expect(calls[0]?.body).toBeInstanceOf(Blob);
    expect(text('[data-field="app_version"]')).toBe('v1.2.3');
    expect(text('[data-field="schema"]')).toContain('55');
    expect(text('tbody')).toContain('orders');
    expect(text('tbody')).toContain('5');
    expect(text('[data-migration-message]')).toContain('まだ何も変更していません');
    expect(document.querySelector('[data-migration-needs-replace]')?.hasAttribute('hidden')).toBe(
      false,
    );
  });

  test('a refused file shows the server detail in an alert', async () => {
    chooseFile();
    responses['POST /api/v1/migration/preview'] = {
      status: 422,
      body: { detail: 'ファイルが壊れています' },
    };
    await press('preview');
    expect(text('[data-inline-alert-message]')).toBe('ファイルが壊れています');
  });
});

describe('confirm', () => {
  async function previewed(hasData: boolean): Promise<void> {
    chooseFile();
    responses['POST /api/v1/migration/preview'] = { status: 200, body: preview(hasData) };
    await press('preview');
    calls.length = 0;
  }
  const accept = () =>
    queueMicrotask(() => document.querySelector<HTMLElement>('[data-confirm-accept]')?.click());

  test('a database with data needs the replace checkbox: without it nothing is sent', async () => {
    await previewed(true);
    await press('confirm');
    expect(calls).toHaveLength(0);
    expect(text('[data-migration-message]')).toContain('チェック');
  });

  test('declining the confirm dialog sends nothing', async () => {
    await previewed(true);
    (document.querySelector('[data-migration-replace]') as HTMLInputElement).checked = true;
    queueMicrotask(() => document.querySelector<HTMLElement>('[data-confirm-cancel]')?.click());
    await press('confirm');
    expect(calls).toHaveLength(0);
  });

  test('checkbox + dialog accepted posts the token with replace_existing, and says the swap happens at the next start', async () => {
    await previewed(true);
    (document.querySelector('[data-migration-replace]') as HTMLInputElement).checked = true;
    accept();
    await press('confirm');
    const post = calls.find((c) => c.path === '/api/v1/migration/confirm');
    expect(JSON.parse(String(post?.body))).toEqual({
      token: 'a'.repeat(32),
      replace_existing: true,
    });
    expect(text('[data-migration-message]')).toContain('再起動');
  });

  test('a fresh database needs no checkbox', async () => {
    await previewed(false);
    accept();
    await press('confirm');
    const post = calls.find((c) => c.path === '/api/v1/migration/confirm');
    expect(JSON.parse(String(post?.body))).toEqual({
      token: 'a'.repeat(32),
      replace_existing: false,
    });
  });
});
