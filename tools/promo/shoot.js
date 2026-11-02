// MineDeck 的宣传图：README 截图（1440×900 CSS 像素、deviceScaleFactor 1、浅色、配色「方块」）和 GitHub 的社交预览图
// （1280×640）。全部是仿真后端（tools/ui-dev/server.js）上的真实界面：只用点击、输入和仿真后端写明的 /__mock/state 开关，
// 不往页面里插东西，不改界面，也不改接口返回。
//
// 用法（在仓库根目录运行）：
//   node tools/promo/shoot.js                 全部（先拍 README 截图，再做社交预览图）
//   node tools/promo/shoot.js readme          只拍 README 截图
//   node tools/promo/shoot.js card            只做社交预览图（用上一次拍的 03-assistant.png）
//   node tools/promo/shoot.js --only=0[34]    只拍名字匹配这个正则的 README 截图
// 环境变量：PROMO_OUT=<输出目录>（默认 tools/promo/out，不入库）；MINEDECK_REPO=<仓库目录>（默认本脚本所在的仓库）；
//          KEEP_RAW=1 保留没压缩的原图（输出目录的 raw/ 下）。
// 输出：<输出目录>/<名字>.png；<输出目录>/assets/icon-256.png（从 assets/icon.ico 里取出的 256 像素那一帧）。
// README 用到的几张手动复制到 docs/images/，社交预览图复制成 assets/social-preview.png（再到仓库设置里上传）。
// 需要：Node 18+、带 Chromium 的 Playwright（全局安装或 tools/ui-dev/node_modules）、python3 和 Pillow；
//       ImageMagick 的 convert 可选（compress.py 多一种无损压缩可比）。
"use strict";
const path = require("path");
const fs = require("fs");
const http = require("http");
const { execFileSync } = require("child_process");

const REPO = path.resolve(process.env.MINEDECK_REPO || path.join(__dirname, "..", ".."));
const UI = path.join(REPO, "internal", "web", "ui");
let pw;
try { pw = require("playwright"); } catch { pw = require(path.join(REPO, "tools", "ui-dev", "node_modules", "playwright")); }
const { start } = require(path.join(REPO, "tools", "ui-dev", "server.js"));

const OUT = path.resolve(process.env.PROMO_OUT || path.join(__dirname, "out"));
const RAW = path.join(OUT, "raw");
const ASSETS = path.join(OUT, "assets");
const W = 1440, H = 900;
const README_LIMIT = 350 * 1024, CARD_LIMIT = 1024 * 1024;
const args = process.argv.slice(2);
const GROUP = args.find(a => !a.startsWith("--")) || "all";
const onlyArg = args.find(a => a.startsWith("--only="));
const ONLY = onlyArg ? new RegExp(onlyArg.slice("--only=".length)) : null;
if (!["all", "readme", "card"].includes(GROUP)) { console.error("usage: node shoot.js [all|readme|card] [--only=regex]"); process.exit(1); }

const PACK = "机械动力整合包";  // 仿真后端里的 Forge 1.20.1 实例（只有它有音乐共享和伙伴）
const SURV = "生存服 · 朋友";    // 仿真后端里运行中的 Paper 实例
const enc = encodeURIComponent;
// AI 配成「设置 → AI」保存后的样子（和 tests.js 用的值一样），外加每日额度和价格数据的日期
const AI = { reset: true, preset: "deepseek", protocol: "openai", baseUrl: "https://api.deepseek.com", model: "deepseek-v4-flash", key: "sk-promo",
  priceUpdated: "2026-10-28", quota: { assistant: 500000, gamechat: 200000, companion: 100000 } };
// 点过「安装」以后伙伴模组的样子（抄自 tests.js 的 CP_INSTALLED）
const CP_INSTALLED = {
  minedeck_companion: { state: "ok", version: "0.2.0", files: [{ file: "minedeck-companion-forge-1.20.1-0.2.0.jar", version: "0.2.0", keep: true }] },
  playerengine: { state: "ok", version: "1.20.1-1.4.0", files: [{ file: "playerengine-forge-1.20.1-1.4.0.jar", version: "1.20.1-1.4.0", keep: true }] },
};
const CP_ZONES = [
  { name: "base", dim: "minecraft:overworld", min: [-120, -64, -80], max: [-40, 320, 10] },
  { name: "farm", dim: "minecraft:overworld", min: [60, 62, -420], max: [92, 80, -396] },
];

// 和 tools/ui-dev/shoot.js 一样模拟宿主注入的窗口函数：标题栏因此显示窗口按钮，和 WebView2 窗口里一样
function hostStubs() {
  window.hostWinClose = () => {}; window.hostWinMin = () => {}; window.hostWinMaxToggle = () => {};
  window.hostWinDrag = () => {}; window.hostWinIsMax = async () => false;
}

// 让 Forge 整合包变成运行中：先经过「启动中」再到「运行中」，和真的开服一样（运行时长、在线人数由仿真后端补上）。
const packRunning = h => h.state({ status: { [PACK]: "starting" } }).then(() => h.state({ status: { [PACK]: "running" } }));

const SHOTS = [
  { name: "01-servers", note: "服务器列表：1 台运行中、1 台启动中、2 台已停止", run: async (page, h) => {
    await h.state({ ai: AI });
    await h.open("#/instances", `.card[data-name="${SURV}"]`);
  } },
  { name: "01-servers-dark", theme: "dark", note: "同 01，深色", run: async (page, h) => {
    await h.state({ ai: AI });
    await h.open("#/instances", `.card[data-name="${SURV}"]`);
  } },
  { name: "02-console-banner", note: "机械动力整合包（已停止）的控制台：报错横幅展开（缺少前置模组 + 客户端模组进了服务端）", run: async (page, h) => {
    await h.state({ ai: AI });
    await h.open(`#/inst/${enc(PACK)}/console`, '.diag-banner [data-x="ai"]');
    await page.click(".db-more summary");
    await page.waitForSelector(".db-more[open] .db-ev");
  } },
  { name: "03-assistant", note: "报错横幅点「让 AI 分析」→ 在发送预览里输入问题 → 发送：AI 先查模组清单（读操作，直接执行），再提议禁用 xaerominimap.jar（确认卡片待处理）", run: async (page, h) => {
    await h.state({ ai: AI, asst: { reset: true } });
    await h.open(`#/inst/${enc(PACK)}/console`, '.diag-banner [data-x="ai"]');
    await page.click('.diag-banner [data-x="ai"]');
    await page.waitForSelector(".modal.ap-pv textarea");
    await page.fill(".modal.ap-pv textarea", "整合包一开服就崩，帮我看看是哪个模组的问题");
    await page.click('.modal [data-x="ok"]');
    await h.assistantSettled();
  } },
  { name: "03b-assistant-typed", note: "（备选）在助手面板里新建关联「生存服 · 朋友」的会话、输入消息回车：AI 先读 server.properties，再提议 max-players 20 → 10", run: async (page, h) => {
    await h.state({ ai: AI, asst: { reset: true } });
    await h.open(`#/inst/${enc(SURV)}/console`, "#console div");
    await page.click("#ai-btn");
    await page.click('#ap-body [data-x="new"]');
    await page.click(`.dropmenu button:has-text("${SURV}（当前）")`);
    await page.waitForSelector("#ap-in");
    await page.fill("#ap-in", "周末人太多有点卡，最多人数改成 10 人吧");
    await page.press("#ap-in", "Enter");
    await h.assistantSettled();
  } },
  { name: "04-gameai", note: "机械动力整合包（Forge 1.20.1，运行中）的「游戏内 AI」标签顶部：已打开、前缀 @AI、提问发给 DeepSeek、授权了 Alex / Steve", run: async (page, h) => {
    await gameaiState(h);
    await h.open(`#/inst/${enc(PACK)}/gameai`, "#gc-log .gc-rec");
  } },
  { name: "04b-gameai-log", note: "（备选）同一标签往下滚到「最近的问答和执行记录」（问答数据是仿真后端给「生存服 · 朋友」的示例记录，经 gc.inst.log 挪到 Forge 服）", run: async (page, h) => {
    await gameaiState(h);
    await h.open(`#/inst/${enc(PACK)}/gameai`, "#gc-log .gc-rec");
    await h.scrollTo("#gc-log", 8); // 留 8 像素：6 条问答记录正好都在一屏里
  } },
  { name: "05-companion", note: "机械动力整合包的「伙伴」标签，滚到禁挖区域 / 伙伴 / 最近的事件：已连上伙伴模组，2 个伙伴（小方正在采集，阿土空闲）", run: async (page, h) => {
    await h.state({ ai: AI, gc: { inst: { [PACK]: { whitelistOn: true, settings: { enabled: true, allowed: ["Alex", "Steve"] } } } },
      cp: { reset: true, inst: { [PACK]: { mods: CP_INSTALLED, settings: { zones: CP_ZONES } } } } });
    await packRunning(h);
    await h.open(`#/inst/${enc(PACK)}/companion`, "#cp-on:not([disabled])");
    await page.click("#cp-on + .sw"); // 打开 AI 伙伴（即时生效）
    await h.toast("已打开 AI 伙伴");
    await h.open(`#/inst/${enc(PACK)}/companion`, "#cp-spawn:not([disabled])"); // 重新打开：这时已经连上了
    for (const [name, owner] of [["小方", "Steve"], ["阿土", "Alex"]]) {
      await page.click("#cp-spawn");
      await page.waitForSelector(".modal #cs-name");
      await page.fill("#cs-name", name);
      await page.fill("#cs-owner", owner);
      await page.click('.modal [data-x="ok"]');
      await h.toast(`刷出了伙伴「${name}」`);
      await page.waitForFunction(() => !document.querySelector("#modal-root").childElementCount);
    }
    await h.state({ cp: { busy: true } }); // 仿真后端的开关：让第一个伙伴在采集
    await h.open(`#/inst/${enc(PACK)}/companion`, '#cp-list [data-comp="2"]');
    await h.scrollTo("#cp-zones", 16);
  } },
  { name: "06-music", note: "机械动力整合包（运行中）的「音乐共享」标签：Win11 按程序录音，网易云音乐正在出声（已选中），检查清单全部通过（含语音隧道）", run: async (page, h) => {
    await h.state({ ai: AI, music: { reset: true, voiceTunnel: true, inst: { [PACK]: { bridge: "main", voiceHost: "frp-abc.natfrp.cloud:34567" } } } });
    await packRunning(h);
    await h.open(`#/inst/${enc(PACK)}/music`, '#mu-src [data-exe="cloudmusic.exe"][aria-checked="true"] .xp i.on');
  } },
  { name: "07-settings-ai", note: "设置 → AI（可选）：已启用 DeepSeek · deepseek-v4-flash，Key 已加密保存，点了「测试连接」", run: async (page, h) => {
    await h.state({ ai: AI });
    await h.open("#/settings/ai", "#ai-status .tag");
    await page.click("#ai-test");
    await page.waitForFunction(() => document.querySelector("#ai-test-sub").textContent.includes("连接正常"));
  } },
];

async function gameaiState(h) {
  // 仿真后端只给 Paper 服准备了问答记录：读出来，经 gc.inst.<名字>.log 交给 Forge 整合包
  const log = await h.api(`/api/instances/${enc(SURV)}/gameai/log`);
  await h.state({ ai: AI, gc: { reset: true, inst: { [PACK]: { whitelistOn: true, log, settings: { enabled: true, allowed: ["Alex", "Steve"] } } } } });
  await packRunning(h);
}

async function withPage(browser, theme, fn) {
  const mock = await start({ ui: UI });
  const ctx = await browser.newContext({ viewport: { width: W, height: H }, deviceScaleFactor: 1, locale: "zh-CN", timezoneId: "Asia/Shanghai", colorScheme: theme });
  await ctx.addInitScript(t => { localStorage.setItem("amh_token", "mock"); localStorage.setItem("amh_theme", t); }, theme);
  await ctx.addInitScript(hostStubs);
  const page = await ctx.newPage();
  const errors = [];
  page.on("pageerror", e => errors.push(e.message));
  let n = 0;
  const h = {
    state: async body => {
      const r = await fetch(mock.url + "__mock/state", { method: "POST", body: JSON.stringify(body) });
      if (!r.ok) throw new Error("mock state failed: " + r.status);
      return (await r.json()).data;
    },
    api: async p => (await (await fetch(mock.url + p.replace(/^\//, ""))).json()).data,
    open: async (hash, sel) => {
      await page.goto(mock.url + "?n=" + (++n) + hash);
      await page.waitForSelector(sel, { timeout: 10000 });
      await page.evaluate(() => document.fonts.ready);
    },
    toast: text => page.waitForFunction(t => [...document.querySelectorAll("#toasts .toast")].some(e => e.textContent.includes(t)), text, { timeout: 5000 }),
    // 滚动内容区（#main），让 `sel` 上面那个小节的标题落在内容区顶边下方 `pad` 像素处（滚不到就停在边上）
    scrolled: false,
    scrollTo: (sel, pad) => (h.scrolled = true) && page.evaluate(([s, p]) => {
      const m = document.querySelector("#main"), box = document.querySelector(s);
      const target = box.previousElementSibling && !box.previousElementSibling.id ? box.previousElementSibling : box;
      const y = target.getBoundingClientRect().top - m.getBoundingClientRect().top + m.scrollTop - p;
      m.scrollTop = Math.max(0, Math.min(y, m.scrollHeight - m.clientHeight));
    }, [sel, pad]),
    assistantSettled: async () => {
      await page.waitForSelector("#ai-panel .ap-card.pending", { timeout: 10000 });
      await page.waitForFunction(() => { const b = document.querySelector("#ap-stop"); return b && b.hidden; }, null, { timeout: 10000 });
      await page.waitForFunction(() => document.querySelectorAll("#ai-panel .ap-usage").length >= 2, null, { timeout: 10000 });
    },
  };
  try { await fn(page, h); } finally { await ctx.close(); await mock.close(); }
  return errors;
}

// 等画面稳定：没有提示条，没有正在开关的弹窗或菜单，没有在跑的有限动画。
async function calm(page) {
  await page.mouse.move(120, 700); // 侧栏的空白处：截图里不出现悬停效果
  await page.waitForFunction(() => !document.querySelector("#toasts").childElementCount
    && !document.querySelector("#modal-root").childElementCount && !document.querySelector(".dropmenu"), null, { timeout: 20000 });
  await page.waitForFunction(() => document.getAnimations().every(a => a.effect && a.effect.getTiming().iterations === Infinity || a.playState !== "running"), null, { timeout: 10000 });
  await page.evaluate(() => document.fonts.ready);
  await page.waitForTimeout(600);
}

// 原图 → 压缩后的 PNG（compress.py）；没设 KEEP_RAW=1 时原图用完就删
function compress(limit, pairs) {
  if (!pairs.length) return [];
  const out = execFileSync("python3", ["-I", path.join(__dirname, "compress.py"), String(limit), ...pairs.flat()], { encoding: "utf8" });
  if (!process.env.KEEP_RAW) {
    for (const [raw] of pairs) fs.rmSync(raw, { force: true });
    if (fs.existsSync(RAW) && !fs.readdirSync(RAW).length) fs.rmdirSync(RAW);
  }
  return out.trim().split("\n").map(l => JSON.parse(l));
}

// ---------- 社交预览图 ----------

// assets/icon.ico 里存的是 PNG 帧：取出 256×256 的那一帧（ICONDIR 加上每帧 16 字节的目录项；宽度字节为 0 表示 256）
function extractIcon() {
  const ico = fs.readFileSync(path.join(REPO, "assets", "icon.ico"));
  const count = ico.readUInt16LE(4);
  for (let i = 0; i < count; i++) {
    const e = 6 + i * 16;
    if (ico[e] === 0 && ico[e + 1] === 0) {
      const size = ico.readUInt32LE(e + 8), off = ico.readUInt32LE(e + 12);
      const png = ico.subarray(off, off + size);
      if (png.readUInt32BE(0) !== 0x89504e47) throw new Error("256px frame is not PNG");
      fs.mkdirSync(ASSETS, { recursive: true });
      fs.writeFileSync(path.join(ASSETS, "icon-256.png"), png);
      return;
    }
  }
  throw new Error("no 256px frame in icon.ico");
}

// 小型静态服务：/card.html 是本目录的卡片页，/ui/ 是界面目录（字体、themes.css），其余是输出目录（截图和图标）
function serveStatic() {
  const types = { ".html": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8", ".png": "image/png", ".woff2": "font/woff2", ".js": "application/javascript" };
  const server = http.createServer((req, res) => {
    const p = decodeURIComponent(new URL(req.url, "http://x").pathname);
    const [root, rel] = p.startsWith("/ui/") ? [UI, p.slice(4)] : p === "/card.html" ? [__dirname, "card.html"] : [OUT, p.slice(1)];
    const fp = path.resolve(root, rel);
    if (!fp.startsWith(root + path.sep) || !fs.existsSync(fp) || !fs.statSync(fp).isFile()) { res.writeHead(404); return res.end(); }
    res.writeHead(200, { "Content-Type": types[path.extname(fp)] || "application/octet-stream", "Cache-Control": "no-store" });
    res.end(fs.readFileSync(fp));
  });
  return new Promise(r => server.listen(0, "127.0.0.1", () => r({ url: `http://127.0.0.1:${server.address().port}/`, close: () => new Promise(c => server.close(c)) })));
}

async function shootCard(browser) {
  extractIcon();
  if (!fs.existsSync(path.join(OUT, "03-assistant.png"))) throw new Error("run the README shots first (card uses 03-assistant.png)");
  const srv = await serveStatic();
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 640 }, deviceScaleFactor: 1, locale: "zh-CN" });
  const page = await ctx.newPage();
  const errors = [];
  page.on("pageerror", e => errors.push(e.message));
  page.on("requestfailed", r => errors.push("request failed: " + r.url()));
  page.on("response", r => { if (r.status() >= 400) errors.push(`HTTP ${r.status()}: ${r.url()}`); });
  try {
    await page.goto(srv.url + "card.html");
    await page.evaluate(() => document.fonts.ready);
    await page.waitForFunction(() => [...document.images].every(i => i.complete && i.naturalWidth));
    // 检查字体覆盖：卡片上的每个字各画两遍，卡片自己的字体后面分别接系统的无衬线、衬线字体（中西文都有）。
    // 内嵌的子集（MD Geist / MD Noto Sans SC）里有这个字，两遍画出来一样；缺字就落到不同的后备字体上，画出来不一样
    // （用 龘 ŋ 鱻 试过，都能查出来）。
    const cov = await page.evaluate(async () => {
      const text = [...document.querySelectorAll("[data-text]")].map(e => e.textContent).join("");
      const chars = [...new Set(text.replace(/\s/g, ""))];
      const out = { checked: chars.length, missing: [], families: {} };
      for (const el of document.querySelectorAll("[data-text]")) {
        const cs = getComputedStyle(el);
        for (const fam of cs.fontFamily.split(",").map(s => s.trim().replace(/"/g, ""))) if (fam.startsWith("MD ")) await document.fonts.load(`${cs.fontWeight} 48px "${fam}"`, el.textContent);
      }
      const cv = document.createElement("canvas"); cv.width = cv.height = 80;
      const g = cv.getContext("2d", { willReadFrequently: true });
      const draw = (ch, stack) => { g.clearRect(0, 0, 80, 80); g.font = `600 48px ${stack}`; g.fillText(ch, 8, 60); return g.getImageData(0, 0, 80, 80).data.join(","); };
      // 两种后备字体都覆盖中文和常见西文：子集里缺的字，两遍画出来就不一样
      const SANS = '"Noto Sans CJK SC", "DejaVu Sans", "FreeSans"', SERIF = '"Noto Serif CJK SC", "DejaVu Serif", "FreeSerif"';
      for (const ch of chars) {
        const own = '"MD Geist", "MD Noto Sans SC"';
        if (draw(ch, `${own}, ${SANS}`) !== draw(ch, `${own}, ${SERIF}`)) out.missing.push(ch);
      }
      for (const el of document.querySelectorAll("[data-text]")) out.families[el.dataset.text] = getComputedStyle(el).fontFamily;
      return out;
    });
    console.log(`card font coverage: ${cov.checked} distinct characters checked, missing from the bundled subsets: ${cov.missing.length ? cov.missing.join(" ") : "none"}`);
    if (cov.missing.length) errors.push("characters not covered by the bundled fonts: " + cov.missing.join(""));
    // GitHub 有些地方会裁掉卡片边缘：文字离卡片边至少 40 像素
    const textBoxes = await page.evaluate(() => [...document.querySelectorAll("[data-text]")].map(e => { const r = e.getBoundingClientRect(); return { k: e.dataset.text, l: r.left, t: r.top, r: r.right, b: r.bottom }; }));
    for (const b of textBoxes) if (b.l < 40 || b.t < 40 || b.r > 1240 || b.b > 600) errors.push(`text "${b.k}" outside the 40 px safe area: ${JSON.stringify(b)}`);
    // ……也不能压到（倾斜的）截图框上
    const shot = await page.evaluate(() => { const r = document.querySelector(".shot").getBoundingClientRect(); return { l: r.left, t: r.top }; });
    for (const b of textBoxes) if (b.r > shot.l - 12) errors.push(`text "${b.k}" (right edge ${Math.round(b.r)}) runs into the screenshot frame (left edge ${Math.round(shot.l)})`);
    console.log("card text boxes: " + textBoxes.map(b => `${b.k} x ${Math.round(b.l)}-${Math.round(b.r)} y ${Math.round(b.t)}-${Math.round(b.b)}`).join("; ") + `; frame left ${Math.round(shot.l)}`);
    fs.mkdirSync(RAW, { recursive: true });
    await page.screenshot({ path: path.join(RAW, "social-preview.png") });
  } finally { await ctx.close(); await srv.close(); }
  const [r] = compress(CARD_LIMIT, [[path.join(RAW, "social-preview.png"), path.join(OUT, "social-preview.png")]]);
  if (errors.length) { console.error("card problems:\n  " + errors.join("\n  ")); process.exitCode = 1; }
  return r;
}

(async () => {
  const browser = await pw.chromium.launch();
  const results = [];
  try {
    if (GROUP !== "card") {
      fs.mkdirSync(RAW, { recursive: true });
      const pairs = [];
      for (const s of SHOTS) {
        if (ONLY && !ONLY.test(s.name)) continue;
        const errors = await withPage(browser, s.theme || "light", async (page, h) => {
          await s.run(page, h);
          // Playwright 点击时会把 #main 滚动一点（比如控制台页比内容区高 27 像素）；
          // 不指定小节的截图都滚回顶部，和刚打开时一样
          if (!h.scrolled) await page.evaluate(() => { document.querySelector("#main").scrollTop = 0; });
          await calm(page);
          await page.screenshot({ path: path.join(RAW, s.name + ".png") });
        });
        if (errors.length) { console.error(`${s.name}: page errors: ${errors.join(" | ")}`); process.exitCode = 1; }
        console.log(`captured ${s.name}`);
        pairs.push([path.join(RAW, s.name + ".png"), path.join(OUT, s.name + ".png")]);
      }
      for (const r of compress(README_LIMIT, pairs)) results.push({ ...r, note: (SHOTS.find(s => r.out.endsWith(s.name + ".png")) || {}).note });
    }
    if (GROUP !== "readme" && !ONLY) results.push({ ...(await shootCard(browser)), note: "GitHub social preview" });
  } finally { await browser.close(); }
  for (const r of results) {
    const over = r.bytes > (r.out.endsWith("social-preview.png") ? CARD_LIMIT : README_LIMIT) ? "  OVER LIMIT" : "";
    console.log(`${path.basename(r.out).padEnd(26)} ${r.w}x${r.h}  ${(r.bytes / 1024).toFixed(1).padStart(6)} KB  (raw ${(r.raw / 1024).toFixed(1)} KB, ${r.method})${over}`);
  }
})().catch(e => { console.error(e); process.exit(1); });
