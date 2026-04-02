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

interface TreeEntry {
  name: string;
  path: string;
  isDir: boolean;
  status?: string; // M=modified, A=added, ?=untracked, D=deleted
  children?: TreeEntry[];
}

const STATUS_COLORS: Record<string, string> = {
  'M': '#d29922', // yellow - modified
  'A': '#3fb950', // green - added
  '?': '#8b949e', // gray - untracked
  'D': '#f85149', // red - deleted
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

// ---- WebSocket (markdown files) ----

function connectWebSocket(file: string) {
  disconnectWebSocket();
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const wsUrl = protocol + '//' + location.host + '/ws?file=' + encodeURIComponent(file);
  ws = new WebSocket(wsUrl);

  ws.onopen = () => {
    statusDot.classList.remove('disconnected');
    statusText.textContent = 'Connected';
  };

  ws.onclose = () => {
    statusDot.classList.add('disconnected');
    statusText.textContent = 'Disconnected';
  };

  ws.onmessage = (event) => {
    content.className = 'markdown-body';
    content.innerHTML = event.data;
    enhanceMarkdown();
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

    // Add line numbers
    addLineNumbers();
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
  renderMath();
  renderAlerts();
  highlightCode();

  // Intercept .md link clicks for in-app navigation
  content.querySelectorAll('a[href]').forEach(a => {
    const href = (a as HTMLAnchorElement).getAttribute('href') || '';
    // Match /?file=... links (our rewritten relative .md links)
    const match = href.match(/^\/\?file=(.+)$/);
    if (match) {
      a.addEventListener('click', (e) => {
        e.preventDefault();
        navigateTo(decodeURIComponent(match[1]));
      });
    }
  });
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

// ---- Theme changes ----
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
function startTreeRefresh() {
  treeRefreshTimer = setInterval(async () => {
    // Silently reload tree to pick up git status changes
    try {
      const resp = await fetch('/api/tree');
      const tree: TreeEntry[] = await resp.json();
      fileTree.innerHTML = '';
      renderTree(tree, fileTree, 0);
      // Re-mark active file
      document.querySelectorAll('.tree-file').forEach(el => {
        el.classList.toggle('active', (el as HTMLElement).dataset.path === currentFile);
      });
    } catch {}
  }, 3000);
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
}

init();
