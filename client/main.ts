import { createHighlighterCore, type HighlighterCore } from 'shiki/core';
import { createJavaScriptRegexEngine } from 'shiki/engine/javascript';
import katex from 'katex';

// Fine-grained theme imports (only the two we need)
import githubDark from 'shiki/themes/github-dark.mjs';
import githubLight from 'shiki/themes/github-light.mjs';

// Fine-grained language imports
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

const preview = document.getElementById('preview')!;
const statusDot = document.getElementById('statusDot')!;
const statusText = document.getElementById('statusText')!;

function connect() {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  // Pass the ?file= param to the WebSocket so the server knows which file to watch
  const fileParam = new URLSearchParams(location.search).get('file') || '';
  const wsUrl = protocol + '//' + location.host + '/ws' + (fileParam ? '?file=' + encodeURIComponent(fileParam) : '');
  const ws = new WebSocket(wsUrl);

  ws.onopen = () => {
    statusDot.classList.remove('disconnected');
    statusText.textContent = 'Connected';
  };

  ws.onclose = () => {
    statusDot.classList.add('disconnected');
    statusText.textContent = 'Disconnected';
    setTimeout(connect, 2000);
  };

  ws.onmessage = (event) => {
    preview.innerHTML = event.data;
    enhance();
  };
}

function enhance() {
  renderMath();
  renderAlerts();
  highlightCode();
}

function renderMath() {
  let html = preview.innerHTML;

  // Protect <code> and <pre> content from math replacement
  const protected_: string[] = [];
  html = html.replace(/<(code|pre)[^>]*>[\s\S]*?<\/\1>/gi, (match) => {
    protected_.push(match);
    return `\x00PROTECTED${protected_.length - 1}\x00`;
  });

  // Block math ($$...$$)
  html = html.replace(
    /\$\$([\s\S]+?)\$\$/g,
    (_, tex: string) => {
      try {
        return katex.renderToString(tex.trim(), { displayMode: true, throwOnError: false });
      } catch {
        return '$$' + tex + '$$';
      }
    }
  );

  // Inline math ($...$), avoiding $$
  html = html.replace(
    /(?<!\$)\$(?!\$)(.+?)(?<!\$)\$(?!\$)/g,
    (_, tex: string) => {
      try {
        return katex.renderToString(tex.trim(), { displayMode: false, throwOnError: false });
      } catch {
        return '$' + tex + '$';
      }
    }
  );

  // Restore protected blocks
  html = html.replace(/\x00PROTECTED(\d+)\x00/g, (_, i) => protected_[parseInt(i)]);
  preview.innerHTML = html;
}

// GitHub-style alerts: > [!NOTE], > [!TIP], > [!IMPORTANT], > [!WARNING], > [!CAUTION]
const ALERT_TYPES: Record<string, { icon: string; color: string; label: string }> = {
  NOTE:      { icon: '\u2139\uFE0F', color: '#0969da', label: 'Note' },
  TIP:       { icon: '\uD83D\uDCA1', color: '#1a7f37', label: 'Tip' },
  IMPORTANT: { icon: '\u2757',       color: '#8250df', label: 'Important' },
  WARNING:   { icon: '\u26A0\uFE0F', color: '#9a6700', label: 'Warning' },
  CAUTION:   { icon: '\uD83D\uDED1', color: '#cf222e', label: 'Caution' },
};

function renderAlerts() {
  preview.querySelectorAll('blockquote').forEach((bq) => {
    const firstP = bq.querySelector('p');
    if (!firstP) return;
    const text = firstP.innerHTML;
    const match = text.match(/^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]\s*<br\s*\/?>\s*/i)
      || text.match(/^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]\s*/i);
    if (!match) return;

    const type = match[1].toUpperCase();
    const info = ALERT_TYPES[type];
    if (!info) return;

    // Remove the alert marker from the text
    firstP.innerHTML = text.slice(match[0].length);
    if (!firstP.innerHTML.trim()) firstP.remove();

    // Style the blockquote
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

  preview.querySelectorAll('pre code').forEach((el) => {
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

// Re-highlight when system theme changes
window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
  highlightCode();
});

// Init
initHighlighter().then(() => {
  connect();
});
