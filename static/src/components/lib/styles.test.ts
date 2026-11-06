import { describe, expect, test } from 'bun:test';
import { buttonStyles, lightDomWsNoticeClass, noticeStyles, tableStyles } from './styles';

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

describe('buttonStyles fill', () => {
  // brand-guide §4: the sub button is transparent with a 1px --border, like
  // atoms.Button's secondary variant, not a --surface face.
  test('the shared button is transparent with a 1px --border', () => {
    expect(buttonStyles.cssText).toMatch(/button\s*\{[^}]*border:\s*1px solid var\(--border\)/);
    expect(buttonStyles.cssText).toMatch(/button\s*\{[^}]*background:\s*transparent/);
    expect(buttonStyles.cssText).not.toMatch(/button\s*\{[^}]*background:\s*var\(--surface\)/);
  });
});

describe('noticeStyles', () => {
  // Notice bodies are text-sm (14px), the same size as the role="alert" error
  // (brand-guide §2 T3); 12px is for notes, captions and badges only.
  test('the ws-disconnected notice is 0.875rem like the error', () => {
    const css = noticeStyles.cssText;
    expect(css).toMatch(/\.pitha-ws-disconnected\s*\{[^}]*font-size:\s*0\.875rem/);
    expect(css).not.toMatch(/font-size:\s*0\.75rem/);
  });

  test('the light-DOM ws notice class is text-sm', () => {
    expect(lightDomWsNoticeClass).toBe('text-sm text-warning');
  });
});
