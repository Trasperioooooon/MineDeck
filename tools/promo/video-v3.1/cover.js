// 出两张封面：out/封面-16x9.png（1920×1080）和 out/封面-9x16.png（1080×1920）。
// 打开 cover.html，等字体和图片都好了，先查三件事再截图：每个字都由内嵌字体画出来（子集里缺字会落到系统字体上）、
// 字都在画面里、竖版的字都在中间 1080×1440（y 240–1680）里。
// 用法（任意目录）：node tools/promo/video-v3.1/cover.js（先跑过 capture.js，要用 out/assets/icon-256.png）
"use strict";
const path = require("path");
const fs = require("fs");

const HERE = __dirname;
const REPO = path.resolve(HERE, "..", "..", "..");
let pw;
try { pw = require("playwright"); } catch { pw = require(path.join(REPO, "tools", "ui-dev", "node_modules", "playwright")); }
const { start } = require("./serve.js");

const JOBS = [{ r: "16x9", w: 1920, h: 1080 }, { r: "9x16", w: 1080, h: 1920 }];

(async () => {
  if (!fs.existsSync(path.join(HERE, "out", "assets", "icon-256.png"))) { console.error("out/assets/icon-256.png is missing: run capture.js first"); process.exit(1); }
  const srv = await start(0);
  const browser = await pw.chromium.launch({ channel: "chrome", args: ["--force-color-profile=srgb", "--font-render-hinting=none"] });
  let bad = 0;
  try {
    for (const j of JOBS) {
      const ctx = await browser.newContext({ viewport: { width: j.w, height: j.h }, deviceScaleFactor: 1, locale: "zh-CN" });
      const page = await ctx.newPage();
      const errors = [];
      page.on("pageerror", e => errors.push(e.message));
      page.on("response", r => { if (r.status() >= 400) errors.push(`HTTP ${r.status()}: ${r.url()}`); });
      await page.goto(`${srv.url}cover.html?r=${j.r}`);
      await page.evaluate(() => window.__ready);
      const res = await page.evaluate(async ({ w, h, r }) => {
        const els = [...document.querySelectorAll("[data-text]")].filter(e => e.getClientRects().length);
        const chars = new Set(els.flatMap(e => [...e.textContent].filter(c => !/\s/.test(c))));
        const cv = document.createElement("canvas"); cv.width = cv.height = 90;
        const g = cv.getContext("2d", { willReadFrequently: true });
        const own = '"MD Geist", "MD Noto Sans SC"';
        const draw = (ch, stack) => { g.clearRect(0, 0, 90, 90); g.font = `600 64px ${stack}`; g.fillText(ch, 10, 70); return g.getImageData(0, 0, 90, 90).data.join(","); };
        const missing = [];
        for (const ch of chars) if (draw(ch, `${own}, "Segoe UI", "Microsoft YaHei", sans-serif`) !== draw(ch, `${own}, "Times New Roman", "SimSun", serif`)) missing.push(ch);
        const out = [];
        for (const e of els) {
          const b = e.getBoundingClientRect();
          const y0 = r === "9x16" ? 240 : 40, y1 = r === "9x16" ? 1680 : h - 40;
          if (b.left < 40 || b.right > w - 40 || b.top < y0 || b.bottom > y1) out.push(`${e.textContent} ${JSON.stringify([b.left, b.top, b.right, b.bottom].map(Math.round))}`);
        }
        return { n: chars.size, missing, out };
      }, j);
      if (errors.length) { bad++; console.error(`${j.r}: page errors: ${errors.join(" | ")}`); }
      if (res.missing.length) { bad++; console.error(`${j.r}: characters not covered by the embedded fonts: ${res.missing.join(" ")}`); }
      if (res.out.length) { bad++; console.error(`${j.r}: text outside the safe area: ${res.out.join("; ")}`); }
      const file = path.join(HERE, "out", `封面-${j.r}.png`);
      await page.screenshot({ path: file });
      console.log(`${j.r}: ${j.w}x${j.h}, ${res.n} distinct characters all drawn by the embedded fonts: ${res.missing.length ? "NO" : "yes"}, text inside the safe area: ${res.out.length ? "NO" : "yes"} -> ${file}`);
      await ctx.close();
    }
  } finally {
    await browser.close();
    await srv.close();
  }
  if (bad) process.exitCode = 1;
})().catch(e => { console.error(e); process.exit(1); });
