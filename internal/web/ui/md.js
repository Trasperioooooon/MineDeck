/* MineDeck 助手回复的 Markdown。自己写，不引库，也不做代码高亮。
   先把整段转义成文本，再认写法：标题、斜体、加粗、删除线、链接、代码、引用、表格、列表。
   链接只放行 http(s)，不渲染图片。合法链接先整体换成占位再做行内替换，标签写不进 href。
   浏览器里挂到 MDMarkdown；Node 测试用 module.exports。不依赖 DOM。 */
(function (root, factory) {
  const api = factory();
  if (typeof module === "object" && module.exports) module.exports = api;
  root.MDMarkdown = api;
})(typeof globalThis !== "undefined" ? globalThis : this, function () {
  "use strict";

  const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));

  /* 外层：整段只在这里转义一遍，再切成行。\u0001 留给行内占位用，源文里有的先去掉。
     回复的内容不全由模型决定（它会引用日志、玩家的聊天），所以解析要扛得住故意构造的输入：
     正则都是线性的、嵌套有上限；万一还是出了异常，整段按转义过的纯文本显示，不让界面卡死或空白。 */
  function parts(src) {
    const lines = esc(String(src ?? "").replace(/\u0001/g, "")).replace(/\r\n?/g, "\n").split("\n");
    try {
      return partsOfLines(lines, 0);
    } catch {
      return [{ sig: lines.join("\n"), html: `<p>${lines.join("<br>")}</p>` }];
    }
  }

  const MAX_NEST = 12; // 引用、强调最多嵌套几层；再往里按普通文字处理

  /* 内层：只处理已经转义过的行。引用块去掉行首的 > 之后递归调这里，不再转义第二遍。 */
  function partsOfLines(lines, depth) {
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
          const inner = depth < MAX_NEST ? partsOfLines(buf, depth + 1).map(p => p.html).join("") : `<p>${buf.map(inline).join("<br>")}</p>`;
          end = i; html = `<blockquote>${inner}</blockquote>`;
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

  /* 标题：「# 文字」，末尾成串的 # 去掉。分步来、不用一个大正则：
     原来的 /\s+(.+?)\s*#*\s*$/ 遇到「# a」加几千个空格再加一个字，回溯会把界面卡上好几秒。 */
  function heading(s) {
    const m = /^\s{0,3}(#{1,6})\s+/.exec(s);
    if (!m) return null;
    const text = s.slice(m[0].length).trimEnd();
    if (!text) return null;
    const closed = text.replace(/(^|\s)#+$/, "").trimEnd();
    return { n: m[1].length, text: closed || text };
  }
  /* 分隔线：三个以上同样的 - * _，中间可以有空格。逐字检查，避免正则在长串空格上回溯。 */
  function isHr(s) {
    const t = s.replace(/^\s{0,3}/, "");
    if (!t || !"-*_".includes(t[0])) return false;
    let n = 0;
    for (const c of t) {
      if (c === t[0]) n++;
      else if (!/\s/.test(c)) return false;
    }
    return n >= 3;
  }
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
  const MAX_COLS = 64, MAX_CELLS = 20000; // 表格最多画多少列、多少个格子（含表头）
  function isTable(lines, i) { return lines[i].includes("|") && i + 1 < lines.length && isSep(lines[i + 1]); }
  function readTable(lines, i) {
    const head = splitCells(lines[i]);
    const align = splitCells(lines[i + 1]).map(alignOf);
    const rows = [];
    let k = i + 2;
    for (; k < lines.length && lines[k].trim() && lines[k].includes("|"); k++) rows.push(splitCells(lines[k]));
    // 每行都按表头补齐格子：几千列的表头配几千行「|x|」，十几 KB 的输入能变出几百万个格子。
    // 列太多或格子总数太大就不画表格，按原样（已经转义过）一行一行显示
    if (head.length > MAX_COLS || head.length * (rows.length + 1) > MAX_CELLS) {
      return { end: k, html: `<p>${lines.slice(i, k).map(inline).join("<br>")}</p>` };
    }
    const th =(c, n) => `<th${align[n] && align[n] !== "left" ? ` style="text-align:${align[n]}"` : ""}>${inline(c)}</th>`;
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
        // 连着的空行只记一次、一次跳过（逐行 i++ 的话每个空行都要把后面的空行重扫一遍，几万个空行就是平方级）
        items[items.length - 1].chunks.push({ type: "blank" });
        i = j;
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
    // 同一项里空行隔开的两段之间空一行（原来直接拼在一起，「a」「续行」成了「a续行」）
    let html = "", para = [], gap = false, lastText = false;
    const flush = () => {
      if (para.length) { html += (gap && lastText ? "<br><br>" : "") + para.map(inline).join("<br>"); para = []; lastText = true; }
      gap = false;
    };
    for (const c of chunks) {
      if (c.type === "blank") { flush(); gap = true; }
      else if (c.type === "text") para.push(c.text);
      else { flush(); html += c.html; lastText = false; }
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

  /* ---------- 行内：先切代码；合法链接整体换成占位，再对整段做加粗 / 斜体 / 删除线，最后换回。__ 不当加粗 ----------
     占位写成 \u0001序号\u0001（源文里的 \u0001 在 parts() 入口已经去掉，撞不上正文）。
     片段放进去之前先把它里面的占位换回，所以每个片段都是换干净的，最后换一遍就够，不会漏在外面。 */
  function inline(s) { return splitCode(s).map(p => p.code ? `<code>${p.text}</code>` : inlineRest(p.text)).join(""); }

  const SLOT = /\u0001(\d+)\u0001/g;

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
    // 一行几千字（多半是贴进来的日志）就不认链接、加粗、斜体了：成串没收尾的 [](、*、_ 会让查找变成平方级，
    // 这样的行原样显示（已经转义过）最稳
    if (s.length > MAX_STYLE_LINE) return s;
    const slots = [];
    const fill = t => t.replace(SLOT, (_, n) => slots[+n]);
    const put = html => { slots.push(fill(html)); return "\u0001" + (slots.length - 1) + "\u0001"; };
    // 下一个 ] 的位置记着用：一长串 [ 时不用每个都从头往后找一遍（否则是平方级）
    let close = 0;
    const closeAfter = k => { if (close !== -1 && close <= k) close = s.indexOf("]", k + 1); return close; };
    let out = "", i = 0;
    while (i < s.length) {
      if (s[i] === "[" && (i === 0 || s[i - 1] !== "!")) {
        const link = readLink(s, i, closeAfter(i));
        if (link) {
          // 链接文字里的强调照常；文字是空的就显示网址本身（已经转义过，不再认写法）
          const text = link.text.trim() ? inlineRest(link.text) : link.href;
          out += put(`<a href="${link.href}" target="_blank" rel="noopener noreferrer">${text}</a>`);
          i = link.end;
          continue;
        }
      }
      const j = s.indexOf("[", i + 1);
      out += s.slice(i, j < 0 ? s.length : j);
      i = j < 0 ? s.length : j;
    }
    return fill(style(out, put));
  }

  const MAX_STYLE_LINE = 4000;
  const MAX_URL = 2048; // 网址最长认多少字；再长的不当链接，免得每个 [ 都一路扫到行尾

  /* URL 可以含成对括号。协议只认 http / https（转义之后仍是这两个开头）。 */
  function readLink(s, i, close) {
    if (close < 0 || s[close + 1] !== "(") return null;
    const inner = s.indexOf("[", i + 1);
    if (inner >= 0 && inner < close) return null;
    // 开头不是 http(s):// 的直接不认，不用往后扫
    if (!/^https?:\/\//i.test(s.slice(close + 2, close + 10))) return null;
    const from = close + 2, stop = Math.min(s.length, from + MAX_URL + 1); // 多看一个字：第 2048 字后面的「)」也要认
    let depth = 1, j = from;
    for (; j < stop; j++) {
      const c = s[j];
      if (c === "(") depth++;
      else if (c === ")") { depth--; if (depth === 0) break; }
      else if (c === " " || c === "\n" || c === "<" || c === ">" || c === '"') return null;
    }
    if (depth !== 0) return null;
    return { text: s.slice(i + 1, close), href: s.slice(from, j), end: j + 1 };
  }

  /* 加粗、斜体、删除线。认出来的片段经 put 换成占位，包在里面的再过一遍（外层斜体里的加粗、删除线都还在）。
     * 斜体：结束的 * 前面不能是空白；开头的 * 后面紧跟「.后缀」的是通配写法（*.jar、*.zip、*.log），不当斜体。
     _ 斜体：前后挨着字母或数字（含中文）的 _ 是名字的一部分（server_properties_file、配置_文件_名），不当斜体。 */
  function style(s, put, depth = 0) {
    if (depth >= MAX_NEST) return s; // 一长串 * 会一层套一层，不设上限会把调用栈撑爆
    const d = depth + 1;
    s = s.replace(/\*\*(?=\S)([\s\S]+?)\*\*(?!\*)/g, (_, t) => put("<b>" + style(t, put, d) + "</b>"));
    s = s.replace(/(?<!\*)\*(?!\*)(?!\.[A-Za-z0-9])(?=\S)([\s\S]+?)(?<=\S)\*(?!\*)/g, (_, t) => put("<em>" + style(t, put, d) + "</em>"));
    s = s.replace(/(?<![\p{L}\p{N}_])_(?!_)(?=\S)([\s\S]+?)(?<=\S)_(?![\p{L}\p{N}_])/gu, (_, t) => put("<em>" + style(t, put, d) + "</em>"));
    s = s.replace(/~~(?=\S)([\s\S]+?)~~/g, (_, t) => put("<s>" + style(t, put, d) + "</s>"));
    return s;
  }

  return { parts, render };
});
