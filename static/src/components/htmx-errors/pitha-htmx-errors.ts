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
//   - polled live banners (`[data-live-banner]`, Header's `#attention-badge` and
//     the Home screen's 取引の状態 card, issues #626 / #896): the container is the fixed
//     `role="status"` live region; a poll answering the markup already shown
//     is not swapped, so a screen reader announces a banner only when it
//     appears or changes, not on every poll;
//   - focus after a swap that removed the clicked button (issue #676): htmx
//     restores focus only to a same-`id` element, so a button that carries
//     `data-focus-after-swap="<id>"` hands focus to that element when the
//     swap left it on `<body>` (`htmx:afterSwap`).
// The toast markup lives only in templ (`#toast-template`, atoms.Toast).

import { CSRF_REJECT_HEADER, CSRF_REJECT_STALE } from '../lib/api';
import { logger } from '../lib/logger';
import {
  EASE_IN,
  EASE_OUT,
  MOTION_FADE_IN_MS,
  MOTION_FADE_OUT_MS,
  prefersReducedMotion,
  REDUCED_FADE_MS,
} from '../lib/motion';

const TOAST_MARKER = 'data-toast';
// A form-validation error comes back as an atoms.InlineAlert for the form's own slot (HX-Retarget), not a toast.
const INLINE_ALERT_MARKER = 'data-inline-alert';
// --toast-duration (design guide O3): 3s, or 5s for a toast carrying an action (the reload button).
const DISMISS_AFTER_MS = 3000;
const DISMISS_WITH_ACTION_MS = 5000;
const TOAST_RELOAD = '[data-toast-reload]';
// A one-line fact (design guide O3); the toast's reload button says what to do.
const STALE_SESSION_TOAST = 'ページの有効期限が切れました。';
const REGIONS = '#toast-region, [data-toast-region]';

const LIVE_BANNER_LAST = 'liveBannerLast';

const FOCUS_AFTER_SWAP = 'data-focus-after-swap';

interface ResponseDetail {
  xhr?: XMLHttpRequest;
  serverResponse?: string;
  shouldSwap?: boolean;
  target?: Element;
  swapOverride?: string;
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

/** Fades out the toasts in every region except `target` (one toast on screen at a time, design guide O3): a toast left in `#toast-region` before a modal opened must not sit beside the modal's own. */
function dismissToastsOutside(target: Element): void {
  for (const region of document.querySelectorAll<HTMLElement>(REGIONS)) {
    if (region === target) continue;
    for (const toast of region.querySelectorAll<HTMLElement>(`[${TOAST_MARKER}]`))
      dismissToast(toast);
  }
}

function carriesToast(xhr: XMLHttpRequest | undefined): boolean {
  return xhr?.responseText?.includes(TOAST_MARKER) ?? false;
}

function carriesInlineAlert(xhr: XMLHttpRequest | undefined): boolean {
  return xhr?.responseText?.includes(INLINE_ALERT_MARKER) ?? false;
}

function isErrorStatus(xhr: XMLHttpRequest | undefined): boolean {
  return (xhr?.status ?? 0) >= 400;
}

/** Shows a toast cloned from `#toast-template`, replacing any other (one at a time, design guide O3); the message already on screen is not shown again (polling failures). `reload` shows the toast's "再読み込み" action. */
export function showToast(message: string, { reload = false } = {}): void {
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
  toast.querySelector<HTMLElement>(TOAST_RELOAD)?.toggleAttribute('hidden', !reload);
  dismissToastsOutside(target);
  target.replaceChildren(toast);
}

/** One short line (design guide O3); the HTTP status goes to the log in `onResponseError`, not the toast. */
function statusMessage(status: number): string {
  if (status >= 500) return 'サーバーでエラーが発生しました。';
  return '操作を完了できませんでした。';
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

/**
 * An inline alert already in the slot fades out (240ms ease-in; <=100ms under reduced motion) before
 * the new response replaces it (design guide §4/§5). htmx holds the swap for the `swap:` delay and
 * marks the slot `htmx-swapping`, which app.css turns into the alert's fade-out. An empty slot is
 * swapped at once, so a first error is not delayed.
 */
function holdSwapForAlertFadeOut(detail: ResponseDetail): void {
  if (!detail.target?.firstElementChild?.hasAttribute(INLINE_ALERT_MARKER)) return;
  const ms = prefersReducedMotion() ? REDUCED_FADE_MS : MOTION_FADE_OUT_MS;
  detail.swapOverride = `innerHTML swap:${ms}ms`;
}

/** `htmx:beforeSwap`: an error response without a toast fragment is never swapped into `#toast-region`; one with a toast lands in the open modal's region instead (issue #353). */
export function onBeforeSwap(event: Event): void {
  const detail = (event as CustomEvent<ResponseDetail>).detail;
  skipUnchangedLiveBanner(detail);
  if (!isErrorStatus(detail.xhr)) return;
  // An inline alert is swapped where the response's HX-Retarget says (the form's error slot).
  if (carriesInlineAlert(detail.xhr)) {
    holdSwapForAlertFadeOut(detail);
    return;
  }
  if (!carriesToast(detail.xhr)) {
    detail.shouldSwap = false;
    return;
  }
  // htmx's `responseHandling` targets `#toast-region`, which an open modal dialog makes inert.
  const inDialog = dialogRegion();
  if (inDialog) {
    detail.target = inDialog;
    dismissToastsOutside(inDialog);
  }
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
    showToast(STALE_SESSION_TOAST, { reload: true });
    return;
  }
  if (!carriesToast(xhr) && !carriesInlineAlert(xhr)) showToast(statusMessage(xhr?.status ?? 0));
}

/** `htmx:sendError` / `htmx:timeout`: the request got no response at all. */
export function onNoResponse(): void {
  showToast('サーバーに接続できませんでした。');
}

/** Close button on a toast. */
export function onClick(event: Event): void {
  const target = event.target;
  if (!(target instanceof Element)) return;
  if (target.closest(TOAST_RELOAD)) {
    location.reload();
    return;
  }
  const toast = target.closest('[data-toast-dismiss]')?.closest<HTMLElement>('[data-toast]');
  if (toast) dismissToast(toast);
}

/** Re-shows the popover so it sits above any modal dialog opened since it was last shown. No-op without Popover API support, and for a region inside a dialog (already above the page). */
function raiseToastRegion(target: HTMLElement): void {
  if (typeof target.showPopover !== 'function' || target.closest('dialog')) return;
  if (target.matches(':popover-open')) target.hidePopover();
  target.showPopover();
}

/** Toast appearance (design guide §4/§5): opacity plus a small (8px) rise over 320ms ease-out; under reduced motion only a <=100ms fade. A no-op without the Web Animations API. */
function animateToastIn(toast: HTMLElement): void {
  if (typeof toast.animate !== 'function') return;
  if (prefersReducedMotion()) {
    toast.animate([{ opacity: 0 }, { opacity: 1 }], { duration: REDUCED_FADE_MS });
    return;
  }
  toast.animate(
    [
      { opacity: 0, transform: 'translateY(8px)' },
      { opacity: 1, transform: 'translateY(0)' },
    ],
    { duration: MOTION_FADE_IN_MS, easing: EASE_OUT },
  );
}

/** Removes `toast` after a fade-out (240ms ease-in; <=100ms under reduced motion). Without the Web Animations API it is removed at once. */
export function dismissToast(toast: HTMLElement): void {
  if (typeof toast.animate !== 'function' || !toast.isConnected) {
    toast.remove();
    return;
  }
  toast.removeAttribute(TOAST_MARKER);
  toast.setAttribute('aria-hidden', 'true');
  toast.animate([{ opacity: 1 }, { opacity: 0 }], {
    duration: prefersReducedMotion() ? REDUCED_FADE_MS : MOTION_FADE_OUT_MS,
    easing: EASE_IN,
    fill: 'forwards',
  }).onfinish = () => toast.remove();
}

function dismissAfterMs(toast: HTMLElement): number {
  return toast.querySelector(`${TOAST_RELOAD}:not([hidden])`)
    ? DISMISS_WITH_ACTION_MS
    : DISMISS_AFTER_MS;
}

/** Auto-dismisses `toast` after DISMISS_AFTER_MS (DISMISS_WITH_ACTION_MS with a visible action), but never while the pointer is over it or focus is inside it (WCAG 2.2.1): the countdown restarts once both are gone. */
function scheduleDismiss(toast: HTMLElement): void {
  let timer: ReturnType<typeof setTimeout> | undefined;
  let hovered = false;
  let focused = false;
  const arm = () => {
    clearTimeout(timer);
    if (!hovered && !focused) timer = setTimeout(() => dismissToast(toast), dismissAfterMs(toast));
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

/** Toast-landed hook (both `showToast` and htmx's `innerHTML` swap into `#toast-region`): raise the region and schedule the auto-dismiss. */
function onToastsAdded(target: HTMLElement, records: MutationRecord[]): void {
  let added = false;
  for (const record of records) {
    for (const node of record.addedNodes) {
      if (node instanceof HTMLElement && node.hasAttribute(TOAST_MARKER)) {
        added = true;
        animateToastIn(node);
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
