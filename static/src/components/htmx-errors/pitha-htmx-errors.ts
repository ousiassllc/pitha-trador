// Global HTMX failure feedback (docs/components/overview.md §4 "エラー表示",
// §8). htmx never swaps 4xx/5xx by default; layout.Shell's `htmx-config`
// sends the atoms.Toast fragment an action handler returns into
// `#toast-region`. This module covers what that config cannot:
//   - error responses that carry no toast fragment (empty body, a plain
//     text/proxy error page) must not be swapped in raw, and get a generic
//     toast instead (`htmx:beforeSwap` / `htmx:responseError`);
//   - a 403 marked stale by the server (page opened before an app restart)
//     gets a "reload the page" toast;
//   - requests that never got a response (`htmx:sendError`, `htmx:timeout`);
//   - dismissing toasts (close button, auto-dismiss that pauses while hovered/focused);
//   - keeping toasts operable above a modal `<dialog>`: `showModal()` puts the
//     dialog in the top layer, which no z-index can beat, and makes everything
//     outside it inert. `#toast-region` is a `popover="manual"` re-shown (moved
//     to the top of the top layer) whenever a toast lands in it (issue #321), but
//     being outside the dialog it stays inert, so while a modal is open toasts go
//     to the `[data-toast-region]` inside that dialog instead, where the close
//     button and text selection work (issue #353).
//   - polled live banners (`[data-live-banner]`, Header's `#update-banner` /
//     `#marketdata-banner`, issue #626): the container is the fixed
//     `role="status"` live region; a poll answering the markup already shown
//     is not swapped, so a screen reader announces a banner only when it
//     appears or changes, not on every poll;
//   - focus after a swap that removed the clicked button (issue #676): htmx
//     restores focus only to a same-`id` element, so a button that carries
//     `data-focus-after-swap="<id>"` hands focus to that element when the
//     swap left it on `<body>` (`htmx:afterSwap`).
// The toast markup lives only in templ (`#toast-template`, atoms.Toast).

import { CSRF_REJECT_HEADER, CSRF_REJECT_STALE, STALE_SESSION_MESSAGE } from '../lib/api';
import { logger } from '../lib/logger';

const TOAST_MARKER = 'data-toast';
const DISMISS_AFTER_MS = 8000;
const REGIONS = '#toast-region, [data-toast-region]';

const LIVE_BANNER_LAST = 'liveBannerLast';

const FOCUS_AFTER_SWAP = 'data-focus-after-swap';

interface ResponseDetail {
  xhr?: XMLHttpRequest;
  serverResponse?: string;
  shouldSwap?: boolean;
  target?: Element;
}

/** The toast region inside the topmost open modal `<dialog>`, if any (`molecules.Modal` renders one). */
function dialogRegion(): HTMLElement | null {
  const regions = document.querySelectorAll<HTMLElement>('dialog[open] [data-toast-region]');
  return regions.item(regions.length - 1);
}

/** Where a toast must land to stay operable: an open modal blocks everything outside it, `#toast-region` included. */
function region(): HTMLElement | null {
  return dialogRegion() ?? document.getElementById('toast-region');
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

/** Cancels a swap whose body equals the previous one for a `[data-live-banner]` target (issue #626). */
function skipUnchangedLiveBanner(detail: ResponseDetail): void {
  const target = detail.target;
  if (!(target instanceof HTMLElement) || !target.hasAttribute('data-live-banner')) return;
  if (isErrorStatus(detail.xhr) || detail.shouldSwap === false) return;
  const body = detail.serverResponse ?? '';
  if (target.dataset[LIVE_BANNER_LAST] === body) {
    detail.shouldSwap = false;
    return;
  }
  target.dataset[LIVE_BANNER_LAST] = body;
}

/** `htmx:beforeSwap`: an error response without a toast fragment is never swapped into `#toast-region`; one with a toast lands in the open modal's region instead (issue #353). */
export function onBeforeSwap(event: Event): void {
  const detail = (event as CustomEvent<ResponseDetail>).detail;
  skipUnchangedLiveBanner(detail);
  if (!isErrorStatus(detail.xhr)) return;
  if (!carriesToast(detail.xhr)) {
    detail.shouldSwap = false;
    return;
  }
  // htmx's `responseHandling` targets `#toast-region`, which an open modal dialog makes inert.
  const inDialog = dialogRegion();
  if (inDialog) detail.target = inDialog;
}

/**
 * `htmx:afterSwap`: htmx has already restored focus to a same-`id` element when it could; if the
 * clicked button is gone and focus fell to `<body>`, move it to the element named by the button's
 * `data-focus-after-swap` (an id; made programmatically focusable) so keyboard users keep their place (issue #676).
 */
export function onAfterSwap(event: Event): void {
  const trigger = (event as CustomEvent<{ requestConfig?: { elt?: Element } }>).detail
    ?.requestConfig?.elt;
  const targetId = trigger?.getAttribute(FOCUS_AFTER_SWAP);
  if (!targetId || trigger?.isConnected) return;
  const active = document.activeElement;
  if (active && active !== document.body) return;
  const target = document.getElementById(targetId);
  if (!target) return;
  if (!target.hasAttribute('tabindex')) target.setAttribute('tabindex', '-1');
  target.focus();
}

/** `htmx:responseError`: generic toast when the server sent no toast of its own. */
export function onResponseError(event: Event): void {
  const xhr = (event as CustomEvent<ResponseDetail>).detail.xhr;
  logger.warn('htmx response error', { status: xhr?.status });
  if (xhr?.status === 403 && xhr.getResponseHeader?.(CSRF_REJECT_HEADER) === CSRF_REJECT_STALE) {
    // Page opened before an app restart: its cookie/CSRF token is stale (issue #138).
    showToast(STALE_SESSION_MESSAGE);
    return;
  }
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

/** Re-shows the popover so it sits above any modal dialog opened since it was last shown. No-op without Popover API support, and for a region inside a dialog (already above the page). */
function raiseToastRegion(target: HTMLElement): void {
  if (typeof target.showPopover !== 'function' || target.closest('dialog')) return;
  if (target.matches(':popover-open')) target.hidePopover();
  target.showPopover();
}

/** Auto-dismisses `toast` after DISMISS_AFTER_MS, but never while the pointer is over it or focus is inside it (WCAG 2.2.1): the countdown restarts once both are gone. */
function scheduleDismiss(toast: HTMLElement): void {
  let timer: ReturnType<typeof setTimeout> | undefined;
  let hovered = false;
  let focused = false;
  const arm = () => {
    clearTimeout(timer);
    if (!hovered && !focused) timer = setTimeout(() => toast.remove(), DISMISS_AFTER_MS);
  };
  toast.addEventListener('mouseenter', () => {
    hovered = true;
    arm();
  });
  toast.addEventListener('mouseleave', () => {
    hovered = false;
    arm();
  });
  toast.addEventListener('focusin', () => {
    focused = true;
    arm();
  });
  toast.addEventListener('focusout', () => {
    focused = false;
    arm();
  });
  arm();
}

/** Toast-landed hook (both `showToast` and htmx's `beforeend` swap): raise the region and schedule the auto-dismiss. */
function onToastsAdded(target: HTMLElement, records: MutationRecord[]): void {
  let added = false;
  for (const record of records) {
    for (const node of record.addedNodes) {
      if (node instanceof HTMLElement && node.hasAttribute(TOAST_MARKER)) {
        added = true;
        scheduleDismiss(node);
      }
    }
  }
  if (added) raiseToastRegion(target);
}

/** Auto-dismisses and raises toasts as they land in `target` (script-added or htmx-swapped alike). */
export function watchToastRegion(target: HTMLElement): void {
  new MutationObserver((records) => onToastsAdded(target, records)).observe(target, {
    childList: true,
  });
}

export function init(): void {
  document.addEventListener('htmx:beforeSwap', onBeforeSwap);
  document.addEventListener('htmx:afterSwap', onAfterSwap);
  document.addEventListener('htmx:responseError', onResponseError);
  document.addEventListener('htmx:sendError', onNoResponse);
  document.addEventListener('htmx:timeout', onNoResponse);
  document.addEventListener('click', onClick);
  for (const target of document.querySelectorAll<HTMLElement>(REGIONS)) watchToastRegion(target);
}

init();
