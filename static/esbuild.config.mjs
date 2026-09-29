// esbuild build/watch script for the Lit/TypeScript frontend
// (docs/components/overview.md §1, §6). Bundles static/src/components/*/pitha-*.ts
// into static/src/dist/js/**, mirroring the source subdirectory layout
// (`outbase`) so each pitha-* component's bundle has a stable, predictable
// path regardless of how many sibling components/lib files exist.
//
// Entry points are discovered by glob (a new pitha-* component is picked up
// without editing this file) and only the components pages load via
// <script type="module"> are entries; lib/*.ts are imported by them and
// bundled in. `splitting` (ESM) hoists code shared between entries (Lit, the
// lib helpers) into dist/js/chunks/ so e.g. the app-wide kill-switch-panel and
// a page's component don't each inline their own copy of Lit.
import fs from 'node:fs';
import path from 'node:path';
import * as esbuild from 'esbuild';

const componentsDir = 'src/components';
const outdir = 'src/dist/js';

// src/components/<name>/pitha-<name>.ts, excluding tests.
const entryPoints = fs
  .readdirSync(componentsDir, { withFileTypes: true })
  .filter((dir) => dir.isDirectory() && dir.name !== 'lib')
  .flatMap((dir) =>
    fs
      .readdirSync(path.join(componentsDir, dir.name))
      .filter((file) => /^pitha-.+\.ts$/.test(file) && !file.endsWith('.test.ts'))
      .map((file) => path.posix.join(componentsDir, dir.name, file)),
  )
  .sort();

const watch = process.argv.includes('--watch');

const buildOptions = {
  entryPoints,
  outdir,
  outbase: 'src/components',
  bundle: true,
  format: 'esm',
  splitting: true,
  chunkNames: 'chunks/[name]-[hash]',
  target: 'es2022',
  sourcemap: true,
  minify: !watch,
};

// Drop stale entries/chunks (hashed chunk names change between builds).
fs.rmSync(outdir, { recursive: true, force: true });

if (watch) {
  const ctx = await esbuild.context(buildOptions);
  await ctx.watch();
  console.log('esbuild: watching src/ for changes...');
} else {
  await esbuild.build(buildOptions);
  console.log('esbuild: build complete');
}

// Vendor Stoplight Elements (the `/swagger` UI, docs/environment/setup.md
// "Swagger / OpenAPI") from the bun-installed `@stoplight/elements` package
// into dist/vendor/, so it is embedded and served same-origin at
// /static/dist/vendor/stoplight-elements/ instead of an unpinned, SRI-less CDN
// script (issues #112/#117). The version is pinned by static/bun.lock.
const elementsSrc = 'node_modules/@stoplight/elements';
const elementsOut = 'src/dist/vendor/stoplight-elements';
fs.rmSync(elementsOut, { recursive: true, force: true });
fs.mkdirSync(elementsOut, { recursive: true });
for (const file of [
  'web-components.min.js',
  'web-components.min.js.LICENSE.txt',
  'styles.min.css',
  'LICENSE',
]) {
  fs.copyFileSync(path.join(elementsSrc, file), path.join(elementsOut, file));
}
