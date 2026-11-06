// Motion tokens of the Ousiass design guide §5, for the script side (the CSS
// side uses the same numbers as Tailwind `duration-[...]` / `--ease-out` in
// static/src/css/app.css and as literals in the Lit `static styles`).
// `prefersReducedMotion()` is the single gate: with `reduce` the decorative
// movement (ripple, slide) is dropped and the opacity fade shrinks to
// REDUCED_FADE_MS (guide: "<= 100ms or none").

export const MOTION_SLOW_MS = 280;
export const MOTION_FADE_IN_MS = 320;
export const MOTION_FADE_OUT_MS = 240;
export const REDUCED_FADE_MS = 100;
export const EASE_OUT = 'cubic-bezier(0.2, 0.8, 0.2, 1)';
export const EASE_IN = 'cubic-bezier(0.4, 0, 1, 1)';

export function prefersReducedMotion(): boolean {
  return typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;
}
