import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { askConfirm, onHtmxConfirm } from './pitha-confirm';

// happy-dom has no top layer: showModal()/close() are stubbed like pitha-modal.test.ts.
function render(): HTMLDialogElement {
  document.body.innerHTML = `
    <button id="opener">削除</button>
    <dialog id="confirm-dialog" data-modal>
      <p data-confirm-message></p>
      <button type="button" data-confirm-cancel>キャンセル</button>
      <button type="button" data-confirm-accept>実行する</button>
    </dialog>`;
  const dialog = document.getElementById('confirm-dialog') as HTMLDialogElement;
  dialog.showModal = () => dialog.setAttribute('open', '');
  dialog.close = () => {
    dialog.removeAttribute('open');
    dialog.dispatchEvent(new Event('close'));
  };
  return dialog;
}

const click = (selector: string) => document.querySelector<HTMLElement>(selector)?.click();

beforeEach(() => {
  document.body.innerHTML = '';
});

describe('askConfirm', () => {
  test('shows the question and resolves true only for 「実行する」', async () => {
    const dialog = render();
    const answer = askConfirm('このポジションを決済しますか？');
    expect(dialog.hasAttribute('open')).toBe(true);
    expect(dialog.querySelector('[data-confirm-message]')?.textContent).toBe(
      'このポジションを決済しますか？',
    );
    click('[data-confirm-accept]');
    expect(await answer).toBe(true);
    expect(dialog.hasAttribute('open')).toBe(false);
  });

  test('「キャンセル」 and a close from outside (Esc / backdrop) resolve false', async () => {
    const dialog = render();
    const cancelled = askConfirm('q');
    click('[data-confirm-cancel]');
    expect(await cancelled).toBe(false);

    const escaped = askConfirm('q');
    dialog.close();
    expect(await escaped).toBe(false);
  });

  test('focus returns to the element that asked', async () => {
    render();
    const opener = document.getElementById('opener') as HTMLButtonElement;
    opener.focus();
    const answer = askConfirm('q');
    click('[data-confirm-cancel]');
    await answer;
    expect(document.activeElement).toBe(opener);
  });

  test('without the dialog in the page it refuses (never runs an unconfirmed action)', async () => {
    expect(await askConfirm('q')).toBe(false);
  });

  test('a second question while one is open is refused', async () => {
    render();
    const first = askConfirm('a');
    expect(await askConfirm('b')).toBe(false);
    click('[data-confirm-cancel]');
    await first;
  });
});

describe('htmx:confirm', () => {
  const event = (question: string | undefined, issueRequest: (skip: boolean) => void) =>
    new CustomEvent('htmx:confirm', { detail: { question, issueRequest }, cancelable: true });

  test('a request with hx-confirm is held, then issued without htmx confirming again once accepted', async () => {
    render();
    const issue = mock((_skip: boolean) => {});
    const e = event('削除しますか？', issue);
    onHtmxConfirm(e);
    expect(e.defaultPrevented).toBe(true);
    expect(issue).not.toHaveBeenCalled();
    click('[data-confirm-accept]');
    await Promise.resolve();
    await Promise.resolve();
    expect(issue).toHaveBeenCalledWith(true);
  });

  test('a cancelled confirmation never issues the request', async () => {
    render();
    const issue = mock((_skip: boolean) => {});
    onHtmxConfirm(event('削除しますか？', issue));
    click('[data-confirm-cancel]');
    await Promise.resolve();
    await Promise.resolve();
    expect(issue).not.toHaveBeenCalled();
  });

  test('a request without a question is left alone', () => {
    render();
    const e = event(undefined, () => {});
    onHtmxConfirm(e);
    expect(e.defaultPrevented).toBe(false);
  });
});
