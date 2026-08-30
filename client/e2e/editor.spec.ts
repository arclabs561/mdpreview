import { expect, test } from '@playwright/test';
import { copyFile, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:net';
import { join, resolve } from 'node:path';
import { tmpdir } from 'node:os';
import { spawn, type ChildProcess } from 'node:child_process';

const repoRoot = resolve(import.meta.dirname, '..', '..');
let server: ChildProcess;
let baseURL: string;
let tempDir: string;
let documentPath: string;
const fixturePath = join(import.meta.dirname, 'fixture.md');

async function unusedPort(): Promise<number> {
  const listener = createServer();
  await new Promise<void>((resolvePort, reject) => {
    listener.once('error', reject);
    listener.listen(0, '127.0.0.1', () => resolvePort());
  });
  const address = listener.address();
  await new Promise<void>((resolveClose, reject) => listener.close(error => error ? reject(error) : resolveClose()));
  if (!address || typeof address === 'string') throw new Error('failed to reserve test port');
  return address.port;
}

async function waitForServer(url: string): Promise<void> {
  const deadline = Date.now() + 15_000;
  while (Date.now() < deadline) {
    try {
      if ((await fetch(url)).ok) return;
    } catch {
      // The process is still compiling or binding its listener.
    }
    await new Promise(resolveDelay => setTimeout(resolveDelay, 100));
  }
  throw new Error('mdpreview server did not start');
}

test.beforeAll(async () => {
  tempDir = await mkdtemp(join(tmpdir(), 'mdpreview-e2e-'));
  documentPath = join(tempDir, 'doc.md');
  await copyFile(fixturePath, documentPath);
  const port = await unusedPort();
  baseURL = `http://127.0.0.1:${port}`;
  server = spawn('go', ['run', '.', '-addr', `127.0.0.1:${port}`, '-no-open', documentPath], {
    cwd: repoRoot,
    stdio: 'ignore',
  });
  await waitForServer(baseURL);
});

test.afterAll(async () => {
  server?.kill('SIGINT');
  await rm(tempDir, { recursive: true, force: true });
});

test.beforeEach(async () => {
  await copyFile(fixturePath, documentPath);
});

test('edits, previews, and saves a Markdown document', async ({ page }) => {
  await page.goto(`${baseURL}/?file=doc.md`);
  await expect(page.locator('#content')).toContainText('Original title');
  await expect(page.locator('#editToggle')).toBeEnabled();

  await page.locator('#editToggle').click();
  await page.locator('#editor').fill('# Edited title\n\nSaved from the browser.');
  await expect(page.locator('#content')).toContainText('Edited title');
  await expect(page.locator('#editorStatus')).toHaveText('Unsaved changes');

  await page.locator('#editor').press(process.platform === 'darwin' ? 'Meta+S' : 'Control+S');
  await expect(page.locator('#editorStatus')).toHaveText('Saved');
  await expect.poll(() => readFile(documentPath, 'utf8')).toContain('Edited title');
});

test('applies Markdown shortcuts without leaving the editor', async ({ page }) => {
  await page.goto(`${baseURL}/?file=doc.md`);
  await page.locator('#editToggle').click();
  const source = page.locator('#editor');
  await source.fill('selected');
  await source.evaluate((element: HTMLTextAreaElement) => element.setSelectionRange(0, 8));
  await source.press(process.platform === 'darwin' ? 'Meta+B' : 'Control+B');
  await expect(source).toHaveValue('**selected**');

  await source.fill('first\nsecond');
  await source.evaluate((element: HTMLTextAreaElement) => element.setSelectionRange(0, element.value.length));
  await source.press('Tab');
  await expect(source).toHaveValue('  first\n  second');
  await source.press('Shift+Tab');
  await expect(source).toHaveValue('first\nsecond');
});

test('loads Mermaid only for a document that contains a diagram', async ({ page }) => {
  await writeFile(documentPath, '```mermaid\nflowchart LR\n  A --> B\n```\n');
  await page.goto(`${baseURL}/?file=doc.md`);
  await expect(page.locator('.mermaid-diagram svg')).toBeVisible();
});

test('preserves a dirty editor when the file changes outside the browser', async ({ page }) => {
  await page.goto(`${baseURL}/?file=doc.md`);
  await page.locator('#editToggle').click();
  await page.locator('#editor').fill('# Local draft');
  await writeFile(documentPath, '# External change\n');
  await expect(page.locator('#editorStatus')).toHaveText('Changed on disk — reload before saving');
  await expect(page.locator('#editorConflictActions')).toBeVisible();
  await page.locator('#keepDraftButton').click();
  await expect(page.locator('#editorStatus')).toHaveText('Keeping local draft');
  await expect(page.locator('#editor')).toHaveValue('# Local draft');
});

test('reloads the disk version after an external change', async ({ page }) => {
  await page.goto(`${baseURL}/?file=doc.md`);
  await page.locator('#editToggle').click();
  await page.locator('#editor').fill('# Local draft');
  await writeFile(documentPath, '# Disk version\n');
  await expect(page.locator('#editorConflictActions')).toBeVisible();

  await page.locator('#reloadDiskButton').click();
  await expect(page.locator('#editorConflictActions')).toBeHidden();
  await expect(page.locator('#editor')).toHaveValue('# Disk version\n');
  await expect(page.locator('#content')).toContainText('Disk version');
});

test('does not overwrite a stale disk version when saving', async ({ page }) => {
  await page.goto(`${baseURL}/?file=doc.md`);
  await page.locator('#editToggle').click();
  await page.locator('#editor').fill('# Local draft');
  await writeFile(documentPath, '# Disk version\n');

  await page.locator('#saveButton').click();
  await expect(page.locator('#editorStatus')).toHaveText('Changed on disk — reload before saving');
  await expect(page.locator('#editorConflictActions')).toBeVisible();
  await expect(page.locator('#editor')).toHaveValue('# Local draft');
  await expect.poll(() => readFile(documentPath, 'utf8')).toBe('# Disk version\n');
});
