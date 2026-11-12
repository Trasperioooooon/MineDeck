// 逐帧渲染：打开 scene.html（2560×1440、DPR 1），对第 i 帧调用 renderAt(i / 60) 再截图，存成 out/frames/f_00000.png …
// 几个浏览器页面并行，各渲染一段连续的帧。先检查一遍上屏文字（字体覆盖、标题安全区）再开始。
//
// 用法（任意目录）：
//   node tools/promo/video-v3.1/render.js                全片 5040 帧
//   node tools/promo/video-v3.1/render.js --every=60          每秒一帧（看片用，存到 out/preview/）
//   node tools/promo/video-v3.1/render.js --from=12 --to=16    只渲染 12–16 秒
//   node tools/promo/video-v3.1/render.js --at=12.5,40        只渲染这几个时间点（存到 out/samples/）
//   --workers=6（默认）  --check-only（只做文字检查）
"use strict";
const path = require("path");
const fs = require("fs");

const HERE = __dirname;
const REPO = path.resolve(HERE, "..", "..", "..");
let pw;
try { pw = require("playwright"); } catch { pw = require(path.join(REPO, "tools", "ui-dev", "node_modules", "playwright")); }
const { start } = require("./serve.js");
const P = require("./captions.js");

const args = Object.fromEntries(process.argv.slice(2).map(a => { const m = /^--([^=]+)(?:=(.*))?$/.exec(a); return m ? [m[1], m[2] ?? "1"] : [a, "1"]; }));
const FPS = P.FPS, TOTAL = Math.round(P.DURATION * FPS);
const WORKERS = +(args.workers || 6);
const every = +(args.every || 1);
let frames;
let outDir;
if (args.at) { frames = args.at.split(",").map(s => Math.round(+s * FPS)); outDir = path.join(HERE, "out", "samples"); }
else {
  const f0 = args.from ? Math.round(+args.from * FPS) : 0, f1 = args.to ? Math.min(TOTAL, Math.round(+args.to * FPS)) : TOTAL;
  frames = []; for (let f = f0; f < f1; f += every) frames.push(f);
  outDir = path.join(HERE, "out", every > 1 ? "preview" : "frames");
}

async function openScene(browser, url) {
  const ctx = await browser.newContext({ viewport: { width: 2560, height: 1440 }, deviceScaleFactor: 1, locale: "zh-CN" });
  const page = await ctx.newPage();
  const errors = [];
  page.on("pageerror", e => errors.push(e.message));
  page.on("console", m => { if (m.type() === "error") errors.push(m.text()); });
  page.on("requestfailed", r => errors.push("request failed: " + r.url()));
  page.on("response", r => { if (r.status() >= 400) errors.push(`HTTP ${r.status()}: ${r.url()}`); });
  await page.goto(url);
  await page.evaluate(() => window.__ready);
  if (errors.length) throw new Error("scene errors: " + errors.join(" | "));
  const cdp = await ctx.newCDPSession(page);
  return { ctx, page, cdp, errors };
}

// 上屏文字的检查：每个字都要由内嵌字体画出来（子集里缺字会落到系统字体上，画两遍对比就能看出来），字幕要在 5% 标题安全区内
async function checkText(page) {
  const res = await page.evaluate(async P => {
    const chars = new Set();
    const add = s => { for (const ch of s || "") if (!/\s/.test(ch)) chars.add(ch); };
    for (const c of P.CAPTIONS) { add(c.eb); add(c.main); add(c.sub); (c.chips || []).forEach(add); }
    for (const v of Object.values(P.TEXT)) { if (typeof v === "string") add(v); else for (const x of v) add(typeof x === "string" ? x : (x.who || "") + (x.head || "") + x.text); }
    const cv = document.createElement("canvas"); cv.width = cv.height = 90;
    const g = cv.getContext("2d", { willReadFrequently: true });
    const draw = (ch, stack) => { g.clearRect(0, 0, 90, 90); g.font = `500 64px ${stack}`; g.fillText(ch, 10, 70); return g.getImageData(0, 0, 90, 90).data.join(","); };
    const own = '"MD Geist", "MD Noto Sans SC"';
    const missing = [];
    for (const ch of chars) {
      await document.fonts.load(`500 64px ${own}`, ch);
      if (draw(ch, `${own}, "Segoe UI", "Microsoft YaHei", sans-serif`) !== draw(ch, `${own}, "Times New Roman", "SimSun", serif`)) missing.push(ch);
    }
    // 字幕：在各自时间段中间那一刻量一下位置
    const boxes = [];
    for (const c of P.CAPTIONS) {
      window.renderAt((c.t0 + c.t1) / 2);
      const e = [...document.querySelectorAll(".cap")].find(x => x.style.display !== "none" && x.querySelector(".main").textContent === c.main);
      const r = e.getBoundingClientRect();
      boxes.push({ main: c.main, l: r.left, t: r.top, r: r.right, b: r.bottom });
    }
    return { checked: chars.size, missing, boxes };
  }, { CAPTIONS: P.CAPTIONS, TEXT: P.TEXT });
  const problems = [];
  if (res.missing.length) problems.push("characters not covered by the embedded fonts: " + res.missing.join(" "));
  for (const b of res.boxes) if (b.l < 128 || b.t < 72 || b.r > 2432 || b.b > 1368) problems.push(`caption "${b.main}" outside the 5% title-safe area: ${JSON.stringify(b)}`);
  console.log(`text check: ${res.checked} distinct characters, all drawn by the embedded fonts: ${res.missing.length ? "NO" : "yes"}; ${res.boxes.length} captions inside the title-safe area: ${problems.some(p => p.startsWith("caption")) ? "NO" : "yes"}`);
  return problems;
}

(async () => {
  const srv = await start(0);
  const browser = await pw.chromium.launch({ channel: "chrome", args: ["--force-color-profile=srgb", "--disable-lcd-text", "--font-render-hinting=none"] });
  let failed = false;
  try {
    const first = await openScene(browser, srv.url);
    const problems = await checkText(first.page);
    if (problems.length) { console.error("text problems:\n  " + problems.join("\n  ")); failed = true; }
    if (args["check-only"] || failed) return;
    fs.mkdirSync(outDir, { recursive: true });
    const k = Math.min(WORKERS, frames.length);
    const pages = [first];
    for (let i = 1; i < k; i++) pages.push(await openScene(browser, srv.url));
    const chunk = Math.ceil(frames.length / k);
    const t0 = Date.now();
    let done = 0;
    await Promise.all(pages.map(async (pg, w) => {
      const mine = frames.slice(w * chunk, (w + 1) * chunk);
      for (const f of mine) {
        await pg.page.evaluate(t => window.renderAt(t), f / FPS);
        const shot = await pg.cdp.send("Page.captureScreenshot", { format: "png", optimizeForSpeed: true, captureBeyondViewport: false, fromSurface: true });
        fs.writeFileSync(path.join(outDir, args.at ? `t_${(f / FPS).toFixed(2).replace(".", "_")}.png` : `f_${String(f).padStart(5, "0")}.png`), Buffer.from(shot.data, "base64"));
        if (++done % 120 === 0 || done === frames.length) {
          const s = (Date.now() - t0) / 1000;
          console.log(`${done}/${frames.length} frames  ${s.toFixed(0)} s  (~${((frames.length - done) * s / done).toFixed(0)} s left)`);
        }
      }
      if (pg.errors.length) throw new Error("scene errors while rendering: " + pg.errors.join(" | "));
    }));
    console.log(`rendered ${frames.length} frames to ${outDir}`);
  } finally {
    await browser.close();
    await srv.close();
  }
  if (failed) process.exitCode = 1;
})().catch(e => { console.error(e); process.exit(1); });
