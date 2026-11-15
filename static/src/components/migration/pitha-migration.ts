// PC 移行 panel of the Settings screen (issue #1075; docs/components/screens.md
// 「PC 移行」). The export is a plain GET form; this module drives the import:
//   1. 「内容を確認」 uploads the chosen file as the raw body of
//      POST /api/v1/migration/preview. The server validates it (version
//      compatibility first, then checksum / integrity / no secrets) and answers
//      with what it holds and what it would replace; NOTHING is changed.
//   2. The preview is drawn from the panel's <template>s: bundle date and
//      version, a table of row counts (current / bundle), and - when the
//      current database has data - a checkbox the operator must tick.
//   3. 「取り込みを予約する」 asks once more in the confirm dialog, then
//      POST /api/v1/migration/confirm. The database is replaced only at the
//      next start of the application; the panel says so.
// CSP forbids inline JS and the panel's markup (with its Tailwind classes)
// lives in internal/web/organisms/migration_panel.templ; this file only fills
// text content and toggles `hidden`, never building HTML from server text.

import { askConfirm } from '../confirm/pitha-confirm';
import { ApiError, get, post } from '../lib/api';
import { formatJstDateTime } from '../lib/jst-datetime';

interface TableRow {
  name: string;
  current: number;
  bundle: number;
}
interface Manifest {
  app_version: string;
  created_at: string;
  schema_version: number;
  excluded_settings: string[];
}
interface Preview {
  token: string;
  manifest: Manifest;
  tables: TableRow[];
  current_schema_version: number;
  target_has_data: boolean;
}
interface Pending {
  manifest: Manifest;
  confirmed_at: string;
}
interface LastResult {
  status: 'applied' | 'failed';
  at: string;
  error?: string;
  safety_copy?: string;
}
interface Status {
  pending?: Pending;
  last_result?: LastResult;
}

const ASK_REPLACE =
  '現在のデータを、選んだファイルの内容で置き換えます。アプリを再起動したときに入れ替わり、入れ替え前のデータは安全コピーとして残ります。取り込みを予約しますか？';

function part<T extends Element>(root: ParentNode, name: string): T | null {
  return root.querySelector<T>(`[data-migration-${name}]`);
}

function setMessage(root: HTMLElement, text: string): void {
  const el = part(root, 'message');
  if (el) el.textContent = text;
}

function clearResult(root: HTMLElement): void {
  const result = part(root, 'result');
  if (result) result.replaceChildren();
}

function showError(root: HTMLElement, text: string): void {
  clearResult(root);
  const result = part(root, 'result');
  const tpl = part<HTMLTemplateElement>(root, 'error-template');
  if (!result || !tpl) {
    setMessage(root, text);
    return;
  }
  const alert = tpl.content.cloneNode(true) as DocumentFragment;
  const message = alert.querySelector('[data-inline-alert-message]');
  if (message) message.textContent = text;
  result.append(alert);
  setMessage(root, '');
}

function errorText(err: unknown): string {
  return err instanceof ApiError || err instanceof Error ? err.message : '失敗しました。';
}

/** An ApiError's `message` is "リクエストに失敗しました（HTTP n）: detail"; the operator wants the detail. */
function detailOf(err: unknown): string {
  return err instanceof ApiError && err.detail ? err.detail : errorText(err);
}

function fill(root: ParentNode, field: string, text: string): void {
  const el = root.querySelector(`[data-field="${field}"]`);
  if (el) el.textContent = text;
}

function drawPreview(root: HTMLElement, preview: Preview): void {
  const tpl = part<HTMLTemplateElement>(root, 'preview-template');
  const rowTpl = part<HTMLTemplateElement>(root, 'row-template');
  const result = part<HTMLElement>(root, 'result');
  if (!tpl || !rowTpl || !result) return;
  const view = tpl.content.cloneNode(true) as DocumentFragment;
  const m = preview.manifest;
  fill(view, 'created_at', formatJstDateTime(m.created_at));
  fill(view, 'app_version', m.app_version);
  const older = m.schema_version < preview.current_schema_version;
  fill(
    view,
    'schema',
    older
      ? `${m.schema_version}（取り込み時に ${preview.current_schema_version} へ更新されます）`
      : String(m.schema_version),
  );
  const body = view.querySelector('tbody');
  for (const row of preview.tables) {
    const tr = rowTpl.content.querySelector('tr')?.cloneNode(true) as HTMLElement | undefined;
    if (!tr) continue;
    fill(tr, 'name', row.name);
    fill(tr, 'current', String(row.current));
    fill(tr, 'bundle', String(row.bundle));
    body?.append(tr);
  }
  view
    .querySelector('[data-migration-needs-replace]')
    ?.toggleAttribute('hidden', !preview.target_has_data);
  result.replaceChildren(view);
  result.dataset.token = preview.token;
  result.dataset.targetHasData = String(preview.target_has_data);
  setMessage(root, '内容を確認しました。まだ何も変更していません。');
}

async function preview(root: HTMLElement): Promise<void> {
  const input = part<HTMLInputElement>(root, 'file');
  const file = input?.files?.[0];
  clearResult(root);
  if (!file) {
    setMessage(root, '取り込むファイルを選んでください。');
    return;
  }
  setMessage(root, 'ファイルを確認しています…');
  try {
    drawPreview(root, await post<Preview>('/api/v1/migration/preview', file));
  } catch (err) {
    showError(root, detailOf(err));
  }
}

async function confirm(root: HTMLElement): Promise<void> {
  const result = part<HTMLElement>(root, 'result');
  const token = result?.dataset.token;
  if (!result || !token) return;
  const needsReplace = result.dataset.targetHasData === 'true';
  const box = result.querySelector<HTMLInputElement>('[data-migration-replace]');
  if (needsReplace && !box?.checked) {
    setMessage(root, '現在のデータを置き換えることに同意するチェックを入れてください。');
    return;
  }
  if (!(await askConfirm(ASK_REPLACE))) return;
  try {
    await post('/api/v1/migration/confirm', { token, replace_existing: needsReplace });
    clearResult(root);
    setMessage(root, '取り込みを予約しました。アプリを再起動すると入れ替わります。');
    await loadStatus(root);
  } catch (err) {
    showError(root, detailOf(err));
  }
}

async function cancel(root: HTMLElement): Promise<void> {
  try {
    await post('/api/v1/migration/cancel');
    clearResult(root);
    setMessage(root, '取り込みをやめました。何も変更していません。');
    await loadStatus(root);
  } catch (err) {
    showError(root, detailOf(err));
  }
}

/** Fills the status line: a pending import (applied at the next start) and what the last start did. */
export async function loadStatus(root: HTMLElement): Promise<void> {
  const el = part(root, 'status');
  if (!el) return;
  const lines: string[] = [];
  try {
    const status = await get<Status>('/api/v1/migration/status', { background: true });
    if (status.pending) {
      lines.push(
        `取り込みを予約済みです（ファイル作成: ${formatJstDateTime(status.pending.manifest.created_at)}）。アプリを再起動すると入れ替わります。`,
      );
    }
    const last = status.last_result;
    if (last?.status === 'applied') {
      lines.push(
        `前回の取り込み: ${formatJstDateTime(last.at)} に完了しました。入れ替え前のデータの安全コピー: ${last.safety_copy ?? 'なし'}`,
      );
    } else if (last?.status === 'failed') {
      lines.push(
        `前回の取り込み: ${formatJstDateTime(last.at)} に失敗したため、元のデータのまま起動しました（${last.error ?? ''}）。`,
      );
    }
  } catch {
    // The status line is a convenience; the panel works without it.
  }
  el.textContent = lines.join('\n');
  part(root, 'cancel-pending')?.toggleAttribute(
    'hidden',
    !lines.some((l) => l.startsWith('取り込みを予約済み')),
  );
}

/** `click`: dispatch the panel's buttons. */
export async function onClick(event: Event): Promise<void> {
  const target = event.target;
  if (!(target instanceof Element)) return;
  const root = target.closest<HTMLElement>('[data-migration-panel]');
  if (!root) return;
  if (target.closest('[data-migration-preview]')) await preview(root);
  else if (target.closest('[data-migration-confirm]')) await confirm(root);
  else if (
    target.closest('[data-migration-cancel]') ||
    target.closest('[data-migration-cancel-pending]')
  )
    await cancel(root);
}

export function init(): void {
  document.addEventListener('click', (event) => void onClick(event));
  for (const root of document.querySelectorAll<HTMLElement>('[data-migration-panel]'))
    void loadStatus(root);
}

init();
