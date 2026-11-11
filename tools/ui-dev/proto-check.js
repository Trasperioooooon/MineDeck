// 界面原型自检：把每个场景都打开一遍，看「自动走到这个状态」的那几下有没有走完、页面有没有报错。
// 用法：node proto-check.js [正则]（只查编号或标题匹配的场景）  加 --shots=<目录> 顺手给每个场景截一张图。
// 改了界面的选择器、或者加了新场景之后跑一下：场景里写的选择器失效了，这里会指出是哪一个场景的哪一步。
"use strict";
const fs = require("fs");
const path = require("path");
const { launch } = require("./browser");
const { startProto } = require("./proto");
const { SCENES } = require("./proto/scenes");

const args = process.argv.slice(2);
const shotsArg = args.find(a => a.startsWith("--shots="));
const shots = shotsArg ? path.resolve(shotsArg.slice(8)) : "";
const only = args.find(a => !a.startsWith("--"));
const ONLY = only ? new RegExp(only) : null;

(async () => {
  const proto = await startProto({ port: 0 });
  const browser = await launch();
  const ctx = await browser.newContext({ viewport: { width: 1760, height: 1040 }, locale: "zh-CN" });
  const page = await ctx.newPage();
  const errors = [];
  page.on("pageerror", e => errors.push(e.message));
  if (shots) fs.mkdirSync(shots, { recursive: true });
  const bad = [];
  let n = 0;
  for (const sc of SCENES) {
    if (ONLY && !ONLY.test(sc.id) && !ONLY.test(sc.title)) continue;
    n++;
    errors.length = 0;
    await page.goto(proto.url + "#" + encodeURIComponent(sc.id));
    await page.reload(); // 只改 # 后面的部分不会重新加载目录页
    // 目录页左下角那行字：走完了是空的，没走完会写原因（.warn）
    await page.waitForFunction(id => {
      const l = document.querySelector("#stat-l"), f = document.querySelector("#wrap iframe");
      return f && f.contentDocument && f.contentDocument.querySelector("#main > *") && l && (l.textContent === "" || l.className === "warn") && document.querySelector(`.sc.on[data-id="${id}"]`);
    }, sc.id, { timeout: 30000 }).catch(() => {});
    await page.waitForTimeout(450);
    const stat = await page.$eval("#stat-l", el => ({ text: el.textContent, warn: el.className === "warn" }));
    const frameErrs = errors.slice();
    if (stat.warn || stat.text !== "" || frameErrs.length) bad.push(`${sc.id}（${sc.title}）：${stat.text || ""}${frameErrs.length ? " 页面报错：" + frameErrs.join(" | ") : ""}`);
    if (shots) await page.screenshot({ path: path.join(shots, `${String(n).padStart(2, "0")}-${sc.id}.png`) });
  }
  await browser.close();
  await proto.close();
  for (const b of bad) console.log("没走通  " + b);
  console.log(`\n${n - bad.length}/${n} 个场景走通`);
  process.exit(bad.length ? 1 : 0);
})().catch(e => { console.error(e); process.exit(1); });
