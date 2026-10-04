// Opens/closes the molecules.Modal `<dialog>`s of the Settings and Setup
// screens (issue #302; docs/components/overview.md "Modal"). The dialog is
// opened with `showModal()`, so the browser supplies the focus containment
// (Tab cycles inside, the page behind is inert), the top layer and Esc to
// close; this module only adds the wiring a bare `<dialog>` lacks:
//   - a `[data-modal-open="<id>"]` button opens that dialog, and focus
//     returns to it when the dialog closes;
//   - a `[data-modal-close]` button and a click on the backdrop (a click
//     whose target is the `<dialog>` itself - the dialog has no padding) close it;
//   - a URL hash that points at, or inside, a dialog opens it - the header's
//     version link `/settings#update-panel` lands in the アップデート modal.
//     Closing such a dialog drops the hash so the same link works again.
// The dialog content is ordinary HTMX markup (SecretFieldRow forms,
// UpdatePanel), so HTMX swaps inside an open dialog need no help from here.

const opened = new WeakMap<HTMLDialogElement, HTMLElement | null>();

function dialogById(id: string | null | undefined): HTMLDialogElement | null {
  const el = id ? document.getElementById(id) : null;
  return el instanceof HTMLDialogElement ? el : null;
}

/** The decoded URL hash without `#`; `''` when it is empty or not valid percent-encoding (a `URIError` must not escape a listener). */
function currentHash(): string {
  try {
    return decodeURIComponent(location.hash.slice(1));
  } catch {
    return '';
  }
}

/** Opens `dialog` modally, remembering `invoker` so focus can return to it. */
export function openModal(dialog: HTMLDialogElement, invoker: HTMLElement | null = null): void {
  if (dialog.open) return;
  opened.set(dialog, invoker);
  dialog.showModal();
}

/** `click`: open/close buttons and the backdrop. */
export function onClick(event: Event): void {
  const target = event.target;
  if (!(target instanceof Element)) return;

  const opener = target.closest<HTMLElement>('[data-modal-open]');
  if (opener) {
    const dialog = dialogById(opener.dataset.modalOpen);
    if (dialog) openModal(dialog, opener);
    return;
  }
  if (target.closest('[data-modal-close]')) {
    target.closest<HTMLDialogElement>('dialog[data-modal]')?.close();
    return;
  }
  if (target instanceof HTMLDialogElement && target.hasAttribute('data-modal')) target.close();
}

/** `close` does not bubble, so it is captured at the document: restore focus, drop a hash that pointed inside. */
export function onClose(event: Event): void {
  const dialog = event.target;
  if (!(dialog instanceof HTMLDialogElement) || !dialog.hasAttribute('data-modal')) return;
  opened.get(dialog)?.focus();
  opened.delete(dialog);
  const hash = currentHash();
  if (hash && dialog.querySelector(`[id="${CSS.escape(hash)}"]`)) {
    history.replaceState(null, '', location.pathname + location.search);
  }
}

/** Opens the dialog containing the element the URL hash names. */
export function openFromHash(): void {
  const hash = currentHash();
  if (!hash) return;
  const target = document.getElementById(hash);
  const dialog = target?.closest<HTMLDialogElement>('dialog[data-modal]');
  if (!dialog) return;
  openModal(dialog);
  target?.scrollIntoView?.();
}

export function init(): void {
  document.addEventListener('click', onClick);
  document.addEventListener('close', onClose, true);
  window.addEventListener('hashchange', openFromHash);
  openFromHash();
}

init();
