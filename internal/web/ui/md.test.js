/* 渲染器的行为测试。Node 自带 test runner，不引库：node --test md.test.js */
"use strict";
const { test } = require("node:test");
const assert = require("node:assert/strict");
const { render, parts, maxInput } = require("./md.js");

const has = (html, s) => assert.ok(html.includes(s), `缺少 ${s}\n${html}`);
const lacks = (html, s) => assert.ok(!html.includes(s), `不该有 ${s}\n${html}`);

test("先转义，不渲染图片，链接只认 http(s)", () => {
  const html = render(`<script>alert(1)</script>\n![x](https://evil.test/a.png)\n[ok](https://ex.com/a)\n[no](javascript:alert(1))\n[no2](data:text/html,hi)`);
  lacks(html, "<script>");
  lacks(html, "<img");
  has(html, "&lt;script&gt;");
  has(html, 'href="https://ex.com/a"');
  lacks(html, 'href="javascript:');
  lacks(html, 'href="data:');
  has(html, 'rel="noopener noreferrer"');
});

test("标题分级，__ 不加粗，有斜体", () => {
  const html = render("# 一\n## 二\n### 三\n#### 四\n##### 五\n###### 六\n**粗** *斜* _斜二_ __不是粗__ ~~删~~");
  has(html, '<h1 class="md-h md-h1">一</h1>');
  has(html, '<h2 class="md-h md-h2">二</h2>');
  has(html, '<h6 class="md-h md-h6">六</h6>');
  has(html, "<b>粗</b>");
  has(html, "<em>斜</em>");
  has(html, "<em>斜二</em>");
  has(html, "__不是粗__");
  lacks(html, "<b>不是粗</b>");
  has(html, "<s>删</s>");
});

test("先切链接，href 不被加粗或删除线改写；URL 可以含括号", () => {
  const html = render("[**x** ~~y~~](https://ex.com/a__b)\n[wiki](https://en.wikipedia.org/wiki/Foo_(bar))");
  has(html, 'href="https://ex.com/a__b"');
  has(html, "<b>x</b>");
  has(html, "<s>y</s>");
  has(html, 'href="https://en.wikipedia.org/wiki/Foo_(bar)"');
  lacks(html, "href=\"https://ex.com/a<b>");
});

test("代码块保留语言、有复制按钮、支持 ~~~，未闭合不吞掉前面的段落", () => {
  const html = render("前面\n```js\nconst a = 1\n```\n~~~py\nprint(1)\n~~~\n> 引用\n后面\n```sh\necho hi");
  has(html, '<span class="md-lang">js</span>');
  has(html, '<span class="md-lang">py</span>');
  has(html, "const a = 1");
  has(html, 'class="md-copy"');
  has(html, "前面");
  has(html, "<blockquote>");
  has(html, "引用");
  has(html, "后面");
  has(html, "代码还没写完");
  has(html, "echo hi");
  const ps = parts("前面\n```js\nconst a = 1");
  assert.equal(ps.length, 2);
  assert.ok(ps[0].html.includes("前面"));
  assert.ok(ps[1].html.includes("md-open"));
});

test("表格不切行内代码和转义竖线，对齐生效", () => {
  const html = render("| 左 | 中 | 右 |\n| :--- | :---: | ---: |\n| `a|b` | c\\|d | e |");
  has(html, "<code>a|b</code>");
  has(html, "c|d");
  has(html, 'style="text-align:center"');
  has(html, 'style="text-align:right"');
  assert.equal((html.match(/<td/g) || []).length, 3);
});

test("列表续行、任务列表、列表里的代码块", () => {
  const html = render("- 甲\n还是甲\n- [ ] 待办\n- [x] 完成\n- 乙\n  ```\n  code\n  ```\n  尾");
  has(html, "甲<br>还是甲");
  has(html, 'class="md-task"');
  has(html, 'type="checkbox" disabled');
  has(html, " checked");
  has(html, "code");
  has(html, "尾");
  const lis = html.match(/<li/g) || [];
  assert.equal(lis.length, 4);
  // 代码块不把列表拆开：整段只有一个 <ul>，代码块和「尾」都在最后那个 <li> 里
  assert.equal((html.match(/<ul>/g) || []).length, 1, html);
  assert.equal((html.match(/<\/ul>/g) || []).length, 1, html);
  assert.ok(html.startsWith("<ul>"), html);
  const last = html.slice(html.lastIndexOf("<li"));
  assert.match(last, /^<li>乙<div class="md-code">(?:(?!<\/?li)[\s\S])*<pre class="md-pre"><code>[^<]*code<\/code><\/pre><\/div>尾<\/li><\/ul>$/);
});

test("引用块里的文字只转义一遍，嵌套引用和引用里的代码块也对", () => {
  const html = render("> a & b < c");
  assert.equal(html, "<blockquote><p>a &amp; b &lt; c</p></blockquote>");
  lacks(html, "&amp;amp;");
  lacks(html, "&amp;lt;");
  assert.equal(render("> > b"), "<blockquote><blockquote><p>b</p></blockquote></blockquote>");
  assert.equal(render("> 外\n> > 里 \"x\" & y\n\n后面"), "<blockquote><p>外</p><blockquote><p>里 &quot;x&quot; &amp; y</p></blockquote></blockquote><p>后面</p>");
  const code = render("> ```\n> if (a < b && c > d) {}\n> ```");
  has(code, "<blockquote><div class=\"md-code\">");
  has(code, "<code>if (a &lt; b &amp;&amp; c &gt; d) {}</code>");
  lacks(code, "&amp;lt;");
  lacks(code, "&amp;amp;");
  // 引用里的 HTML 仍然只是文字
  const x = render("> <img src=x onerror=alert(1)>");
  lacks(x, "<img");
  has(x, "&lt;img src=x onerror=alert(1)&gt;");
});

test("斜体、删除线里套加粗：加粗还在，不露出占位用的控制字符", () => {
  const a = render("*x **y** z*");
  assert.equal(a, "<p><em>x <b>y</b> z</em></p>");
  const b = render("~~a **b** c~~");
  assert.equal(b, "<p><s>a <b>b</b> c</s></p>");
  const c = render("~~*a **b** c*~~ 和 _d **e**_");
  assert.equal(c, "<p><s><em>a <b>b</b> c</em></s> 和 <em>d <b>e</b></em></p>");
  assert.equal(render("*a ~~b~~ c*"), "<p><em>a <s>b</s> c</em></p>");
  for (const html of [a, b, c]) lacks(html, "\u0001");
});

test("源文里自带的占位控制字符先去掉，不会被换成别处的片段", () => {
  const html = render("x\u00010\u0001y **粗** [链](https://ex.com/a) \u00011\u0001");
  lacks(html, "\u0001");
  has(html, "x0y ");
  assert.equal((html.match(/<b>粗<\/b>/g) || []).length, 1, html);
  assert.equal((html.match(/<a /g) || []).length, 1, html);
  lacks(html, "undefined");
  const [p] = parts("a\u0001b");
  assert.equal(p.sig, "ab");
});

test("*.jar 这类通配写法不当斜体；中文里的下划线不当斜体；原有斜体写法照旧", () => {
  for (const src of ["删除 *.jar 和 *.zip 文件", "*.jar/*.zip/*.log", "把 *.jar、*.zip 都删掉", "日志在 logs/*.log, 备份是 *.zip", "2 * 3 * 4", "a * b* c"]) {
    lacks(render(src), "<em>");
    has(render(src), src);
  }
  for (const src of ["配置_文件_名", "server_properties_file", "这是_强调_的", "__不是粗__", "a _ b _ c", "level_name 和 max_players"]) {
    lacks(render(src), "<em>");
    has(render(src), src);
  }
  assert.equal(render("*斜* _斜二_ 和 *两 个 词* 和 _两 个 词_"), "<p><em>斜</em> <em>斜二</em> 和 <em>两 个 词</em> 和 <em>两 个 词</em></p>");
  assert.equal(render("先删 *.jar，*再* 重启"), "<p>先删 *.jar，<em>再</em> 重启</p>");
  assert.equal(render("（_注意_）"), "<p>（<em>注意</em>）</p>");
});

test("链接外面的加粗、斜体生效，href 原样；不是链接的方括号不打断加粗", () => {
  const A = (href, text) => `<a href="${href}" target="_blank" rel="noopener noreferrer">${text}</a>`;
  assert.equal(render("**[a](https://x.y)**"), `<p><b>${A("https://x.y", "a")}</b></p>`);
  assert.equal(render("*见 [文档](https://ex.com/a_b_c) 和 [二](https://ex.com/d_e_f)*"),
    `<p><em>见 ${A("https://ex.com/a_b_c", "文档")} 和 ${A("https://ex.com/d_e_f", "二")}</em></p>`);
  // 网址里的 _ * ~ 不被行内写法改写，两个链接之间也不会连成一对
  assert.equal(render("[a](https://x.y/_1) 和 [b](https://x.y/2_) [c](https://x.y/**d**) [e](https://x.y/~~f~~) [g](https://x.y/*h*)"),
    `<p>${A("https://x.y/_1", "a")} 和 ${A("https://x.y/2_", "b")} ${A("https://x.y/**d**", "c")} ${A("https://x.y/~~f~~", "e")} ${A("https://x.y/*h*", "g")}</p>`);
  assert.equal(render("[x](https://ex.com/a__b)"), `<p>${A("https://ex.com/a__b", "x")}</p>`);
  assert.equal(render("~~**[*x*](https://ex.com/a)**~~"), `<p><s><b>${A("https://ex.com/a", "<em>x</em>")}</b></s></p>`);
  assert.equal(render("**[21:30 INFO] Done**"), "<p><b>[21:30 INFO] Done</b></p>");
  // 图片仍然不渲染，也不当链接；别的协议仍然不成链接
  const img = render("**![x](https://evil.test/a.png)** 和 **[no](javascript:alert(1))**");
  lacks(img, "<img");
  lacks(img, "<a ");
  has(img, "<b>![x](https://evil.test/a.png)</b>");
  has(img, "<b>[no](javascript:alert(1))</b>");
});

test("链接文字是空的就显示网址本身", () => {
  assert.equal(render("[](https://x.y/a_b_c?d=1&e=*2*)"),
    '<p><a href="https://x.y/a_b_c?d=1&amp;e=*2*" target="_blank" rel="noopener noreferrer">https://x.y/a_b_c?d=1&amp;e=*2*</a></p>');
  assert.equal(render("[ ](https://x.y)"), '<p><a href="https://x.y" target="_blank" rel="noopener noreferrer">https://x.y</a></p>');
  const no = render("[](javascript:alert(1))");
  lacks(no, "<a ");
});

test("安全回归：链接拼不出别的属性，代码块语言只认普通字符，转了码的协议不成链接", () => {
  // 每个 <a 标签都只能是这一种样子：href 里没有引号，后面固定跟 target 和 rel
  const tagOK = html => { for (const t of html.match(/<a\b[^>]*>/g) || []) assert.match(t, /^<a href="https?:\/\/[^"<>\s]*" target="_blank" rel="noopener noreferrer">$/i, html); };
  const a = render('[x](https://ex.com/"onmouseover="alert(1))');
  assert.equal((a.match(/<a\b/g) || []).length, 1, a);
  tagOK(a);
  has(a, 'href="https://ex.com/&quot;onmouseover=&quot;alert(1)"');
  lacks(a, '"onmouseover');
  const b = render('[x](https://ex.com/" onmouseover="alert(1))');
  lacks(b, "<a");
  lacks(b, '"');
  has(b, "&quot; onmouseover=&quot;alert(1)");
  const c = render("[y](https://ex.com/'onclick='alert(1)) [\"onfocus=\"z](https://ex.com/a) **[z](https://ex.com/\"x)**");
  assert.equal((c.match(/<a\b/g) || []).length, 3, c);
  tagOK(c);
  lacks(c, "'");
  lacks(c, '"onfocus');
  // 代码块的语言
  const code = render('```"><img onerror>\nx\n```\n~~~js"onload="1\ny\n~~~');
  assert.equal((code.match(/<span class="md-lang"><\/span>/g) || []).length, 2, code);
  lacks(code, "<img");
  lacks(code, "onerror");
  lacks(code, "onload");
  // 转了码的协议
  const d = render("[x](javascript&#58;alert(1)) [y](java&#x73;cript:alert(1)) [z](&#104;ttps://ex.com) [w](//ex.com) [v](HTTPS://EX.COM/ok)");
  assert.equal((d.match(/<a\b/g) || []).length, 1, d);
  tagOK(d);
  has(d, 'href="HTTPS://EX.COM/ok"');
  has(d, "javascript&amp;#58;alert(1)");
  lacks(d, "href=\"javascript");
});

test("已完成的块签名在后续增长时不变", () => {
  const a = parts("第一段\n\n第二");
  const b = parts("第一段\n\n第二段还在长");
  assert.equal(a[0].sig, b[0].sig);
  assert.notEqual(a[1].sig, b[1].sig);
});

test("行内代码里的标记原样", () => {
  const html = render("用 `**不是粗**` 和 `<b>`");
  has(html, "<code>**不是粗**</code>");
  has(html, "<code>&lt;b&gt;</code>");
  lacks(html, "<b>不是粗</b>");
});

test("乱拼的输入：输出里只有白名单里的标签，标签成对，文字里没有裸的尖括号和引号", () => {
  const PIECES = ["*", "**", "_", "__", "~~", "`", "```", "~~~", "[", "]", "](", "(", ")", "https://e.x/", "http://e.x/a_b", "javascript:", "data:", "\"", "'",
    "<img src=x onerror=1>", "<script>", "&", "&#58;", ">", "> ", "- ", "- [x] ", "1. ", "|", "| --- |", "---", "\n", "\n\n", " ", "  ", "a", "字", "\u0001", "0", "1", "# ", "!", "\\", ".jar", "onload="];
  const TAGS = [
    /^<\/?(p|b|em|s|code|blockquote|ul|li|label|span|div|button|pre|table|thead|tbody|tr|th|td|h[1-6]|ol|a)>$/, /^<(br|hr)>$/,
    /^<a href="https?:\/\/[^"<>\s]*" target="_blank" rel="noopener noreferrer">$/i,
    /^<h([1-6]) class="md-h md-h\1">$/, /^<ol start="\d{1,9}">$/, /^<li class="md-task">$/,
    /^<input type="checkbox" disabled( checked)?>$/, /^<span class="(md-lang|md-open-note)">$/,
    /^<div class="(md-code|md-code md-open|md-code-bar|md-table)">$/, /^<button type="button" class="md-copy">$/, /^<pre class="md-pre">$/,
    /^<t[hd] style="text-align:(center|right)">$/,
  ];
  const VOID = new Set(["br", "hr", "input"]);
  let seed = 20261114; // 固定种子：每次跑的都是同一批输入
  const rnd = n => { seed = (seed * 1103515245 + 12345) & 0x7fffffff; return seed % n; };
  let links = 0;
  for (let k = 0; k < 6000; k++) {
    let src = "";
    for (let j = rnd(40) + 1; j > 0; j--) src += PIECES[rnd(PIECES.length)];
    const html = render(src);
    const where = `\n输入 ${JSON.stringify(src)}\n输出 ${JSON.stringify(html)}`;
    assert.ok(!html.includes("\u0001") && !html.includes("undefined"), "露出了占位" + where);
    assert.ok(!/[<>"']/.test(html.replace(/<[^>]*>/g, "")), "文字里有裸的尖括号或引号" + where);
    const open = [];
    for (const m of html.matchAll(/<(\/?)([a-z0-9]+)[^>]*>/g)) {
      assert.ok(TAGS.some(r => r.test(m[0])), "白名单之外的标签 " + m[0] + where);
      if (m[2] === "a" && !m[1]) links++;
      if (VOID.has(m[2])) continue;
      if (!m[1]) open.push(m[2]);
      else assert.equal(open.pop(), m[2], "标签没配对" + where);
    }
    assert.equal(open.length, 0, "标签没闭合" + where);
  }
  assert.ok(links > 0, "这批输入里应该拼得出链接");
});

test("故意构造的输入不卡住界面、不抛异常（回复里会引用日志和玩家的话）", t => {
  const cases = {
    "标题后面几万个空格": "# a" + " ".repeat(40000) + "x",
    "十万个星号": "x" + "*".repeat(100000) + "x",
    "五千层引用": "> ".repeat(5000) + "a",
    "十万个方括号": "[".repeat(100000) + "](",
    "一行几万个没收尾的星号": "*a ".repeat(20000),
    "一行几万个没收尾的下划线": "_a ".repeat(20000),
    "分隔线后面几万个空格": "-" + " ".repeat(40000) + "x",
    // Codex 第二轮：成串没收尾的链接，每个 [ 都往后扫到行尾
    "一行几万个没收尾的链接": "[](".repeat(16000),
    "一行几千个没收尾的 https 链接": "[x](https://".repeat(8000),
    "很多行、每行都在上限以内的没收尾链接": ("[x](https://".repeat(330) + "\n").repeat(30),
    // Codex 第三轮：列表里几万个空行（每个空行都重扫后面的空行）；几千列的稀疏表格（每行按表头补齐格子）
    "列表里几万个空行": "- a\n" + "\n".repeat(32000) + "- b",
    "几千列的稀疏表格": "|" + "h|".repeat(2000) + "\n|" + "--|".repeat(2000) + "\n" + "|x|\n".repeat(2000),
  };
  // 上限跟着这台机器的快慢走，计时用本进程占用的 CPU 时间。
  // 这条原来量墙上的时间，上限写死 500 毫秒。2026-11-14 四个 PR 合进 main 以后在 CI 上接连挂了几次：本机 60 毫秒的
  // 那种输入（很多行没收尾的链接），CI 上从 170 到 1175 毫秒都量到过。那边的机器只有 4 个核，同时跑着别的测试包，
  // 各次之间还会差两三倍；头一遍跑的又是没优化好的代码，最慢。
  // 所以先跑一段固定的活（两千万次取字符再比较，开发用的电脑上 50 毫秒上下），量出这台机器慢多少倍，上限跟着乘；
  // 超了就重新量一遍快慢，再试一次。用 CPU 时间是因为轮不到这个进程的时候，墙上的时间照样在走。
  // 渲染器真卡住的时候是它自己在算，和这段参照的活一样跟着机器的快慢走，所以原来那几种卡法在哪台机器上都超出上限好几倍。
  const cpuMs = () => { const u = process.cpuUsage(); return (u.user + u.system) / 1000; };
  const refText = "[x](https://".repeat(4000);
  const refOnce = () => {
    const t = cpuMs();
    let depth = 0;
    for (let r = 0; r < 400; r++) {
      for (let j = 0; j < refText.length; j++) { const c = refText[j]; if (c === "(") depth++; else if (c === ")") depth--; }
    }
    assert.equal(depth, 400 * 4000); // 用上结果，免得这段活被优化掉
    return cpuMs() - t;
  };
  // 参照的活连跑三遍取中间那个：CPU 时间在 Windows 上是十五六毫秒一跳，只量一遍 50 毫秒的活偏粗（Codex 合并复审第 9 条）。
  // 参照的活和正则、分配不一定同比变慢；量的是 CPU 时间，等待型的卡顿量不出来。
  // 首次耗时必须保留并单独检查，不能被重试值覆盖。输入/输出和表格工作量另有下面的确定性断言。
  // 渲染器真的不返回时，下面的断言走不到，由外面 md_render_test.go 套的超时兜住。
  const limitHere = () => {
    const ms = [refOnce(), refOnce(), refOnce()].sort((a, b) => a - b)[1];
    return Math.round(500 * Math.max(1, ms / 50));
  };
  const timed = src => { const t = cpuMs(); const html = render(src); return { ms: Math.round(cpuMs() - t), html }; };
  let limit = limitHere();
  for (const [name, src] of Object.entries(cases)) {
    const first = timed(src);
    let r = first;
    if (first.ms >= limit) { limit = limitHere(); r = timed(src); }
    t.diagnostic(`${name}：首次 ${first.ms} ms；${r === first ? "未重试" : `重试 ${r.ms} ms`}；上限 ${limit} ms`);
    assert.ok(first.ms < limit * 2, `${name}：首次用了 ${first.ms} ms，不能被较快的重试覆盖（上限 ${limit * 2} ms）`);
    const { ms, html } = r;
    assert.ok(ms < limit, `${name}：用了 ${ms} ms（CPU 时间；这台机器的上限是 ${limit} ms）`);
    assert.ok(typeof html === "string" && html.length > 0 && !html.includes("\u0001"), `${name}：输出不对`);
    lacks(html, "<script");
  }
  // 标题照常：末尾成串的 # 去掉，「# #」仍是标题
  has(render("## 标题 ##"), '<h2 class="md-h md-h2">标题</h2>');
  has(render("# #"), "<h1");
  lacks(render("#没有空格"), "<h1");
  has(render("- - -"), "<hr>");
  has(render("***"), "<hr>");
  lacks(render("-- x"), "<hr>");
  // 链接照常：括号配对、网址里的括号；太长的网址（超过 2048 字）不当链接
  has(render("[wiki](https://en.wikipedia.org/wiki/Foo_(bar))"), 'href="https://en.wikipedia.org/wiki/Foo_(bar)"');
  has(render("**[a](https://x.y)** 后面"), '<b><a href="https://x.y"');
  lacks(render("[a](https://x.y/" + "a".repeat(3000) + ")"), "<a ");
  lacks(render("[a](javascript:alert(1))"), "<a ");
  // 网址正好 2048 字还算链接，2049 字不算
  const url = n => "https://x.y/" + "a".repeat(n - 12);
  has(render("[a](" + url(2047) + ")"), "<a ");
  has(render("[a](" + url(2048) + ")"), "<a ");
  lacks(render("[a](" + url(2049) + ")"), "<a ");
  // 表格：格子太多不画表格，原样一行一行显示，输出和输入差不多大；正常大小的照常画
  const sparse = "|" + "h|".repeat(2000) + "\n|" + "--|".repeat(2000) + "\n" + "|x|\n".repeat(2000);
  const out = render(sparse);
  lacks(out, "<td");
  assert.ok(out.length < sparse.length * 2, `稀疏表格的输出 ${out.length} 字`);
  // 格子正好到上限（10 列 × 2000 行，含表头）的正常表格照常画；第二张就没有额度了，按原样显示
  const full = "|" + "h|".repeat(10) + "\n|" + "--|".repeat(10) + "\n" + ("|" + "1|".repeat(10) + "\n").repeat(1999);
  assert.equal((render(full).match(/<td/g) || []).length, 10 * 1999);
  assert.equal((render(full + "\n" + full).match(/<td/g) || []).length, 10 * 1999);
  // 几十张各自卡在上限以内的表，加起来也不超过额度（Codex 第三轮之后自查出来的）；
  // 稀疏的宽表格每行四个字就能变出六十多个格子，输出太大就整段按纯文字显示
  const many = ("|" + "h|".repeat(64) + "\n|" + "--|".repeat(64) + "\n" + "|x|\n".repeat(311) + "\n").repeat(30);
  const manyOut = render(many);
  assert.ok((manyOut.match(/<td/g) || []).length <= 20000 && manyOut.length < many.length * 32 + 65536, `几十张表：${manyOut.length} 字`);
  // 输出兜底（Codex 第四轮：原来没有直接测到）：一张格子没超额度、但每行四个字变出六十四个格子的宽表格，
  // 单独一段时输出超过「64 KB + 原文 32 倍」，整段按纯文字显示，只剩转义过的文字和 <p>、<br>；后面字多了就照常画表格
  const wideSparse = "|" + "h|".repeat(64) + "\n|" + "--|".repeat(64) + "\n" + "|x|\n".repeat(311);
  const fb = render(wideSparse + "<img src=x onerror=alert(1)>");
  lacks(fb, "<td"); lacks(fb, "<img");
  assert.ok(fb.startsWith("<p>") && fb.replace(/<\/?p>|<br>/g, "").indexOf("<") < 0 && fb.length < 65536, `兜底的输出：${fb.slice(0, 80)}`);
  has(render(wideSparse + "\n" + "说".repeat(3000)), "<td");
  assert.equal(render(null), "");
  assert.equal(render(123), "<p>123</p>");
  // 列表：连着的空行只算一次分段；同一项里空行隔开的两段不拼成一个词
  assert.equal(render("- a\n\n\n\n- b"), "<ul><li>a</li><li>b</li></ul>");
  assert.equal(render("- a\n\n  续行\n- b"), "<ul><li>a<br><br>续行</li><li>b</li></ul>");
});

test("整段输入先限长，继续增长的尾部不会增加解析内容或输出，转义和代理对仍正确", () => {
  const prefix = "[x](https://ex.com)\n".repeat(Math.ceil(maxInput / 19)).slice(0, maxInput);
  const short = parts(prefix + "后面的内容");
  const huge = parts(prefix + "<script>".repeat(maxInput));
  assert.deepEqual(huge, short, "超限之后的尾部不应改变解析结果或块签名");
  assert.ok(huge.reduce((n, p) => n + p.sig.length, 0) < maxInput + 64);
  const html = render("&".repeat(maxInput * 8));
  assert.ok(html.length < 1024 * 1024, "转义后的回退输出也必须有绝对上限");
  has(html, "已省略后面的显示");
  lacks(html, "<script>");
  const emoji = render("a".repeat(maxInput - 1) + "😀" + "tail");
  assert.ok(!/[\uD800-\uDFFF]/.test(emoji), "不能留下半个代理对");
  lacks(render("a".repeat(maxInput)), "已省略后面的显示");
});
