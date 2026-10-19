import { afterEach, beforeEach, describe, expect, test } from 'bun:test';
import './pitha-copy-button';
import { onClick, onClose } from './pitha-copy-button';

let written: string[];

beforeEach(() => {
  written = [];
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: { writeText: async (text: string) => void written.push(text) },
  });
});
afterEach(() => {
  document.body.innerHTML = '';
});

function click(button: HTMLElement): Promise<void> {
  return onClick({ target: button } as unknown as Event);
}

describe('copy', () => {
  test('an input source copies its value, not its text content', async () => {
    document.body.innerHTML = `
      <input id="key" value="pitha-mcp-secret" readonly>
      <button type="button" data-copy-from="key" id="btn">キーをコピー</button>`;
    const button = document.getElementById('btn') as HTMLElement;
    await click(button);
    expect(written).toEqual(['pitha-mcp-secret']);
    expect(button.textContent).toBe('コピーしました');
  });

  test('a plain element source copies its text content', async () => {
    document.body.innerHTML = `
      <code id="url">http://127.0.0.1:48110/</code>
      <button type="button" data-copy-from="url" id="btn">コピー</button>`;
    await click(document.getElementById('btn') as HTMLElement);
    expect(written).toEqual(['http://127.0.0.1:48110/']);
  });

  test('a rejected clipboard write is reported on the button, not thrown', async () => {
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: async () => {
          throw new Error('denied');
        },
      },
    });
    document.body.innerHTML = `
      <code id="url">x</code>
      <button type="button" data-copy-from="url" id="btn">コピー</button>`;
    const button = document.getElementById('btn') as HTMLElement;
    await click(button);
    expect(button.textContent).toBe('コピーできませんでした');
  });

  test('a missing source and unrelated clicks copy nothing', async () => {
    document.body.innerHTML = `
      <button type="button" data-copy-from="nope" id="btn">コピー</button>
      <button type="button" id="other">他</button>`;
    await click(document.getElementById('btn') as HTMLElement);
    await click(document.getElementById('other') as HTMLElement);
    expect(written).toEqual([]);
  });
});

describe('clear on close', () => {
  test('closing the dialog removes the one-time secret block only', () => {
    document.body.innerHTML = `
      <dialog id="d"><div data-clear-on-close id="reveal"><input value="k"></div><p id="keep">x</p></dialog>`;
    const dialog = document.getElementById('d') as HTMLDialogElement;
    onClose({ target: dialog } as unknown as Event);
    expect(document.getElementById('reveal')).toBeNull();
    expect(document.getElementById('keep')).not.toBeNull();
  });

  test('a close event from a non-dialog is ignored', () => {
    document.body.innerHTML = `<div id="d"><div data-clear-on-close id="reveal"></div></div>`;
    onClose({ target: document.getElementById('d') } as unknown as Event);
    expect(document.getElementById('reveal')).not.toBeNull();
  });
});
