// Confirmation dialog for destructive and irreversible actions (design guide
// §4 O2: confirmations are a centered modal, never the browser's native
// `confirm()`). `molecules.ConfirmDialog` (rendered once by layout.document as
// `#confirm-dialog`) is the markup; this module fills in the question and
// resolves the answer:
//   - `askConfirm(message)` opens it with `showModal()` (focus containment, Esc,
//     the backdrop click that molecules.Modal's pitha-modal also handles) and
//     resolves true only for 「実行する」; 「キャンセル」, Esc and a backdrop click
//     resolve false. Focus starts on 「キャンセル」 (the safe choice) and returns
//     to the element that asked.
//   - `htmx:confirm` (fired by htmx before every request) with a question - an
//     `hx-confirm` attribute - is taken over: the request is held, asked here,
//     and issued only when the user accepts. The `hx-confirm` attributes in the
//     templates stay the single place a confirmation is declared.
// The Lit components (pitha-kill-switch-panel) call `askConfirm` directly; the
// dialog lives in the document, so a shadow root does not matter.

import { logger } from '../lib/logger';

const DIALOG_ID = 'confirm-dialog';

interface ConfirmDetail {
  question?: string;
  issueRequest: (skipConfirmation: boolean) => void;
}

/** Asks `message` in the confirm dialog; resolves true only when the user accepts. */
export function askConfirm(message: string): Promise<boolean> {
  const dialog = document.getElementById(DIALOG_ID);
  if (!(dialog instanceof HTMLDialogElement) || typeof dialog.showModal !== 'function') {
    // Never run a destructive action nobody confirmed.
    logger.error('confirm dialog missing', { message });
    return Promise.resolve(false);
  }
  if (dialog.open) return Promise.resolve(false);
  const text = dialog.querySelector('[data-confirm-message]');
  if (text) text.textContent = message;
  const invoker = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  return new Promise((resolve) => {
    let accepted = false;
    const onClick = (event: Event) => {
      const target = event.target;
      if (!(target instanceof Element)) return;
      if (target.closest('[data-confirm-accept]')) {
        accepted = true;
        dialog.close();
      } else if (target.closest('[data-confirm-cancel]')) {
        dialog.close();
      }
    };
    dialog.addEventListener('click', onClick);
    dialog.addEventListener(
      'close',
      () => {
        dialog.removeEventListener('click', onClick);
        if (invoker?.isConnected) invoker.focus();
        resolve(accepted);
      },
      { once: true },
    );
    dialog.showModal();
    dialog.querySelector<HTMLElement>('[data-confirm-cancel]')?.focus();
  });
}

/** `htmx:confirm`: hold a request that has an `hx-confirm` question until the dialog accepts it. */
export function onHtmxConfirm(event: Event): void {
  const detail = (event as CustomEvent<ConfirmDetail>).detail;
  if (!detail?.question) return;
  event.preventDefault();
  void askConfirm(detail.question).then((accepted) => {
    if (accepted) detail.issueRequest(true);
  });
}

export function init(): void {
  document.addEventListener('htmx:confirm', onHtmxConfirm);
}

init();
