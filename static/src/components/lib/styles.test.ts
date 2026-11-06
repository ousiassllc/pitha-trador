import { describe, expect, test } from 'bun:test';
import { buttonStyles } from './styles';

describe('buttonStyles', () => {
  // Document CSS does not cross the shadow boundary, so the shared Lit button
  // draws its own --focus ring (brand-guide C3: 2px, offset 2).
  test('draws the --focus ring on :focus-visible', () => {
    expect(buttonStyles.cssText).toMatch(
      /button:focus-visible\s*\{[^}]*outline:\s*2px solid var\(--focus\);[^}]*outline-offset:\s*2px/,
    );
  });
});
