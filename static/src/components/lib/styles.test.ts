import { describe, expect, test } from 'bun:test';
import { buttonStyles, tableStyles } from './styles';

describe('buttonStyles', () => {
  // Document CSS does not cross the shadow boundary, so the shared Lit button
  // draws its own --focus ring (brand-guide C3: 2px, offset 2).
  test('draws the --focus ring on :focus-visible', () => {
    expect(buttonStyles.cssText).toMatch(
      /button:focus-visible\s*\{[^}]*outline:\s*2px solid var\(--focus\);[^}]*outline-offset:\s*2px/,
    );
  });
});

describe('tableStyles', () => {
  // Numeric columns are JetBrains Mono, tabular and right-aligned; header cells
  // are SemiBold 12px --mute (brand-guide §2).
  test('defines the numeric column and the header row recipe', () => {
    const css = tableStyles.cssText;
    expect(css).toMatch(/\.num\s*\{[^}]*JetBrains Mono[^}]*tabular-nums[^}]*text-align:\s*right/);
    expect(css).toMatch(
      /th\s*\{[^}]*color:\s*var\(--mute\)[^}]*font-size:\s*0\.75rem[^}]*font-weight:\s*600/,
    );
  });
});
