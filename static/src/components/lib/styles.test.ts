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
  // Numeric cells are JetBrains Mono and tabular; numeric columns (headings
  // included) are right-aligned; header cells are SemiBold 12px --mute and stay
  // in the body font (brand-guide §2, issue #1013).
  test('defines the numeric column and the header row recipe', () => {
    const css = tableStyles.cssText;
    expect(css).toMatch(/td\.num\s*\{[^}]*JetBrains Mono[^}]*tabular-nums/);
    expect(css).toMatch(/(^|[\s}])\.num\s*\{[^}]*text-align:\s*right/);
    expect(css).toMatch(
      /th\s*\{[^}]*color:\s*var\(--mute\)[^}]*font-size:\s*0\.75rem[^}]*font-weight:\s*600/,
    );
  });

  // issue #1013: `th.num` must not pick up the Mono stack, which would drop the
  // Japanese column headings into the system monospace font.
  test('only td.num cells use the Mono font', () => {
    const css = tableStyles.cssText;
    expect(css.match(/JetBrains Mono/g)).toHaveLength(1);
    expect(css).not.toMatch(/(^|[\s}])\.num\s*\{[^}]*font-family/);
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

  // brand-guide O7: the small (32px) button is text-sm, the same as atoms.Button's small size.
  // issue #984: one box model (border-box, no vertical padding) so the Lit
  // buttons and atoms.Button's small size are both 32px tall, whatever the label.
  test('the shared small button is border-box, one line, without vertical padding', () => {
    const css = buttonStyles.cssText;
    expect(css).toMatch(/button\s*\{[^}]*box-sizing:\s*border-box/);
    expect(css).toMatch(/button\s*\{[^}]*padding:\s*0 0\.75rem/);
    expect(css).toMatch(/button\s*\{[^}]*white-space:\s*nowrap/);
  });

  test('the shared small button is 2rem tall with 0.875rem text', () => {
    expect(buttonStyles.cssText).toMatch(
      /button\s*\{[^}]*min-height:\s*2rem[^}]*font-size:\s*0\.875rem/,
    );
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
