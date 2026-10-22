import { cp, mkdtemp, readdir, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFileSync } from 'node:child_process';

const client = path.dirname(fileURLToPath(import.meta.url));
const committed = path.resolve(client, '../server/static');
const scratch = await mkdtemp(path.join(tmpdir(), 'mdpreview-build-'));

async function files(directory, prefix = '') {
  const entries = await readdir(directory, { withFileTypes: true });
  const result = [];
  for (const entry of entries) {
    const name = path.join(prefix, entry.name);
    if (entry.isDirectory()) result.push(...await files(path.join(directory, entry.name), name));
    else result.push(name);
  }
  return result;
}

try {
  // Keep node_modules physically beside the source: symlinking the dependency
  // tree changes esbuild's module paths and therefore its chunk hashes.
  const copy = path.join(scratch, 'client');
  await cp(client, copy, { recursive: true, dereference: true });
  execFileSync(process.execPath, ['build.mjs'], { cwd: copy, stdio: 'inherit' });

  const generated = path.join(scratch, 'server/static');
  const expected = new Set((await files(committed)).filter((name) =>
    name === 'bundle.js' || name === 'bundle.css' || name === 'THIRD_PARTY_NOTICES.txt' ||
    name.startsWith(`chunks${path.sep}`)));
  const actual = new Set(await files(generated));
  const differences = [];
  for (const name of [...new Set([...expected, ...actual])].sort()) {
    if (!expected.has(name)) differences.push(`Not committed: ${name}`);
    else if (!actual.has(name)) differences.push(`No longer generated: ${name}`);
    else if (!(await readFile(path.join(committed, name))).equals(await readFile(path.join(generated, name)))) {
      differences.push(`Content differs: ${name}`);
    }
  }
  if (differences.length) {
    console.error(differences.join('\n'));
    console.error('Rebuild with cd client && bun run build and commit the generated assets.');
    process.exitCode = 1;
  } else {
    console.log(`Embedded client is fresh (${actual.size} files).`);
  }
} finally {
  await rm(scratch, { recursive: true, force: true });
}
