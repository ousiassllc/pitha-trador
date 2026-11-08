// Toast lifecycle for pitha-htmx-errors (docs/components/overview.md §4, §8):
// how a toast that landed in a toast region appears (320ms rise), is raised
// above any modal dialog, auto-dismisses and fades out. The message / region
// choice and the HTMX error handling stay in pitha-htmx-errors.ts.

import {
  EASE_IN,
  EASE_OUT,
  MOTION_FADE_IN_MS,
  MOTION_FADE_OUT_MS,
  prefersReducedMotion,
  REDUCED_FADE_MS,
} from '../lib/motion';

export const TOAST_MARKER = 'data-toast';
export const TOAST_RELOAD = '[data-toast-reload]';
// --toast-duration (design guide O3): 3s, or 5s for a toast carrying an action (the reload button).
const DISMISS_AFTER_MS = 3000;
const DISMISS_WITH_ACTION_MS = 5000;

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
