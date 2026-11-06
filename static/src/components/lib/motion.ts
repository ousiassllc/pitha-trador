// Motion tokens of the Ousiass design guide §5, for the script side. The CSS
// side reads --motion-* / --ease-* in static/src/css/app.css (Tailwind
// `duration-*`, Lit `static styles`); the Web Animations API cannot resolve
// `var()`, so the numbers are mirrored here and motion.test.ts fails if the
// two drift apart.
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
