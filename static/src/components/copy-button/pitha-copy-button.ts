// Copy buttons and one-time secrets of the Settings screen's MCP panel
// (issue #792; docs/components/overview.md "McpPanel"). CSP forbids inline
// JS, so the markup is wired here by attribute:
//   - a `[data-copy-from="<id>"]` button copies the text of the element with
//     that id to the clipboard (an <input>/<textarea> contributes its value)
//     and briefly labels itself 「コピーしました」;
//   - an element with `[data-clear-on-close]` (the freshly generated API key's
//     reveal block) is removed when the dialog containing it closes, so the
//     plaintext key does not linger in the DOM of a page left open.

const COPIED_LABEL = 'コピーしました';
const FAILED_LABEL = 'コピーできませんでした';
const RESET_MS = 2000;

const labels = new WeakMap<HTMLElement, { original: string; timer: number }>();

function sourceText(id: string | undefined): string | null {
  const el = id ? document.getElementById(id) : null;
  if (!el) return null;
  return el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement
    ? el.value
    : (el.textContent ?? '');
}

/** Shows `label` on `button` for a moment, then restores the original text. */
function flash(button: HTMLElement, label: string): void {
  const prev = labels.get(button);
  if (prev) window.clearTimeout(prev.timer);
  const original = prev?.original ?? button.textContent ?? '';
  button.textContent = label;
  const timer = window.setTimeout(() => {
    button.textContent = original;
    labels.delete(button);
  }, RESET_MS);
  labels.set(button, { original, timer });
}

/** `click`: copy the text a `[data-copy-from]` button points at. */
export async function onClick(event: Event): Promise<void> {
  const target = event.target;
  if (!(target instanceof Element)) return;
  const button = target.closest<HTMLElement>('[data-copy-from]');
  if (!button) return;
  const text = sourceText(button.dataset.copyFrom);
  if (text === null) return;
  try {
    await navigator.clipboard.writeText(text);
    flash(button, COPIED_LABEL);
  } catch {
    flash(button, FAILED_LABEL);
  }
}

/** `close` (captured; it does not bubble): drop one-time secrets shown in the closed dialog. */
export function onClose(event: Event): void {
  const dialog = event.target;
  if (!(dialog instanceof HTMLDialogElement)) return;
  for (const el of dialog.querySelectorAll('[data-clear-on-close]')) el.remove();
}

export function init(): void {
  document.addEventListener('click', (event) => void onClick(event));
  document.addEventListener('close', onClose, true);
}

init();
