import { describe, expect, mock, test } from 'bun:test';
import { EXIT_REASONS, exitReasonLabel, screenWarning, UNKNOWN_REASON } from './labels';

describe('labels', () => {
  test('names a known exit reason in Japanese', () => {
    expect(exitReasonLabel('take_profit')).toBe(EXIT_REASONS.take_profit);
    expect(exitReasonLabel('take_profit')).toBe('利確');
  });

  test('an unknown code is never printed: the generic name shows and the code is logged once', () => {
    const warn = mock(() => {});
    const original = console.warn;
    console.warn = warn;
    try {
      expect(exitReasonLabel('brand_new_code')).toBe(UNKNOWN_REASON);
      expect(exitReasonLabel('brand_new_code')).toBe(UNKNOWN_REASON);
    } finally {
      console.warn = original;
    }
    expect(UNKNOWN_REASON).not.toContain('brand_new_code');
    expect(warn.mock.calls.length).toBeLessThanOrEqual(1);
  });

  test('screenWarning keeps the shared warning and names the setting key', () => {
    const shared = '確認した Jev のモデル名を metalabel.llm_cutoff_model に記録してください。';
    const shown = screenWarning(shared);
    expect(shown).not.toContain('metalabel.');
    expect(shown).toContain('に記録してください。');
  });
});
