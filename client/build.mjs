import * as esbuild from 'esbuild';

// JS bundle
await esbuild.build({
  entryPoints: ['main.ts'],
  bundle: true,
  minify: true,
  outfile: '../server/static/bundle.js',
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
