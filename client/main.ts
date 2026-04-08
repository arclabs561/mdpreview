import { createHighlighterCore, type HighlighterCore } from 'shiki/core';
import { createJavaScriptRegexEngine } from 'shiki/engine/javascript';
import katex from 'katex';

import githubDark from 'shiki/themes/github-dark.mjs';
import githubLight from 'shiki/themes/github-light.mjs';

import langBash from 'shiki/langs/bash.mjs';
import langC from 'shiki/langs/c.mjs';
import langCpp from 'shiki/langs/cpp.mjs';
import langCss from 'shiki/langs/css.mjs';
import langDiff from 'shiki/langs/diff.mjs';
import langDockerfile from 'shiki/langs/dockerfile.mjs';
import langGo from 'shiki/langs/go.mjs';
import langHtml from 'shiki/langs/html.mjs';
import langJava from 'shiki/langs/java.mjs';
import langJavascript from 'shiki/langs/javascript.mjs';
import langJson from 'shiki/langs/json.mjs';
import langMarkdown from 'shiki/langs/markdown.mjs';
import langPython from 'shiki/langs/python.mjs';
import langRuby from 'shiki/langs/ruby.mjs';
import langRust from 'shiki/langs/rust.mjs';
import langShell from 'shiki/langs/shellscript.mjs';
import langSql from 'shiki/langs/sql.mjs';
import langToml from 'shiki/langs/toml.mjs';
import langTypescript from 'shiki/langs/typescript.mjs';
import langTsx from 'shiki/langs/tsx.mjs';
import langXml from 'shiki/langs/xml.mjs';
import langYaml from 'shiki/langs/yaml.mjs';

let highlighter: HighlighterCore | null = null;
let ws: WebSocket | null = null;
let currentFile = '';
let previousBlocks: string[] = [];
let wsRetries = 0;
const WS_MAX_RETRIES = 5;

const LANG_MAP: Record<string, string> = {
  sh: 'bash', zsh: 'bash', bash: 'bash',
  c: 'c', h: 'c',
  cpp: 'cpp', cc: 'cpp', cxx: 'cpp', hpp: 'cpp',
  css: 'css', scss: 'css',
  diff: 'diff', patch: 'diff',
  dockerfile: 'dockerfile',
  go: 'go',
  htm: 'html', html: 'html',
  java: 'java',
  js: 'javascript', mjs: 'javascript', cjs: 'javascript',
  json: 'json', jsonc: 'json',
  md: 'markdown',
  py: 'python',
  rb: 'ruby',
  rs: 'rust',
  sql: 'sql',
  swift: 'swift',
  toml: 'toml',
  ts: 'typescript', mts: 'typescript', cts: 'typescript',
  tsx: 'tsx', jsx: 'tsx',
  xml: 'xml', svg: 'xml',
  yml: 'yaml', yaml: 'yaml',
  makefile: 'bash',
};

// DOM elements
const sidebar = document.getElementById('sidebar')!;
const fileTree = document.getElementById('fileTree')!;
const breadcrumb = document.getElementById('breadcrumb')!;
const content = document.getElementById('content')!;
const statusDot = document.getElementById('statusDot')!;
const statusText = document.getElementById('statusText')!;
const lastUpdated = document.getElementById('lastUpdated')!;
const diffStats = document.getElementById('diffStats')!;

interface TreeEntry {
  name: string;
  path: string;
  isDir: boolean;
  status?: string; // M=modified, A=added, ?=untracked, D=deleted
  children?: TreeEntry[];
}

const STATUS_COLORS: Record<string, string> = {
  'M': 'var(--attention-fg)',
  'A': 'var(--success-fg)',
  '?': 'var(--text-secondary)',
  'D': 'var(--danger-fg)',
};
const STATUS_LABELS: Record<string, string> = {
  'M': 'M', 'A': 'A', '?': 'U', 'D': 'D',
};

async function initHighlighter() {
  highlighter = await createHighlighterCore({
    themes: [githubDark, githubLight],
    langs: [
      langBash, langC, langCpp, langCss, langDiff, langDockerfile,
      langGo, langHtml, langJava, langJavascript, langJson, langMarkdown,
      langPython, langRuby, langRust, langShell, langSql, langToml,
      langTypescript, langTsx, langXml, langYaml,
    ],
    engine: createJavaScriptRegexEngine(),
  });
}

// ---- File Tree ----

async function loadTree() {
  const resp = await fetch('/api/tree');
  const tree: TreeEntry[] = await resp.json();
  fileTree.innerHTML = '';
  renderTree(tree, fileTree, 0);
}

function renderTree(entries: TreeEntry[], parent: HTMLElement, depth: number) {
  for (const entry of entries) {
    const item = document.createElement('div');
    item.className = 'tree-item' + (entry.isDir ? ' tree-dir' : ' tree-file');
    if (entry.status) item.classList.add('has-status');
    item.style.paddingLeft = (12 + depth * 16) + 'px';
    item.dataset.path = entry.path;

    const statusBadge = entry.status
      ? `<span class="tree-status" style="color:${STATUS_COLORS[entry.status] || ''}">${STATUS_LABELS[entry.status] || ''}</span>`
      : '';

    if (entry.isDir) {
      // Auto-expand dirs with modified files, or first level
      const hasChanges = !!entry.status;
      const expanded = depth < 1 || hasChanges;
      item.innerHTML = `<span class="tree-toggle">${expanded ? '\u25BE' : '\u25B8'}</span> <span class="tree-name">${entry.name}</span>${statusBadge}`;
      parent.appendChild(item);

      const children = document.createElement('div');
      children.className = 'tree-children';
      children.style.display = expanded ? 'block' : 'none';
      parent.appendChild(children);

      item.addEventListener('click', (e) => {
        e.stopPropagation();
        const isExpanded = children.style.display !== 'none';
        children.style.display = isExpanded ? 'none' : 'block';
        item.querySelector('.tree-toggle')!.textContent = isExpanded ? '\u25B8' : '\u25BE';
      });

      if (entry.children) {
        renderTree(entry.children, children, depth + 1);
      }
    } else {
      const nameColor = entry.status ? `style="color:${STATUS_COLORS[entry.status]}"` : '';
      item.innerHTML = `<span class="tree-name" ${nameColor}>${entry.name}</span>${statusBadge}`;
      item.addEventListener('click', (e) => {
        e.stopPropagation();
        navigateTo(entry.path);
      });
      parent.appendChild(item);
    }
  }
}

// ---- Navigation ----

function navigateTo(file: string) {
  currentFile = file;
  previousBlocks = [];

  // Update URL without reload
  const url = new URL(window.location.href);
  url.searchParams.set('file', file);
  history.pushState(null, '', url.toString());

  // Update breadcrumb
  updateBreadcrumb(file);

  // Highlight active file in tree
  document.querySelectorAll('.tree-file').forEach(el => {
    el.classList.toggle('active', (el as HTMLElement).dataset.path === file);
  });

  // Load content
  fetchModTime(file);
  fetchDiffInfo(file);
  if (file.endsWith('.md')) {
    connectWebSocket(file);
  } else {
    disconnectWebSocket();
    loadRawFile(file);
  }
}

function updateBreadcrumb(file: string) {
  const repoName = (window as any).__repoName || 'repo';
  const parts = file.split('/');
  let html = `<a href="/" class="breadcrumb-link">${repoName}</a>`;
  for (let i = 0; i < parts.length; i++) {
    const isLast = i === parts.length - 1;
    if (isLast) {
      html += ` / <span class="breadcrumb-current">${parts[i]}</span>`;
    } else {
      html += ` / <span class="breadcrumb-dir">${parts[i]}</span>`;
    }
  }
  breadcrumb.innerHTML = html;
}

// ---- File metadata ----

async function fetchModTime(file: string) {
  try {
    const resp = await fetch('/api/meta?file=' + encodeURIComponent(file));
    if (!resp.ok) return;
    const data = await resp.json();
    if (data.modTime) {
      const date = new Date(data.modTime * 1000);
      lastUpdated.textContent = 'Updated ' + formatRelativeTime(date);
      lastUpdated.title = date.toLocaleString();
    }
  } catch {}
}

function formatRelativeTime(date: Date): string {
  const now = Date.now();
  const diffMs = now - date.getTime();
  const diffSec = Math.floor(diffMs / 1000);
  if (diffSec < 5) return 'just now';
  if (diffSec < 60) return diffSec + 's ago';
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return diffMin + 'm ago';
  const diffHr = Math.floor(diffMin / 60);
  if (diffHr < 24) return diffHr + 'h ago';
  const diffDay = Math.floor(diffHr / 24);
  if (diffDay < 30) return diffDay + 'd ago';
  return date.toLocaleDateString();
}

// ---- Git diff info ----

interface GitDiffInfo {
  additions: number;
  deletions: number;
  addedLines?: number[];
  changedLines?: number[];
  deletedAt?: number[];
}

let currentDiffInfo: GitDiffInfo | null = null;

async function fetchDiffInfo(file: string) {
  diffStats.innerHTML = '';
  currentDiffInfo = null;
  try {
    const resp = await fetch('/api/diff?file=' + encodeURIComponent(file));
    if (!resp.ok) return;
    const data: GitDiffInfo = await resp.json();
    currentDiffInfo = data;

    if (data.additions > 0 || data.deletions > 0) {
      let parts: string[] = [];
      if (data.additions > 0) parts.push(`<span class="diff-add-count">+${data.additions}</span>`);
      if (data.deletions > 0) parts.push(`<span class="diff-del-count">\u2212${data.deletions}</span>`);
      diffStats.innerHTML = parts.join(' ');
    }

    // Re-apply gutter now that diff data is available (fixes race with async load)
    if (currentFile.endsWith('.md')) {
      applyGitGutter();
      updateGitChangeMap();
    } else {
      applyCodeGitGutter();
    }
  } catch {}
}

function applyGitGutter() {
  if (!currentDiffInfo) return;
  const info = currentDiffInfo;
  if (!info.addedLines?.length && !info.changedLines?.length && !info.deletedAt?.length) return;

  const addedSet = new Set(info.addedLines || []);
  const changedSet = new Set(info.changedLines || []);
  const deletedSet = new Set(info.deletedAt || []);

  // Use data-source-line attributes injected by the server for precise mapping.
  // Each top-level element has data-source-line="N" indicating its source line.
  const children = Array.from(content.children) as HTMLElement[];
  if (children.length === 0) return;

  for (let i = 0; i < children.length; i++) {
    const child = children[i];
    const startLine = parseInt(child.dataset.sourceLine || '0', 10);
    if (startLine === 0) continue;

    // Block spans from this element's start line to the next element's start line
    const nextChild = children[i + 1] as HTMLElement | undefined;
    const endLine = nextChild?.dataset.sourceLine
      ? parseInt(nextChild.dataset.sourceLine, 10)
      : startLine + 10; // generous fallback for last block

    let hasAdded = false;
    let hasChanged = false;
    let hasDeleted = false;
    for (let l = startLine; l < endLine; l++) {
      if (addedSet.has(l)) hasAdded = true;
      if (changedSet.has(l)) hasChanged = true;
      if (deletedSet.has(l)) hasDeleted = true;
    }

    if (hasChanged) {
      child.classList.add('git-gutter-changed');
    } else if (hasAdded) {
      child.classList.add('git-gutter-added');
    } else if (hasDeleted) {
      child.classList.add('git-gutter-deleted');
    }
  }
}

// Git gutter for code file view (line-number-based, exact)
function applyCodeGitGutter() {
  if (!currentDiffInfo) return;
  const info = currentDiffInfo;
  const addedSet = new Set(info.addedLines || []);
  const changedSet = new Set(info.changedLines || []);
  const deletedSet = new Set(info.deletedAt || []);

  const pre = content.querySelector('pre');
  if (!pre) return;
  const code = pre.querySelector('code') || pre;
  const lines = code.querySelectorAll('.line');

  // Shiki wraps each line in a .line span. If not present, fall back to line-number gutter.
  if (lines.length > 0) {
    lines.forEach((line, idx) => {
      const lineNum = idx + 1;
      if (changedSet.has(lineNum)) {
        (line as HTMLElement).classList.add('git-line-changed');
      } else if (addedSet.has(lineNum)) {
        (line as HTMLElement).classList.add('git-line-added');
      }
      if (deletedSet.has(lineNum)) {
        (line as HTMLElement).classList.add('git-line-deleted-before');
      }
    });
  }

  // Also color the line number gutter
  const gutter = content.querySelector('.line-numbers');
  if (gutter) {
    const nums = gutter.querySelectorAll('span');
    nums.forEach((span, idx) => {
      const lineNum = idx + 1;
      if (changedSet.has(lineNum)) {
        (span as HTMLElement).style.borderLeft = '3px solid rgba(210, 153, 34, 0.7)';
        (span as HTMLElement).style.paddingLeft = '4px';
      } else if (addedSet.has(lineNum)) {
        (span as HTMLElement).style.borderLeft = '3px solid rgba(63, 185, 80, 0.7)';
        (span as HTMLElement).style.paddingLeft = '4px';
      }
      if (deletedSet.has(lineNum)) {
        (span as HTMLElement).style.borderBottom = '2px solid rgba(248, 81, 73, 0.5)';
      }
    });
  }
}

function updateGitChangeMap() {
  if (!currentDiffInfo) return;
  const info = currentDiffInfo;
  if (!info.addedLines?.length && !info.changedLines?.length) return;

  let map = document.getElementById('changeMap');
  if (!map) {
    map = document.createElement('div');
    map.id = 'changeMap';
    const wrapper = content.closest('.content-wrapper');
    if (wrapper) {
      (wrapper as HTMLElement).style.position = 'relative';
      wrapper.appendChild(map);
    }
  }
  map.style.display = 'block';

  // Add git markers (don't clear -- live-edit markers may also be present)
  const children = Array.from(content.children) as HTMLElement[];
  if (children.length === 0) return;

  const wrapper = content.closest('.content-wrapper');
  if (!wrapper) return;
  const contentHeight = content.scrollHeight;
  if (contentHeight <= 0) return;

  const gutterEls = content.querySelectorAll('.git-gutter-added, .git-gutter-changed');
  gutterEls.forEach(el => {
    const rect = el.getBoundingClientRect();
    const wrapperRect = wrapper.getBoundingClientRect();
    const offsetTop = rect.top - wrapperRect.top + wrapper.scrollTop;

    const marker = document.createElement('div');
    marker.className = 'change-marker change-marker-git';
    if (el.classList.contains('git-gutter-changed')) {
      marker.classList.add('change-marker-git-changed');
    }
    const topPct = (offsetTop / contentHeight) * 100;
    const heightPct = Math.max((rect.height / contentHeight) * 100, 0.5);
    marker.style.top = topPct + '%';
    marker.style.height = heightPct + '%';
    marker.addEventListener('click', () => {
      el.scrollIntoView({ behavior: 'smooth', block: 'center' });
    });
    map!.appendChild(marker);
  });
}

// ---- WebSocket (markdown files) ----

function connectWebSocket(file: string) {
  disconnectWebSocket();
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const wsUrl = protocol + '//' + location.host + '/ws?file=' + encodeURIComponent(file);
  ws = new WebSocket(wsUrl);

  ws.onopen = () => {
    statusDot.classList.remove('disconnected');
    statusText.textContent = 'Connected';
    wsRetries = 0;
  };

  ws.onclose = () => {
    statusDot.classList.add('disconnected');
    statusText.textContent = 'Disconnected';
    if (wsRetries < WS_MAX_RETRIES && currentFile === file && currentFile.endsWith('.md')) {
      wsRetries++;
      const delay = Math.min(2000 * Math.pow(2, wsRetries - 1), 30000);
      setTimeout(() => connectWebSocket(file), delay);
    }
  };

  ws.onmessage = (event) => {
    const wrapper = content.closest('.content-wrapper');
    const scrollTop = wrapper ? wrapper.scrollTop : 0;
    content.className = 'markdown-body';

    // Snapshot old block text for diffing
    const oldBlocks = previousBlocks;

    // Parse new HTML into a temp container to extract blocks
    const tmp = document.createElement('div');
    tmp.innerHTML = event.data;
    const newBlockEls = Array.from(tmp.children) as HTMLElement[];
    const newBlocks = newBlockEls.map(el => el.textContent || '');

    // Replace content
    content.innerHTML = event.data;
    enhanceMarkdown();

    // Diff and highlight changed blocks
    if (oldBlocks.length > 0) {
      markChangedBlocks(oldBlocks, newBlocks);
    }

    // Save current blocks for next diff
    previousBlocks = newBlocks;

    // Update change map
    updateChangeMap();

    if (wrapper) {
      requestAnimationFrame(() => { wrapper.scrollTop = scrollTop; });
    }
    fetchModTime(file);
    fetchDiffInfo(file);
  };
}

function disconnectWebSocket() {
  if (ws) {
    ws.onclose = null; // prevent reconnect
    ws.close();
    ws = null;
  }
}

// ---- Raw file viewing ----

async function loadRawFile(file: string) {
  statusDot.classList.remove('disconnected');
  statusText.textContent = 'Viewing';

  try {
    const resp = await fetch('/api/raw?file=' + encodeURIComponent(file));
    if (!resp.ok) {
      content.className = 'file-error';
      content.textContent = resp.status === 415 ? 'Binary file (cannot preview)' : 'Failed to load file';
      return;
    }

    const text = await resp.text();
    const lines = text.split('\n');
    const lineCount = lines.length;
    const size = formatSize(new Blob([text]).size);
    const ext = file.split('.').pop()?.toLowerCase() || '';
    const lang = LANG_MAP[ext] || LANG_MAP[file.split('/').pop()?.toLowerCase() || ''] || '';
    const langLabel = lang || 'Plain Text';

    content.className = 'code-view';

    // File header
    const header = `<div class="file-header">
      <span class="file-header-info">${lineCount} lines | ${size} | ${langLabel}</span>
    </div>`;

    let codeHtml: string;
    if (highlighter && lang && highlighter.getLoadedLanguages().includes(lang as any)) {
      const isDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
      const theme = isDark ? 'github-dark' : 'github-light';
      codeHtml = highlighter.codeToHtml(text, { lang, theme });
    } else {
      codeHtml = '<pre><code>' + escapeHtml(text) + '</code></pre>';
    }

    content.innerHTML = header + '<div class="code-body">' + codeHtml + '</div>';

    // Add line numbers and git gutter for code files
    addLineNumbers();
    applyCodeGitGutter();
  } catch (e) {
    content.className = 'file-error';
    content.textContent = 'Failed to load file';
  }
}

function addLineNumbers() {
  const pre = content.querySelector('pre');
  if (!pre) return;
  const code = pre.querySelector('code') || pre;
  const lines = code.innerHTML.split('\n');
  // Don't count trailing empty line
  const count = lines.length > 0 && lines[lines.length - 1] === '' ? lines.length - 1 : lines.length;

  const gutter = document.createElement('div');
  gutter.className = 'line-numbers';
  for (let i = 1; i <= count; i++) {
    gutter.innerHTML += `<span>${i}</span>\n`;
  }
  pre.style.display = 'flex';
  pre.insertBefore(gutter, pre.firstChild);
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return bytes + ' B';
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
  return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

// ---- Markdown enhancement ----

function enhanceMarkdown() {
  renderMathBlocks(); // ```math code blocks -> KaTeX
  renderMath();       // $...$ and $$...$$ inline/block
  renderAlerts();
  highlightCode();
  renderMermaid();
  buildTOC();
  applyGitGutter();
  updateGitChangeMap();

  // Intercept .md link clicks for in-app navigation
  content.querySelectorAll('a[href]').forEach(a => {
    const href = (a as HTMLAnchorElement).getAttribute('href') || '';
    const match = href.match(/^\/\?file=(.+)$/);
    if (match) {
      a.addEventListener('click', (e) => {
        e.preventDefault();
        navigateTo(decodeURIComponent(match[1]));
      });
    }
  });

  // Image lightbox
  content.querySelectorAll('img').forEach(img => {
    (img as HTMLElement).style.cursor = 'pointer';
    img.addEventListener('click', () => {
      const overlay = document.createElement('div');
      overlay.className = 'lightbox';
      overlay.innerHTML = `<img src="${(img as HTMLImageElement).src}">`;
      overlay.addEventListener('click', () => overlay.remove());
      document.body.appendChild(overlay);
    });
  });
}

// ```math blocks -> KaTeX display math
function renderMathBlocks() {
  content.querySelectorAll('code.language-math').forEach(code => {
    const pre = code.parentElement;
    if (!pre || pre.tagName !== 'PRE') return;
    const tex = code.textContent || '';
    try {
      pre.outerHTML = katex.renderToString(tex.trim(), { displayMode: true, throwOnError: false });
    } catch {}
  });
}

// Mermaid diagrams (lazy-loaded)
let mermaidLoaded = false;
async function renderMermaid() {
  const blocks = content.querySelectorAll('code.language-mermaid');
  if (blocks.length === 0) return;

  if (!mermaidLoaded) {
    const { default: mermaid } = await import('mermaid');
    const isDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
    mermaid.initialize({
      startOnLoad: false,
      theme: isDark ? 'dark' : 'default',
    });
    mermaidLoaded = true;
  }

  const { default: mermaid } = await import('mermaid');
  let i = 0;
  for (const code of blocks) {
    const pre = code.parentElement;
    if (!pre || pre.tagName !== 'PRE') continue;
    const source = code.textContent || '';
    try {
      const { svg } = await mermaid.render(`mermaid-${i++}`, source);
      const div = document.createElement('div');
      div.className = 'mermaid-diagram';
      div.innerHTML = svg;
      pre.replaceWith(div);
    } catch (e) {
      // Leave the code block as-is on parse error
    }
  }
}

// On-this-page TOC with scroll spy
function buildTOC() {
  const tocEl = document.getElementById('toc');
  if (!tocEl) return;

  // Skip h1 (page title), show h2 and h3 only
  const headings = content.querySelectorAll('h2, h3');
  if (headings.length < 2) {
    tocEl.innerHTML = '';
    return;
  }

  let html = '<div class="toc-title">On this page</div><ul class="toc-list">';
  headings.forEach((h, i) => {
    const level = parseInt(h.tagName[1]);
    const id = h.id || `heading-${i}`;
    if (!h.id) h.id = id;
    const indent = (level - 2) * 12; // h2=0, h3=12
    html += `<li style="padding-left:${indent}px"><a href="#${id}" class="toc-link">${h.textContent}</a></li>`;
  });
  html += '</ul>';
  tocEl.innerHTML = html;

  // Scroll spy
  const links = tocEl.querySelectorAll('.toc-link');
  const observer = new IntersectionObserver((entries) => {
    for (const entry of entries) {
      if (entry.isIntersecting) {
        links.forEach(l => l.classList.remove('toc-active'));
        const link = tocEl.querySelector(`a[href="#${entry.target.id}"]`);
        if (link) link.classList.add('toc-active');
      }
    }
  }, { rootMargin: '-20% 0px -80% 0px' });

  headings.forEach(h => observer.observe(h));
}

function renderMath() {
  let html = content.innerHTML;

  // Protect <code> and <pre> content from math replacement
  const protected_: string[] = [];
  html = html.replace(/<(code|pre)[^>]*>[\s\S]*?<\/\1>/gi, (match) => {
    protected_.push(match);
    return `\x00PROTECTED${protected_.length - 1}\x00`;
  });

  html = html.replace(/\$\$([\s\S]+?)\$\$/g, (_, tex: string) => {
    try { return katex.renderToString(tex.trim(), { displayMode: true, throwOnError: false }); }
    catch { return '$$' + tex + '$$'; }
  });

  html = html.replace(/(?<!\$)\$(?!\$)(.+?)(?<!\$)\$(?!\$)/g, (_, tex: string) => {
    try { return katex.renderToString(tex.trim(), { displayMode: false, throwOnError: false }); }
    catch { return '$' + tex + '$'; }
  });

  html = html.replace(/\x00PROTECTED(\d+)\x00/g, (_, i) => protected_[parseInt(i)]);
  content.innerHTML = html;
}

const ALERT_TYPES: Record<string, { color: string; label: string }> = {
  NOTE:      { color: '#0969da', label: 'Note' },
  TIP:       { color: '#1a7f37', label: 'Tip' },
  IMPORTANT: { color: '#8250df', label: 'Important' },
  WARNING:   { color: '#9a6700', label: 'Warning' },
  CAUTION:   { color: '#cf222e', label: 'Caution' },
};

function renderAlerts() {
  content.querySelectorAll('blockquote').forEach((bq) => {
    const firstP = bq.querySelector('p');
    if (!firstP) return;
    const text = firstP.innerHTML;
    const match = text.match(/^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]\s*(?:<br\s*\/?>)?\s*/i);
    if (!match) return;

    const type = match[1].toUpperCase();
    const info = ALERT_TYPES[type];
    if (!info) return;

    firstP.innerHTML = text.slice(match[0].length);
    if (!firstP.innerHTML.trim()) firstP.remove();

    bq.style.borderLeftColor = info.color;
    bq.style.padding = '8px 16px';
    const title = document.createElement('p');
    title.innerHTML = `<strong style="color: ${info.color}">${info.label}</strong>`;
    title.style.marginBottom = '4px';
    bq.prepend(title);
  });
}

function highlightCode() {
  if (!highlighter) return;

  const isDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
  const theme = isDark ? 'github-dark' : 'github-light';

  content.querySelectorAll('pre code').forEach((el) => {
    const code = el as HTMLElement;
    const langClass = Array.from(code.classList).find(c => c.startsWith('language-'));
    const lang = langClass?.replace('language-', '') || '';
    const text = code.textContent || '';

    if (!text.trim()) return;

    const loadedLangs = highlighter!.getLoadedLanguages();
    if (lang && loadedLangs.includes(lang as any)) {
      const html = highlighter!.codeToHtml(text, { lang, theme });
      const pre = code.parentElement;
      if (pre && pre.tagName === 'PRE') {
        pre.outerHTML = html;
      }
    }
  });
}

// ---- Diff highlighting ----

function markChangedBlocks(oldBlocks: string[], newBlocks: string[]) {
  const children = Array.from(content.children) as HTMLElement[];
  const lcs = computeLCS(oldBlocks, newBlocks);
  let oi = 0, ni = 0, li = 0;

  while (ni < newBlocks.length) {
    if (li < lcs.length && oi < oldBlocks.length
        && oldBlocks[oi] === lcs[li] && newBlocks[ni] === lcs[li]) {
      // All three match: unchanged block
      oi++; ni++; li++;
    } else if (oi < oldBlocks.length && (li >= lcs.length || oldBlocks[oi] !== lcs[li])) {
      // Old block doesn't match next LCS element: it was removed or changed
      if (newBlocks[ni] !== oldBlocks[oi]) {
        // Old block was changed to new block
        if (children[ni]) children[ni].classList.add('diff-changed');
        oi++; ni++;
      } else {
        // Old block was removed (next old doesn't match LCS, but new does match old -- skip old)
        oi++;
      }
    } else {
      // New block doesn't match next LCS element but old does: insertion
      if (children[ni]) children[ni].classList.add('diff-added');
      ni++;
    }
  }
}

function computeLCS(a: string[], b: string[]): string[] {
  const m = a.length, n = b.length;
  // For large docs, limit to avoid perf issues
  if (m * n > 500000) return [];
  const dp: number[][] = Array.from({ length: m + 1 }, () => new Array(n + 1).fill(0));
  for (let i = 1; i <= m; i++) {
    for (let j = 1; j <= n; j++) {
      dp[i][j] = a[i - 1] === b[j - 1] ? dp[i - 1][j - 1] + 1 : Math.max(dp[i - 1][j], dp[i][j - 1]);
    }
  }
  const result: string[] = [];
  let i = m, j = n;
  while (i > 0 && j > 0) {
    if (a[i - 1] === b[j - 1]) {
      result.unshift(a[i - 1]);
      i--; j--;
    } else if (dp[i - 1][j] > dp[i][j - 1]) {
      i--;
    } else {
      j--;
    }
  }
  return result;
}

// ---- Change map (minimap) ----

function updateChangeMap() {
  let map = document.getElementById('changeMap');
  if (!map) {
    map = document.createElement('div');
    map.id = 'changeMap';
    const wrapper = content.closest('.content-wrapper');
    if (wrapper) {
      (wrapper as HTMLElement).style.position = 'relative';
      wrapper.appendChild(map);
    }
  }

  map.innerHTML = '';
  const wrapper = content.closest('.content-wrapper');
  if (!wrapper) return;

  const contentHeight = content.scrollHeight;
  if (contentHeight <= 0) return;

  const changed = content.querySelectorAll('.diff-changed, .diff-added');
  if (changed.length === 0) {
    map.style.display = 'none';
    return;
  }
  map.style.display = 'block';

  changed.forEach(el => {
    const rect = el.getBoundingClientRect();
    const wrapperRect = wrapper.getBoundingClientRect();
    const offsetTop = rect.top - wrapperRect.top + wrapper.scrollTop;
    const height = rect.height;

    const marker = document.createElement('div');
    marker.className = 'change-marker';
    if (el.classList.contains('diff-added')) {
      marker.classList.add('change-marker-added');
    }
    // Position proportionally within the map
    const topPct = (offsetTop / contentHeight) * 100;
    const heightPct = Math.max((height / contentHeight) * 100, 0.5);
    marker.style.top = topPct + '%';
    marker.style.height = heightPct + '%';

    // Click to scroll to that block
    marker.addEventListener('click', () => {
      el.scrollIntoView({ behavior: 'smooth', block: 'center' });
    });

    map.appendChild(marker);
  });
}
window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
  // Re-render current content for new theme
  if (currentFile.endsWith('.md')) {
    highlightCode();
  } else if (currentFile) {
    loadRawFile(currentFile);
  }
});

// ---- Browser back/forward ----
window.addEventListener('popstate', () => {
  const file = new URLSearchParams(location.search).get('file') || '';
  if (file) navigateTo(file);
});

// ---- Toggle sidebar ----
(window as any).toggleSidebar = function() {
  sidebar.classList.toggle('collapsed');
};

// ---- Tree auto-refresh ----
let treeRefreshTimer: ReturnType<typeof setInterval>;
let lastTreeJSON = '';

function getExpandedPaths(): Set<string> {
  const expanded = new Set<string>();
  fileTree.querySelectorAll('.tree-dir').forEach(dir => {
    const children = dir.nextElementSibling as HTMLElement;
    if (children?.style.display !== 'none') {
      expanded.add((dir as HTMLElement).dataset.path!);
    }
  });
  return expanded;
}

function restoreExpandedPaths(expanded: Set<string>) {
  fileTree.querySelectorAll('.tree-dir').forEach(dir => {
    const path = (dir as HTMLElement).dataset.path!;
    const children = dir.nextElementSibling as HTMLElement;
    if (!children) return;
    const shouldExpand = expanded.has(path);
    children.style.display = shouldExpand ? 'block' : 'none';
    const toggle = dir.querySelector('.tree-toggle');
    if (toggle) toggle.textContent = shouldExpand ? '\u25BE' : '\u25B8';
  });
}

function startTreeRefresh() {
  treeRefreshTimer = setInterval(async () => {
    try {
      const resp = await fetch('/api/tree');
      const text = await resp.text();
      // Skip rebuild if tree data hasn't changed
      if (text === lastTreeJSON) return;
      lastTreeJSON = text;

      const expanded = getExpandedPaths();
      const tree: TreeEntry[] = JSON.parse(text);
      fileTree.innerHTML = '';
      renderTree(tree, fileTree, 0);
      restoreExpandedPaths(expanded);
      // Re-mark active file
      document.querySelectorAll('.tree-file').forEach(el => {
        el.classList.toggle('active', (el as HTMLElement).dataset.path === currentFile);
      });
    } catch {}
  }, 5000);
}

// ---- Sidebar resize ----

function initSidebarResize() {
  const handle = document.getElementById('sidebarResize');
  if (!handle) return;

  let startX = 0;
  let startWidth = 0;

  handle.addEventListener('mousedown', (e: MouseEvent) => {
    e.preventDefault();
    startX = e.clientX;
    startWidth = sidebar.offsetWidth;
    handle.classList.add('dragging');
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';

    const onMouseMove = (e: MouseEvent) => {
      const newWidth = startWidth + (e.clientX - startX);
      sidebar.style.width = Math.max(140, Math.min(500, newWidth)) + 'px';
    };
    const onMouseUp = () => {
      handle.classList.remove('dragging');
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
      document.removeEventListener('mousemove', onMouseMove);
      document.removeEventListener('mouseup', onMouseUp);
    };
    document.addEventListener('mousemove', onMouseMove);
    document.addEventListener('mouseup', onMouseUp);
  });
}

// ---- Init ----
async function init() {
  await initHighlighter();
  await loadTree();

  const fileParam = new URLSearchParams(location.search).get('file');
  const startFile = fileParam || document.title;
  if (startFile) {
    navigateTo(startFile);
  }

  startTreeRefresh();
  initSidebarResize();
}

init();
