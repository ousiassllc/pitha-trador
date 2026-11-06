// Press ripple for buttons and close buttons (design guide §5 / M4): a small
// circle grows from the pressed point and fades out over --motion-slow
// (280ms, ease-out). Opt-in per element with `data-ripple` (`atoms.Button`,
// `atoms.ButtonLink`, the toast's close button, the Lit components' buttons),
// so one listener on `document` serves the light DOM and every shadow root:
// the target is found in `event.composedPath()`, which sees through retargeting.
//
// The circle is a plain `<span>` styled through the CSSOM (allowed under the
// CSP, unlike a `style` attribute in markup) and animated with the Web
// Animations API, so it needs no @keyframes in either the document CSS or a
// shadow root's styles. The host must clip it (`relative overflow-hidden`;
// buttonClass / buttonStyles carry both). Nothing is drawn under
// `prefers-reduced-motion: reduce` (guide M3) or when the Web Animations API is
// missing.

import { EASE_OUT, MOTION_SLOW_MS, prefersReducedMotion } from '../lib/motion';

const PEAK_OPACITY = 0.2;

function rippleHost(event: Event): HTMLElement | null {
  for (const node of event.composedPath()) {
    if (node instanceof HTMLElement && node.hasAttribute('data-ripple')) return node;
  }
  return null;
}

/** `pointerdown`: spawn one ripple at the press point of the pressed `[data-ripple]` element. */
export function onPointerDown(event: Event): void {
  if (!(event instanceof PointerEvent) || (event.pointerType === 'mouse' && event.button !== 0)) {
    return;
  }
  const host = rippleHost(event);
  if (!host || host.matches(':disabled, [aria-disabled="true"]') || prefersReducedMotion()) return;
  const box = host.getBoundingClientRect();
  const x = event.clientX - box.left;
  const y = event.clientY - box.top;
  // Radius that reaches the farthest corner, so the wave always covers the host.
  const radius = Math.hypot(Math.max(x, box.width - x), Math.max(y, box.height - y));
  const wave = document.createElement('span');
  wave.setAttribute('aria-hidden', 'true');
  Object.assign(wave.style, {
    position: 'absolute',
    left: `${x - radius}px`,
    top: `${y - radius}px`,
    width: `${radius * 2}px`,
    height: `${radius * 2}px`,
    borderRadius: '9999px',
    background: 'currentColor',
    opacity: '0',
    pointerEvents: 'none',
  });
  host.append(wave);
  if (typeof wave.animate !== 'function') {
    wave.remove();
    return;
  }
  const animation = wave.animate(
    [
      { transform: 'scale(0)', opacity: PEAK_OPACITY },
      { transform: 'scale(1)', opacity: 0 },
    ],
    { duration: MOTION_SLOW_MS, easing: EASE_OUT },
  );
  animation.onfinish = () => wave.remove();
  animation.oncancel = () => wave.remove();
}

export function init(): void {
  document.addEventListener('pointerdown', onPointerDown, true);
}

init();
