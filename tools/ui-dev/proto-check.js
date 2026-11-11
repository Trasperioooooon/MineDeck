// 界面原型自检：把每个场景都打开一遍，看「自动走到这个状态」的那几下有没有走完、页面有没有报错。
// 改了界面的选择器、或者加了新场景之后跑一下：场景里写的选择器失效了，这里会指出是哪一个场景的哪一步。
//
// 用法：node proto-check.js [正则] [选项]        正则：只查编号或标题匹配的场景
//   （不加选项）      查 Node 版原型：起一份仿真后端，开目录页
//   --static         查静态版：先生成到临时目录，用 file:// 直接打开，同双击 index.html
//   --static=http    查静态版：放到本机的静态服务器上，而且放在子目录 /some/sub/ 里，同部署到网站上
//   --dir=<目录>     静态版用现成的产物，不重新生成
//   --tour           查给访客看的导览页，不加就查目录页的全部场景
//   --flows          不查场景，改查演示里多演的几样（proto/demo.js）和上传：启动服务器、控制台发命令、
//                    走完向导建一台、加白名单、立即备份、导入整合包的提示、上传交接包
//   --phone          用手机大小的窗口查（导览页在窄屏上是另一种版式）
//   --shots=<目录>   顺手给每个场景截一张图
// 静态版另外多查两样：页面有没有往产物以外的任何地方发请求（演示不联网）、浏览器控制台有没有报错。
"use strict";
const fs = require("fs");
const os = require("os");
const path = require("path");
const { pathToFileURL } = require("url");
const { launch } = require("./browser");
const { SCENES } = require("./proto/scenes");
const { TOUR } = require("./proto/tour");

const args = process.argv.slice(2);
const opt = name => { const a = args.find(x => x === `--${name}` || x.startsWith(`--${name}=`)); return a === undefined ? undefined : a.includes("=") ? a.slice(name.length + 3) : ""; };
const shots = opt("shots") ? path.resolve(opt("shots")) : "";
const mode = opt("static") === undefined ? "node" : opt("static") === "http" ? "http" : "file";
const tour = opt("tour") !== undefined, phone = opt("phone") !== undefined, flows = opt("flows") !== undefined;
const only = args.find(a => !a.startsWith("--"));
const ONLY = only ? new RegExp(only) : null;
const SUB = "/some/sub/"; // 静态服务器上故意不放在根上：界面里写成 /api/…、/img/… 的地方要是真发了请求，会落到子目录外面，一查就知道
const enc = encodeURIComponent;

// ---- 演示里多演的几样：每条是「开哪个场景、停在哪一页」加一段操作，走不通就抛错 ----
const has = (page, text, ms = 12000) => page.waitForFunction(t => document.body.innerText.includes(t), text, { timeout: ms }).catch(() => { throw new Error(`页面上没有出现「${text}」`); });
const fakeFile = (name, text) => ({ name, mimeType: "application/octet-stream", buffer: Buffer.from(text) });
const FLOWS = [
  { id: "f-start", title: "点「启动」，服务器真的变成运行中", scene: "l-four", hash: "#/instances", async run(page) {
    const card = '.card[data-name="机械动力整合包"]';
    await page.click(`${card} [data-act="start"]`);
    await page.waitForFunction(sel => /运行中/.test(document.querySelector(sel).innerText), card, { timeout: 12000 }).catch(() => { throw new Error("等了 12 秒，卡片上还不是「运行中」"); });
  } },
  { id: "f-command", title: "控制台里发 list，看得到回答", scene: "d-paper", hash: `#/inst/${enc("生存服 · 朋友")}/console`, async run(page) {
    await page.fill("#cmd-in", "list");
    await page.click("#cmd-send");
    await page.waitForFunction(() => /> list[\s\S]*players online: Steve/.test(document.querySelector("#console").innerText), null, { timeout: 8000 }).catch(() => { throw new Error("控制台里没有出现 list 的回显和回答"); });
  } },
  { id: "f-create", title: "走完向导建一台，进度走到完成，能进这台服务器", scene: "l-four", hash: "#/create/new", async run(page) {
    await page.click('.core-card[data-id="paper"]');
    await page.click("#wnext"); await page.waitForSelector(".ver-item");
    await page.click("#wnext"); await page.waitForSelector("#build-list .ver-item");
    await page.click("#wnext"); await page.waitForSelector("#wiz-mod-body");
    await page.click("#wnext"); await page.waitForSelector("#f-name");
    await page.fill("#f-name", "流程测试服");
    await page.check("#f-eula");
    await page.click("#wcreate");
    await page.waitForFunction(() => location.hash.startsWith("#/task/"), null, { timeout: 8000 }).catch(() => { throw new Error("点了创建以后没有去任务页"); });
    const go = `a.btn.primary[href="#/inst/${enc("流程测试服")}"]`;
    await page.waitForSelector(go, { timeout: 20000 }).catch(() => { throw new Error("等了 20 秒，部署进度没有走到完成"); });
    await page.click(go);
    await has(page, "流程测试服");
    await page.waitForSelector("#console", { timeout: 8000 }).catch(() => { throw new Error("没能进到刚建的那台服务器"); });
  } },
  { id: "f-players", title: "加一个白名单，名单里真的多出来", scene: "l-four", hash: `#/inst/${enc("生存服 · 朋友")}/players`, async run(page) {
    await page.fill('input[data-add="whitelist-add"]', "FlowTester");
    await page.press('input[data-add="whitelist-add"]', "Enter");
    await has(page, "FlowTester");
  } },
  { id: "f-backup", title: "点「立即备份」，列表里多出一份", scene: "l-four", hash: `#/inst/${enc("生存服 · 朋友")}/backups`, async run(page) {
    await page.waitForSelector("#bk-create");
    const before = await page.locator("[data-restore]").count();
    await page.fill("#bk-label", "流程测试");
    await page.click("#bk-create");
    await page.waitForFunction(n => document.querySelectorAll("[data-restore]").length === n + 1 && /-流程测试\.zip/.test(document.body.innerText), before, { timeout: 8000 }).catch(() => { throw new Error("点了立即备份，列表里没有多出带「流程测试」的那一份"); });
  } },
  { id: "f-modpack", title: "导入整合包：明说演示里不能导入", scene: "l-four", hash: "#/create/import", async run(page) {
    await page.setInputFiles("#im-file", fakeFile("我的整合包.mrpack", "PK\u0003\u0004 demo"));
    await page.fill("#im-name", "整合包测试");
    await page.check("#im-eula");
    await page.click("#im-go");
    await has(page, "这里是演示，不能真的导入整合包");
  } },
  { id: "f-transfer", title: "上传交接包，看得到摘要", scene: "l-four", hash: "#/create/transfer", async run(page) {
    await page.setInputFiles("#tp-file", fakeFile("朋友的服.mdpack", "PK\u0003\u0004 " + "x".repeat(4096)));
    await has(page, "A 的服");
  } },
];

(async () => {
  // ---- 这次查的是哪一份：页面都在哪个地址下面、哪些地址算「自己人」、查完怎么收拾 ----
  let root, names, mine, close = async () => {};
  if (mode === "node") {
    const proto = await require("./proto").startProto({ port: 0 });
    const origin = proto.url.replace(/__proto\/$/, "");
    root = proto.url; names = { catalog: "", tour: "tour.html" };
    mine = u => u.startsWith(origin);
    close = () => proto.close();
  } else {
    const { buildStatic, serveStatic } = require("./proto-static");
    let dir = opt("dir") ? path.resolve(opt("dir")) : "", tmp = "";
    if (!dir) { tmp = fs.mkdtempSync(path.join(os.tmpdir(), "minedeck-proto-")); dir = path.join(tmp, "out"); buildStatic({ out: dir }); }
    names = { catalog: "catalog.html", tour: "index.html" };
    if (mode === "file") {
      root = pathToFileURL(dir).href + "/";
      const base = root.toLowerCase();
      mine = u => u.toLowerCase().startsWith(base);
    } else {
      const srv = await serveStatic(dir, { port: 0, prefix: SUB });
      root = srv.url;
      mine = u => u.startsWith(srv.url);
      close = () => srv.close();
    }
    const done = close;
    close = async () => { await done(); if (tmp) fs.rmSync(tmp, { recursive: true, force: true }); };
  }

  const browser = await launch();
  const ctx = await browser.newContext(phone
    ? { viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, isMobile: true, hasTouch: true, locale: "zh-CN" }
    : { viewport: { width: 1760, height: 1040 }, locale: "zh-CN" });
  const page = await ctx.newPage();
  const errors = [], leaks = [];
  page.on("pageerror", e => errors.push(e.message));
  // 控制台的报错只在静态版里算数：Node 版里接口回 4xx / 5xx 时（有的场景故意这样），浏览器自己会往控制台记一笔「Failed to load resource」
  if (mode !== "node") page.on("console", m => { if (m.type() === "error") errors.push("控制台：" + m.text()); });
  page.on("request", r => { const u = r.url(); if (!/^(data|blob|about):/.test(u) && !mine(u)) leaks.push(u); });
  // 没读到自己的文件算错。ERR_ABORTED 不算：那是上一页还没加载完就翻走了
  page.on("requestfailed", r => { const why = (r.failure() || {}).errorText || ""; if (mine(r.url()) && !/ERR_ABORTED/.test(why)) errors.push(`没读到 ${r.url()}（${why}）`); });
  page.on("response", r => { if (r.status() === 404) errors.push("404 " + r.url()); });
  if (shots) fs.mkdirSync(shots, { recursive: true });

  const all = flows ? FLOWS : tour ? TOUR.map(s => ({ id: s.id, title: s.text.zh.title })) : SCENES;
  const list = all.filter(sc => !ONLY || ONLY.test(sc.id) || ONLY.test(sc.title));
  const bad = [];
  let n = 0;
  for (const sc of list) {
    n++;
    errors.length = 0; leaks.length = 0;
    const why = [];
    if (flows) {
      // 不经过目录页，直接开界面那一页（带上场景，不带 proto=1：没人替它点，下面自己点）
      await page.goto("about:blank");
      await page.goto(`${root}app/index.html?scene=${enc(sc.scene)}&host=1${sc.hash}`);
      try { await page.waitForSelector("#main > *", { timeout: 10000 }); await sc.run(page); } catch (e) { why.push(String(e.message || e).split("\n")[0]); }
      await page.waitForTimeout(300);
    } else {
      await page.goto(root + (tour ? names.tour + "?autostart=1" : names.catalog) + "#" + enc(sc.id));
      await page.reload(); // 只改 # 后面的部分不会重新加载页面
      // 页面左下角那行字：走完了是空的，没走完会写原因（.warn）
      await page.waitForFunction(id => {
        const l = document.querySelector("#stat-l");
        return document.querySelector("#wrap iframe") && l && (l.textContent === "" || l.className === "warn") && document.querySelector(`.sc.on[data-id="${id}"]`);
      }, sc.id, { timeout: 30000 }).catch(() => {});
      await page.waitForTimeout(450);
      const stat = await page.$eval("#stat-l", el => ({ text: el.textContent, warn: el.className === "warn", why: el.title })).catch(() => ({ text: "页面没有打开", warn: true, why: "" }));
      // 嵌着的界面真的画出来了没有。file:// 下外面的页面摸不到里面，所以从浏览器这头直接问那个 iframe
      const inner = page.frames().find(f => f !== page.mainFrame());
      const drawn = inner ? await inner.$("#main > *").then(Boolean, () => false) : false;
      if (stat.warn || stat.text !== "") why.push(stat.text + (stat.why ? `（${stat.why}）` : ""));
      if (!drawn) why.push("界面没有画出来");
    }
    if (errors.length) why.push("页面报错：" + [...new Set(errors)].join(" | "));
    if (leaks.length) why.push("往外发了请求：" + [...new Set(leaks)].join(" | "));
    if (why.length) bad.push(`${sc.id}（${sc.title}）：${why.join("；")}`);
    if (shots) await page.screenshot({ path: path.join(shots, `${String(n).padStart(2, "0")}-${sc.id}.png`) });
  }
  await browser.close();
  await close();
  for (const b of bad) console.log("没走通  " + b);
  const what = (mode === "node" ? "Node 版" : mode === "file" ? "静态版（file://）" : `静态版（静态服务器，放在 ${SUB} 下）`) + (flows ? "的几段流程" : tour ? "的导览页" : "的目录页") + (phone ? "，手机大小" : "");
  console.log(`\n${what}：${n - bad.length}/${n} ${flows ? "段" : "个场景"}走通`);
  process.exit(bad.length ? 1 : 0);
})().catch(e => { console.error(e); process.exit(1); });
