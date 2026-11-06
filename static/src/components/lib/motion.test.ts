import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { EASE_IN, EASE_OUT, MOTION_FADE_IN_MS, MOTION_FADE_OUT_MS, MOTION_SLOW_MS } from './motion';

const appCss = readFileSync(new URL('../../css/app.css', import.meta.url), 'utf8');
const token = (name: string) => new RegExp(`${name}:\\s*([^;]+);`).exec(appCss)?.[1]?.trim();

// motion.ts mirrors the CSS tokens for the Web Animations API (no var()).
describe('motion constants match the CSS tokens in app.css', () => {
  test('durations', () => {
    expect(token('--motion-slow')).toBe(`${MOTION_SLOW_MS}ms`);
    expect(token('--motion-fade-in')).toBe(`${MOTION_FADE_IN_MS}ms`);
    expect(token('--motion-fade-out')).toBe(`${MOTION_FADE_OUT_MS}ms`);
  });

  test('easing curves', () => {
    expect(token('--ease-out')).toBe(EASE_OUT);
    expect(token('--ease-in')).toBe(EASE_IN);
  });
});
