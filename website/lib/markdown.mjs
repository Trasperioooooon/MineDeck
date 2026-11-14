// 极简 Markdown 渲染，只覆盖 GitHub Release 说明里实际用到的语法：
// 标题、段落、无序/有序列表（可嵌套）、引用块、表格、代码块、行内代码、粗体、斜体、链接、裸 URL。
// 原文里的 HTML 一律转义，不透传。输出再经过 sanitize() 的标签白名单。

const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

function safeUrl(u) {
  u = u.trim();
  if (/^https?:\/\//i.test(u) || u.startsWith('#') || u.startsWith('/')) return u;
  return null;
}

export function inline(src) {
  // 先抽出行内代码，避免其中内容被继续处理
  const codes = [];
  let s = src.replace(/`([^`]+)`/g, (_, c) => { codes.push(c); return `\u0000${codes.length - 1}\u0000`; });
  s = esc(s);
  // 链接 [text](url)
  s = s.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (m, t, u) => {
    const url = safeUrl(u.replace(/&amp;/g, '&'));
    return url ? `<a href="${esc(url)}">${t}</a>` : t;
  });
  // 裸 URL（不在已生成的 href/标签里）
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
    // 代码块
    if (/^\s*```/.test(line)) {
      const buf = []; i++;
      while (i < lines.length && !/^\s*```/.test(lines[i])) buf.push(lines[i++]);
      i++;
      out.push(`<pre><code>${esc(buf.join('\n'))}</code></pre>`);
      continue;
    }
    // 标题：页面里 h2 是区块标题，说明里的标题整体降两级
    const h = line.match(/^(#{1,6})\s+(.*)$/);
    if (h) { const lv = Math.min(6, h[1].length + 2); out.push(`<h${lv}>${inline(h[2].trim())}</h${lv}>`); i++; continue; }
    // 引用
    if (/^\s*>/.test(line)) {
      const buf = [];
      while (i < lines.length && /^\s*>/.test(lines[i])) buf.push(lines[i++].replace(/^\s*>\s?/, ''));
      out.push(`<blockquote>${renderLines(buf)}</blockquote>`);
      continue;
    }
    // 表格
    if (/^\s*\|/.test(line) && i + 1 < lines.length && /^\s*\|?\s*:?-{3,}/.test(lines[i + 1])) {
      const cells = (l) => l.trim().replace(/^\||\|$/g, '').split('|').map((c) => c.trim());
      const head = cells(line); i += 2;
      const rows = [];
      while (i < lines.length && /^\s*\|/.test(lines[i])) rows.push(cells(lines[i++]));
      out.push('<div class="tbl"><table><thead><tr>' + head.map((c) => `<th>${inline(c)}</th>`).join('') + '</tr></thead><tbody>' +
        rows.map((r) => '<tr>' + r.map((c) => `<td>${inline(c)}</td>`).join('') + '</tr>').join('') + '</tbody></table></div>');
      continue;
    }
    // 列表
    if (/^\s*([-*+]|\d+\.)\s+/.test(line)) {
      const r = parseList(lines, i); out.push(r.html); i = r.next; continue;
    }
    // 段落
    const buf = [];
    while (i < lines.length && lines[i].trim() && !/^\s*([-*+]|\d+\.)\s+|^\s*>|^#{1,6}\s|^\s*```|^\s*\|/.test(lines[i])) buf.push(lines[i++].trim());
    if (!buf.length) { buf.push(lines[i++].trim()); }
    out.push(`<p>${buf.map(inline).join('<br>')}</p>`);
  }
  return out.join('\n');
}

function parseList(lines, i) {
  const indentOf = (l) => l.match(/^\s*/)[0].length;
  const base = indentOf(lines[i]);
  const ordered = /^\s*\d+\./.test(lines[i]);
  const items = [];
  while (i < lines.length) {
    const l = lines[i];
    if (!l.trim()) {
      // 空行后若仍是同级列表项则继续
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
  const html = `<${tag}>` + items.map((it) => `<li>${inline(it.text.join(' '))}${it.sub.length ? renderLines(it.sub) : ''}</li>`).join('') + `</${tag}>`;
  return { html, next: i };
}

// 标签白名单：只留下列标签，属性只留 a[href]（且必须是 http/https/#/相对），其余标签整段转义
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

// 纯文本摘要：取第一段不在引用块里的正文
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
  // 第一段太短（如「这是第一个正式版。」）时接上下一段
  if (p.length < 40 && usable[1]) p += usable[1];
  p = p.replace(/\[([^\]]+)\]\([^)]+\)/g, '$1').replace(/[*`]/g, '');
  return p.length > max ? p.slice(0, max - 1) + '…' : p;
}
