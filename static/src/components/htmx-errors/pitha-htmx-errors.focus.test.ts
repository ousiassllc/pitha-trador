import { beforeEach, describe, expect, test } from 'bun:test';
import './pitha-htmx-errors';

beforeEach(() => {
  document.body.innerHTML = '';
});

/** Fires htmx:afterSwap the way htmx does: on the swapped-in element, `requestConfig.elt` being the clicked button. */
function afterSwap(trigger: Element, swapped: Element): void {
  swapped.dispatchEvent(
    new CustomEvent('htmx:afterSwap', {
      detail: { requestConfig: { elt: trigger } },
      bubbles: true,
    }),
  );
}

function clickedButtonGone(attr = 'scan-list-heading'): HTMLButtonElement {
  const button = document.createElement('button');
  button.setAttribute('data-focus-after-swap', attr);
  document.body.appendChild(button);
  button.focus();
  button.remove(); // swapped out: htmx removed it and focus fell to <body>
  return button;
}

describe('focus after a swap that removed the clicked button (issue #676)', () => {
  test('moves focus to the element named by data-focus-after-swap', () => {
    const trigger = clickedButtonGone();
    const panel = document.createElement('section');
    panel.innerHTML = '<h3 id="scan-list-heading">スキャン対象銘柄</h3>';
    document.body.appendChild(panel);

    afterSwap(trigger, panel);

    const heading = document.getElementById('scan-list-heading');
    expect(document.activeElement).toBe(heading);
    expect(heading?.getAttribute('tabindex')).toBe('-1');
  });

  test('keeps an existing tabindex', () => {
    const trigger = clickedButtonGone();
    const panel = document.createElement('section');
    panel.innerHTML = '<h3 id="scan-list-heading" tabindex="0">x</h3>';
    document.body.appendChild(panel);

    afterSwap(trigger, panel);

    expect(document.getElementById('scan-list-heading')?.getAttribute('tabindex')).toBe('0');
  });

  test('does not steal focus htmx already restored to a same-id button', () => {
    const trigger = clickedButtonGone('scan-total');
    const panel = document.createElement('section');
    panel.innerHTML = '<button id="scan-prev">前へ</button><p id="scan-total">x</p>';
    document.body.appendChild(panel);
    document.getElementById('scan-prev')?.focus();

    afterSwap(trigger, panel);

    expect(document.activeElement).toBe(document.getElementById('scan-prev'));
  });

  test('does nothing when the clicked button is still in the document', () => {
    const button = document.createElement('button');
    button.setAttribute('data-focus-after-swap', 'scan-list-heading');
    document.body.appendChild(button);
    const heading = document.createElement('h3');
    heading.id = 'scan-list-heading';
    document.body.appendChild(heading);
    document.body.focus();

    afterSwap(button, heading);

    expect(document.activeElement).not.toBe(heading);
  });

  test('does nothing for a button without data-focus-after-swap', () => {
    const button = document.createElement('button');
    document.body.appendChild(button);
    button.focus();
    button.remove();
    const heading = document.createElement('h3');
    heading.id = 'scan-list-heading';
    document.body.appendChild(heading);

    afterSwap(button, heading);

    expect(document.activeElement).not.toBe(heading);
  });

  test('does nothing when the target id is absent from the response', () => {
    const trigger = clickedButtonGone('scan-universe-imported');
    const panel = document.createElement('section');
    document.body.appendChild(panel);

    afterSwap(trigger, panel);

    expect(document.activeElement === document.body || document.activeElement === null).toBe(true);
  });
});
