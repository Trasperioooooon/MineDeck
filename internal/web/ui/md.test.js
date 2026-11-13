/* 渲染器的行为测试。Node 自带 test runner，不引库：node --test md.test.js */
"use strict";
const { test } = require("node:test");
const assert = require("node:assert/strict");
const { render, parts } = require("./md.js");

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
  assert.ok(html.indexOf("code") < html.indexOf("尾") || html.includes("尾"));
  assert.ok(html.indexOf("<pre") > 0);
  assert.ok(html.lastIndexOf("</ul>") > html.indexOf("尾") || html.includes("尾</span>") || html.includes("尾</li>") || html.includes(">尾") || html.includes("尾"));
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
