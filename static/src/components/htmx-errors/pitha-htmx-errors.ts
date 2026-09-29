// Global HTMX failure feedback (docs/components/overview.md §4 "エラー表示",
// §8). htmx never swaps 4xx/5xx by default; layout.Shell's `htmx-config`
// sends the atoms.Toast fragment an action handler returns into
// `#toast-region`. This module covers what that config cannot:
//   - error responses that carry no toast fragment (empty body, a plain
//     text/proxy error page) must not be swapped in raw, and get a generic
//     toast instead (`htmx:beforeSwap` / `htmx:responseError`);
//   - requests that never got a response (`htmx:sendError`, `htmx:timeout`);
//   - dismissing toasts (close button, auto-dismiss).
// The toast markup lives only in templ (`#toast-template`, atoms.Toast).

import { logger } from '../lib/logger';

const TOAST_MARKER = 'data-toast';
const DISMISS_AFTER_MS = 8000;

interface ResponseDetail {
  xhr?: XMLHttpRequest;
  shouldSwap?: boolean;
}

function region(): HTMLElement | null {
  return document.getElementById('toast-region');
}

function carriesToast(xhr: XMLHttpRequest | undefined): boolean {
  return xhr?.responseText?.includes(TOAST_MARKER) ?? false;
}

function isErrorStatus(xhr: XMLHttpRequest | undefined): boolean {
  return (xhr?.status ?? 0) >= 400;
}

/** Appends a toast cloned from `#toast-template`; an identical message already on screen is not duplicated (polling failures). */
export function showToast(message: string): void {
  const target = region();
  const template = document.getElementById('toast-template');
  if (!target || !(template instanceof HTMLTemplateElement)) {
    logger.error('toast region/template missing', { message });
    return;
  }
  for (const existing of target.querySelectorAll('[data-toast-message]')) {
    if (existing.textContent === message) return;
  }
  const toast = template.content.firstElementChild?.cloneNode(true) as HTMLElement | undefined;
  const text = toast?.querySelector('[data-toast-message]');
  if (!toast || !text) return;
  text.textContent = message;
  target.appendChild(toast);
}

function statusMessage(status: number): string {
  if (status >= 500) return `サーバーでエラーが発生しました（HTTP ${status}）。`;
  return `操作を完了できませんでした（HTTP ${status}）。`;
}

/** `htmx:beforeSwap`: an error response without a toast fragment is never swapped into `#toast-region`. */
export function onBeforeSwap(event: Event): void {
  const detail = (event as CustomEvent<ResponseDetail>).detail;
  if (isErrorStatus(detail.xhr) && !carriesToast(detail.xhr)) {
    detail.shouldSwap = false;
  }
}

/** `htmx:responseError`: generic toast when the server sent no toast of its own. */
export function onResponseError(event: Event): void {
  const xhr = (event as CustomEvent<ResponseDetail>).detail.xhr;
  logger.warn('htmx response error', { status: xhr?.status });
  if (!carriesToast(xhr)) showToast(statusMessage(xhr?.status ?? 0));
}

/** `htmx:sendError` / `htmx:timeout`: the request got no response at all. */
export function onNoResponse(): void {
  showToast('サーバーに接続できませんでした。');
}

/** Close button on a toast. */
export function onClick(event: Event): void {
  const target = event.target;
  if (!(target instanceof Element)) return;
  target.closest('[data-toast-dismiss]')?.closest('[data-toast]')?.remove();
}

function scheduleDismissals(records: MutationRecord[]): void {
  for (const record of records) {
    for (const node of record.addedNodes) {
      if (node instanceof HTMLElement && node.hasAttribute(TOAST_MARKER)) {
        setTimeout(() => node.remove(), DISMISS_AFTER_MS);
      }
    }
  }
}

export function init(): void {
  document.addEventListener('htmx:beforeSwap', onBeforeSwap);
  document.addEventListener('htmx:responseError', onResponseError);
  document.addEventListener('htmx:sendError', onNoResponse);
  document.addEventListener('htmx:timeout', onNoResponse);
  document.addEventListener('click', onClick);
  const target = region();
  if (target) new MutationObserver(scheduleDismissals).observe(target, { childList: true });
}

init();
