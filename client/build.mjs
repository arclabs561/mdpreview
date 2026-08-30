import * as esbuild from 'esbuild';
import { rm } from 'node:fs/promises';

// JS bundle
await rm('../server/static/chunks', { recursive: true, force: true });
await esbuild.build({
  entryPoints: { bundle: 'main.ts' },
  bundle: true,
  minify: true,
  outdir: '../server/static',
  entryNames: '[name]',
  chunkNames: 'chunks/[name]-[hash]',
  splitting: true,
  format: 'esm',
  target: 'es2020',
});

// CSS bundle (github-markdown-css + KaTeX with inlined fonts)
await esbuild.build({
  entryPoints: ['main.css'],
  bundle: true,
  minify: true,
  outfile: '../server/static/bundle.css',
  loader: {
    '.woff2': 'dataurl',
    '.woff': 'dataurl',
    '.ttf': 'dataurl',
  },
});
