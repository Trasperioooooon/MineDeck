// 极简 Markdown 渲染：
// 1) Release 笔记使用 inline/render/sanitize/summary（保持现有标题偏移与白名单行为）；
// 2) 文档中心使用 parseFrontMatter/parseDoc/renderParsedDoc/sanitizeDoc（支持 front-matter、提示块、标题锚点、图片、相对 .md 转换、代码块复制与轻量高亮）。
// 原文里的 HTML 一律转义，不透传。

export const esc = (s) => String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

function safeUrl(u) {
  u = String(u || '').trim();
  if (/^https?:\/\//i.test(u) || u.startsWith('#') || u.startsWith('/') || u.startsWith('./') || u.startsWith('../')) return u;
  return null;
}

export function inline(src) {
  const codes = [];
  let s = src.replace(/`([^`]+)`/g, (_, c) => { codes.push(c); return `\u0000${codes.length - 1}\u0000`; });
  s = esc(s);
  s = s.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (m, t, u) => {
    const url = safeUrl(u.replace(/&amp;/g, '&'));
    return url ? `<a href="${esc(url)}">${t}</a>` : t;
  });
  s = s.replace(/(^|[\s(（：:])(https?:\/\/[^\s<)）]+)/g, (m, pre, u) => `${pre}<a href="${u}">${u}</a>`);
  s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  s = s.replace(/(^|[^*])\*([^*\s][^*]*)\*/g, '$1<em>$2</em>');
  s = s.replace(/\\([\\`*_{}\[\]()#+\-.!|])/g, '$1');
  s = s.replace(/\u0000(\d+)\u0000/g, (_, i) => `<code>${esc(codes[+i])}</code>`);
  return s;
}

export function render(md) {
  const lines = (md || '').replace(/\r\n?/g, '\n').split('\n');
  return renderLines(lines);
}

function renderLines(lines) {
  const out = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (!line.trim()) { i++; continue; }
    if (/^\s*```/.test(line)) {
      const buf = []; i++;
      while (i < lines.length && !/^\s*```/.test(lines[i])) buf.push(lines[i++]);
      i++;
      out.push(`<pre><code>${esc(buf.join('\n'))}</code></pre>`);
      continue;
    }
    const h = line.match(/^(#{1,6})\s+(.*)$/);
    if (h) { const lv = Math.min(6, h[1].length + 2); out.push(`<h${lv}>${inline(h[2].trim())}</h${lv}>`); i++; continue; }
    if (/^\s*>/.test(line)) {
      const buf = [];
      while (i < lines.length && /^\s*>/.test(lines[i])) buf.push(lines[i++].replace(/^\s*>\s?/, ''));
      out.push(`<blockquote>${renderLines(buf)}</blockquote>`);
      continue;
    }
    if (/^\s*\|/.test(line) && i + 1 < lines.length && /^\s*\|?\s*:?-{3,}/.test(lines[i + 1])) {
      const cells = (l) => l.trim().replace(/^\||\|$/g, '').split('|').map((c) => c.trim());
      const head = cells(line); i += 2;
      const rows = [];
      while (i < lines.length && /^\s*\|/.test(lines[i])) rows.push(cells(lines[i++]));
      out.push('<div class="tbl"><table><thead><tr>' + head.map((c) => `<th>${inline(c)}</th>`).join('') + '</tr></thead><tbody>' +
        rows.map((r) => '<tr>' + r.map((c) => `<td>${inline(c)}</td>`).join('') + '</tr>').join('') + '</tbody></table></div>');
      continue;
    }
    if (/^\s*([-*+]|\d+\.)\s+/.test(line)) {
      const r = parseList(lines, i, (sub) => renderLines(sub)); out.push(r.html); i = r.next; continue;
    }
    const buf = [];
    while (i < lines.length && lines[i].trim() && !/^\s*([-*+]|\d+\.)\s+|^\s*>|^#{1,6}\s|^\s*```|^\s*\|/.test(lines[i])) buf.push(lines[i++].trim());
    if (!buf.length) { buf.push(lines[i++].trim()); }
    out.push(`<p>${buf.map(inline).join('<br>')}</p>`);
  }
  return out.join('\n');
}

function parseList(lines, i, renderSub) {
  const indentOf = (l) => l.match(/^\s*/)[0].length;
  const base = indentOf(lines[i]);
  const ordered = /^\s*\d+\./.test(lines[i]);
  const items = [];
  while (i < lines.length) {
    const l = lines[i];
    if (!l.trim()) {
      if (i + 1 < lines.length && indentOf(lines[i + 1]) >= base && /^\s*([-*+]|\d+\.)\s+/.test(lines[i + 1])) { i++; continue; }
      break;
    }
    const ind = indentOf(l);
    const m = l.match(/^\s*([-*+]|\d+\.)\s+(.*)$/);
    if (m && ind === base) { items.push({ text: [m[2]], sub: [] }); i++; continue; }
    if (ind > base && items.length) { items[items.length - 1].sub.push(l.slice(base + 2)); i++; continue; }
    if (!m && items.length && ind >= base) { items[items.length - 1].text.push(l.trim()); i++; continue; }
    break;
  }
  const tag = ordered ? 'ol' : 'ul';
  const html = `<${tag}>` + items.map((it) => `<li>${inline(it.text.join(' '))}${it.sub.length ? renderSub(it.sub) : ''}</li>`).join('') + `</${tag}>`;
  return { html, next: i };
}

const ALLOWED = new Set(['p', 'br', 'strong', 'em', 'code', 'pre', 'ul', 'ol', 'li', 'blockquote', 'h3', 'h4', 'h5', 'h6', 'table', 'thead', 'tbody', 'tr', 'th', 'td', 'a', 'div']);
export function sanitize(html) {
  return html.replace(/<\/?([a-zA-Z0-9]+)([^>]*)>/g, (m, tag, attrs) => {
    tag = tag.toLowerCase();
    if (!ALLOWED.has(tag)) return esc(m);
    const closing = m.startsWith('</');
    if (closing) return `</${tag}>`;
    if (tag === 'a') {
      const h = attrs.match(/href="([^"]*)"/);
      const url = h && safeUrl(h[1].replace(/&amp;/g, '&'));
      return url ? `<a href="${esc(url)}" rel="noopener nofollow">` : '<a>';
    }
    if (tag === 'div') return /class="tbl"/.test(attrs) ? '<div class="tbl">' : '<div>';
    return `<${tag}>`;
  });
}

export function summary(md, max = 110) {
  const lines = (md || '').replace(/\r\n?/g, '\n').split('\n');
  const paras = []; let buf = [];
  for (const l of lines) {
    if (!l.trim()) { if (buf.length) { paras.push(buf.join('')); buf = []; } continue; }
    if (/^\s*(>|#|\||[-*+]\s|\d+\.\s|```)/.test(l)) { if (buf.length) { paras.push(buf.join('')); buf = []; } continue; }
    buf.push(l.trim());
  }
  if (buf.length) paras.push(buf.join(''));
  const usable = paras.filter((x) => !/^\*\*Full Changelog\*\*/.test(x));
  if (!usable.length) return '';
  let p = usable[0];
  if (p.length < 40 && usable[1]) p += usable[1];
  p = p.replace(/\[([^\]]+)\]\([^)]+\)/g, '$1').replace(/[*`]/g, '');
  return p.length > max ? p.slice(0, max - 1) + '…' : p;
}

// =========================================================================
// 文档中心专用解析与渲染（零依赖）
// =========================================================================

function parseScalar(raw) {
  const s = raw.trim();
  if (s === 'true') return true;
  if (s === 'false') return false;
  if (/^-?\d+(?:\.\d+)?$/.test(s)) return Number(s);
  if ((s.startsWith('"') && s.endsWith('"')) || (s.startsWith("'") && s.endsWith("'"))) {
    if (s.startsWith("'")) return s.slice(1, -1).replace(/''/g, "'");
    return s.slice(1, -1).replace(/\\(["\\])/g, '$1');
  }
  return s;
}

function parseInlineList(raw, errors, lineNo) {
  const inner = raw.slice(1, -1).trim();
  if (!inner) return [];
  const items = [];
  let cur = '', quote = '';
  for (let i = 0; i < inner.length; i++) {
    const ch = inner[i];
    if (quote) {
      if (ch === '\\' && quote === '"' && i + 1 < inner.length) { cur += inner[++i]; continue; }
      if (ch === quote) { quote = ''; cur += ch; continue; }
      cur += ch;
      continue;
    }
    if (ch === '"' || ch === "'") { quote = ch; cur += ch; continue; }
    if (ch === ',') {
      if (cur.trim()) items.push(String(parseScalar(cur)));
      cur = '';
      continue;
    }
    cur += ch;
  }
  if (quote) errors.push({ line: lineNo, message: 'front-matter 数组引号未闭合' });
  if (cur.trim()) items.push(String(parseScalar(cur)));
  return items;
}

/** 轻量 YAML-like front-matter 解析，记录字段行号以便报错精准定位 */
export function parseFrontMatter(rawSrc) {
  const text = String(rawSrc || '').replace(/^\uFEFF/, '').replace(/\r\n?/g, '\n');
  const all = text.split('\n');
  const errors = [];
  if (all[0]?.trim() !== '---') {
    errors.push({ line: 1, message: '文档开头缺少 --- front-matter 分隔线' });
    return { data: {}, fmLines: {}, body: text, bodyStartLine: 1, errors };
  }
  let close = -1;
  for (let i = 1; i < all.length; i++) {
    if (all[i].trim() === '---') { close = i; break; }
  }
  if (close < 0) {
    errors.push({ line: 1, message: 'front-matter 缺少结束的 --- 分隔线' });
    return { data: {}, fmLines: {}, body: '', bodyStartLine: 1, errors };
  }
  const data = {};
  const fmLines = {};
  let listKey = null;
  for (let i = 1; i < close; i++) {
    const lineNo = i + 1;
    const raw = all[i];
    const trimmed = raw.trim();
    if (!trimmed || trimmed.startsWith('#')) continue;
    const li = raw.match(/^\s*-\s+(.*)$/);
    if (li && listKey) {
      data[listKey].push(String(parseScalar(li[1])));
      continue;
    }
    const kv = raw.match(/^([A-Za-z_][A-Za-z0-9_-]*)\s*:\s*(.*)$/);
    if (!kv) {
      errors.push({ line: lineNo, message: `front-matter 语法无法识别：${trimmed}` });
      listKey = null;
      continue;
    }
    const key = kv[1], valRaw = kv[2].trim();
    fmLines[key] = lineNo;
    if (valRaw === '') {
      data[key] = [];
      listKey = key;
    } else if (valRaw.startsWith('[') && valRaw.endsWith(']')) {
      data[key] = parseInlineList(valRaw, errors, lineNo);
      listKey = null;
    } else if (valRaw.startsWith('[')) {
      errors.push({ line: lineNo, message: `front-matter 字段 ${key} 数组格式缺少闭合 ]` });
      data[key] = [];
      listKey = null;
    } else {
      data[key] = parseScalar(valRaw);
      listKey = null;
    }
  }
  return { data, fmLines, body: all.slice(close + 1).join('\n'), bodyStartLine: close + 2, errors };
}

/** 生成确定性小标题锚点：支持 {#custom-id}，兼容中文与英文 */
export function slugifyHeading(raw, used) {
  let text = String(raw || '').trim();
  let customId = null;
  const cm = text.match(/\s*\{#([A-Za-z0-9_-]+)\}\s*$/);
  if (cm) {
    customId = cm[1];
    text = text.slice(0, cm.index).trim();
  }
  const plainText = text
    .replace(/`([^`]+)`/g, '$1')
    .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
    .replace(/[*~]/g, '')
    .trim();
  let base = customId || plainText
    .toLowerCase()
    .replace(/['"`“”‘’「」『』【】（）()《》<>:：,，.。!！?？/\\|、；;]+/g, ' ')
    .trim()
    .replace(/\s+/g, '-')
    .replace(/-+/g, '-');
  if (!base) base = 'section';
  let id = base;
  let n = 2;
  while (used.has(id)) id = `${base}-${n++}`;
  used.add(id);
  return { id, text: plainText, rawText: text };
}

function stripInlineMd(s) {
  return String(s || '')
    .replace(/!\[([^\]]*)\]\([^)]+\)/g, '$1')
    .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
    .replace(/`([^`]+)`/g, '$1')
    .replace(/\*\*([^*]+)\*\*/g, '$1')
    .replace(/\*([^*]+)\*/g, '$1')
    .replace(/~~([^~]+)~~/g, '$1')
    .trim();
}

function parseDocLines(lines, startLine, state) {
  const blocks = [];
  let i = 0;
  const lineAt = (idx) => startLine + idx;
  const isBlockStart = (l) => /^\s*(```|#{1,6}\s|>|\||([-*_]){3,}\s*$|([-*+]|\d+\.)\s+)/.test(l);

  while (i < lines.length) {
    const line = lines[i];
    if (!line.trim()) { i++; continue; }

    // 分隔线
    if (/^\s*([-*_])\s*\1\s*\1[\s\1]*$/.test(line.trim())) {
      blocks.push({ type: 'hr', line: lineAt(i) });
      i++;
      continue;
    }

    // 代码块
    const fence = line.match(/^\s*```([\w+-]*)/);
    if (fence) {
      const lang = (fence[1] || '').toLowerCase();
      const firstLine = lineAt(i);
      const buf = [];
      i++;
      while (i < lines.length && !/^\s*```\s*$/.test(lines[i])) buf.push(lines[i++]);
      if (i < lines.length) i++;
      blocks.push({ type: 'code', lang, code: buf.join('\n'), line: firstLine });
      continue;
    }

    // 小标题
    const hm = line.match(/^(#{1,6})\s+(.*)$/);
    if (hm) {
      const rawLv = hm[1].length;
      const lineNo = lineAt(i);
      if (rawLv === 1) state.h1Warnings.push(lineNo);
      const lv = Math.min(6, Math.max(2, rawLv));
      const h = slugifyHeading(hm[2], state.usedIds);
      state.headings.push({ level: lv, id: h.id, text: h.text, line: lineNo });
      state.headingIds.add(h.id);
      blocks.push({ type: 'heading', level: lv, id: h.id, text: h.text, rawText: h.rawText, line: lineNo });
      state.currentSec = { a: h.id, h: h.text, buf: [] };
      state.sections.push(state.currentSec);
      i++;
      continue;
    }

    // 提示块 / 普通引用块
    if (/^\s*>/.test(line)) {
      const firstLine = lineAt(i);
      const buf = [];
      while (i < lines.length && /^\s*>/.test(lines[i])) buf.push(lines[i++].replace(/^\s*>\s?/, ''));
      const alertMatch = buf[0]?.trim().match(/^\[!(NOTE|TIP|WARNING|CAUTION|IMPORTANT)\](?:\s+(.*))?$/i);
      if (alertMatch) {
        const rawKind = alertMatch[1].toLowerCase();
        const kind = rawKind === 'important' ? 'note' : rawKind;
        const rest = (alertMatch[2] || '').trim();
        let title = null;
        let innerLines = buf.slice(1);
        if (rest) {
          if (innerLines.some((x) => x.trim())) title = rest;
          else innerLines = [rest];
        }
        const innerBlocks = parseDocLines(innerLines, firstLine + 1, state);
        blocks.push({ type: 'callout', kind, title, blocks: innerBlocks, line: firstLine });
      } else {
        const innerBlocks = parseDocLines(buf, firstLine, state);
        blocks.push({ type: 'blockquote', blocks: innerBlocks, line: firstLine });
      }
      continue;
    }

    // 表格
    if (/^\s*\|/.test(line) && i + 1 < lines.length && /^\s*\|?\s*:?-{3,}/.test(lines[i + 1])) {
      const firstLine = lineAt(i);
      const cells = (l) => l.trim().replace(/^\||\|$/g, '').split('|').map((c) => c.trim());
      const head = cells(line);
      i += 2;
      const rows = [];
      while (i < lines.length && /^\s*\|/.test(lines[i])) {
        const rowLine = lineAt(i);
        rows.push({ cells: cells(lines[i++]), line: rowLine });
      }
      head.forEach((c) => { state.currentSec.buf.push(stripInlineMd(c)); collectInlineRefs(c, firstLine, state); });
      rows.forEach((r) => r.cells.forEach((c) => { state.currentSec.buf.push(stripInlineMd(c)); collectInlineRefs(c, r.line, state); }));
      blocks.push({ type: 'table', head, rows, line: firstLine });
      continue;
    }

    // 列表
    if (/^\s*([-*+]|\d+\.)\s+/.test(line)) {
      const res = parseDocList(lines, i, startLine, state);
      blocks.push(res.block);
      i = res.next;
      continue;
    }

    // 独立图片行 ![alt](src)
    const imgOnly = line.trim().match(/^!\[([^\]]*)\]\(([^)\s]+)\)$/);
    if (imgOnly && (i + 1 >= lines.length || !lines[i + 1].trim() || isBlockStart(lines[i + 1]))) {
      const lineNo = lineAt(i);
      state.images.push({ alt: imgOnly[1], src: imgOnly[2], line: lineNo });
      if (imgOnly[1]) state.currentSec.buf.push(imgOnly[1]);
      blocks.push({ type: 'figure', alt: imgOnly[1], src: imgOnly[2], line: lineNo });
      i++;
      continue;
    }

    // 段落
    const firstLine = lineAt(i);
    const pLines = [];
    while (i < lines.length && lines[i].trim() && !isBlockStart(lines[i])) {
      const curLineNo = lineAt(i);
      const t = lines[i++].trim();
      collectInlineRefs(t, curLineNo, state);
      state.currentSec.buf.push(stripInlineMd(t));
      pLines.push({ text: t, line: curLineNo });
    }
    if (!pLines.length) {
      const curLineNo = lineAt(i);
      const t = lines[i++].trim();
      collectInlineRefs(t, curLineNo, state);
      state.currentSec.buf.push(stripInlineMd(t));
      pLines.push({ text: t, line: curLineNo });
    }
    blocks.push({ type: 'paragraph', lines: pLines, line: firstLine });
  }
  return blocks;
}

function parseDocList(lines, i, startLine, state) {
  const indentOf = (l) => l.match(/^\s*/)[0].length;
  const base = indentOf(lines[i]);
  const ordered = /^\s*\d+\./.test(lines[i]);
  const firstLine = startLine + i;
  const items = [];
  while (i < lines.length) {
    const l = lines[i];
    if (!l.trim()) {
      if (i + 1 < lines.length && indentOf(lines[i + 1]) >= base && /^\s*([-*+]|\d+\.)\s+/.test(lines[i + 1])) { i++; continue; }
      break;
    }
    const ind = indentOf(l);
    const m = l.match(/^\s*([-*+]|\d+\.)\s+(.*)$/);
    const lineNo = startLine + i;
    if (m && ind === base) {
      collectInlineRefs(m[2], lineNo, state);
      state.currentSec.buf.push(stripInlineMd(m[2]));
      items.push({ text: m[2], line: lineNo, sub: [], subStart: lineNo + 1 });
      i++;
      continue;
    }
    if (ind > base && items.length) {
      if (!items[items.length - 1].sub.length) items[items.length - 1].subStart = lineNo;
      items[items.length - 1].sub.push(l.slice(Math.min(l.length, base + 2)));
      i++;
      continue;
    }
    if (!m && items.length && ind >= base) {
      collectInlineRefs(l.trim(), lineNo, state);
      state.currentSec.buf.push(stripInlineMd(l.trim()));
      items[items.length - 1].text += ' ' + l.trim();
      i++;
      continue;
    }
    break;
  }
  for (const it of items) {
    it.subBlocks = it.sub.length ? parseDocLines(it.sub, it.subStart, state) : [];
  }
  return { block: { type: 'list', ordered, items, line: firstLine }, next: i };
}

function collectInlineRefs(line, lineNo, state) {
  const scrubbed = line.replace(/`[^`]+`/g, '');
  for (const m of scrubbed.matchAll(/(!?)\[([^\]]*)\]\(([^)\s]+)\)/g)) {
    if (m[1] === '!') state.images.push({ alt: m[2], src: m[3], line: lineNo });
    else state.refs.push({ url: m[3], text: m[2], line: lineNo });
  }
}

/** 解析文档 Markdown：提取 AST 块、标题目录、外链/内链引用与分节纯文本 */
export function parseDoc(body, { bodyStartLine = 1 } = {}) {
  const lines = String(body || '').replace(/\r\n?/g, '\n').split('\n');
  const introSec = { a: '', h: '', buf: [] };
  const state = {
    usedIds: new Set(),
    headingIds: new Set(),
    headings: [],
    refs: [],
    images: [],
    h1Warnings: [],
    sections: [introSec],
    currentSec: introSec,
  };
  const blocks = parseDocLines(lines, bodyStartLine, state);
  const sections = state.sections
    .map((s) => ({ a: s.a, h: s.h, x: s.buf.join(' ').replace(/\s+/g, ' ').trim() }))
    .filter((s) => s.h || s.x);
  return { blocks, headings: state.headings, headingIds: state.headingIds, refs: state.refs, images: state.images, h1Warnings: state.h1Warnings, sections };
}

function highlightWith(code, re, wrap) {
  let out = '', last = 0;
  for (const m of code.matchAll(re)) {
    if (m.index > last) out += esc(code.slice(last, m.index));
    out += wrap(m);
    last = m.index + m[0].length;
  }
  if (last < code.length) out += esc(code.slice(last));
  return out;
}

function highlightCode(code, lang) {
  if (!lang || /^(text|txt|plain)$/.test(lang)) return esc(code);
  if (/^(json|jsonc)$/.test(lang)) {
    return highlightWith(code, /("(?:\\.|[^"\\])*")(\s*:)?|(\b(?:true|false|null)\b|\b-?\d+(?:\.\d+)?\b)|(\/\/[^\n]*)/g, (m) => {
      if (m[4]) return `<span class="tok-c">${esc(m[4])}</span>`;
      if (m[1] && m[2]) return `<span class="tok-k">${esc(m[1])}</span>${esc(m[2])}`;
      if (m[1]) return `<span class="tok-s">${esc(m[1])}</span>`;
      if (m[3]) return `<span class="tok-n">${esc(m[3])}</span>`;
      return esc(m[0]);
    });
  }
  if (/^(bash|sh|shell|powershell|ps1|cmd|bat)$/.test(lang)) {
    return highlightWith(code, /(#[^\n]*)|("(?:\\.|[^"\\])*"|'[^'\n]*')|\b(Get-FileHash|cd|git|node|java|curl|echo|set|if|then|fi|for|in|do|done)\b/g, (m) => {
      if (m[1]) return `<span class="tok-c">${esc(m[1])}</span>`;
      if (m[2]) return `<span class="tok-s">${esc(m[2])}</span>`;
      if (m[3]) return `<span class="tok-k">${esc(m[3])}</span>`;
      return esc(m[0]);
    });
  }
  if (/^(properties|ini|toml|yaml|yml)$/.test(lang)) {
    return highlightWith(code, /([#;][^\n]*)|(^\s*[\w.-]+(?=\s*[=:]))|(\b(?:true|false)\b|\b\d+\b)/gm, (m) => {
      if (m[1]) return `<span class="tok-c">${esc(m[1])}</span>`;
      if (m[2]) return `<span class="tok-k">${esc(m[2])}</span>`;
      if (m[3]) return `<span class="tok-n">${esc(m[3])}</span>`;
      return esc(m[0]);
    });
  }
  return esc(code);
}

function renderInlineDoc(src, lineNo, h) {
  const codes = [];
  let s = String(src || '').replace(/`([^`]+)`/g, (_, c) => { codes.push(c); return `\u0000${codes.length - 1}\u0000`; });
  s = esc(s);
  s = s.replace(/(!?)\[([^\]]*)\]\(([^)\s]+)\)/g, (m, bang, t, u) => {
    const rawU = u.replace(/&amp;/g, '&');
    if (bang === '!') {
      const img = h.resolveImage(rawU, lineNo);
      if (!img) return esc(t);
      const attrs = [
        `src="${esc(img.src)}"`,
        `alt="${t}"`,
        img.width ? `width="${img.width}"` : '',
        img.height ? `height="${img.height}"` : '',
        'loading="lazy"',
      ].filter(Boolean).join(' ');
      return `<img ${attrs}>`;
    }
    const r = h.resolveLink(rawU, lineNo);
    if (!r || !r.url) return t;
    const labelHtml = t.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    return r.ext
      ? `<a class="ext" href="${esc(r.url)}" data-ext target="_blank" rel="noopener nofollow">${labelHtml}<span class="sr">${esc(h.extSr || '')}</span></a>`
      : `<a href="${esc(r.url)}">${labelHtml}</a>`;
  });
  s = s.replace(/(^|[\s(（：:])(https?:\/\/[^\s<)）*]+)/g, (m, pre, u) => {
    const rawU = u.replace(/&amp;/g, '&');
    const r = h.resolveLink(rawU, lineNo);
    if (!r || !r.url) return m;
    return r.ext
      ? `${pre}<a class="ext" href="${esc(r.url)}" data-ext target="_blank" rel="noopener nofollow">${u}<span class="sr">${esc(h.extSr || '')}</span></a>`
      : `${pre}<a href="${esc(r.url)}">${u}</a>`;
  });
  s = s.replace(/\*\*(「[^*]+」)\*\*/g, '<strong class="ui-btn">$1</strong>');
  s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  s = s.replace(/(^|[^*])\*([^*\s][^*]*)\*/g, '$1<em>$2</em>');
  s = s.replace(/~~([^~]+)~~/g, '<del>$1</del>');
  s = s.replace(/\\([\\`*_{}\[\]()#+\-.!|])/g, '$1');
  s = s.replace(/\u0000(\d+)\u0000/g, (_, i) => `<code>${esc(codes[+i])}</code>`);
  return s;
}

function renderBlocksDoc(blocks, h) {
  return blocks.map((b) => {
    if (b.type === 'hr') return '<hr>';
    if (b.type === 'code') {
      const langLabel = b.lang ? `<span class="doc-code-lang">${esc(b.lang)}</span>` : '<span class="doc-code-lang">text</span>';
      const copyBtn = `<button type="button" class="copy-btn doc-copy" data-copy="${esc(b.code)}" aria-label="${esc(h.copyLabel)}" data-done="${esc(h.copiedLabel)}">${h.copyIcon}${h.doneIcon}</button>`;
      return `<div class="doc-code">${`<div class="doc-code-head">${langLabel}${copyBtn}</div>`}<pre><code>${highlightCode(b.code, b.lang)}</code></pre></div>`;
    }
    if (b.type === 'heading') {
      const inner = renderInlineDoc(b.rawText, b.line, h);
      return `<h${b.level} id="${esc(b.id)}"><span>${inner}</span><a class="h-anchor" href="#${esc(b.id)}" aria-label="${esc(b.text)}">#</a></h${b.level}>`;
    }
    if (b.type === 'callout') {
      const titleText = b.title || (h.calloutTitles && h.calloutTitles[b.kind]) || b.kind.toUpperCase();
      return `<div class="callout callout-${esc(b.kind)}" role="note"><p class="callout-title">${(h.calloutIcons && h.calloutIcons[b.kind]) || ''}<span>${esc(titleText)}</span></p><div class="callout-body">${renderBlocksDoc(b.blocks, h)}</div></div>`;
    }
    if (b.type === 'blockquote') return `<blockquote>${renderBlocksDoc(b.blocks, h)}</blockquote>`;
    if (b.type === 'table') {
      const ths = b.head.map((c) => `<th>${renderInlineDoc(c, b.line, h)}</th>`).join('');
      const trs = b.rows.map((r) => `<tr>${r.cells.map((c) => `<td>${renderInlineDoc(c, r.line, h)}</td>`).join('')}</tr>`).join('');
      return `<div class="tbl"><table><thead><tr>${ths}</tr></thead><tbody>${trs}</tbody></table></div>`;
    }
    if (b.type === 'list') {
      const tag = b.ordered ? 'ol' : 'ul';
      const lis = b.items.map((it) => `<li>${renderInlineDoc(it.text, it.line, h)}${it.subBlocks.length ? renderBlocksDoc(it.subBlocks, h) : ''}</li>`).join('');
      return `<${tag}>${lis}</${tag}>`;
    }
    if (b.type === 'figure') {
      const img = h.resolveImage(b.src, b.line);
      if (!img) return '';
      const attrs = [
        `src="${esc(img.src)}"`,
        `alt="${esc(b.alt || '')}"`,
        img.width ? `width="${img.width}"` : '',
        img.height ? `height="${img.height}"` : '',
        'loading="lazy"',
      ].filter(Boolean).join(' ');
      const cap = b.alt ? `<figcaption>${esc(b.alt)}</figcaption>` : '';
      return `<figure class="doc-fig"><img ${attrs}>${cap}</figure>`;
    }
    if (b.type === 'paragraph') {
      return `<p>${b.lines.map((pl) => renderInlineDoc(pl.text, pl.line, h)).join('<br>')}</p>`;
    }
    return '';
  }).join('\n');
}

const DOC_ALLOWED = new Set([
  'p', 'br', 'hr', 'strong', 'em', 'del', 'code', 'pre', 'span', 'div', 'ul', 'ol', 'li',
  'blockquote', 'h2', 'h3', 'h4', 'h5', 'h6', 'table', 'thead', 'tbody', 'tr', 'th', 'td',
  'a', 'figure', 'figcaption', 'img', 'button', 'svg', 'path', 'rect', 'g',
]);
const isSafeCls = (c) => /^(?:icon(?:-[\w-]+)?|tok-[a-z]|callout(?:-[a-z]+)?|doc-[\w-]+|i-(?:copy|check)|p-[ab]|tbl|ui-btn|ext|sr|h-anchor|copy-btn|tick|lamp(?:-\d)?)$/.test(c);

export function sanitizeDoc(html) {
  return html.replace(/<\/?([a-zA-Z0-9]+)([^>]*)>/g, (m, rawTag, attrs) => {
    const tag = rawTag.toLowerCase();
    if (!DOC_ALLOWED.has(tag)) return esc(m);
    if (m.startsWith('</')) return `</${tag}>`;
    if (tag === 'svg') {
      const vb = attrs.match(/viewBox="([^"]*)"/)?.[1] || '0 0 16 16';
      const cls = (attrs.match(/class="([^"]*)"/)?.[1] || '').split(/\s+/).filter(isSafeCls).join(' ');
      return `<svg class="${esc(cls)}" viewBox="${esc(vb)}" aria-hidden="true" focusable="false" shape-rendering="crispEdges">`;
    }
    if (tag === 'path' || tag === 'rect') {
      const keep = [];
      for (const name of ['d', 'x', 'y', 'width', 'height', 'fill', 'stroke', 'stroke-width', 'stroke-dasharray', 'stroke-dashoffset', 'stroke-linejoin', 'class', 'transform']) {
        const v = attrs.match(new RegExp(`(?:^|\\s)${name}="([^"]*)"`))?.[1];
        if (v != null) keep.push(`${name}="${v}"`);
      }
      return `<${tag}${keep.length ? ' ' + keep.join(' ') : ''}/>`;
    }
    if (tag === 'a') {
      const hrefVal = attrs.match(/(?:^|\s)href="([^"]*)"/)?.[1];
      const url = hrefVal && safeUrl(hrefVal.replace(/&amp;/g, '&'));
      const isExt = /\bdata-ext\b/.test(attrs);
      const cls = (attrs.match(/(?:^|\s)class="([^"]*)"/)?.[1] || '').split(/\s+/).filter(isSafeCls).join(' ');
      const aria = attrs.match(/(?:^|\s)aria-label="([^"]*)"/)?.[1];
      const extra = [
        url ? `href="${esc(url)}"` : '',
        cls ? `class="${esc(cls)}"` : '',
        aria ? `aria-label="${aria}"` : '',
        isExt ? 'data-ext target="_blank" rel="noopener nofollow"' : (url && /^https?:\/\//i.test(url) ? 'rel="noopener nofollow"' : ''),
      ].filter(Boolean).join(' ');
      return `<a${extra ? ' ' + extra : ''}>`;
    }
    if (tag === 'img') {
      const srcVal = attrs.match(/(?:^|\s)src="([^"]*)"/)?.[1];
      const src = srcVal && safeUrl(srcVal.replace(/&amp;/g, '&'));
      if (!src) return '';
      const alt = attrs.match(/(?:^|\s)alt="([^"]*)"/)?.[1] ?? '';
      const w = attrs.match(/(?:^|\s)width="(\d+)"/)?.[1];
      const h = attrs.match(/(?:^|\s)height="(\d+)"/)?.[1];
      return `<img src="${esc(src)}" alt="${alt}"${w ? ` width="${w}"` : ''}${h ? ` height="${h}"` : ''} loading="lazy">`;
    }
    if (tag === 'button') {
      const cls = (attrs.match(/(?:^|\s)class="([^"]*)"/)?.[1] || '').split(/\s+/).filter(isSafeCls).join(' ');
      const copy = attrs.match(/(?:^|\s)data-copy="([^"]*)"/)?.[1];
      const done = attrs.match(/(?:^|\s)data-done="([^"]*)"/)?.[1];
      const aria = attrs.match(/(?:^|\s)aria-label="([^"]*)"/)?.[1];
      return `<button type="button" class="${esc(cls)}"${copy != null ? ` data-copy="${copy}"` : ''}${done != null ? ` data-done="${done}"` : ''}${aria != null ? ` aria-label="${aria}"` : ''}>`;
    }
    const id = attrs.match(/(?:^|\s)id="([^"]*)"/)?.[1];
    const cls = (attrs.match(/(?:^|\s)class="([^"]*)"/)?.[1] || '').split(/\s+/).filter(isSafeCls).join(' ');
    const role = tag === 'div' && /\brole="note"/.test(attrs) ? 'role="note"' : '';
    const outAttrs = [id ? `id="${id}"` : '', cls ? `class="${esc(cls)}"` : '', role].filter(Boolean).join(' ');
    return `<${tag}${outAttrs ? ' ' + outAttrs : ''}>`;
  });
}

export function renderParsedDoc(parsed, helpers) {
  return sanitizeDoc(renderBlocksDoc(parsed.blocks, helpers));
}
