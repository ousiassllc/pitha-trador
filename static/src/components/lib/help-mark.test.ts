import { describe, expect, test } from 'bun:test';
import { helpMarkStyles } from './help-mark';

describe('helpMarkStyles', () => {
  // Brand-guide §2: xs text uses the scale's 1.6 line height (not 1.625).
  test('the tooltip uses the xs line height 1.6', () => {
    expect(helpMarkStyles.cssText).toMatch(/line-height:\s*1\.6;/);
    expect(helpMarkStyles.cssText).not.toContain('1.625');
  });
});
