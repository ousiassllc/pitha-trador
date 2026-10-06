// Static guard for the tables the Lit components render at runtime: they
// replace the SSR tables after hydration, so they must keep the a11y the
// .templ side has (internal/web/table_a11y_test.go): every <th> carries
// scope and every <table> an accessible name (aria-label or <caption>).
import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { Glob } from 'bun';

const COMPONENTS_DIR = join(import.meta.dir, '..');

const sources = [...new Glob('**/*.ts').scanSync(COMPONENTS_DIR)]
  .filter((file) => !/\.test\.ts$|test-support\.ts$/.test(file))
  .map((file) => ({ file, text: readFileSync(join(COMPONENTS_DIR, file), 'utf8') }));

/** Opening tags `<tag ...>` (attributes may span lines and hold `${...}`). */
function openingTags(text: string, tag: string): string[] {
  return [...text.matchAll(new RegExp(`<${tag}(?=[\\s>])[^]*?>`, 'g'))].map((m) => m[0]);
}

describe('Lit table templates', () => {
  test('finds the table-rendering components', () => {
    const withTables = sources.filter((s) => openingTags(s.text, 'table').length > 0);
    expect(withTables.map((s) => s.file).sort()).toEqual([
      'activity-feed/activity-feed-views.ts',
      'activity-feed/pitha-activity-feed.ts',
      'calibration-heatmap/pitha-calibration-heatmap.ts',
      'scanner-table/pitha-scanner-table.ts',
    ]);
  });

  test('every <th> has a scope attribute', () => {
    const missing = sources.flatMap((s) =>
      openingTags(s.text, 'th')
        .filter((tag) => !/\sscope=/.test(tag))
        .map((tag) => `${s.file}: ${tag}`),
    );
    expect(missing).toEqual([]);
  });

  test('every <table> has an accessible name (aria-label or <caption>)', () => {
    const missing = sources.flatMap((s) => {
      const hasCaption = /<caption[\s>]/.test(s.text);
      return openingTags(s.text, 'table')
        .filter((tag) => !/\saria-label(ledby)?=/.test(tag) && !hasCaption)
        .map((tag) => `${s.file}: ${tag}`);
    });
    expect(missing).toEqual([]);
  });
});
