import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test';
import { logger } from './logger';

let infoSpy: ReturnType<typeof mock>;
let errorSpy: ReturnType<typeof mock>;
let originalInfo: typeof console.info;
let originalError: typeof console.error;

beforeEach(() => {
  originalInfo = console.info;
  originalError = console.error;
  infoSpy = mock(() => {});
  errorSpy = mock(() => {});
  console.info = infoSpy as unknown as typeof console.info;
  console.error = errorSpy as unknown as typeof console.error;
});

afterEach(() => {
  console.info = originalInfo;
  console.error = originalError;
});

describe('logger.info', () => {
  test('writes a structured entry to console.info with the given fields', () => {
    logger.info('scanner refreshed', { symbolCount: 12 });

    expect(infoSpy).toHaveBeenCalledTimes(1);
    const [entry] = infoSpy.mock.calls[0] as [Record<string, unknown>];
    expect(entry.level).toBe('info');
    expect(entry.message).toBe('scanner refreshed');
    expect(entry.symbolCount).toBe(12);
    expect(typeof entry.timestamp).toBe('string');
  });
});

describe('logger.fatal', () => {
  test('routes to console.error with a fatal marker field', () => {
    logger.fatal('kill switch failed', { symbol: 'AAPL' });

    expect(errorSpy).toHaveBeenCalledTimes(1);
    const [entry] = errorSpy.mock.calls[0] as [Record<string, unknown>];
    expect(entry.level).toBe('error');
    expect(entry.fatal).toBe(true);
    expect(entry.symbol).toBe('AAPL');
  });
});
