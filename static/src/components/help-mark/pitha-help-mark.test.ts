import { beforeEach, describe, expect, test } from 'bun:test';
import './pitha-help-mark';

function render(): { button: HTMLButtonElement; text: HTMLElement; other: HTMLButtonElement } {
  document.body.innerHTML = `
    <span data-help-mark id="a">
      <button type="button" data-help-mark-button aria-describedby="tip-a" aria-label="Aの説明">?</button>
      <span id="tip-a" role="tooltip" data-help-mark-text>説明A</span>
    </span>
    <span data-help-mark id="b">
      <button type="button" data-help-mark-button aria-describedby="tip-b" aria-label="Bの説明">?</button>
      <span id="tip-b" role="tooltip" data-help-mark-text>説明B</span>
    </span>
    <p id="outside">外</p>`;
  const [button, other] = Array.from(document.querySelectorAll<HTMLButtonElement>('button'));
  return { button, other, text: document.getElementById('tip-a') as HTMLElement };
}

function pointer(type: string, target: Element, init: PointerEventInit = {}): void {
  target.dispatchEvent(new PointerEvent(type, { bubbles: true, pointerType: 'mouse', ...init }));
}

const isOpen = (el: HTMLElement): boolean => el.hasAttribute('data-open');

beforeEach(() => {
  document.body.innerHTML = '';
});

describe('help mark', () => {
  test('mouse hover opens and leaving closes', () => {
    const { button, text } = render();
    pointer('pointerover', button);
    expect(isOpen(text)).toBe(true);
    pointer('pointerout', button, { relatedTarget: document.getElementById('outside') });
    expect(isOpen(text)).toBe(false);
  });

  test('moving the pointer from the button onto the text keeps it open', () => {
    const { button, text } = render();
    pointer('pointerover', button);
    pointer('pointerout', button, { relatedTarget: text });
    expect(isOpen(text)).toBe(true);
  });

  test('keyboard focus opens and blur closes', () => {
    const { button, text } = render();
    button.dispatchEvent(new FocusEvent('focusin', { bubbles: true }));
    expect(isOpen(text)).toBe(true);
    button.dispatchEvent(
      new FocusEvent('focusout', { bubbles: true, relatedTarget: document.body }),
    );
    expect(isOpen(text)).toBe(false);
  });

  test('a touch tap (click without hover or focus) opens, a second tap closes', () => {
    const { button, text } = render();
    button.click();
    expect(isOpen(text)).toBe(true);
    button.click();
    expect(isOpen(text)).toBe(false);
  });

  test('a tap on a browser that focuses the button on press opens, a second tap closes', () => {
    const { button, text } = render();
    button.dispatchEvent(new FocusEvent('focusin', { bubbles: true }));
    button.click();
    expect(isOpen(text)).toBe(true);
    button.click();
    expect(isOpen(text)).toBe(false);
  });

  test('hovering one mark while another has focus leaves only the newer one open', () => {
    const { button, other, text } = render();
    const otherText = document.getElementById('tip-b') as HTMLElement;
    button.dispatchEvent(new FocusEvent('focusin', { bubbles: true }));
    pointer('pointerover', other);
    expect(isOpen(text)).toBe(false);
    expect(isOpen(otherText)).toBe(true);
  });

  test('a touch pointer does not open by hover', () => {
    const { button, text } = render();
    pointer('pointerover', button, { pointerType: 'touch' });
    expect(isOpen(text)).toBe(false);
  });

  test('Esc closes an open text even while the button keeps focus', () => {
    const { button, text } = render();
    button.dispatchEvent(new FocusEvent('focusin', { bubbles: true }));
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    expect(isOpen(text)).toBe(false);
  });

  test('a press elsewhere closes it, a press on another mark closes the first', () => {
    const { button, other, text } = render();
    button.click();
    pointer('pointerdown', document.getElementById('outside') as Element);
    expect(isOpen(text)).toBe(false);
    button.click();
    pointer('pointerdown', other);
    expect(isOpen(text)).toBe(false);
  });

  test('the open text stays inside the viewport horizontally', () => {
    const { button, text } = render();
    button.getBoundingClientRect = () => ({
      left: 5000,
      right: 5016,
      top: 10,
      bottom: 26,
      width: 16,
      height: 16,
      x: 5000,
      y: 10,
      toJSON: () => ({}),
    });
    button.click();
    const left = Number.parseFloat(text.style.left);
    expect(left).toBeLessThanOrEqual(window.innerWidth);
    expect(left).toBeGreaterThanOrEqual(0);
  });

  test('a mark inside a shadow root opens from the retargeted document events and closes on Esc', () => {
    const host = document.createElement('div');
    document.body.append(host);
    const root = host.attachShadow({ mode: 'open' });
    root.innerHTML = `<span data-help-mark><button type="button" data-help-mark-button aria-describedby="tip-s">?</button><span id="tip-s" role="tooltip" data-help-mark-text>説明S</span></span>`;
    const button = root.querySelector('button') as HTMLButtonElement;
    const text = root.getElementById('tip-s') as HTMLElement;
    button.dispatchEvent(new FocusEvent('focusin', { bubbles: true, composed: true }));
    expect(isOpen(text)).toBe(true);
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    expect(isOpen(text)).toBe(false);
    button.dispatchEvent(new MouseEvent('click', { bubbles: true, composed: true }));
    expect(isOpen(text)).toBe(true);
  });
});
