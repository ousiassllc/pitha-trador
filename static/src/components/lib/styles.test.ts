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

  // Guide M3: no colour transition under prefers-reduced-motion (issue #1032).
  test('drops the background transition under prefers-reduced-motion', () => {
    expect(buttonStyles.cssText).toMatch(
      /@media \(prefers-reduced-motion:\s*reduce\)\s*\{\s*button\s*\{[^}]*transition:\s*none/,
    );
  });
});

describe('tableStyles', () => {
  // Numeric cells are JetBrains Mono and tabular; numeric columns (headings
  // included) are right-aligned; header cells are Regular 13px --mute without an
  // underline and stay in the body font (brand-guide #app-ui, issue #1013).
  test('defines the numeric column and the header row recipe', () => {
    const css = tableStyles.cssText;
    expect(css).toMatch(/td\.num\s*\{[^}]*JetBrains Mono[^}]*tabular-nums/);
    expect(css).toMatch(/(^|[\s}])\.num\s*\{[^}]*text-align:\s*right/);
    expect(css).toMatch(
      /th\s*\{[^}]*border-bottom:\s*0;[^}]*color:\s*var\(--mute\)[^}]*font-size:\s*0\.8125rem[^}]*font-weight:\s*400/,
    );
  });

  // Body cells: 15px, a --line-soft rule (none under the last row), --surface-sel hover.
  test('body cells carry a --line-soft divider and a --surface-sel hover', () => {
    const css = tableStyles.cssText;
    expect(css).toMatch(/table\s*\{[^}]*font-size:\s*0\.9375rem/);
    expect(css).toMatch(/td\s*\{[^}]*border-bottom:\s*1px solid var\(--line-soft\)/);
    expect(css).toMatch(/tbody tr:last-child td[^{]*\{[^}]*border-bottom:\s*0/);
    expect(css).toMatch(/tbody tr:hover\s*\{[^}]*background:\s*var\(--surface-sel\)/);
    expect(css).not.toMatch(/font-size:\s*0\.(75|875)rem/);
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
  // brand-guide #app-ui: the sub button is a quiet borderless --surface-sel pill.
  test('the shared button is a borderless --surface-sel pill', () => {
    const css = buttonStyles.cssText;
    expect(css).toMatch(/button\s*\{[^}]*border:\s*0/);
    expect(css).toMatch(/button\s*\{[^}]*border-radius:\s*var\(--r-pill\)/);
    expect(css).toMatch(/button\s*\{[^}]*background:\s*var\(--surface-sel\)/);
    expect(css).toMatch(
      /button:hover:not\(:disabled\)\s*\{[^}]*background:\s*var\(--surface-sel-hover\)/,
    );
    expect(css).not.toMatch(/border:\s*1px/);
  });

  // issue #984: border-box, no vertical padding, so Lit and atoms.Button small are both 32px.
  test('the shared small button is 2rem tall, border-box, one line, 15px / 1.6', () => {
    const css = buttonStyles.cssText;
    expect(css).toMatch(/button\s*\{[^}]*box-sizing:\s*border-box/);
    expect(css).toMatch(/button\s*\{[^}]*padding:\s*0 0\.75rem/);
    expect(css).toMatch(/button\s*\{[^}]*white-space:\s*nowrap/);
    expect(css).toMatch(
      /button\s*\{[^}]*min-height:\s*2rem[^}]*font-size:\s*0\.9375rem;\s*line-height:\s*1\.6/,
    );
  });
});

describe('noticeStyles', () => {
  // Notice bodies are the 15px item step, the same size as the role="alert" error
  // (brand-guide app UI: only 13 / 15 / 28 are used).
  test('both notices are 0.9375rem / 1.6, like the error', () => {
    const css = noticeStyles.cssText;
    expect(css).toMatch(
      /\[role=['"]alert['"]\]\s*\{[^}]*font-size:\s*0\.9375rem;\s*line-height:\s*1\.6/,
    );
    expect(css).toMatch(
      /\.pitha-ws-disconnected\s*\{[^}]*font-size:\s*0\.9375rem;\s*line-height:\s*1\.6/,
    );
    expect(css).not.toMatch(/font-size:\s*0\.(75|875)rem/);
    // The light-DOM twin (scanner / activity feed) uses the same step.
    expect(lightDomWsNoticeClass).toBe('text-item text-warning');
  });
});
