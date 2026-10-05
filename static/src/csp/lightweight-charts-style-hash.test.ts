// Guards the CSP `style-src` hash for lightweight-charts' attribution <style>.
// The Go middleware allows that one inline style element by SHA-256; if a
// lightweight-charts upgrade changes its text, the browser silently blocks it
// (Console-only CSP violation). This test makes such an upgrade fail loudly.
import { describe, expect, test } from 'bun:test';
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { createChart } from 'lightweight-charts';

const middlewarePath = new URL(
  '../../../internal/web/middleware/security_headers.go',
  import.meta.url,
);

function allowedHashFromGo(): string {
  const source = readFileSync(middlewarePath, 'utf8');
  const m = source.match(/lightweightChartsAttributionStyleHash\s*=\s*"'sha256-([^']+)'"/);
  if (!m) {
    throw new Error(
      `lightweightChartsAttributionStyleHash not found in ${middlewarePath.pathname}`,
    );
  }
  return m[1];
}

describe('CSP style hash for lightweight-charts', () => {
  test('matches the style element the installed library injects', () => {
    const container = document.createElement('div');
    document.body.append(container);
    const chart = createChart(container, { width: 300, height: 200 });
    try {
      const hashes = [...document.querySelectorAll('style')].map((s) =>
        createHash('sha256')
          .update(s.textContent ?? '')
          .digest('base64'),
      );
      expect(hashes.length).toBeGreaterThan(0);
      // Every inline <style> the library adds must be allowed by the CSP.
      // If this fails after bumping lightweight-charts, set
      // lightweightChartsAttributionStyleHash in
      // internal/web/middleware/security_headers.go to the new hash.
      for (const h of hashes) {
        expect(h).toBe(allowedHashFromGo());
      }
    } finally {
      chart.remove();
      container.remove();
    }
  });
});
