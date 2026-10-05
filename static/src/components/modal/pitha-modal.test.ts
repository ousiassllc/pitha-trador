import { afterEach, beforeEach, describe, expect, spyOn, test } from 'bun:test';
import './pitha-modal';
import { onClose, openFromHash } from './pitha-modal';

// happy-dom has no top layer, but it implements <dialog>'s open state, so
// showModal()/close() are stubbed only where the DOM lacks them.
function stubDialog(dialog: HTMLDialogElement): void {
  dialog.showModal = () => dialog.setAttribute('open', '');
  dialog.close = () => {
    dialog.removeAttribute('open');
    dialog.dispatchEvent(new Event('close'));
  };
}

function render(): { opener: HTMLButtonElement; dialog: HTMLDialogElement } {
  document.body.innerHTML = `
    <button type="button" data-modal-open="modal-a" id="opener">開く</button>
    <dialog id="modal-a" data-modal>
      <button type="button" data-modal-close id="closer">閉じる</button>
      <div id="inside"><input id="field"></div>
    </dialog>
    <dialog id="modal-b" data-modal></dialog>`;
  for (const d of document.querySelectorAll('dialog')) stubDialog(d);
  return {
    opener: document.getElementById('opener') as HTMLButtonElement,
    dialog: document.getElementById('modal-a') as HTMLDialogElement,
  };
}

beforeEach(() => {
  location.hash = '';
});
afterEach(() => {
  location.hash = '';
});

describe('open / close', () => {
  test('the opener button opens only its own dialog', () => {
    const { opener, dialog } = render();
    opener.click();
    expect(dialog.open).toBe(true);
    expect((document.getElementById('modal-b') as HTMLDialogElement).open).toBe(false);
  });

  test('the close button closes and focus returns to the opener', () => {
    const { opener, dialog } = render();
    opener.click();
    (document.getElementById('closer') as HTMLElement).click();
    expect(dialog.open).toBe(false);
    expect(document.activeElement).toBe(opener);
  });

  test('a click on the backdrop (the dialog itself) closes; a click inside does not', () => {
    const { opener, dialog } = render();
    opener.click();
    (document.getElementById('inside') as HTMLElement).click();
    expect(dialog.open).toBe(true);
    dialog.click();
    expect(dialog.open).toBe(false);
  });

  test('a missing target dialog is ignored', () => {
    document.body.innerHTML = '<button data-modal-open="nope" id="x"></button>';
    expect(() => (document.getElementById('x') as HTMLElement).click()).not.toThrow();
  });
});

describe('URL hash', () => {
  test('a hash inside a dialog opens it, closing drops the hash', () => {
    const { dialog } = render();
    location.hash = '#inside';
    openFromHash();
    expect(dialog.open).toBe(true);
    dialog.close();
    expect(location.hash).toBe('');
  });

  test("a hash naming the dialog's own id opens it, closing drops the hash", () => {
    const { dialog } = render();
    location.hash = '#modal-a';
    openFromHash();
    expect(dialog.open).toBe(true);
    dialog.close();
    expect(location.hash).toBe('');
  });

  test("closing a dialog leaves a hash that names another dialog's id alone", () => {
    const { dialog } = render();
    location.hash = '#modal-b';
    dialog.close();
    expect(location.hash).toBe('#modal-b');
  });

  test('a hash that names no dialog content is left alone', () => {
    const { dialog } = render();
    location.hash = '#elsewhere';
    const spy = spyOn(dialog, 'showModal');
    openFromHash();
    expect(spy).not.toHaveBeenCalled();
  });

  test('an encoded hash id opens its dialog', () => {
    document.body.innerHTML = '<dialog id="modal-a" data-modal><div id="a b"></div></dialog>';
    const dialog = document.getElementById('modal-a') as HTMLDialogElement;
    stubDialog(dialog);
    location.hash = '#a%20b';
    openFromHash();
    expect(dialog.open).toBe(true);
  });

  for (const bad of ['#100%', '#%E0%A4%A']) {
    test(`a malformed percent-encoded hash (${bad}) opens nothing and does not throw`, () => {
      const { dialog } = render();
      location.hash = bad;
      expect(() => openFromHash()).not.toThrow();
      expect(dialog.open).toBe(false);
      expect((document.getElementById('modal-b') as HTMLDialogElement).open).toBe(false);
    });

    test(`closing a dialog under a malformed hash (${bad}) does not throw and keeps the hash`, () => {
      const { dialog } = render();
      location.hash = bad;
      expect(() => onClose({ target: dialog } as unknown as Event)).not.toThrow();
      expect(location.hash).toBe(bad);
    });
  }
});
