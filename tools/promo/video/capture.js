// 宣传视频的素材：在仿真后端（tools/ui-dev/server.js）上拍真实界面。
// 桌面 1280×800、deviceScaleFactor 2（出 2560×1600 的图），浅色、配色「方块」；手机 390×844、DPR 3、isMobile + hasTouch。
// 每张图各起一份全新的仿真后端（端口 27410，拍完就关），只用点击、输入和仿真后端写明的 /__mock/state 开关来布置场景。
// 例外（都是在浏览器里改仿真数据，不改界面代码）：
//   - /api/app 和 /api/update/check 的版本号改成 3.0.0，侧栏底部和片尾一致；
//   - 部署进度页的 /api/tasks/* 按帧给出递增的进度（仿真后端的进度是固定的 60%）；
//   - 「刚开服」那张把生存服的运行时长改短，控制台换成一段干净的开服日志（仿真后端的示例日志里有一段插件报错，
//     放在「开好了」这一幕不合适）：页面里的 EventSource 只对这台服务器的控制台换成本脚本给的几行，其余照常。
//
// 用法（任意目录）：node tools/promo/video/capture.js [--only=正则]
// 输出：out/shots/<名字>.png、out/assets/icon-256.png、out/assets/pixel-icon.json（启动画面用的 16×16 像素网格）
"use strict";
const path = require("path");
const fs = require("fs");

const HERE = __dirname;
const REPO = path.resolve(HERE, "..", "..", "..");
const UI = path.join(REPO, "internal", "web", "ui");
const PW_DIR = path.join(REPO, "tools", "ui-dev", "node_modules");
let pw;
try { pw = require("playwright"); } catch { pw = require(path.join(PW_DIR, "playwright")); }
const { start } = require(path.join(REPO, "tools", "ui-dev", "server.js"));

const OUT = path.join(HERE, "out");
const SHOTS = path.join(OUT, "shots");
const ASSETS = path.join(OUT, "assets");
const PORT = 27410;
const VERSION = "3.0.0";
const onlyArg = process.argv.find(a => a.startsWith("--only="));
const ONLY = onlyArg ? new RegExp(onlyArg.slice(7)) : null;

const PACK = "机械动力整合包"; // 仿真后端里的 Forge 1.20.1 实例
const SURV = "生存服 · 朋友";   // 仿真后端里的 Paper 1.21.4 实例
const enc = encodeURIComponent;
const AI = { reset: true, preset: "deepseek", protocol: "openai", baseUrl: "https://api.deepseek.com", model: "deepseek-v4-flash", key: "sk-promo",
  priceUpdated: "2026-10-28", quota: { assistant: 500000, gamechat: 200000, companion: 100000 } };
const CP_INSTALLED = {
  minedeck_companion: { state: "ok", version: "0.2.0", files: [{ file: "minedeck-companion-forge-1.20.1-0.2.0.jar", version: "0.2.0", keep: true }] },
  playerengine: { state: "ok", version: "1.20.1-1.4.0", files: [{ file: "playerengine-forge-1.20.1-1.4.0.jar", version: "1.20.1-1.4.0", keep: true }] },
};
const CP_ZONES = [
  { name: "base", dim: "minecraft:overworld", min: [-120, -64, -80], max: [-40, 320, 10] },
  { name: "farm", dim: "minecraft:overworld", min: [60, 62, -420], max: [92, 80, -396] },
];

function hostStubs() {
  window.hostWinClose = () => {}; window.hostWinMin = () => {}; window.hostWinMaxToggle = () => {};
  window.hostWinDrag = () => {}; window.hostWinIsMax = async () => false;
}

// ---------- 部署进度（按帧给的仿真数据） ----------
const MB = 1048576;
const STEP_NAMES = ["下载 Java 21 运行时", "下载 Paper 1.21.4 核心", "同意 EULA 并生成配置", "生成启动脚本"];
// [第几步在跑, 当前下载项, 已下载 MB, 总 MB]；stage 4 = 完成
const DEPLOY = [
  [0, "下载 Java 21 运行时", 6, 46], [0, "下载 Java 21 运行时", 19, 46], [0, "下载 Java 21 运行时", 33, 46], [0, "下载 Java 21 运行时", 46, 46],
  [1, "下载 paper-1.21.4-232.jar", 9, 50], [1, "下载 paper-1.21.4-232.jar", 27, 50], [1, "下载 paper-1.21.4-232.jar", 42, 50],
  [3, "下载 paper-1.21.4-232.jar", 50, 50], [4, "", 50, 50],
];
function deployTask(k) {
  const [st, label, done, total] = DEPLOY[k];
  const steps = STEP_NAMES.map((name, i) => ({ name, status: i < st ? "done" : i === st ? "running" : "pending" }));
  const ended = st >= 4;
  const log = ["[1/4] 按游戏版本选择 Java 21，开始下载（BMCLAPI 镜像）"];
  if (k <= 3) log.push(`  已下载 ${done.toFixed(1)} MB / ${total.toFixed(1)} MB`);
  if (k >= 3) log.push("  Java 21 已下载并校验");
  if (k >= 4) log.push("[2/4] 正在下载核心 paper-1.21.4-232.jar（BMCLAPI 镜像）");
  if (k >= 4 && k <= 6) log.push(`  已下载 ${done.toFixed(1)} MB / ${total.toFixed(1)} MB`);
  if (k >= 7) log.push("  核心已下载并校验", "[3/4] 已同意 EULA，写入 server.properties", "[4/4] 正在生成启动脚本…");
  if (k >= 8) log.push("  已生成 start.bat", "部署完成");
  return { steps, label: ended ? "" : label, done: done * MB, total: total * MB, error: "", warnings: [], log, ended, result: SURV };
}

// ---------- 视频里要标框、要让光标去点的元素（CSS 像素坐标，存进 out/shots/rects.json） ----------
// 写法：选择器，或「选择器|包含的文字」（取符合条件、看得见的元素里面积最小的那个）
const RECTS = {
  "wiz-core": { paper: '.core-card[data-id="paper"]', next: "#wnext" },
  "wiz-core-sel": { paper: '.core-card[data-id="paper"]', next: "#wnext" },
  "wiz-version": { v: '.ver-item[data-id="1.21.4"]', next: "#wnext" },
  "wiz-version-sel": { v: '.ver-item[data-id="1.21.4"]', next: "#wnext" },
  "wiz-settings-eula": { name: '#main input[type="text"]', eula: ".eula-box", go: "#wcreate", foot: ".wiz-foot, .save-bar" },
  "deploy-0": { steps: "#t-steps", prog: "#t-prog", log: "#task-log" },
  "deploy-8": { steps: "#t-steps", prog: "#t-prog", log: "#task-log", done: "#t-foot .btn.primary", toast: "#toasts .toast" },
  "console-stopped": { start: "#d-start", pill: "#main .st-pill", con: "#console" },
  "console-running": { start: "#d-start", pill: "#main .st-pill", con: "#console", done: "#console div|Done (", join: "#console div|Alex joined" },
  "servers": { card: '.card[data-name="生存服 · 朋友"]' },
  "banner": { banner: ".diag-banner", more: ".db-more summary", fix: ".diag-banner *|怎么修", ev: ".db-more[open] .db-ev" },
  "import": { file: ".field:has(#im-file)" },
  "import-filled": { file: ".field:has(#im-file)", name: "#im-name", go: "#im-go", eula: ".eula-box" },
  "tunnels": { t1: '[data-tun="t1"]', t2: '[data-tun="t2"]', copy: '[data-tun="t1"] [data-copy]', addr: '[data-tun="t1"] *|openfrp.top:23456', addr2: '[data-tun="t2"] *|natfrp.cloud' },
  "tunnels-copied": { t1: '[data-tun="t1"]', copy: '[data-tun="t1"] [data-copy]', toast: "#toasts .toast" },
  "remote": { qr: "#qr-box", card: ".qr-card" },
  "assistant": { panel: "#ai-panel", card: "#ai-panel .ap-card.pending", ok: '#ai-panel .ap-card.pending [data-x="ok"]', banner: ".diag-banner", user: "#ai-panel *|帮我看看是哪个模组" },
  "gameai": { prefix: "#main *|聊天以它开头才会触发", provider: "#main *|提问会发给 DeepSeek", on: "#main *|关着时 MineDeck 不看聊天" },
  "companion": { list: "#cp-list", c1: '#cp-list [data-comp="1"]', c2: '#cp-list [data-comp="2"]' },
  "music": { src: "#mu-src", start: "#main button|开始广播" },
  "settings-ai": { key: "#main *|加密保存在这台电脑上", test: "#main *|连接正常", quota: "#main *|按功能分别设每天最多用多少 token" },
  "settings-ai-top": { status: "#ai-status", intro: "#main *|不连接任何 AI 服务" },
  "settings-ai-quota": { quota: "#main .group|应用内助手", head: "#main *|每日额度" },
};

// ---------- 镜头清单 ----------
const LIST = [
  { name: "wiz-core", run: async (page, h) => {
    await h.open("#/create", "#mode-new");
    await page.click("#mode-new");
    await page.waitForSelector('.core-card[data-id="paper"]');
    await h.snap("wiz-core");
    await page.click('.core-card[data-id="paper"]');
    await h.snap("wiz-core-sel");
    await page.click("#wnext");
    await page.waitForSelector('.ver-item[data-id="1.21.4"]');
    await h.snap("wiz-version");
    await page.click('.ver-item[data-id="1.21.4"]');
    await h.snap("wiz-version-sel");
    await page.click("#wnext");
    await page.waitForTimeout(300);
    await page.click("#wnext");
    await page.waitForTimeout(300);
    // 第 4 步：实例名称填成「生存服 · 朋友」，滚到底勾上 EULA
    const name = await page.$('#main input[type="text"]');
    await name.fill(SURV);
    await h.snap("wiz-settings");
    const eula = await page.$('#main input[type="checkbox"][id*="eula"]');
    if (eula) { await eula.evaluate(e => e.scrollIntoView({ block: "center" })); await eula.check({ force: true }); }
    await h.snap("wiz-settings-eula");
  } },
  { name: "deploy", run: async (page, h) => {
    let k = 0;
    await page.route("**/api/tasks/**", r => r.fulfill({ contentType: "application/json; charset=utf-8", body: JSON.stringify({ ok: true, data: deployTask(k) }) }));
    await h.open("#/task/task-1", "#t-steps .task-step");
    for (k = 0; k < DEPLOY.length; k++) {
      const want = DEPLOY[k][0] >= 4 ? "100%" : Math.round(DEPLOY[k][2] / DEPLOY[k][3] * 100) + "%";
      await page.waitForFunction(w => { const e = document.querySelector("#t-pct"); return e && e.textContent === w; }, want, { timeout: 8000 });
      if (DEPLOY[k][0] >= 4) await page.waitForSelector("#t-foot .btn.primary");
      await page.waitForTimeout(900); // 至少再轮询一次，日志和步骤都换好
      await h.snap(`deploy-${k}`, { allowToast: true });
    }
  } },
  { name: "console-stopped", dpr: 3, run: async (page, h) => {
    await h.state({ status: { [SURV]: "stopped" } });
    await h.open(`#/inst/${enc(SURV)}/console`, "#console div");
    await h.snap("console-stopped");
  } },
  { name: "console-running", dpr: 3, console: true, run: async (page, h) => {
    // 刚开服十几分钟：运行时长改短（仿真后端固定给 2h13m）
    await page.route("**/api/instances", async r => {
      const res = await r.fetch(); const j = await res.json();
      if (Array.isArray(j.data)) for (const i of j.data) if (i.name === SURV) i.uptimeSec = 1000;
      await r.fulfill({ response: res, body: JSON.stringify(j) });
    });
    await h.open(`#/inst/${enc(SURV)}/console`, "#console div");
    await page.waitForFunction(() => document.querySelector("#console").textContent.includes("Done ("));
    await h.snap("console-running");
  } },
  { name: "servers", run: async (page, h) => {
    await h.open("#/instances", `.card[data-name="${SURV}"]`);
    await h.snap("servers");
  } },
  { name: "banner", run: async (page, h) => {
    await h.open(`#/inst/${enc(PACK)}/console`, ".diag-banner");
    await page.click(".db-more summary");
    await page.waitForSelector(".db-more[open] .db-ev");
    await h.snap("banner");
  } },
  { name: "import", run: async (page, h) => {
    await h.open("#/create", "#mode-import");
    await page.click("#mode-import");
    await page.waitForSelector("#im-file");
    await h.snap("import");
    await page.setInputFiles("#im-file", { name: "机械动力整合包-1.4.mrpack", mimeType: "application/x-modrinth-modpack+zip", buffer: Buffer.from("PK\u0003\u0004") });
    await page.fill("#im-name", PACK);
    await page.check("#im-eula", { force: true });
    await h.snap("import-filled");
  } },
  { name: "tunnels", dpr: 3, run: async (page, h) => {
    // 第二条隧道换成樱花穿透、绑定整合包服、正在运行（仿真后端初始是一条报错的自建线路）
    await h.state({ tunnel: { id: "t2", name: "整合包联机", provider: "natfrp", running: true, online: true, boundInstance: PACK, autoStart: true, publicAddr: "frp-oak.natfrp.cloud:41823", lastError: "" } });
    await h.open("#/tunnels", '[data-tun="t1"] [data-copy]');
    await h.snap("tunnels");
    await page.click('[data-tun="t1"] [data-copy]');
    await h.toast("已复制公网地址");
    await page.waitForTimeout(400);
    await h.snap("tunnels-copied", { allowToast: true });
  } },
  { name: "remote", run: async (page, h) => {
    await h.state({ listenLan: true });
    await h.open("#/settings/remote", "#qr-box svg");
    await h.snap("remote");
  } },
  { name: "phone-servers", mobile: true, run: async (page, h) => {
    await h.open("#/instances", `.card[data-name="${SURV}"]`);
    await h.snap("phone-servers");
  } },
  { name: "phone-console", mobile: true, console: true, run: async (page, h) => {
    await h.open(`#/inst/${enc(SURV)}/console`, "#console div");
    await page.waitForFunction(() => document.querySelector("#console").textContent.includes("Done ("));
    await h.snap("phone-console");
  } },
  { name: "assistant", dpr: 3, run: async (page, h) => {
    await h.state({ ai: AI, asst: { reset: true } });
    await h.open(`#/inst/${enc(PACK)}/console`, '.diag-banner [data-x="ai"]');
    await page.click('.diag-banner [data-x="ai"]');
    await page.waitForSelector(".modal.ap-pv textarea");
    await page.fill(".modal.ap-pv textarea", "整合包一开服就崩，帮我看看是哪个模组的问题");
    await page.click('.modal [data-x="ok"]');
    await page.waitForSelector("#ai-panel .ap-card.pending", { timeout: 10000 });
    await page.waitForFunction(() => { const b = document.querySelector("#ap-stop"); return b && b.hidden; }, null, { timeout: 10000 });
    await page.waitForFunction(() => document.querySelectorAll("#ai-panel .ap-usage").length >= 2, null, { timeout: 10000 });
    await h.snap("assistant");
  } },
  { name: "gameai", run: async (page, h) => {
    const log = await h.api(`/api/instances/${enc(SURV)}/gameai/log`);
    await h.state({ ai: AI, gc: { reset: true, inst: { [PACK]: { whitelistOn: true, log, settings: { enabled: true, allowed: ["Alex", "Steve"] } } } } });
    await h.state({ status: { [PACK]: "starting" } }); await h.state({ status: { [PACK]: "running" } });
    await h.open(`#/inst/${enc(PACK)}/gameai`, "#gc-log .gc-rec");
    await h.snap("gameai");
  } },
  { name: "companion", run: async (page, h) => {
    await h.state({ ai: AI, gc: { inst: { [PACK]: { whitelistOn: true, settings: { enabled: true, allowed: ["Alex", "Steve"] } } } },
      cp: { reset: true, inst: { [PACK]: { mods: CP_INSTALLED, settings: { zones: CP_ZONES } } } } });
    await h.state({ status: { [PACK]: "starting" } }); await h.state({ status: { [PACK]: "running" } });
    await h.open(`#/inst/${enc(PACK)}/companion`, "#cp-on:not([disabled])");
    await page.click("#cp-on");
    await h.toast("已打开 AI 伙伴");
    await h.open(`#/inst/${enc(PACK)}/companion`, "#cp-spawn:not([disabled])");
    for (const [name, owner] of [["小方", "Steve"], ["阿土", "Alex"]]) {
      await page.click("#cp-spawn");
      await page.waitForSelector(".modal #cs-name");
      await page.fill("#cs-name", name);
      await page.fill("#cs-owner", owner);
      await page.click('.modal [data-x="ok"]');
      await h.toast(`刷出了伙伴「${name}」`);
      await page.waitForFunction(() => !document.querySelector("#modal-root").childElementCount);
    }
    await h.state({ cp: { busy: true } });
    await h.open(`#/inst/${enc(PACK)}/companion`, '#cp-list [data-comp="2"]');
    await h.scrollTo("#cp-list", 120);
    await h.snap("companion");
  } },
  { name: "music", run: async (page, h) => {
    await h.state({ ai: AI, music: { reset: true, voiceTunnel: true, inst: { [PACK]: { bridge: "main", voiceHost: "frp-abc.natfrp.cloud:34567" } } } });
    await h.state({ status: { [PACK]: "starting" } }); await h.state({ status: { [PACK]: "running" } });
    await h.open(`#/inst/${enc(PACK)}/music`, '#mu-src [data-exe="cloudmusic.exe"][aria-checked="true"] .xp i.on');
    await h.snap("music");
  } },
  { name: "settings-ai", run: async (page, h) => {
    await h.state({ ai: AI });
    await h.open("#/settings/ai", "#ai-status .tag");
    await page.click("#ai-test");
    await page.waitForFunction(() => document.querySelector("#ai-test-sub").textContent.includes("连接正常"));
    await h.snap("settings-ai");
    // 往下滚到「每日额度」（按 token 算）
    await page.evaluate(() => {
      const m = document.querySelector("#main");
      const head = [...m.querySelectorAll("h2, h3, .sh-title, .set-head *")].find(e => e.textContent.trim().startsWith("每日额度"));
      if (head) m.scrollTop += head.getBoundingClientRect().top - m.getBoundingClientRect().top - 90;
    });
    await h.snap("settings-ai-quota");
    await page.evaluate(() => { document.querySelector("#main").scrollTop = 0; });
    await h.snap("settings-ai-top");
  } },
];

// 「刚开服」的控制台：仿真后端的示例日志去掉插件报错那几行，第三个进服的人换个名字
const CLEAN_CONSOLE = [
  "[21:30:02 INFO]: Starting minecraft server version 1.21.4",
  "[21:30:02 INFO]: Loading properties",
  "[21:30:03 INFO]: This server is running Paper version 1.21.4-232-main@a1b2c3d (2026-10-01T12:00:00Z)",
  "[21:30:04 WARN]: Using offline mode! Players will not be authenticated.",
  "[21:30:05 INFO]: Preparing level \"world\"",
  "[21:30:09 INFO]: Done (6.812s)! For help, type \"help\"",
  "[21:42:17 INFO]: Steve joined the game",
  "[21:43:01 INFO]: Alex joined the game",
  "[21:44:30 INFO]: <Steve> 大家好！",
  "> say 欢迎来到生存服",
  "[21:44:35 INFO]: [Server] 欢迎来到生存服",
  "[21:46:12 INFO]: Kai_07 joined the game",
];
// 页面里：只把这台服务器的控制台流换成上面几行（不断开、不报错），别的 EventSource 照常
function fakeConsole({ name, lines }) {
  const Real = window.EventSource;
  const path = "/api/instances/" + encodeURIComponent(name) + "/console";
  window.EventSource = function (url, opts) {
    if (String(url).includes(path)) {
      const fake = { readyState: 1, url: String(url), onmessage: null, onerror: null, onopen: null, close() { this.readyState = 2; }, addEventListener() {}, removeEventListener() {} };
      setTimeout(() => { for (const l of lines) if (fake.readyState === 1 && fake.onmessage) fake.onmessage({ data: JSON.stringify(l) }); }, 30);
      return fake;
    }
    return new Real(url, opts);
  };
}

// ---------- 工具 ----------
async function calm(page, { allowToast = false } = {}) {
  await page.mouse.move(-10, -10).catch(() => {});
  await page.mouse.move(150, 600);
  await page.waitForFunction(a => (a || !document.querySelector("#toasts").childElementCount)
    && !document.querySelector("#modal-root").childElementCount && !document.querySelector(".dropmenu"), allowToast, { timeout: 20000 });
  await page.waitForFunction(() => document.getAnimations().every(a => (a.effect && a.effect.getTiming().iterations === Infinity) || a.playState !== "running"), null, { timeout: 10000 });
  await page.evaluate(() => document.fonts.ready);
  await page.waitForTimeout(500);
}

async function withPage(browser, shot, fn) {
  const mock = await start({ port: PORT, ui: UI });
  const mobile = !!shot.mobile;
  const ctx = await browser.newContext(mobile
    ? { viewport: { width: 390, height: 844 }, deviceScaleFactor: 3, isMobile: true, hasTouch: true, locale: "zh-CN", timezoneId: "Asia/Shanghai", colorScheme: "light" }
    : { viewport: { width: 1280, height: 800 }, deviceScaleFactor: shot.dpr || 2, locale: "zh-CN", timezoneId: "Asia/Shanghai", colorScheme: "light" });
  if (shot.console) await ctx.addInitScript(fakeConsole, { name: SURV, lines: CLEAN_CONSOLE });
  await ctx.grantPermissions(["clipboard-read", "clipboard-write"], { origin: mock.url.replace(/\/$/, "") });
  await ctx.addInitScript(() => { localStorage.setItem("amh_token", "mock"); localStorage.setItem("amh_theme", "light"); localStorage.setItem("amh_scheme", "craft"); localStorage.setItem("amh_accent", "auto"); });
  if (!mobile) await ctx.addInitScript(hostStubs);
  // 版本号：和片尾一样写 3.0.0
  await ctx.route("**/api/app", async r => { const res = await r.fetch(); const j = await res.json(); if (j.data) j.data.version = VERSION; await r.fulfill({ response: res, body: JSON.stringify(j) }); });
  await ctx.route("**/api/update/check", async r => { const res = await r.fetch(); const j = await res.json(); if (j.data) j.data.current = VERSION; await r.fulfill({ response: res, body: JSON.stringify(j) }); });
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
    scrollTo: (sel, pad) => page.evaluate(([s, p]) => {
      const m = document.querySelector("#main"), box = document.querySelector(s);
      const target = box.previousElementSibling && !box.previousElementSibling.id ? box.previousElementSibling : box;
      const y = target.getBoundingClientRect().top - m.getBoundingClientRect().top + m.scrollTop - p;
      m.scrollTop = Math.max(0, Math.min(y, m.scrollHeight - m.clientHeight));
    }, [sel, pad]),
    snap: async (name, opts = {}) => {
      await calm(page, opts);
      const file = path.join(SHOTS, name + ".png");
      await page.screenshot({ path: file });
      if (RECTS[name]) {
        const r = await page.evaluate(specs => {
          const out = {};
          for (const [k, spec] of Object.entries(specs)) {
            const i = spec.indexOf("|"), sel = i < 0 ? spec : spec.slice(0, i), txt = i < 0 ? "" : spec.slice(i + 1);
            const els = [...document.querySelectorAll(sel)].filter(e => e.getClientRects().length && (!txt || e.textContent.includes(txt)))
              .map(e => ({ e, b: e.getBoundingClientRect() })).filter(o => o.b.width > 0 && o.b.height > 0);
            if (!els.length) continue;
            els.sort((a, b) => a.b.width * a.b.height - b.b.width * b.b.height);
            const b = txt ? els[0].b : els.find(o => o.e === [...document.querySelectorAll(sel)].find(x => x.getClientRects().length)).b;
            out[k] = { x: Math.round(b.left * 10) / 10, y: Math.round(b.top * 10) / 10, w: Math.round(b.width * 10) / 10, h: Math.round(b.height * 10) / 10 };
          }
          return out;
        }, RECTS[name]);
        const missing = Object.keys(RECTS[name]).filter(k => !r[k]);
        if (missing.length) console.warn(`  ${name}: rect not found: ${missing.join(", ")}`);
        rects[name] = r;
      }
      console.log("captured", name);
    },
  };
  try { await fn(page, h); } finally { await ctx.close(); await mock.close(); }
  return errors;
}

// assets/icon.ico 的 256×256 PNG 帧
function extractIcon() {
  const ico = fs.readFileSync(path.join(REPO, "assets", "icon.ico"));
  const count = ico.readUInt16LE(4);
  for (let i = 0; i < count; i++) {
    const e = 6 + i * 16;
    if (ico[e] === 0 && ico[e + 1] === 0) {
      const size = ico.readUInt32LE(e + 8), off = ico.readUInt32LE(e + 12);
      const png = ico.subarray(off, off + size);
      if (png.readUInt32BE(0) !== 0x89504e47) throw new Error("256px frame is not PNG");
      fs.writeFileSync(path.join(ASSETS, "icon-256.png"), png);
      return;
    }
  }
  throw new Error("no 256px frame in icon.ico");
}

// 启动画面的 16×16 像素网格（internal/web/ui/index.html 里的 PAL / PX），视频开头的方块拼图标用它
function extractPixelIcon() {
  const html = fs.readFileSync(path.join(UI, "index.html"), "utf8");
  const pal = /var PAL = "([^"]+)"/.exec(html), px = /var PX = "([^"]+)"/.exec(html);
  if (!pal || !px) throw new Error("splash PAL/PX not found in index.html");
  const PAL = pal[1].split(" ").map(c => "#" + c), PX = px[1];
  if (PX.length !== 256) throw new Error("PX is not 16×16");
  const cells = [];
  for (let i = 0; i < 256; i++) { const c = PX[i]; if (c !== ".") cells.push({ x: i % 16, y: i >> 4, c: PAL[c.charCodeAt(0) - 97] }); }
  fs.writeFileSync(path.join(ASSETS, "pixel-icon.json"), JSON.stringify({ size: 16, cells }));
}

const rects = {};

(async () => {
  fs.mkdirSync(SHOTS, { recursive: true });
  const rectFile = path.join(SHOTS, "rects.json");
  if (fs.existsSync(rectFile)) Object.assign(rects, JSON.parse(fs.readFileSync(rectFile, "utf8")));
  fs.mkdirSync(ASSETS, { recursive: true });
  extractIcon();
  extractPixelIcon();
  const browser = await pw.chromium.launch({ channel: "chrome" });
  let bad = 0;
  try {
    for (const s of LIST) {
      if (ONLY && !ONLY.test(s.name)) continue;
      const errors = await withPage(browser, s, s.run);
      if (errors.length) { bad++; console.error(`${s.name}: page errors: ${errors.join(" | ")}`); }
    }
  } finally {
    await browser.close();
    fs.writeFileSync(rectFile, JSON.stringify(rects, null, 1));
  }
  if (bad) process.exitCode = 1;
})().catch(e => { console.error(e); process.exit(1); });
