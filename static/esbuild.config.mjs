// esbuild build/watch script for the Lit/TypeScript frontend
// (docs/components/overview.md §1, §6). Bundles static/src/components/**
// into static/src/dist/js/**, mirroring the source subdirectory layout
// (`outbase`) so each pitha-* component's bundle has a stable, predictable
// path regardless of how many sibling components/lib files exist. The
// remaining pitha-* Lit components listed in docs/components/overview.md
// §5 are added to `entryPoints` by later sub-scopes once they exist.
import * as esbuild from 'esbuild';

const watch = process.argv.includes('--watch');

const buildOptions = {
  entryPoints: [
    'src/components/lib/api.ts',
    'src/components/lib/ws.ts',
    'src/components/lib/logger.ts',
    'src/components/scanner-table/pitha-scanner-table.ts',
  ],
  outdir: 'src/dist/js',
  outbase: 'src/components',
  bundle: true,
  format: 'esm',
  target: 'es2022',
  sourcemap: true,
  minify: !watch,
};

if (watch) {
  const ctx = await esbuild.context(buildOptions);
  await ctx.watch();
  console.log('esbuild: watching src/ for changes...');
} else {
  await esbuild.build(buildOptions);
  console.log('esbuild: build complete');
}
