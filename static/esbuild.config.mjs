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
  // Dev (--watch) only: production output is go:embed'ed and served under
  // /static, so shipping .map files would bloat the binary and expose the TS
  // sources (issue #146). PITHA_STATIC_DIR dev serving still gets them.
  sourcemap: watch,
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

// Self-host the guide's fonts (Sora, Noto Sans JP, JetBrains Mono; all SIL
// OFL, issue #941) from the bun-installed @fontsource-variable packages into
// dist/fonts/, served same-origin (CSP `font-src 'self'`). Each package's
// `wght.css` is copied as one dist/fonts/fonts.css with the family renamed to
// the guide's names (`Sora`, `Noto Sans JP`, `JetBrains Mono`: the
// `font-family` values of docs/components/design-tokens.md) and the url()s
// pointed at the copied files. Only the upright variable files are shipped
// (weights 400 / 600 are the only ones the guide uses). Versions are pinned
// by static/bun.lock.
const fontsOut = 'src/dist/fonts';
fs.rmSync(fontsOut, { recursive: true, force: true });
fs.mkdirSync(fontsOut, { recursive: true });
const fontFaces = [];
for (const [pkg, family] of [
  ['sora', 'Sora'],
  ['noto-sans-jp', 'Noto Sans JP'],
  ['jetbrains-mono', 'JetBrains Mono'],
]) {
  const pkgDir = `node_modules/@fontsource-variable/${pkg}`;
  const css = fs.readFileSync(path.join(pkgDir, 'wght.css'), 'utf8');
  fontFaces.push(
    css
      .replaceAll(`'${family} Variable'`, `'${family}'`)
      .replaceAll('url(./files/', `url(/static/dist/fonts/${pkg}/`),
  );
  const filesOut = path.join(fontsOut, pkg);
  fs.mkdirSync(filesOut, { recursive: true });
  for (const file of fs.readdirSync(path.join(pkgDir, 'files'))) {
    if (file.endsWith('-wght-normal.woff2')) {
      fs.copyFileSync(path.join(pkgDir, 'files', file), path.join(filesOut, file));
    }
  }
  fs.copyFileSync(path.join(pkgDir, 'LICENSE'), path.join(filesOut, 'LICENSE'));
}
fs.writeFileSync(path.join(fontsOut, 'fonts.css'), fontFaces.join('\n'));
