// 逐帧录制的「导演」：在仿真后端上打开真实界面，脚本像人一样点、打字，每帧推一次虚拟时间再截图。
// 场景脚本（scenes.js）是一串 await：每个动作都会往前走若干帧，所以脚本的先后就是片子的先后。
//
// 为什么这样录（试出来的，详见 docs/交接说明.md）：
//   - CDP 的虚拟时钟不能用：时钟一停，鼠标事件就卡死。这里用 Playwright 的 page.clock 接管页面里的定时器、Date、rAF，
//     CSS 动画和过渡每帧暂停后手动拨到位（overlay.js 的 installSeek）。
//   - 镜头推近用截图的 clip：clip 的 scale 会让 Chrome 按更高倍率重新栅格化，推近以后字还是清楚的。
//   - 另开的 CDP 会话不带 Playwright 的 DPR，clip 里的 scale 要自己算。
"use strict";
const path = require("path");
const fs = require("fs");
const { spawn } = require("child_process");
const O = require("./overlay.js");

const HERE = __dirname;
const REPO = path.resolve(HERE, "..", "..", "..");
const UI = path.join(REPO, "internal", "web", "ui");
let pw;
try { pw = require("playwright"); } catch { pw = require(path.join(REPO, "tools", "ui-dev", "node_modules", "playwright")); }
const { start } = require(path.join(REPO, "tools", "ui-dev", "server.js"));

const W = 1280, H = 720, OUT_W = 2560, OUT_H = 1440, FPS = 60;
const FIXED = Date.UTC(2026, 10, 6, 12, 0, 0); // 虚拟时钟的起点：北京时间 11 月 6 日 20:00
const START_MS = 2000;                          // 录制从虚拟时间第 2 秒开始，页面要在这之前就绪
const VERSION = "3.0.0";
const msAt = f => Math.round(f * 1000 / FPS);
const clamp = (v, a, b) => Math.max(a, Math.min(b, v));
const ease = x => x < 0.5 ? 4 * x * x * x : 1 - Math.pow(-2 * x + 2, 3) / 2;
const easeOut = x => 1 - Math.pow(1 - x, 3);

// 上屏文字的规矩，和宣传片一样：只用 GB2312 里的字（内嵌的 Noto Sans SC 子集按它做的），一行不超过 22 个字符，没有感叹号和夸张词
const gb = new Set();
{
  const dec = new TextDecoder("gbk");
  for (let hi = 0xa1; hi <= 0xf7; hi++) {
    if (hi > 0xa9 && hi < 0xb0) continue;
    for (let lo = 0xa1; lo <= 0xfe; lo++) { const c = dec.decode(new Uint8Array([hi, lo])); if (c && c !== "�") gb.add(c); }
  }
}
const BANNED = ["轻松", "一键搞定", "告别", "黑科技", "神器", "!", "！"];
function checkText(s, max) {
  for (const ch of s) if (ch.codePointAt(0) >= 0x80 && !gb.has(ch)) throw new Error(`"${ch}" is not in GB2312: ${s}`);
  for (const b of BANNED) if (s.includes(b)) throw new Error(`banned "${b}": ${s}`);
  if (max && [...s].length > max) throw new Error(`${[...s].length} characters > ${max}: ${s}`);
}

function hostStubs() {
  window.hostWinClose = () => {}; window.hostWinMin = () => {}; window.hostWinMaxToggle = () => {};
  window.hostWinDrag = () => {}; window.hostWinIsMax = async () => false;
}

// assets/icon.ico 里 256×256 的 PNG 帧，做成 data URL 给章节卡用
function iconDataUrl() {
  const ico = fs.readFileSync(path.join(REPO, "assets", "icon.ico"));
  const count = ico.readUInt16LE(4);
  for (let i = 0; i < count; i++) {
    const e = 6 + i * 16;
    if (ico[e] === 0 && ico[e + 1] === 0) {
      const png = ico.subarray(ico.readUInt32LE(e + 12), ico.readUInt32LE(e + 12) + ico.readUInt32LE(e + 8));
      return "data:image/png;base64," + png.toString("base64");
    }
  }
  throw new Error("no 256px frame in icon.ico");
}

class Director {
  constructor(opts) {
    Object.assign(this, opts); // seg, out, preview(每隔多少帧存一张，0 = 正式录)
    this.f = 0; this.vnow = 0; this.rng = 12345; this.tw = []; this.capLog = [];
    this.s = { t: 0, cam: { x: 0, y: 0, w: W }, cur: { x: 760, y: 520, show: true }, ring: 0, caret: true, caretT: 0, marks: [],
      cap: { op: 0, dy: 0 }, badge: 0, sand: 1, chap: 0, card: { op: 0, t0: 0 }, chip: { op: 0, x: 0, y: 0 } };
    this.curCap = null; this.icon = iconDataUrl();
  }

  rand() { this.rng = (this.rng * 16807) % 2147483647; return this.rng / 2147483647; }

  // ---------- 启动 ----------
  async boot({ hash, ready, seedLog, setup, routes, viewport }) {
    this.mock = await start({ port: 0, ui: UI });
    if (setup) await setup(this.mock.state, this.mock, this);
    this.browser = await pw.chromium.launch({ channel: "chrome" });
    const ctx = this.ctx = await this.browser.newContext({ viewport: viewport || { width: W, height: H }, deviceScaleFactor: 2, locale: "zh-CN", timezoneId: "Asia/Shanghai", colorScheme: "light" });
    await ctx.grantPermissions(["clipboard-read", "clipboard-write"], { origin: this.mock.url.replace(/\/$/, "") });
    await ctx.addInitScript(() => { localStorage.setItem("amh_token", "mock"); localStorage.setItem("amh_theme", "light"); localStorage.setItem("amh_scheme", "craft"); localStorage.setItem("amh_accent", "auto"); });
    await ctx.addInitScript(hostStubs);
    await ctx.addInitScript(O.fakeEventSource, seedLog || {});
    await ctx.addInitScript(O.instantScroll);
    await ctx.addInitScript(O.installSeek);
    // 仿真数据里的 Notch_CN 借的是游戏作者的名字，公开的片子里换掉（事件流是长连接，不经过这里）
    await ctx.route("**/api/**", async r => {
      if (r.request().resourceType() === "eventsource") return r.fallback();
      const res = await r.fetch();
      const body = await res.text();
      await r.fulfill({ response: res, body: body.split("Notch_CN").join("Kai_07") });
    });
    const patch = async (r, fn) => { const res = await r.fetch(); const j = await res.json(); if (j.data) fn(j.data); await r.fulfill({ response: res, body: JSON.stringify(j) }); };
    await ctx.route("**/api/app", r => patch(r, d => { d.version = VERSION; }));
    await ctx.route("**/api/update/check", r => patch(r, d => { d.current = VERSION; }));
    const st = this.mock.state;
    const json = (r, data) => r.fulfill({ contentType: "application/json; charset=utf-8", body: JSON.stringify({ ok: true, data }) });
    this.json = json;
    // 仿真后端的启停只回 ok、不改状态：这里替它改
    await ctx.route("**/api/instances/*/start", async r => {
      if (r.request().method() !== "POST") return r.fallback();
      const i = st.instances.find(x => x.name === decodeURIComponent(new URL(r.request().url()).pathname.split("/")[3]));
      if (i) { i.status = "starting"; i.uptimeSec = 2; i.onlineCount = 0; }
      await json(r, {});
    });
    await ctx.route("**/api/instances/*/stop", async r => {
      if (r.request().method() !== "POST") return r.fallback();
      const i = st.instances.find(x => x.name === decodeURIComponent(new URL(r.request().url()).pathname.split("/")[3]));
      if (i) { i.status = "stopped"; i.uptimeSec = 0; i.onlineCount = 0; }
      await json(r, {});
    });
    if (routes) await routes(ctx, json, st, this);
    const page = this.page = await ctx.newPage();
    this.errors = [];
    page.on("pageerror", e => this.errors.push(e.message));
    this.pending = new Set(); this.netSeq = 0; this.lastNet = 0;
    const net = () => { this.netSeq++; this.lastNet = Date.now(); };
    page.on("request", r => { if (r.resourceType() !== "eventsource") { this.pending.add(r); net(); } });
    page.on("requestfinished", r => { if (this.pending.delete(r)) net(); });
    page.on("requestfailed", r => { if (this.pending.delete(r)) net(); });
    this.cdp = await ctx.newCDPSession(page);
    await page.clock.install({ time: FIXED });
    await page.clock.pauseAt(FIXED + 1000); // 时钟停住再加载：页面注册定时器时的虚拟时刻每次都一样
    await page.goto(this.mock.url + (hash || "#/instances"), { waitUntil: "load" });
    await this.netIdle(-1);
    let k = 0;
    while (this.vnow < START_MS) { await this.tick(msAt(k + 1) - msAt(k)); k++; }
    const ok = await page.evaluate(s => document.fonts.status === "loaded" && (!s || !!document.querySelector(s)), ready || "");
    if (!ok) throw new Error(`page not ready by ${START_MS} ms: ${ready}`);
    await page.evaluate(O.installOverlay, { icon: this.icon });
    if (!this.preview) this.openSink();
  }

  openSink() {
    fs.mkdirSync(path.dirname(this.out), { recursive: true });
    // 中间文件用无损 RGB：最后 assemble.js 才转 BT.709 的 yuv420p，这里不损失任何东西
    this.ff = spawn("ffmpeg", ["-hide_banner", "-loglevel", "error", "-y", "-f", "image2pipe", "-framerate", String(FPS), "-c:v", "png", "-i", "-",
      "-c:v", "libx264rgb", "-preset", "ultrafast", "-crf", "0", "-r", String(FPS), this.out], { stdio: ["pipe", "inherit", "inherit"] });
    this.ffDone = new Promise((res, rej) => this.ff.on("close", c => c ? rej(new Error("ffmpeg exited with " + c)) : res()));
  }

  async finish() {
    if (this.ff) { this.ff.stdin.end(); await this.ffDone; }
    await this.ctx.close(); await this.browser.close(); await this.mock.close();
    if (this.errors.length) throw new Error("page errors: " + this.errors.join(" | "));
    return { frames: this.f, captions: this.capLog };
  }

  // ---------- 一帧 ----------
  async netIdle(seq0) {
    const t0 = Date.now();
    while (this.pending.size > 0 || (this.netSeq !== seq0 && Date.now() - this.lastNet < 40)) {
      if (Date.now() - t0 > 15000) throw new Error("network never idle: " + [...this.pending].map(r => r.method() + " " + r.url()).join(", "));
      await new Promise(r => setTimeout(r, 4));
    }
  }

  async tick(ms) {
    const seq0 = this.netSeq;
    await this.page.clock.runFor(ms);
    await this.netIdle(seq0);
    this.vnow += ms;
    await this.page.evaluate(n => window.__seek(n), this.vnow);
  }

  async frame() {
    const t = this.f / FPS, s = this.s;
    s.t = t;
    for (const w of this.tw) {
      const u = clamp((t - w.t0) / w.dur, 0, 1);
      w.fn(w.ease(u));
      if (u >= 1) w.done = true;
    }
    this.tw = this.tw.filter(w => !w.done);
    if (s.cur.x !== this.mx || s.cur.y !== this.my) { await this.page.mouse.move(s.cur.x, s.cur.y); this.mx = s.cur.x; this.my = s.cur.y; }
    await this.tick(msAt(this.f + 1) - msAt(this.f));
    await this.page.evaluate(x => window.__ov(x), s);
    if (this.preview) {
      if (this.f % this.preview === 0) fs.writeFileSync(path.join(this.out, `f_${String(this.f).padStart(5, "0")}.png`), await this.shot(0.5));
    } else {
      const buf = await this.shot(1);
      if (!this.ff.stdin.write(buf)) await new Promise(r => this.ff.stdin.once("drain", r));
    }
    this.f++;
  }

  async shot(scale) {
    const c = this.s.cam, h = c.w * 9 / 16;
    const { data } = await this.cdp.send("Page.captureScreenshot", { format: "png", optimizeForSpeed: true, clip: { x: c.x, y: c.y, width: c.w, height: h, scale: OUT_W / c.w * scale } });
    return Buffer.from(data, "base64");
  }

  // ---------- 基本动作 ----------
  async frames(n) { for (let i = 0; i < n; i++) await this.frame(); }
  async wait(sec) { await this.frames(Math.round(sec * FPS)); }
  bg(dur, fn, e = ease) { this.tw.push({ t0: this.f / FPS, dur, fn, ease: e }); }
  async run(dur, fn, e = ease) { this.bg(dur, fn, e); await this.wait(dur + 1 / FPS); }

  async until(spec, { max = 8, then = 0.1 } = {}) {
    for (let i = 0; i < max * FPS; i++) {
      if (await this.page.evaluate(x => typeof x === "string" ? window.__exists(x) : !!eval(x.js), spec)) { if (then) await this.wait(then); return; }
      await this.frame();
    }
    throw new Error(`timed out waiting for ${typeof spec === "string" ? spec : spec.js}`);
  }

  async rect(spec) {
    const r = await this.page.evaluate(x => window.__rect(x), spec);
    if (!r) throw new Error("not found: " + spec);
    return r;
  }
  async point(t) {
    if (typeof t !== "string") return t;
    const r = await this.rect(t);
    return { x: r.x + r.w / 2, y: r.y + r.h / 2 };
  }

  async moveTo(target, { dur, dx = 0, dy = 0 } = {}) {
    if (typeof target === "string" && await this.page.evaluate(x => window.__needScroll(x), target)) await this.scrollTo(target, { pad: 240, dur: 0.5 });
    const p = await this.point(target), c = this.s.cur, x0 = c.x, y0 = c.y, x1 = p.x + dx, y1 = p.y + dy;
    const dist = Math.hypot(x1 - x0, y1 - y0), d = dur ?? clamp(0.28 + dist / 1500, 0.28, 0.85);
    const arc = dist * 0.05 * (this.rand() < 0.5 ? -1 : 1), nx = dist ? -(y1 - y0) / dist : 0, ny = dist ? (x1 - x0) / dist : 0;
    await this.run(d, u => { const b = Math.sin(Math.PI * u) * arc; c.x = x0 + (x1 - x0) * u + nx * b; c.y = y0 + (y1 - y0) * u + ny * b; });
    c.x = x1; c.y = y1;
  }

  async click(target, { after = 0.3, dx, dy, dur } = {}) {
    await this.moveTo(target, { dx, dy, dur });
    await this.wait(0.1);
    await this.page.mouse.down();
    const s = this.s;
    this.bg(0.34, u => { s.ring = u >= 1 ? 0 : u; }, x => x);
    await this.frames(4);
    await this.page.mouse.up();
    await this.wait(after);
  }

  async type(target, text, { gap = 0.09, click = true, clear = false } = {}) {
    if (target && click) await this.click(target, { after: 0.12 });
    if (clear) { await this.page.keyboard.press("Control+A"); await this.page.keyboard.press("Delete"); await this.frames(3); }
    for (const ch of text) {
      await this.page.keyboard.insertText(ch);
      this.s.caretT = this.f / FPS;
      await this.frames(Math.round(FPS * gap * (0.7 + this.rand() * 0.6)));
    }
  }
  async press(key, { after = 0.2 } = {}) { await this.page.keyboard.press(key); this.s.caretT = this.f / FPS; await this.wait(after); }

  async drag(fromSpec, toX, { dur = 0.8, y } = {}) { // 拖滑条：从滑块现在的位置拖到 toX
    const p = await this.point(fromSpec);
    await this.moveTo({ x: p.x, y: y ?? p.y });
    await this.page.mouse.down();
    const c = this.s.cur, x0 = c.x;
    await this.run(dur, u => { c.x = x0 + (toX - x0) * u; });
    await this.page.mouse.up();
    await this.wait(0.2);
  }

  async scrollTo(spec, { pad = 90, dur = 0.7, box } = {}) {
    const r = await this.page.evaluate(([s, p, b]) => window.__mainTop(s, p, b), [spec, pad, box || ""]);
    if (!r) throw new Error("scroll target not found: " + spec);
    await this.scrollY(r.from, r.to, dur, box);
  }
  async scrollY(from, to, dur, box) {
    const n = Math.round(dur * FPS);
    for (let i = 1; i <= n; i++) { await this.page.evaluate(([y, b]) => window.__scrollMain(y, b), [from + (to - from) * ease(i / n), box || ""]); await this.frame(); }
  }

  // ---------- 镜头 ----------
  async cam(target, { pad = 26, dur = 0.8, minW = 620, up = 0 } = {}) {
    const r = typeof target === "string" ? await this.rect(target) : target;
    const c = this.s.cam, c0 = { ...c };
    const w = clamp(Math.max(r.w + pad * 2, (r.h + pad * 2) * 16 / 9), minW, W), h = w * 9 / 16;
    const x = clamp(r.x + r.w / 2 - w / 2, 0, W - w), y = clamp(r.y + r.h / 2 - h / 2 + up, 0, H - h);
    this.bg(dur, u => { // 宽度按比例插值，推近的速度看上去才均匀
      c.w = c0.w * Math.pow(w / c0.w, u); c.x = c0.x + (x - c0.x) * u; c.y = c0.y + (y - c0.y) * u;
    });
  }
  async camReset({ dur = 0.7 } = {}) { await this.cam({ x: 0, y: 0, w: W, h: H }, { pad: 0, dur, minW: W }); }

  // ---------- 高亮框 ----------
  mark(spec, { dim = false, pad = 6, id = spec, radius } = {}) {
    const m = { id, spec, dim, pad, op: 0, radius };
    this.s.marks.push(m);
    this.bg(0.22, u => { m.op = u; });
  }
  unmark(id) {
    const list = this.s.marks.filter(m => !id || m.id === id);
    for (const m of list) this.bg(0.2, u => { m.op = 1 - u; if (u >= 1) this.s.marks = this.s.marks.filter(x => x !== m); });
  }

  // ---------- 字幕卡 ----------
  // 下一条字幕要等上一条读完：按字数算最短停留时间，所以没有口播也不会赶
  async caption({ eb, main, sub, pos = "left", hold } = {}) {
    await this.readGuard();
    const s = this.s, now = () => this.f / FPS;
    if (s.cap.op > 0) await this.run(0.12, u => { s.cap.op = 1 - u; }, x => x);
    if (this.curCap) { this.curCap.t1 = now(); this.capLog.push(this.curCap); this.curCap = null; }
    if (!main) return;
    if (eb) checkText(eb, 12);
    checkText(main, 26);
    if (sub) checkText(sub, 34);
    await this.page.evaluate(x => window.__capText(x), { eb, main, sub, pos });
    const n = [...(main + (sub || ""))].length;
    this.curCap = { t0: now(), eb, main, sub, need: hold ?? 0.9 + n * 0.1 };
    this.bg(0.22, u => { s.cap.op = u; s.cap.dy = (1 - u) * 8; }, easeOut);
  }
  async readGuard() {
    if (!this.curCap) return;
    const left = this.curCap.need - (this.f / FPS - this.curCap.t0);
    if (left > 0) await this.wait(left);
  }
  async noCaption() { await this.caption({}); }

  async badge(text) { const s = this.s; await this.page.evaluate(x => window.__badgeText(x), text); this.bg(0.2, u => { s.badge = u; }); }
  badgeOff() { const s = this.s; this.bg(0.2, u => { s.badge = 1 - u; }); }

  // ---------- 章节卡和全屏卡 ----------
  async chapter(no, title, sub, { keep = false, hold = 1.15 } = {}) {
    checkText(title, 20);
    if (sub) checkText(sub, 40);
    const s = this.s;
    s.chap = 0; s.sand = 1;
    await this.page.evaluate(x => window.__chapText(x), { no, title, sub });
    await this.run(0.3, u => { s.chap = u; }, x => x);
    await this.wait(hold);
    await this.run(0.25, u => { s.chap = 1 - u; }, x => x);
    if (!keep) await this.run(0.35, u => { s.sand = 1 - u; }, x => x); // keep：后面接全屏卡，沙色底不撤
  }
  async fadeOut() {
    await this.readGuard();
    await this.noCaption();
    const s = this.s;
    await this.run(0.35, u => { s.sand = u; }, x => x);
  }
  // 换一个页面：沙色蒙版闪一下，盖住页面切换
  async go(hash, ready) {
    const s = this.s;
    await this.run(0.16, u => { s.sand = u; }, x => x);
    await this.page.evaluate(h => { document.querySelector("#main").scrollTop = 0; location.hash = h; }, hash);
    await this.frames(2);
    if (ready) await this.until(ready);
    await this.wait(0.45); // 入场动画在蒙版后面走完
    await this.run(0.16, u => { s.sand = 1 - u; }, x => x);
  }

  // 全屏示意卡：html 里写了 data-in="秒" 的元素按时间依次出现
  async card(html) {
    for (const t of html.replace(/src="[^"]*"/g, "").split(/<[^>]+>/)) checkText(t, 40);
    const s = this.s;
    if (s.card.op > 0) await this.run(0.16, u => { s.sand = u; }, x => x); // 卡换卡：沙色闪一下
    await this.page.evaluate(x => window.__card(x), html);
    s.card.t0 = this.f / FPS;
    s.cur.show = false;
    if (s.sand > 0) { s.card.op = 1; s.sand = 0; } // 章节卡（keep）接过来：底色一样，直接换
    else await this.run(0.3, u => { s.card.op = u; }, x => x);
  }
  // AI 面板的回答可能比面板长：滚到底，让待确认的卡片露出来
  async scrollIntoPanel() {
    await this.page.evaluate(() => { const c = document.querySelector("#ai-panel .ap-card.pending"); if (c) c.scrollIntoView({ block: "center" }); });
    await this.frames(2);
  }
  async cardOff() { const s = this.s; s.cur.show = true; await this.run(0.3, u => { s.card.op = 1 - u; }, x => x); }

  // 拖文件的示意：一个文件小标签从 from 飘到 to，光标跟着
  async chip(text, from, to, { dur = 0.9 } = {}) {
    const s = this.s, c = s.cur;
    await this.page.evaluate(x => window.__chipText(x), text);
    Object.assign(s.chip, { op: 0, x: from.x, y: from.y });
    Object.assign(c, { x: from.x, y: from.y + 8 });
    await this.run(0.18, u => { s.chip.op = u; }, x => x);
    await this.run(dur, u => { s.chip.x = from.x + (to.x - from.x) * u; s.chip.y = from.y + (to.y - from.y) * u; c.x = s.chip.x; c.y = s.chip.y + 8; });
    await this.run(0.18, u => { s.chip.op = 1 - u; }, x => x);
  }

  // 下拉框不点开（原生下拉列表画不进截图），直接改值并触发 change
  async setSelect(sel, value) {
    await this.page.evaluate(([s, v]) => { const e = document.querySelector(s); e.value = v; e.dispatchEvent(new Event("input", { bubbles: true })); e.dispatchEvent(new Event("change", { bubbles: true })); }, [sel, value]);
    await this.frames(2);
  }

  // 控制台的一行日志
  async log(name, line) { await this.page.evaluate(([n, l]) => window.__emit(n, l), [name, line]); }

  // 手机竖屏截图，返回 data URL（给示意卡用）
  async phoneShot(hash, ready) {
    const ctx = await this.browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, isMobile: true, hasTouch: true, locale: "zh-CN", timezoneId: "Asia/Shanghai", colorScheme: "light" });
    await ctx.addInitScript(() => { localStorage.setItem("amh_token", "mock"); localStorage.setItem("amh_theme", "light"); localStorage.setItem("amh_scheme", "craft"); localStorage.setItem("amh_accent", "auto"); });
    await ctx.route("**/api/app", async r => { const res = await r.fetch(); const j = await res.json(); if (j.data) j.data.version = VERSION; await r.fulfill({ response: res, body: JSON.stringify(j) }); });
    const p = await ctx.newPage();
    await p.goto(this.mock.url + hash);
    await p.waitForSelector(ready);
    await p.evaluate(() => document.fonts.ready);
    await p.waitForTimeout(700);
    const buf = await p.screenshot();
    await ctx.close();
    return "data:image/png;base64," + buf.toString("base64");
  }
}

module.exports = { Director, W, H, OUT_W, OUT_H, FPS, checkText, REPO, HERE };
