import { afterEach, beforeEach, describe, expect, test } from 'bun:test';
import './pitha-ripple';

type AnimateCall = { keyframes: Keyframe[]; options: KeyframeAnimationOptions };

let calls: AnimateCall[] = [];
let finish: (() => void) | undefined;
const realAnimate = HTMLElement.prototype.animate;
const realMatchMedia = globalThis.matchMedia;

function press(target: Element, init: PointerEventInit = {}): void {
  const event = new PointerEvent('pointerdown', {
    bubbles: true,
    composed: true,
    pointerType: 'mouse',
    button: 0,
    clientX: 10,
    clientY: 5,
    ...init,
  });
  target.dispatchEvent(event);
}

function render(extra = ''): HTMLButtonElement {
  document.body.innerHTML = `<button type="button" data-ripple ${extra}>保存</button><button type="button" id="plain">plain</button>`;
  return document.querySelector('button[data-ripple]') as HTMLButtonElement;
}

beforeEach(() => {
  calls = [];
  finish = undefined;
  HTMLElement.prototype.animate = (keyframes, options) => {
    calls.push({
      keyframes: keyframes as Keyframe[],
      options: options as KeyframeAnimationOptions,
    });
    const animation = {} as Animation;
    Object.defineProperty(animation, 'onfinish', {
      set(fn: () => void) {
        finish = fn;
      },
    });
    return animation;
  };
  globalThis.matchMedia = (() => ({ matches: false })) as unknown as typeof matchMedia;
});

afterEach(() => {
  HTMLElement.prototype.animate = realAnimate;
  globalThis.matchMedia = realMatchMedia;
});

describe('press ripple (design guide M4)', () => {
  test('a press on a [data-ripple] button adds one circle that grows over 280ms ease-out and is removed when done', () => {
    const button = render();
    press(button);

    const wave = button.querySelector('span[aria-hidden="true"]');
    expect(wave).not.toBeNull();
    expect(calls).toHaveLength(1);
    expect(calls[0].options.duration).toBe(280);
    expect(calls[0].options.easing).toBe('cubic-bezier(0.2, 0.8, 0.2, 1)');
    expect(calls[0].keyframes[1]).toMatchObject({ opacity: 0, transform: 'scale(1)' });

    finish?.();
    expect(button.querySelector('span')).toBeNull();
  });

  test('a button without data-ripple and a disabled button get no ripple', () => {
    render();
    press(document.getElementById('plain') as Element);
    expect(calls).toHaveLength(0);

    const disabled = render('disabled');
    press(disabled);
    expect(disabled.querySelector('span')).toBeNull();
    expect(calls).toHaveLength(0);
  });

  test('prefers-reduced-motion: reduce draws nothing', () => {
    globalThis.matchMedia = (() => ({ matches: true })) as unknown as typeof matchMedia;
    const button = render();
    press(button);
    expect(button.querySelector('span')).toBeNull();
    expect(calls).toHaveLength(0);
  });

  test('a right-button press does not ripple', () => {
    const button = render();
    press(button, { button: 2 });
    expect(calls).toHaveLength(0);
  });
});
