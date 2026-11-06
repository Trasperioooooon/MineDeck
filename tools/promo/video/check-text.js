// 检查 captions.js 里所有上屏文字：
//   1. 中文和全角标点都在 GB2312 里（内嵌的 Noto Sans SC 子集按 GB2312 做的）；半角 ASCII 由 Geist 覆盖
//   2. 字幕每行不超过 18 个字符（中西文、空格都算一个）
//   3. 不用夸张词（brief 列出的那几个），不用感叹号；「一键开服」全片只出现一次
// 字体覆盖的实测（每个字真的由内嵌字体画出来）在 render.js 开头做。
// 用法：node tools/promo/video/check-text.js
"use strict";
const P = require("./captions.js");

// GB2312 字符集：用 WHATWG 的 GBK 解码器把 GB2312 的编码区（第 1–9 区符号、第 16–87 区汉字）逐个解出来
const gb = new Set();
const dec = new TextDecoder("gbk");
for (let hi = 0xa1; hi <= 0xf7; hi++) {
  if (hi > 0xa9 && hi < 0xb0) continue; // 10–15 区未用
  for (let lo = 0xa1; lo <= 0xfe; lo++) {
    const ch = dec.decode(new Uint8Array([hi, lo]));
    if (ch && ch !== "�") gb.add(ch);
  }
}
// WHATWG 的 GBK 把 A1A4 解成 U+00B7「·」（GB2312 原表是 U+30FB）；两种都当 GB2312 符号，但片中不用这个字符（用像素小方块代替）
const problems = [];
const texts = [];
for (const c of P.CAPTIONS) for (const [k, v] of Object.entries({ eb: c.eb, main: c.main, sub: c.sub })) if (v) texts.push({ where: `caption ${c.t0}s ${k}`, s: v, line: true });
for (const c of P.CAPTIONS) for (const v of c.chips || []) texts.push({ where: `caption ${c.t0}s chip`, s: v, line: true });
for (const [k, v] of Object.entries(P.TEXT)) {
  if (typeof v === "string") texts.push({ where: `TEXT.${k}`, s: v });
  else v.forEach((x, i) => texts.push({ where: `TEXT.${k}[${i}]`, s: typeof x === "string" ? x : (x.who ? `<${x.who}> ` : x.head) + x.text }));
}
const BANNED = ["轻松", "一键搞定", "告别", "黑科技", "神器", "!", "！"];
let yijian = 0;
for (const t of texts) {
  for (const ch of t.s) {
    const cp = ch.codePointAt(0);
    if (cp < 0x80) continue;
    if (!gb.has(ch)) problems.push(`${t.where}: "${ch}" (U+${cp.toString(16).toUpperCase()}) is not in GB2312 — in "${t.s}"`);
  }
  if (t.line && [...t.s].length > 18) problems.push(`${t.where}: ${[...t.s].length} characters > 18 — "${t.s}"`);
  for (const b of BANNED) if (t.s.includes(b)) problems.push(`${t.where}: uses "${b}" — "${t.s}"`);
  yijian += t.s.split("一键开服").length - 1;
}
if (yijian !== 1) problems.push(`「一键开服」 appears ${yijian} times (should be exactly once)`);
// 字幕时间：不重叠、都在全片时长以内
const caps = [...P.CAPTIONS].sort((a, b) => a.t0 - b.t0);
caps.forEach((c, i) => {
  if (c.t1 <= c.t0) problems.push(`caption "${c.main}" ends before it starts`);
  if (i && c.t0 < caps[i - 1].t1) problems.push(`caption "${c.main}" overlaps "${caps[i - 1].main}"`);
  if (c.t1 - c.t0 < 1.5) problems.push(`caption "${c.main}" is on screen only ${(c.t1 - c.t0).toFixed(2)} s`);
});
const chars = new Set(texts.flatMap(t => [...t.s]));
console.log(`checked ${texts.length} strings, ${chars.size} distinct characters, GB2312 table ${gb.size} entries`);
if (problems.length) { console.error("problems:\n  " + problems.join("\n  ")); process.exit(1); }
console.log("all on-screen text: GB2312 only, caption lines <= 18 characters, no banned words or exclamation marks, 「一键开服」 once");
