/* MineDeck 助手回复的 Markdown。自己写，不引库，也不做代码高亮。
   先把整段转义成文本，再认写法：标题、斜体、加粗、删除线、链接、代码、引用、表格、列表。
   链接只放行 http(s)，不渲染图片。行内替换在链接切开之后做，避免把标签写进 href。
   浏览器里挂到 MDMarkdown；Node 测试用 module.exports。不依赖 DOM。 */
(function (root, factory) {
  const api = factory();
  if (typeof module === "object" && module.exports) module.exports = api;
  root.MDMarkdown = api;
})(typeof globalThis !== "undefined" ? globalThis : this, function () {
  "use strict";

  const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));

  function parts(src) {
    const lines = esc(src).replace(/\r\n?/g, "\n").split("\n");
    const out = [];
    let i = 0;
    while (i < lines.length) {
      if (!lines[i].trim()) { i++; continue; }
      const start = i;
      let end, html;
      const fence = fenceOpen(lines[i]);
      if (fence) {
        const f = readFence(lines, i);
        end = f.end; html = f.html;
      } else {
        const h = heading(lines[i]);
        if (h) { end = i + 1; html = `<h${h.n} class="md-h md-h${h.n}">${inline(h.text)}</h${h.n}>`; }
        else if (isHr(lines[i])) { end = i + 1; html = "<hr>"; }
        else if (isQuote(lines[i])) {
          const buf = [];
          for (; i < lines.length && isQuote(lines[i]); i++) buf.push(lines[i].replace(/^\s{0,3}&gt;\s?/, ""));
          end = i; html = `<blockquote>${render(buf.join("\n"))}</blockquote>`;
        } else if (isTable(lines, i)) {
          const t = readTable(lines, i);
          end = t.end; html = t.html;
        } else if (listMarker(lines[i])) {
          const l = readList(lines, i, listMarker(lines[i]).ind);
          end = l.end; html = l.html;
        } else {
          const buf = [];
          for (; i < lines.length && lines[i].trim() && !blockStart(lines, i); i++) buf.push(lines[i].trim());
          end = i; html = `<p>${buf.map(inline).join("<br>")}</p>`;
        }
      }
      out.push({ sig: lines.slice(start, end).join("\n"), html });
      i = end;
    }
    return out;
  }

  function render(src) { return parts(src).map(p => p.html).join(""); }

  function blockStart(lines, i) {
    const s = lines[i];
    return !!(fenceOpen(s) || heading(s) || isHr(s) || isQuote(s) || listMarker(s) || isTable(lines, i));
  }

  function indentOf(s) {
    const m = /^( *)/.exec(s);
    return m ? m[1].length : 0;
  }

  /* ---------- 围栏代码：``` 和 ~~~；没闭合时把已收到的内容放进代码块，并标成还没写完 ---------- */
  function fenceOpen(s) {
    const m = /^ {0,3}(`{3,}|~{3,})(.*)$/.exec(s);
    if (!m) return null;
    const ch = m[1][0], info = m[2].trim();
    if (ch === "`" && info.includes("`")) return null;
    return { ch, len: m[1].length, info, ind: indentOf(s) };
  }
  function fenceClose(s, open) {
    const re = new RegExp("^ {0," + open.ind + "}(" + open.ch + "{" + open.len + ",})\\s*$");
    return re.test(s);
  }
  function langOf(info) {
    const w = (info || "").split(/\s+/)[0] || "";
    return /^[A-Za-z0-9_+#.+-]{1,32}$/.test(w) ? w : "";
  }
  function readFence(lines, i) {
    const open = fenceOpen(lines[i]);
    const buf = [];
    let k = i + 1, closed = false;
    for (; k < lines.length; k++) {
      if (fenceClose(lines[k], open)) { closed = true; k++; break; }
      buf.push(lines[k]);
    }
    const lang = langOf(open.info);
    const bar = `<div class="md-code-bar">${lang ? `<span class="md-lang">${lang}</span>` : `<span class="md-lang"></span>`}` +
      `${closed ? "" : `<span class="md-open-note">代码还没写完</span>`}<button type="button" class="md-copy">复制</button></div>`;
    const html = `<div class="md-code${closed ? "" : " md-open"}">${bar}<pre class="md-pre"><code>${buf.join("\n")}</code></pre></div>`;
    return { end: k, html };
  }

  function heading(s) {
    const m = /^\s{0,3}(#{1,6})\s+(.+?)\s*#*\s*$/.exec(s);
    return m ? { n: m[1].length, text: m[2] } : null;
  }
  const isHr = s => /^\s{0,3}([-*_])(?:\s*\1){2,}\s*$/.test(s);
  const isQuote = s => /^\s{0,3}&gt;(?:\s|$)/.test(s) || /^\s{0,3}&gt;$/.test(s);

  /* ---------- 表格：行内代码和 \| 不切；对齐行决定左右中 ---------- */
  function splitCells(line) {
    let s = line.trim();
    if (s.startsWith("|")) s = s.slice(1);
    if (s.endsWith("|") && !s.endsWith("\\|")) s = s.slice(0, -1);
    const cells = [];
    let cur = "", code = false;
    for (let i = 0; i < s.length; i++) {
      const c = s[i];
      if (c === "`") { code = !code; cur += c; continue; }
      if (!code && c === "\\" && s[i + 1] === "|") { cur += "|"; i++; continue; }
      if (!code && c === "|") { cells.push(cur.trim()); cur = ""; continue; }
      cur += c;
    }
    cells.push(cur.trim());
    return cells;
  }
  function alignOf(cell) {
    const s = cell.trim();
    if (!/^:?-{2,}:?$/.test(s)) return null;
    const L = s.startsWith(":"), R = s.endsWith(":");
    if (L && R) return "center";
    if (R) return "right";
    return "left";
  }
  function isSep(s) {
    if (!s.includes("|") && !/^[\s|:-]+$/.test(s)) return false;
    const cells = splitCells(s);
    return cells.length > 0 && cells.every(c => alignOf(c));
  }
  function isTable(lines, i) { return lines[i].includes("|") && i + 1 < lines.length && isSep(lines[i + 1]); }
  function readTable(lines, i) {
    const head = splitCells(lines[i]);
    const align = splitCells(lines[i + 1]).map(alignOf);
    const rows = [];
    let k = i + 2;
    for (; k < lines.length && lines[k].trim() && lines[k].includes("|"); k++) rows.push(splitCells(lines[k]));
    const th = (c, n) => `<th${align[n] && align[n] !== "left" ? ` style="text-align:${align[n]}"` : ""}>${inline(c)}</th>`;
    const td = (c, n) => `<td${align[n] && align[n] !== "left" ? ` style="text-align:${align[n]}"` : ""}>${inline(c || "")}</td>`;
    const html = `<div class="md-table"><table><thead><tr>${head.map(th).join("")}</tr></thead><tbody>` +
      rows.map(r => `<tr>${head.map((_, n) => td(r[n], n)).join("")}</tr>`).join("") + `</tbody></table></div>`;
    return { end: k, html };
  }

  /* ---------- 列表：懒续行、任务列表、列表里的代码块和嵌套列表 ---------- */
  function listMarker(s) {
    let m = /^(\s*)([-*+])\s+\[([ xX])\]\s+(.*)$/.exec(s);
    if (m) return { ind: m[1].length, ord: false, n: 0, task: true, on: m[3] !== " ", rest: m[4] };
    m = /^(\s*)([-*+])\s+(.*)$/.exec(s);
    if (m) return { ind: m[1].length, ord: false, n: 0, task: false, on: false, rest: m[3] };
    m = /^(\s*)(\d{1,9})[.)、]\s+(.*)$/.exec(s);
    if (m) return { ind: m[1].length, ord: true, n: +m[2] || 1, task: false, on: false, rest: m[3] };
    return null;
  }
  function readList(lines, i, minIndent) {
    const items = [];
    while (i < lines.length) {
      const line = lines[i];
      if (!line.trim()) {
        let j = i + 1;
        while (j < lines.length && !lines[j].trim()) j++;
        if (!items.length || j >= lines.length) break;
        const mk = listMarker(lines[j]);
        const ind = indentOf(lines[j]);
        const cont = (mk && mk.ind >= minIndent) || ind >= minIndent + 2 || (fenceOpen(lines[j]) && ind >= minIndent + 2);
        if (!cont) break;
        items[items.length - 1].chunks.push({ type: "blank" });
        i++;
        continue;
      }
      const mk = listMarker(line);
      if (mk && mk.ind >= minIndent) {
        if (items.length && mk.ind > items[items.length - 1].ind) {
          const nested = readList(lines, i, mk.ind);
          items[items.length - 1].chunks.push({ type: "html", html: nested.html });
          i = nested.end;
          continue;
        }
        if (items.length && mk.ind < minIndent) break;
        items.push({ ind: mk.ind, ord: mk.ord, n: mk.n, task: mk.task, on: mk.on, chunks: [{ type: "text", text: mk.rest }] });
        i++;
        continue;
      }
      if (!items.length) break;
      const item = items[items.length - 1];
      const ind = indentOf(line);
      if (fenceOpen(line) && ind >= item.ind + 2) {
        const f = readFence(lines, i);
        item.chunks.push({ type: "html", html: f.html });
        i = f.end;
        continue;
      }
      if (ind >= item.ind + 2) { item.chunks.push({ type: "text", text: line.trim() }); i++; continue; }
      if (!blockStart(lines, i)) { item.chunks.push({ type: "text", text: line.trim() }); i++; continue; }
      break;
    }
    return { end: i, html: renderItems(items) };
  }
  function chunksHTML(chunks) {
    let html = "", para = [];
    const flush = () => { if (para.length) { html += para.map(inline).join("<br>"); para = []; } };
    for (const c of chunks) {
      if (c.type === "blank") flush();
      else if (c.type === "text") para.push(c.text);
      else { flush(); html += c.html; }
    }
    flush();
    return html;
  }
  function renderItems(items) {
    let html = "", i = 0;
    while (i < items.length) {
      const ord = items[i].ord;
      const tag = ord ? "ol" : "ul";
      const start = ord && items[i].n > 1 ? ` start="${items[i].n}"` : "";
      html += `<${tag}${start}>`;
      while (i < items.length && !!items[i].ord === !!ord) {
        const it = items[i];
        const body = chunksHTML(it.chunks);
        if (it.task) html += `<li class="md-task"><label><input type="checkbox" disabled${it.on ? " checked" : ""}> <span>${body}</span></label></li>`;
        else html += `<li>${body}</li>`;
        i++;
      }
      html += `</${tag}>`;
    }
    return html;
  }

  /* ---------- 行内：先切代码，再切链接，最后才加粗 / 斜体 / 删除线。__ 不当加粗 ---------- */
  function inline(s) { return splitCode(s).map(p => p.code ? `<code>${p.text}</code>` : inlineRest(p.text)).join(""); }

  function splitCode(s) {
    const out = [];
    let i = 0;
    while (i < s.length) {
      const tick = s.indexOf("`", i);
      if (tick < 0) { out.push({ code: false, text: s.slice(i) }); break; }
      const end = s.indexOf("`", tick + 1);
      if (end < 0 || end === tick + 1) { out.push({ code: false, text: s.slice(i) }); break; }
      if (tick > i) out.push({ code: false, text: s.slice(i, tick) });
      out.push({ code: true, text: s.slice(tick + 1, end) });
      i = end + 1;
    }
    return out.filter(p => p.text !== "" || p.code);
  }

  function inlineRest(s) {
    let out = "", i = 0;
    while (i < s.length) {
      if (s[i] === "[" && (i === 0 || s[i - 1] !== "!")) {
        const link = readLink(s, i);
        if (link) { out += `<a href="${link.href}" target="_blank" rel="noopener noreferrer">${inlineRest(link.text)}</a>`; i = link.end; continue; }
      }
      let j = i + 1;
      while (j < s.length && !(s[j] === "[" && s[j - 1] !== "!")) j++;
      out += style(s.slice(i, j));
      i = j;
    }
    return out;
  }

  /* URL 可以含成对括号。协议只认 http / https（转义之后仍是这两个开头）。 */
  function readLink(s, i) {
    const close = s.indexOf("]", i + 1);
    if (close < 0 || s[close + 1] !== "(" || s.slice(i + 1, close).includes("[")) return null;
    let url = "", depth = 1, j = close + 2;
    for (; j < s.length; j++) {
      const c = s[j];
      if (c === "(") { depth++; url += c; }
      else if (c === ")") { depth--; if (depth === 0) { j++; break; } url += c; }
      else if (c === " " || c === "\n" || c === "<" || c === ">" || c === '"') return null;
      else url += c;
    }
    if (depth !== 0 || !/^https?:\/\//i.test(url)) return null;
    return { text: s.slice(i + 1, close), href: url, end: j };
  }

  function style(s) {
    const slots = [];
    const put = html => { slots.push(html); return "\u0001" + (slots.length - 1) + "\u0001"; };
    s = s.replace(/\*\*(?=\S)([\s\S]+?)\*\*(?!\*)/g, (_, t) => put("<b>" + style(t) + "</b>"));
    s = s.replace(/(?<!\*)\*(?!\*)(?=\S)([\s\S]+?)\*(?!\*)/g, (_, t) => put("<em>" + t + "</em>"));
    s = s.replace(/(?<![A-Za-z0-9_])_(?!_)(?=\S)([\s\S]+?)_(?![A-Za-z0-9_])/g, (_, t) => put("<em>" + t + "</em>"));
    s = s.replace(/~~(?=\S)([\s\S]+?)~~/g, (_, t) => put("<s>" + t + "</s>"));
    return s.replace(/\u0001(\d+)\u0001/g, (_, n) => slots[+n]);
  }

  return { parts, render };
});
