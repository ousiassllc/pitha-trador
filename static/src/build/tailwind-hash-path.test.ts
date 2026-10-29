// Regression test for issue #926: the Tailwind CLI build must work when the
// checkout path contains `#` (e.g. a worktree for branch `feat/#925`).
// Without tailwind-hash-path-fix.cjs, the resolved `@import` path comes back
// with an enhanced-resolve `\0#` escape and the build dies with
// ERR_INVALID_ARG_VALUE.
import { afterAll, beforeAll, describe, expect, test } from 'bun:test';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

const staticDir = path.resolve(import.meta.dir, '../..');
const preload = path.join(staticDir, 'tailwind-hash-path-fix.cjs');
const cli = path.join(staticDir, 'node_modules/@tailwindcss/cli/dist/index.mjs');

let workDir: string;

beforeAll(() => {
  workDir = mkdtempSync(path.join(tmpdir(), 'feat-#926-'));
  mkdirSync(path.join(workDir, 'css'));
  writeFileSync(path.join(workDir, 'css/app.css'), '@import "./imported.css";\n');
  writeFileSync(path.join(workDir, 'css/imported.css'), '.from-imported { color: red; }\n');
});

afterAll(() => {
  rmSync(workDir, { recursive: true, force: true });
});

function buildCss(args: string[]): { exitCode: number; stderr: string } {
  const result = Bun.spawnSync(['node', ...args, cli, '-i', 'css/app.css', '-o', 'out.css'], {
    cwd: workDir,
    stdout: 'pipe',
    stderr: 'pipe',
  });
  return { exitCode: result.exitCode, stderr: result.stderr.toString() };
}

describe('tailwind build in a path containing #', () => {
  test('workDir really contains #', () => {
    expect(workDir).toContain('#');
  });

  test('fails without the preload (documents the upstream bug)', () => {
    const { exitCode, stderr } = buildCss([]);
    expect(exitCode).not.toBe(0);
    expect(stderr).toContain('null bytes');
  });

  test('succeeds with tailwind-hash-path-fix.cjs preloaded', () => {
    const { exitCode, stderr } = buildCss(['--require', preload]);
    expect(stderr).not.toContain('null bytes');
    expect(exitCode).toBe(0);
    expect(readFileSync(path.join(workDir, 'out.css'), 'utf-8')).toContain('.from-imported');
  });
});
