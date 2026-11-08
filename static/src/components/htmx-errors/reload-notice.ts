// Persistent "reload the page" notice for pitha-htmx-errors (docs/components/overview.md §4,
// PR #1044 M5). The stale-session toast (403 + `X-CSRF-Reject: stale`) auto-dismisses after 5s
// (design guide O3) and is the only way to tell the user that a page reload is the next step.
// This notice keeps that warning and the 「再読み込み」 action on screen after the toast is gone,
// where the toast cannot be: it never fades, so a keyboard user has no time limit.
//
//   - one notice per document, kept in the area the user can operate: the open modal
//     `<dialog>`'s `[data-reload-notice-region]` (a modal makes the rest of the page inert),
//     else the page's `#reload-notice-region`. It follows dialogs opening and closing, so it is
//     never left inside a closed dialog;
//   - a recurrence while it is shown adds nothing (no second notice); moving between areas replays
//     only its 320ms fade-in;
//   - it is removed only when the problem is gone: a page load (the new document has none), or a
//     successful state-changing request, which proves the session cookie/CSRF token work again.
//     Polling GETs do not count: the CSRF check does not cover them;
//   - it never takes focus and has no `role` / `aria-live`: the toast beside it is the
//     announcement (`role="alert"`), so a second live region would read the same fact twice;
//   - reloading is the user's click on the button only; nothing reloads by itself.
// The markup lives only in templ (`#reload-notice-template`, atoms.ReloadNotice).

import { logger } from '../lib/logger';

export const RELOAD_NOTICE_RELOAD = '[data-reload-notice-reload]';
const NOTICE = '[data-reload-notice]';
const NOTICE_MESSAGE = '[data-reload-notice-message]';
const DIALOG_SLOT = 'dialog[open] [data-reload-notice-region]';
const PAGE_SLOT = '#reload-notice-region';

/** Where the notice must sit to stay operable: the topmost open modal, else the page. */
function slot(): HTMLElement | null {
  const inDialogs = document.querySelectorAll<HTMLElement>(DIALOG_SLOT);
  return inDialogs.item(inDialogs.length - 1) ?? document.querySelector<HTMLElement>(PAGE_SLOT);
}

/** Shows the notice with `message`, or just keeps the existing one where it belongs. */
export function showReloadNotice(message: string): void {
  const target = slot();
  const template = document.getElementById('reload-notice-template');
  if (!target || !(template instanceof HTMLTemplateElement)) {
    logger.error('reload notice region/template missing', { message });
    return;
  }
  let notice = document.querySelector<HTMLElement>(NOTICE);
  if (!notice) {
    const clone = template.content.firstElementChild?.cloneNode(true);
    if (!(clone instanceof HTMLElement)) return;
    notice = clone;
  }
  const text = notice.querySelector(NOTICE_MESSAGE);
  if (text && text.textContent !== message) text.textContent = message;
  if (notice.parentElement !== target) target.append(notice);
}

/** The problem is gone: remove the notice wherever it is. */
export function clearReloadNotice(): void {
  document.querySelector(NOTICE)?.remove();
}

/** Re-homes the notice after a dialog opened or closed. */
function followDialogs(): void {
  const notice = document.querySelector<HTMLElement>(NOTICE);
  const target = slot();
  if (notice && target && notice.parentElement !== target) target.append(notice);
}

/** `htmx:afterRequest`: a successful non-GET request means the stale session is resolved. */
export function onAfterRequest(event: Event): void {
  const detail = (event as CustomEvent<{ successful?: boolean; requestConfig?: { verb?: string } }>)
    .detail;
  const verb = detail?.requestConfig?.verb?.toLowerCase();
  if (detail?.successful && verb && verb !== 'get') clearReloadNotice();
}

/** Keeps the notice in the right area as dialogs open and close (`open` is toggled by `showModal()` / `close()` / Esc alike). */
export function watchDialogs(): void {
  new MutationObserver(followDialogs).observe(document.documentElement, {
    attributes: true,
    attributeFilter: ['open'],
    subtree: true,
  });
}
