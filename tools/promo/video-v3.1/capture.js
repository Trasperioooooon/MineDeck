// v3.1.0 宣传片的素材：在仿真后端（tools/ui-dev/server.js）上拍真实界面。
// 桌面 1280×800、deviceScaleFactor 2（推得近的几张用 3），浅色、配色「方块」。
// 每组镜头各起一份全新的仿真后端（端口 27410，拍完就关），只用点击、输入和仿真后端写明的 /__mock/state 开关来布置场景。
// 例外（都是在浏览器里改仿真数据，界面代码一行不动）：
//   - /api/app 和 /api/update/check 的版本号改成 3.1.0（「升级」那张改成 3.0.0，检查更新回「发现 v3.1.0」）；
//   - 几个任务页（安装待装清单、导入交接包、导入现成服务端）由本脚本给出任务快照，名字和日志跟片子里的故事对上；
//   - 交接包上传后的摘要换成「机械动力整合包」那台（仿真后端写的是「A 的服」）；
//   - 「开好了」的控制台换成一段干净的开服日志；「报错」那张让启动接口回一条带路径的英文报错。
//
// 用法（任意目录）：node tools/promo/video-v3.1/capture.js [--only=正则]
// 输出：out/shots/<名字>.png、out/shots/rects.json（要标框、要让光标去点的元素位置，CSS 像素）、out/shots/extra.json、
//       out/assets/icon-256.png、out/assets/pixel-icon.json（启动画面用的 16×16 像素网格）
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
const VERSION = "3.1.0";
const onlyArg = process.argv.find(a => a.startsWith("--only="));
const ONLY = onlyArg ? new RegExp(onlyArg.slice(7)) : null;

const PACK = "机械动力整合包"; // 仿真后端里的 Forge 1.20.1 服务器（停着）
const ADOPTED = "create-pack";  // 接管进来的那台（仿真后端「识别」默认给的名字）
const enc = encodeURIComponent;
const MB = 1048576, GB = 1024 * MB;

function hostStubs() {
  window.hostWinClose = () => {}; window.hostWinMin = () => {}; window.hostWinMaxToggle = () => {};
  window.hostWinDrag = () => {}; window.hostWinIsMax = async () => false;
}

// ---------- 「补前置」那一镜用的真实模组 ----------
// 仿真后端的依赖检查写死成「Architectury API 是 JEI 的前置」，现实里 JEI 没有必装前置，懂模组的人一眼能看出来。
// 所以「建好的服务器也有待装清单」那一镜换成真实存在的一对：REI 在 Forge 1.20.1 上必装 Cloth Config API 和 Architectury API。
// 2026-11-13 在 Modrinth 上核对过：项目编号、名字、简介、许可证、版本号、文件名、大小都是原样（下载量是当天的）；
// REI 在 Forge 1.20.1 上只发过 beta 版，真后端装 beta 版时日志里会写「（beta 版）」。仿真后端的目录里没有 REI，
// 搜索结果、清单里那一项的名字和依赖检查的结果由本脚本给出。
const REI = { project_id: "nfn13YXA", slug: "rei", title: "Roughly Enough Items (REI)", description: "Clean and Customizable. Alternative to Just Enough Items/JEI.",
  downloads: 27370018, icon_url: "", license: "MIT" };
const REI_FILE = { versionNumber: "12.1.785+forge", fileName: "RoughlyEnoughItems-12.1.785-forge.jar", size: 2384003 };
const REI_DEPS = [ // REI 声明的顺序：先 Cloth Config，后 Architectury
  { project: { projectId: "9s6osm5g", title: "Cloth Config API", versionNumber: "11.1.136+forge", fileName: "cloth-config-11.1.136-forge.jar", size: 1181413, license: "LGPL-3.0-only" }, dependencyOf: REI.title },
  { project: { projectId: "lhGA9TYQ", title: "Architectury API", versionNumber: "9.2.14+forge", fileName: "architectury-9.2.14-forge.jar", size: 580602, license: "LGPL-3.0-only" }, dependencyOf: REI.title },
];

// ---------- 本脚本给出的任务快照 ----------
// 安装待装清单：步骤名和日志的写法照真后端（internal/ops/catalog/modcart.go 的 installCart）：清单里的 REI 加上自动补的两个前置，一共下载 3 个文件。
const CART_STEPS = ["挑版本、补前置", "下载文件", "放入本地文件", "核对本地依赖"];
const CART_FILES = [
  [`${REI.title} ${REI_FILE.versionNumber}（beta 版）`, REI_FILE.fileName, REI_FILE.size],
  ...REI_DEPS.map(d => [`${d.project.title} ${d.project.versionNumber}（前置，${d.dependencyOf} 需要）`, d.project.fileName, d.project.size]),
];
const CART = [[1, 0, .36], [1, 1, .81], [4, 2, 1]]; // [正在第几步, 正在下第几个文件, 这个文件下了几成]
function cartTask(k) {
  const [st, fi, frac] = CART[k], ended = st >= CART_STEPS.length, f = CART_FILES[fi], n = CART_FILES.length;
  const mib = b => (b / MB).toFixed(1) + " MB";
  return { id: "task-cart", kind: "mods", title: `给「${PACK}」安装待装清单里的 1 项`,
    steps: CART_STEPS.map((name, i) => ({ name, status: ended || i < st ? "done" : i === st ? "running" : "pending" })),
    label: ended ? "" : "下载 " + f[1], done: Math.round(f[2] * frac), total: f[2], error: "", warnings: [],
    log: [`待装清单里有 1 个 Modrinth 项目：${REI.title}`,
      ...CART_FILES.slice(0, fi + 1).map(([label, file, size], i) => `[${i + 1}/${n}] ${label}：${file}（${mib(size)}）`),
      ...(ended ? ["读了 145 个模组的依赖声明", `装好了 ${n} 个文件`] : [])],
    ended, result: PACK };
}
const TRANSFER_IN = { id: "task-transfer-in", kind: "transfer-in",
  steps: ["校验交接包", "准备 Java 运行时", "下载服务端核心", "安装服务端", "解包服务器文件", "生成启动脚本"].map(name => ({ name, status: "done" })),
  label: "", done: 0, total: 0, warnings: [], error: "",
  log: ["交接包：机械动力整合包（Forge 1.20.1，142 个模组）", "Java 17 已就绪", "服务端核心 Forge 47.3.0 已下载并安装", "解出 1.8 GB",
    "交接完成 ✔ 存档、配置和模组都带过来了"], ended: true, result: PACK };
const SERVER_IN = { id: "task-srv", kind: "server-in",
  steps: ["检查压缩包", "解压服务端文件", "准备 Java 运行时", "写入配置和启动脚本"].map(name => ({ name, status: "done" })),
  label: "", done: 0, total: 0, warnings: [], error: "",
  log: ["压缩包：机械动力冒险-服务端-1.3.zip", "认出核心：Forge 47.3.0（Minecraft 1.20.1）", "解出 1.2 GB，用压缩包里的存档、模组和配置",
    "导入完成 ✔ 没有重新下载核心"], ended: true, result: "机械动力冒险" };
const IMPORT_PENDING = {
  id: "task-im", title: "导入整合包 机械动力冒险", ended: true, result: "机械动力冒险", label: "", done: 0, total: 0, error: "",
  steps: ["解析整合包清单", "下载模组和资源", "安装 Forge 47.3.0", "写入配置和启动脚本"].map(name => ({ name, status: "done" })),
  log: ["整合包：机械动力冒险 1.3（Forge 1.20.1，312 个文件）", "309 个文件已下载并校验", "3 个文件没法自动下载，列在下面的「待决文件」里", "导入完成"],
  pendingCount: 3,
  warnings: [{ kind: "import-pending", title: "3 个文件待处理", note: "这些文件没法自动下载。" }],
  pending: [
    { id: "p1", path: "mods/create-sabers-1.2.0.jar", reason: "host", host: "files.example.net",
      note: "下载地址不在 MineDeck 允许的站点列表里（只允许 Modrinth、GitHub、GitLab），确认站点可信后才能下载",
      urls: ["https://files.example.net/create-sabers-1.2.0.jar"], options: ["local", "skip", "trust"] },
    { id: "p2", path: "mods/ftb-quests-2001.4.8.jar", reason: "nohash", note: "整合包没有提供这个文件的校验值，不能自动下载", options: ["skip", "local"] },
    { id: "p3", path: "mods/voicechat-2.5.25.jar", reason: "host", host: "files.example.net", note: "下载地址不在 MineDeck 允许的站点列表里",
      urls: ["https://files.example.net/voicechat-2.5.25.jar"], options: ["local", "skip", "trust"] },
  ],
};
const TRANSFER_SUMMARY = {
  name: PACK, core: "forge", mc: "1.20.1", build: "47.3.0", kind: "game", mode: "handover", full: false,
  port: 25566, worldBytes: Math.round(1.6 * GB), mods: 142, plugins: 0, xmxMb: 6144, javaMajor: 17,
  exportedAt: "2026-11-13 20:15", minedeckVersion: VERSION, fileBytes: Math.round(1.9 * GB),
  file: "C:\\MineDeck\\cache\\transfer\\import\\1731500100000-机械动力整合包.mdpack",
};
// internal/tunnel/blocked.go 里 frpcBlockedHint 的原话（frpc 所在的文件夹换成仿真里的路径）
const BLOCKED = "frpc 被杀毒软件拦下了（frp 这类内网穿透程序常被当成风险软件）。要继续用：Windows 自带的防护，到 Windows 安全中心的「保护历史记录」里找到 frpc，" +
  "允许或还原它；别的杀毒软件，到它的隔离区里恢复。然后再启动隧道。frpc 在这个文件夹：D:\\MineDeck\\runtimes\\frp";

// ---------- 视频里要标框、要让光标去点的元素（CSS 像素坐标，存进 out/shots/rects.json） ----------
// 写法：选择器，或「选择器|包含的文字」（取符合条件、看得见的元素里面积最小的那个）
const RECTS = {
  "create-home": { new: "#mode-new", import: "#mode-import", transfer: "#mode-transfer", adopt: "#mode-adopt" },
  "adopt-filled": { dir: "#ad-dir", scan: "#ad-scan" },
  "adopt-scan": { dir: "#ad-dir", form: "#ad-form", target: ".field:has(#ad-target)", core: ".field:has(#ad-core)", notes: "#ad-notes", go: "#ad-go", eula: "#ad-eula-box", name: "#ad-name", mc: ".field:has(#ad-mc)" },
  "adopt-task": { steps: "#t-steps", log: "#task-log", go: "#t-foot .btn.primary" },
  "servers-adopted": { card: `.card[data-name="${ADOPTED}"]` },
  "jvm-java-before": { box: "#jm-java-box", sel: "#jm-java", set: "#jm-java-set", hint: "#jm-java-hint" },
  "jvm-java": { box: "#jm-java-box", sel: "#jm-java", set: "#jm-java-set", hint: "#jm-java-hint" },
  "delete-adopted": { modal: "#modal-root .modal", sw: "#modal-root label.switch", hint: "#modal-root .hint|接管进来的", ok: "#modal-root [data-x=ok]" },
  "wiz-mods": { hits: "#mc-hits", side: "#mc-side", chips: ".mc-chips", panel: "#mc-panel" },
  "wiz-mods-cat": { chip: '.mc-chips .cp-chip[data-cat="optimization"]', hits: "#mc-hits", chips: ".mc-chips" },
  "wiz-mods-search": { q: "#mc-q", go: "#mc-go", jei: '.mc-add[data-pid="jei"]', hits: "#mc-hits" },
  "wiz-mods-added": { list: "#mc-list", count: "#mc-count", side: "#mc-side", jei: '.mc-add[data-pid="jei"]' },
  "wiz-mods-local": { list: "#mc-list", side: "#mc-side", pick: "#mc-local-pick", warnrow: "#mc-list *|这个文件是给 Fabric 用的" },
  "cart-deps": { dep: "#mc-dep-box", list: "#mc-list", install: "#mc-install", side: "#mc-side", depnote: "#mc-dep-box *|还缺的前置",
    depsec: "#mc-dep-box .mc-dep-sec|还缺的前置", q: "#mc-q" },
  "cart-task-0": { steps: "#t-steps", prog: "#t-prog", log: "#task-log" },
  "cart-task-2": { steps: "#t-steps", prog: "#t-prog", log: "#task-log", done: "#t-foot .btn.primary" },
  "import-cf": { file: ".field:has(#im-file)", src: ".field:has(#im-src)", go: "#im-go", name: "#im-name" },
  "pending": { box: "#t-pending", p1: '#t-pending .pnd-item[data-id="p1"]', p2: '#t-pending .pnd-item[data-id="p2"]', apply: "#t-pending-apply", rLocal: '#t-pending .pnd-item[data-id="p1"] label|用本地文件代替' },
  "pending-local": { box: "#t-pending", p1: '#t-pending .pnd-item[data-id="p1"]', local: '#t-pending .pnd-item[data-id="p1"] .pnd-local', apply: "#t-pending-apply",
    radios: '#t-pending .pnd-item[data-id="p1"] [role=radiogroup], #t-pending .pnd-item[data-id="p1"] .row' },
  "server-in": { steps: "#t-steps", log: "#task-log", go: "#t-foot .btn.primary" },
  "card-menu": { card: `.card[data-name="${PACK}"]`, menu: ".dropmenu", item: ".dropmenu button|导出交接包" },
  "transfer-dialog": { modal: "#tr-mask .modal", who: "#tr-mask [role=radiogroup]", full: "#tr-mask label.switch", dest: "#tr-dest", go: "#tr-go" },
  "transfer-import": { file: ".field:has(#tp-file)", summary: "#tp-form .group", go: "#tp-go", name: "#tp-name", warn: "#tp-form .warn-card" },
  "transfer-task": { steps: "#t-steps", log: "#task-log", go: "#t-foot .btn.primary" },
  "friend-console": { pill: "#main .st-pill", con: "#console", done: "#console div|Done (", join: "#console div|Mochi233 joined" },
  "toast-err": { toast: "#toasts .toast.error", det: '.toast.error button[aria-label="查看报错详情"]', card: `.card[data-name="${PACK}"]` },
  "err-detail": { modal: "#modal-root .modal", pre: "#modal-root .modal *|fork/exec", copy: "#modal-root button|复制报错信息", toast: "#toasts .toast.error" },
  "tunnel-blocked": { t1: '[data-tun="t1"]', t2: '[data-tun="t2"]', err: '[data-tun="t2"] .tun-err', state: '[data-tun="t2"] *|上次出错' },
  "java-hint": { hint: "#main a.warn-text|有模组要求" },
  "create-tasks": { list: "#task-list" },
  "update-before": { check: "#up-check", result: "#up-result", repo: "#up-repo" },
  "update": { check: "#up-check", result: "#up-result", repo: "#up-repo" },
};

// 「开好了」的控制台：Forge 1.20.1 一段干净的开服日志
const FRIEND_CONSOLE = [
  "[20:31:02] [main/INFO]: ModLauncher running: Forge 47.3.0, Java 17.0.12",
  "[20:31:06] [main/INFO]: Loading 142 mods",
  "[20:31:27] [Server thread/INFO]: Starting minecraft server version 1.20.1",
  "[20:31:27] [Server thread/INFO]: Loading properties",
  "[20:31:28] [Server thread/INFO]: Preparing level \"world\"",
  "[20:31:40] [Server thread/INFO]: Done (12.604s)! For help, type \"help\"",
  "[20:33:10] [Server thread/INFO]: Kai_07 joined the game",
  "[20:33:52] [Server thread/INFO]: Mochi233 joined the game",
  "[20:34:05] [Server thread/INFO]: <Kai_07> 存档都在，接着造",
];
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

const json = data => ({ contentType: "application/json; charset=utf-8", body: JSON.stringify({ ok: true, data }) });

// ---------- 镜头清单 ----------
const LIST = [
  { name: "create-home", run: async (page, h) => {
    await h.open("#/create", "#mode-adopt");
    await h.snap("create-home");
  } },
  { name: "adopt", run: async (page, h) => {
    await h.open("#/create/adopt", "#ad-dir");
    await page.fill("#ad-dir", "E:\\MCServers\\create-pack");
    await h.snap("adopt-filled");
    await page.click("#ad-scan");
    await page.waitForSelector("#ad-go", { timeout: 8000 });
    await page.waitForSelector("#ad-eula", { timeout: 8000 });
    await page.check("#ad-eula", { force: true });
    await h.snap("adopt-scan");
    await page.click("#ad-go");
    await page.waitForSelector("#t-foot .btn.primary", { timeout: 8000 });
    await h.snap("adopt-task", { allowToast: true });
  } },
  { name: "servers-adopted", run: async (page, h) => {
    await h.post("/api/instances", { name: ADOPTED, core: "neoforge", mc: "1.21.1", build: "21.1.265", xmxMb: 6144, port: 25570 });
    await h.state({ inst: { name: ADOPTED, adopted: true, dir: "E:\\MCServers\\create-pack", javaMajor: 21, extraJvm: ["-XX:+UseG1GC"] } });
    await h.open("#/instances", `.card[data-name="${ADOPTED}"]`);
    await page.evaluate(() => { const m = document.querySelector("#main"); m.scrollTop = m.scrollHeight; });
    await h.snap("servers-adopted");
    // 换 Java：先拍现在用的，再换成本机装的那份 Java 21，「更换 Java」按钮亮起来
    await h.open(`#/inst/${enc(ADOPTED)}/jvm`, "#jm-java");
    await page.waitForFunction(() => { const s = document.querySelector("#jm-java"); return s && s.options.length > 1; }, null, { timeout: 8000 });
    const opts = await page.$$eval("#jm-java option", os => os.map(o => ({ text: o.textContent.trim(), value: o.value, selected: o.selected, disabled: o.disabled })));
    extra["jvm-java-options"] = opts;
    await h.scrollTo("#jm-java-box", 260);
    await h.snap("jvm-java-before");
    const pick = opts.findIndex(o => !o.selected && !o.disabled && /本机装的 21/.test(o.text));
    if (pick < 0) throw new Error("no local Java 21 option: " + JSON.stringify(opts));
    await page.selectOption("#jm-java", opts[pick].value);
    extra["jvm-java-picked"] = opts[pick].text;
    await h.snap("jvm-java");
    // 删除：接管进来的默认不删文件夹，按钮写「移出列表」
    await h.open("#/instances", `.card[data-name="${ADOPTED}"] [data-act="more"]`);
    await page.click(`.card[data-name="${ADOPTED}"] [data-act="more"]`);
    await page.click(".dropmenu button.danger");
    await page.waitForSelector("#modal-root [data-x=ok]");
    await h.snap("delete-adopted", { allowModal: true });
  } },
  { name: "wiz-mods", run: async (page, h) => {
    await h.open("#/create", "#mode-new");
    await page.click("#mode-new");
    await page.click('.core-card[data-id="neoforge"]');
    await page.click("#wnext"); await page.waitForSelector(".ver-item");
    await page.click("#wnext"); await page.waitForSelector("#build-list .ver-item");
    await page.click("#wnext"); await page.waitForSelector("#wiz-mod-body");
    await page.waitForSelector("#mc-hits .mc-add", { timeout: 8000 });
    await h.snap("wiz-mods");
    await page.fill("#mc-q", "JEI");
    await page.click("#mc-go");
    await page.waitForSelector('.mc-add[data-pid="jei"]', { timeout: 8000 });
    await page.waitForTimeout(400);
    await h.snap("wiz-mods-search");
    await page.click('.mc-add[data-pid="jei"]');
    await page.waitForFunction(() => document.querySelector("#mc-list") && document.querySelector("#mc-list").textContent.includes("JEI"), null, { timeout: 8000 });
    await page.waitForTimeout(500);
    await h.snap("wiz-mods-added", { allowToast: true });
    // 和界面测试（tests.js 的 jarFile）一样：PK 开头，仿真后端按文件名里的字样认（sodium 认得出；fabric 的放进 NeoForge 服，行上提醒加载器不对）
    const jar = name => ({ name, mimeType: "application/java-archive", buffer: Buffer.from("PK" + name) });
    await page.setInputFiles("#mc-file", [jar("sodium-extra-neoforge-0.6.0.jar"), jar("lithium-fabric-0.13.1.jar")]);
    await page.waitForTimeout(1400);
    await h.snap("wiz-mods-local", { allowToast: true });
  } },
  { name: "cart", run: async (page, h) => {
    // 仿真后端给这台挂着「缺少前置模组 jei」的示例横幅（服务器里已装模组的依赖核对），这一镜不要
    await page.route(`**/api/instances/${enc(PACK)}/mods/deps`, r => r.fulfill(json({ count: 0, missing: [], stale: [], checked: 142 })));
    // REI 不在仿真后端的目录里：搜索结果直接给；加进清单以后仿真后端只知道编号，名字、许可证在返回里补上；依赖检查给真实的那两个前置
    const api = p => u => new URL(u).pathname === p;
    await page.route(api("/api/modcart/search"), r => r.fulfill(json({ type: "mod", hits: [REI], offset: 0, total: 1, more: false })));
    await page.route(api("/api/modcart"), async r => {
      const res = await r.fetch(), body = await res.json();
      for (const it of (body.data && body.data.items) || []) if (it.projectId === REI.project_id) Object.assign(it, { slug: REI.slug, title: REI.title, license: REI.license, kind: "mod" });
      await r.fulfill({ response: res, json: body });
    });
    await page.route(api("/api/modcart/deps"), r => r.fulfill(json({ files: [], missing: REI_DEPS, optional: [], conflicts: [], localHits: [], unresolved: [], local: [], manual: [] })));
    await h.open(`#/inst/${enc(PACK)}/cart`, "#mc-q");
    await page.fill("#mc-q", "REI");
    await page.click("#mc-go");
    await page.waitForSelector(`.mc-add[data-pid="${REI.project_id}"]`, { timeout: 8000 });
    await page.click(`.mc-add[data-pid="${REI.project_id}"]`);
    await page.waitForFunction(t => document.querySelector("#mc-list") && document.querySelector("#mc-list").textContent.includes(t), REI.title, { timeout: 8000 });
    await page.waitForTimeout(500);
    await page.click("#mc-dep-go");
    await page.waitForFunction(() => { const b = document.querySelector("#mc-dep-box"); return b && b.textContent.trim().length > 4 && !/检查中/.test(b.textContent); }, null, { timeout: 8000 });
    await page.waitForTimeout(500);
    await h.scrollTo("#mc-list", 70); // 右边一列：清单、全部安装、检查前置的结果
    await h.snap("cart-deps"); // 等「已加入清单」的提示消失，免得挡住清单的标题
    let k = 0;
    await page.route("**/api/tasks/task-cart", r => r.fulfill(json(cartTask(k))));
    for (k = 0; k < CART.length; k++) {
      await h.open("#/task/task-cart", "#t-steps .task-step");
      const tk = cartTask(k), want = tk.ended ? "100%" : Math.round(tk.done / tk.total * 100) + "%";
      await page.waitForFunction(w => { const e = document.querySelector("#t-pct"); return e && e.textContent === w; }, want, { timeout: 8000 });
      if (tk.ended) await page.waitForSelector("#t-foot .btn.primary");
      await page.waitForTimeout(700);
      await h.snap(`cart-task-${k}`, { allowToast: true });
    }
  } },
  { name: "import-cf", run: async (page, h) => {
    await h.open("#/create/import", "#im-file");
    await page.setInputFiles("#im-file", { name: "机械动力冒险-1.3.zip", mimeType: "application/zip", buffer: Buffer.from("PK\u0003\u0004") });
    await page.fill("#im-src", "E:\\MCServers\\旧的机械动力服");
    await page.fill("#im-name", "机械动力冒险");
    await page.check("#im-eula", { force: true }).catch(() => {});
    await h.snap("import-cf");
  } },
  { name: "pending", dpr: 3, run: async (page, h) => {
    await h.state({ taskList: [IMPORT_PENDING], trustedHosts: [] });
    await h.open("#/task/task-im", '#t-pending .pnd-item[data-id="p1"]');
    await h.scrollTo("#t-pending", 40);
    await h.snap("pending");
    await page.check('#t-pending input[name="pnd-p1"][value="local"]', { force: true });
    await page.waitForSelector('#t-pending .pnd-item[data-id="p1"] .pnd-local:not([hidden])', { timeout: 5000 }).catch(() => {});
    await page.fill('#t-pending .pnd-item[data-id="p1"] .pnd-local', "E:\\MCServers\\旧的机械动力服\\mods");
    await page.check('#t-pending input[name="pnd-p2"][value="skip"]', { force: true });
    await page.waitForTimeout(300);
    await h.snap("pending-local");
  } },
  { name: "server-in", run: async (page, h) => {
    await page.route("**/api/tasks/task-srv", r => r.fulfill(json(SERVER_IN)));
    await h.open("#/task/task-srv", "#t-foot .btn.primary");
    await h.snap("server-in");
  } },
  { name: "transfer-out", run: async (page, h) => {
    await h.open("#/instances", `.card[data-name="${PACK}"] [data-act="more"]`);
    await page.click(`.card[data-name="${PACK}"] [data-act="more"]`);
    await page.waitForSelector(".dropmenu");
    await h.snap("card-menu", { allowMenu: true });
    await page.click(".dropmenu button:has-text('导出交接包')");
    await page.waitForSelector("#tr-mask #tr-go");
    await h.snap("transfer-dialog", { allowModal: true });
  } },
  { name: "transfer-in", run: async (page, h) => {
    await h.state({ empty: true });
    await page.route("**/api/transfer/upload", r => r.fulfill(json(TRANSFER_SUMMARY)));
    await h.open("#/create/transfer", "#tp-file");
    await page.setInputFiles("#tp-file", { name: "机械动力整合包-交接包-20261113-201500.mdpack", mimeType: "application/zip", buffer: Buffer.from("PK\u0003\u0004") });
    await page.waitForSelector("#tp-name", { timeout: 8000 });
    await page.waitForTimeout(400);
    await h.snap("transfer-import");
    await page.route("**/api/tasks/task-transfer-in", r => r.fulfill(json(TRANSFER_IN)));
    await h.open("#/task/task-transfer-in", "#t-foot .btn.primary");
    await h.snap("transfer-task");
  } },
  { name: "friend-console", dpr: 3, console: [PACK, FRIEND_CONSOLE], run: async (page, h) => {
    await page.route(`**/api/instances/${enc(PACK)}/diag`, r => r.fulfill(json({ findings: [], crashed: false, lines: 15 }))); // 开好了的服，没有排错横幅
    await h.state({ status: { [PACK]: "starting" } }); await h.state({ status: { [PACK]: "running" } });
    await page.route("**/api/instances", async r => {
      const res = await r.fetch(); const j = await res.json();
      if (Array.isArray(j.data)) for (const i of j.data) if (i.name === PACK) { i.uptimeSec = 240; i.onlineCount = 2; }
      await r.fulfill({ response: res, body: JSON.stringify(j) });
    });
    await h.open(`#/inst/${enc(PACK)}/console`, "#console div");
    await page.waitForFunction(() => document.querySelector("#console").textContent.includes("Done ("));
    await h.snap("friend-console");
  } },
  { name: "toast-err", dpr: 3, run: async (page, h) => {
    await page.route(`**/api/instances/${enc(PACK)}/start`, r => r.fulfill({ status: 500, contentType: "application/json; charset=utf-8",
      body: JSON.stringify({ ok: false, error: "启动失败: fork/exec D:\\MineDeck\\runtimes\\java17\\bin\\java.exe: The system cannot find the file specified." }) }));
    await h.open("#/instances", `.card[data-name="${PACK}"] [data-act="start"]`);
    await page.click(`.card[data-name="${PACK}"] [data-act="start"]`);
    await page.waitForSelector('.toast.error button[aria-label="查看报错详情"]', { timeout: 5000 });
    await h.snap("toast-err", { allowToast: true });
    await page.click('.toast.error button[aria-label="查看报错详情"]');
    await page.waitForSelector("#modal-root .modal", { timeout: 5000 });
    await h.snap("err-detail", { allowToast: true, allowModal: true });
  } },
  { name: "tunnel-blocked", dpr: 3, run: async (page, h) => {
    await h.state({ tunnel: { id: "t2", name: "整合包联机", provider: "openfrp", running: false, online: false, boundInstance: PACK, autoStart: true, publicAddr: "", lastError: BLOCKED } });
    await h.open("#/tunnels", '[data-tun="t2"] .tun-err');
    await h.snap("tunnel-blocked");
  } },
  { name: "java-hint", dpr: 3, run: async (page, h) => {
    await page.route(`**/api/instances/${enc(PACK)}/diag`, r => r.fulfill(json({ findings: [], crashed: false, lines: 15 })));
    await h.state({ modsNeed: { [PACK]: 21 } });
    await h.open(`#/inst/${enc(PACK)}/console`, "#main a.warn-text");
    await h.snap("java-hint");
  } },
  { name: "create-tasks", run: async (page, h) => {
    await h.state({ taskList: [
      { id: "task-1", title: "创建 Paper 生存服", ended: false, label: "下载 paper-1.21.4-232.jar", done: 31 * MB, total: 50 * MB },
      { id: "task-pack", title: "导入整合包 机械动力冒险", ended: false, label: "下载 create-1.20.1-6.0.6.jar", done: 9 * MB, total: 16 * MB },
      { id: "task-adopt", title: "接管 create-pack", ended: true, result: ADOPTED, kind: "adopt" },
    ] });
    await h.open("#/create", "#task-list .java-row");
    await h.snap("create-tasks");
  } },
  { name: "update", version: "3.0.0", run: async (page, h) => {
    await page.route("**/api/update/check", r => r.fulfill(json({ hasUpdate: true, current: "3.0.0", latest: { tag: "v3.1.0", name: "v3.1.0" } })));
    await h.open("#/settings/about", "#up-check");
    await h.snap("update-before");
    await page.click("#up-check");
    await page.waitForSelector("#up-result button", { timeout: 5000 });
    await h.snap("update");
  } },
];

// ---------- 工具 ----------
async function calm(page, { allowToast = false, allowModal = false, allowMenu = false } = {}) {
  await page.mouse.move(-10, -10).catch(() => {});
  await page.mouse.move(150, 790);
  await page.waitForFunction(([t, m, d]) => (t || !document.querySelector("#toasts").childElementCount)
    && (m || !document.querySelector("#modal-root").childElementCount) && (d || !document.querySelector(".dropmenu")), [allowToast, allowModal, allowMenu], { timeout: 20000 });
  await page.waitForFunction(() => document.getAnimations().every(a => (a.effect && a.effect.getTiming().iterations === Infinity) || a.playState !== "running"), null, { timeout: 10000 });
  await page.evaluate(() => document.fonts.ready);
  await page.waitForTimeout(500);
}

async function withPage(browser, shot, fn) {
  const mock = await start({ port: PORT, ui: UI });
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 }, deviceScaleFactor: shot.dpr || 2, locale: "zh-CN", timezoneId: "Asia/Shanghai", colorScheme: "light" });
  if (shot.console) await ctx.addInitScript(fakeConsole, { name: shot.console[0], lines: shot.console[1] });
  await ctx.grantPermissions(["clipboard-read", "clipboard-write"], { origin: mock.url.replace(/\/$/, "") });
  await ctx.addInitScript(() => { localStorage.setItem("amh_token", "mock"); localStorage.setItem("amh_theme", "light"); localStorage.setItem("amh_scheme", "craft"); localStorage.setItem("amh_accent", "auto"); });
  await ctx.addInitScript(hostStubs);
  const ver = shot.version || VERSION;
  await ctx.route("**/api/app", async r => { const res = await r.fetch(); const j = await res.json(); if (j.data) j.data.version = ver; await r.fulfill({ response: res, body: JSON.stringify(j) }); });
  if (!shot.version) await ctx.route("**/api/update/check", async r => { const res = await r.fetch(); const j = await res.json(); if (j.data) j.data.current = ver; await r.fulfill({ response: res, body: JSON.stringify(j) }); });
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
    post: async (p, body) => {
      const r = await fetch(mock.url + p.replace(/^\//, ""), { method: "POST", headers: { "X-Token": "mock", "Content-Type": "application/json" }, body: JSON.stringify(body) });
      const j = await r.json(); if (!j.ok) throw new Error(`POST ${p}: ${j.error}`); return j.data;
    },
    open: async (hash, sel) => {
      await page.goto(mock.url + "?n=" + (++n) + hash);
      await page.waitForSelector(sel, { timeout: 10000 });
      await page.evaluate(() => document.fonts.ready);
    },
    scrollTo: (sel, pad) => page.evaluate(([s, p]) => {
      const m = document.querySelector("#main"), box = document.querySelector(s);
      const y = box.getBoundingClientRect().top - m.getBoundingClientRect().top + m.scrollTop - p;
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
            let b;
            if (txt) { els.sort((a, b) => a.b.width * a.b.height - b.b.width * b.b.height); b = els[0].b; } else b = els[0].b;
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

// 启动画面的 16×16 像素网格（internal/web/ui/index.html 里的 PAL / PX），开场的方块拼图标用它
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

const rects = {}, extra = {};

(async () => {
  fs.mkdirSync(SHOTS, { recursive: true });
  const rectFile = path.join(SHOTS, "rects.json"), extraFile = path.join(SHOTS, "extra.json");
  if (fs.existsSync(rectFile)) Object.assign(rects, JSON.parse(fs.readFileSync(rectFile, "utf8")));
  if (fs.existsSync(extraFile)) Object.assign(extra, JSON.parse(fs.readFileSync(extraFile, "utf8")));
  fs.mkdirSync(ASSETS, { recursive: true });
  extractIcon();
  extractPixelIcon();
  const browser = await pw.chromium.launch({ channel: "chrome" });
  let bad = 0;
  try {
    for (const s of LIST) {
      if (ONLY && !ONLY.test(s.name)) continue;
      try {
        const errors = await withPage(browser, s, s.run);
        if (errors.length) { bad++; console.error(`${s.name}: page errors: ${errors.join(" | ")}`); }
      } catch (e) { bad++; console.error(`${s.name}: FAILED: ${e.message.split("\n")[0]}`); }
    }
  } finally {
    await browser.close();
    fs.writeFileSync(rectFile, JSON.stringify(rects, null, 1));
    fs.writeFileSync(extraFile, JSON.stringify(extra, null, 1));
  }
  if (bad) process.exitCode = 1;
})().catch(e => { console.error(e); process.exit(1); });
