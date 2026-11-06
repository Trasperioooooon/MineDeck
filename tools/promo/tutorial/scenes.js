// 演示视频的全部场景。每段一个对象：boot 布置仿真数据和起始页面，run 是一串动作，写在前面的先发生。
// 所有上屏文字都在这个文件里，导演（director.js）会检查：只用 GB2312 里的字、不超长、没有感叹号和夸张词。
// 没有口播，节奏靠字幕：下一条字幕要等上一条读完（按字数算），所以改文案不用手动调时间。
"use strict";
const SURV = "生存服 · 朋友", PACK = "机械动力整合包";
const enc = encodeURIComponent;
const MB = 1048576;
const keep = (st, names) => { st.instances = st.instances.filter(i => names.includes(i.name)); };
const inst = (st, name) => st.instances.find(i => i.name === name);
const post = (mock, body) => fetch(mock.url + "__mock/state", { method: "POST", body: JSON.stringify(body) });
const AI = { reset: true, preset: "deepseek", protocol: "openai", baseUrl: "https://api.deepseek.com", model: "deepseek-v4-flash", key: "sk-demo",
  priceUpdated: "2026-10-28", quota: { assistant: 500000, gamechat: 200000, companion: 100000 } };
const CP_INSTALLED = {
  minedeck_companion: { state: "ok", version: "0.2.0", files: [{ file: "minedeck-companion-forge-1.20.1-0.2.0.jar", version: "0.2.0", keep: true }] },
  playerengine: { state: "ok", version: "1.20.1-1.4.0", files: [{ file: "playerengine-forge-1.20.1-1.4.0.jar", version: "1.20.1-1.4.0", keep: true }] },
};
const CP_ZONES = [
  { name: "base", dim: "minecraft:overworld", min: [-120, -64, -80], max: [-40, 320, 10] },
  { name: "farm", dim: "minecraft:overworld", min: [60, 62, -420], max: [92, 80, -396] },
];

// ---------- 部署进度（仿真后端的进度是固定的 60%，这里按 k 给） ----------
const STEP_NAMES = ["下载 Java 21 运行时", "下载 Paper 1.21.4 核心", "同意 EULA 并生成配置", "生成启动脚本"];
const DEPLOY = [ // [第几步在跑, 当前下载项, 已下载 MB, 总 MB]；stage 4 = 完成
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
const PACK_STEPS = ["解析整合包", "下载 Java 17 运行时", "安装 Forge 1.20.1", "下载模组文件", "生成启动脚本"];
function packTask(k) {
  const steps = PACK_STEPS.map((name, i) => ({ name, status: i < k ? "done" : i === k ? "running" : "pending" }));
  const ended = k >= PACK_STEPS.length;
  const log = ["[1/5] 已识别：Forge 1.20.1，214 个文件，跳过 6 个只给客户端用的文件"];
  if (k >= 1) log.push("[2/5] 下载 Java 17（BMCLAPI 镜像）");
  if (k >= 2) log.push("[3/5] 安装 Forge 47.3.0");
  if (k >= 3) log.push("[4/5] 下载模组文件");
  if (k >= 4) log.push("[5/5] 正在生成启动脚本…");
  if (ended) log.push("部署完成");
  return { steps, label: ended ? "" : PACK_STEPS[k], done: (k + 1) * 40 * MB, total: 200 * MB, error: "", warnings: [], log, ended, result: PACK };
}

const BOOT_LOG = [
  "[21:30:01 INFO]: Starting minecraft server version 1.21.4",
  "[21:30:01 INFO]: Loading properties",
  "[21:30:02 INFO]: This server is running Paper version 1.21.4-232-main@a1b2c3d (2026-10-01T12:00:00Z)",
  "[21:30:02 INFO]: Default game type: SURVIVAL",
  "[21:30:03 INFO]: Starting Minecraft server on *:25565",
  "[21:30:04 WARN]: Using offline mode! Players will not be authenticated.",
  "[21:30:05 INFO]: Preparing level \"world\"",
  "[21:30:07 INFO]: Preparing spawn area: 52%",
  "[21:30:08 INFO]: Preparing spawn area: 100%",
  "[21:30:09 INFO]: Done (6.812s)! For help, type \"help\"",
];
const CRASH_LOG = [
  "[21:02:11 INFO]: Loading Minecraft 1.20.1 with Forge 47.3.0",
  "[21:02:13 INFO]: Found 214 mods",
  "[21:02:14 ERROR]: Missing or unsupported mandatory dependencies:",
  "[21:02:14 ERROR]:   Mod ID: 'geckolib', Requested by: 'create', Expected range: '[4.4,)', Actual version: '[MISSING]'",
  "[21:02:14 ERROR]: Failed to start the minecraft server",
];
const JOIN_LOG = [
  "[21:42:17 INFO]: Steve joined the game",
  "[21:43:01 INFO]: Alex joined the game",
  "[21:44:30 INFO]: <Steve> 大家好",
  "[21:46:12 INFO]: Kai_07 joined the game",
];

// 玩家名单（有状态，增删会写回来）
async function playersRoute(ctx, json, st, d) {
  d.pl = { online: ["Steve", "Alex", "Kai_07"], whitelist: [{ name: "Steve" }, { name: "Alex" }], ops: [{ name: "Steve" }], banned: [{ name: "Griefer233" }] };
  await ctx.route("**/api/instances/*/players", async r => {
    if (r.request().method() === "GET") return json(r, d.pl);
    const b = r.request().postDataJSON();
    if (b.action === "whitelist-add") d.pl.whitelist.push({ name: b.player });
    if (b.action === "op") d.pl.ops.push({ name: b.player });
    await json(r, {});
  });
}

const svgFolder = '<svg viewBox="0 0 24 24" fill="#d9b45a"><path d="M2 6.5A2.5 2.5 0 0 1 4.5 4h5l2.2 2.5h7.8A2.5 2.5 0 0 1 22 9v8.5a2.5 2.5 0 0 1-2.5 2.5h-15A2.5 2.5 0 0 1 2 17.5z"/></svg>';
const svgPC = '<svg viewBox="0 0 24 24" fill="none" stroke="#3f7d20" stroke-width="1.7" stroke-linejoin="round" stroke-linecap="round"><rect x="3" y="4" width="18" height="12" rx="2"/><path d="M9 20h6M12 16v4"/></svg>';
const svgRouter = '<svg viewBox="0 0 24 24" fill="none" stroke="#3f7d20" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="13" width="18" height="7" rx="2"/><path d="M7 16.5h.01M11 16.5h.01M8 9a6 6 0 0 1 8 0M5.5 6.5a10 10 0 0 1 13 0"/></svg>';

module.exports = [
  // ---------- 开场 ----------
  { id: "00", title: "开场", boot: { hash: "#/instances", ready: `.card[data-name="${SURV}"]`, setup: st => keep(st, [SURV, PACK]) },
    run: async d => {
      await d.chapter("", "MineDeck 上手演示", "从下载、开服，到日常管理，完整走一遍", { hold: 1.7 });
      await d.caption({ main: "这是开好以后的样子", sub: "几台服务器，一个窗口管" });
      await d.cam(`.card[data-name="${SURV}"]`, { pad: 30, dur: 1.5, minW: 800 });
      await d.wait(2.4);
      await d.camReset({ dur: 1 });
      await d.caption({ main: "下面从零开始", sub: "下载、建服、让朋友连进来，都在窗口里点" });
      await d.wait(1.2);
      await d.fadeOut();
    } },

  // ---------- 下载和第一次打开 ----------
  { id: "01", title: "下载和第一次打开",
    boot: { hash: "#/instances", ready: "#ob-agree", setup: st => { st.empty = true; st.config.onboarded = false; st.config.tosAccepted = false; } },
    run: async d => {
      await d.chapter("01", "下载和第一次打开", "", { keep: true });
      await d.card(`<div class="wrap"><div class="eb"><i></i>准备</div><div class="h">只要一个文件</div>
        <div class="row">
          <div class="tile" data-in="0.35"><img src="${d.icon}" alt=""><b>MineDeck.exe</b><span>到 GitHub 发布页下载</span></div>
          <div class="arrow" data-in="1">→</div>
          <div class="tile" data-in="1.2">${svgFolder}<b>新建一个文件夹</b><span>把它放进去，双击运行</span></div>
          <div class="arrow" data-in="1.9">→</div>
          <div class="tile" data-in="2.1">${svgFolder}<b>以后都在这里面</b><span>服务器、Java、备份</span></div>
        </div></div>`);
      await d.wait(5);
      await d.card(`<div class="wrap"><div class="eb"><i></i>第一次运行</div><div class="h">Windows 提示已保护你的电脑？</div>
        <div class="s">程序没有数字签名才会这样，源码是公开的</div>
        <div class="row" style="justify-content:flex-start;align-items:center">
          <div class="pill" data-in="0.6">更多信息</div><div class="arrow" data-in="1.1">→</div><div class="pill" data-in="1.3">仍要运行</div>
        </div></div>`);
      await d.wait(3.8);
      await d.cardOff();
      await d.caption({ eb: "第一次打开", main: "先看一遍用户协议" });
      await d.wait(1.2);
      await d.click("#ob-agree");
      await d.caption({ eb: "第一次打开", main: "勾上同意，就能开始用了" });
      await d.scrollTo("#ob-go", { box: ".modal.onboard", pad: 400, dur: 0.6 });
      await d.click("#ob-go", { after: 0.5 });
      await d.until({ js: "!document.querySelector('#modal-root').childElementCount" });
      await d.wait(1.2);
      await d.fadeOut();
    } },

  // ---------- 新建服务器 ----------
  { id: "02", title: "新建服务器",
    boot: { hash: "#/instances", ready: "#main", setup: st => { st.empty = true; },
      routes: async (ctx, json, st, d) => {
        d.k = 0;
        await ctx.route("**/api/instances", r => r.request().method() === "POST" ? json(r, { taskId: "task-1" }) : r.fallback());
        await ctx.route("**/api/tasks/**", r => json(r, deployTask(d.k)));
      } },
    run: async d => {
      const eb = "新建服务器";
      await d.chapter("02", "新建服务器", "选核心、选版本、起名字，剩下的它自己做");
      await d.caption({ eb, main: "左边点「新建服务器」" });
      await d.click('.nav-item[data-nav="create"]');
      await d.until("#mode-new");
      await d.caption({ eb, main: "选「从零创建」", sub: "有整合包的话选另一张，后面会讲" });
      await d.click("#mode-new");
      await d.until('.core-card[data-id="paper"]');
      await d.caption({ eb: "第 1 步 选核心", main: "只想玩原版生存，选 Paper", sub: "玩法和原版基本一样，还能装插件", pos: "right" });
      await d.mark('.core-card[data-id="paper"]', { dim: true });
      await d.wait(3.2);
      d.unmark();
      await d.caption({ eb: "第 1 步 选核心", main: "要玩模组，选对应的加载器", sub: "Fabric、Forge、NeoForge，看模组是给谁做的", pos: "right" });
      await d.scrollTo('.core-card[data-id="forge"]', { pad: 170, dur: 0.6 });
      d.mark('.core-card[data-id="fabric"]', { pad: 5 }); d.mark('.core-card[data-id="forge"]', { pad: 5 }); d.mark('.core-card[data-id="neoforge"]', { pad: 5 });
      await d.wait(3.2);
      d.unmark();
      await d.wait(0.2);
      await d.click('.core-card[data-id="paper"]');
      await d.click("#wnext");
      await d.until("#ver-search");
      await d.caption({ eb: "第 2 步 选版本", main: "选游戏版本", sub: "要和朋友们的游戏版本一致", pos: "right" });
      await d.type("#ver-search", "1.21.4");
      await d.until('.ver-item[data-id="1.21.4"]');
      await d.wait(0.5);
      await d.click('.ver-item[data-id="1.21.4"]');
      await d.click("#wnext");
      await d.wait(0.4);
      await d.caption({ eb: "第 3 步 选构建", main: "构建用默认的就行", sub: "默认选中最新的稳定版", pos: "right" });
      await d.wait(1.4);
      await d.click("#wnext");
      await d.until("#f-name");
      await d.caption({ eb: "第 4 步 基本设置", main: "起个名字", pos: "right" });
      await d.type("#f-name", SURV, { clear: true });
      await d.wait(0.3);
      await d.scrollTo("#mem-mount", { pad: 120 });
      const v = await d.page.evaluate(() => { const e = document.querySelector("#f-mem"), b = e.getBoundingClientRect(), f = x => b.left + (x - e.min) / (e.max - e.min) * b.width; return { x0: f(+e.value), x1: f(4096), y: b.top + b.height / 2 }; });
      await d.caption({ eb: "第 4 步 基本设置", main: "没有模组的话，内存给 2 到 4 GB", pos: "right" });
      await d.drag({ x: v.x0, y: v.y }, v.x1);
      await d.wait(0.6);
      await d.scrollTo("#f-diff", { pad: 150 });
      await d.caption({ eb: "第 4 步 基本设置", main: "难度和游戏模式也在这里选", pos: "right" });
      d.mark("#f-diff", { pad: 4 }); d.mark("#f-mode", { pad: 4 });
      await d.setSelect("#f-diff", "normal");
      await d.wait(1.8);
      d.unmark();
      await d.scrollTo("#f-eula", { pad: 300 });
      await d.caption({ eb: "第 4 步 基本设置", main: "同意 EULA，点「开始部署」", pos: "right" });
      await d.click("#f-eula");
      await d.click("#wcreate", { after: 0.2 });
      await d.until("#t-steps .task-step");
      await d.caption({ eb: "自动部署", main: "Java 和服务端自动下载", sub: "优先走国内镜像，下完会校验", pos: "right" });
      await d.badge("快进");
      await d.wait(0.5);
      for (let k = 1; k < DEPLOY.length - 1; k++) { d.k = k; await d.wait(0.72); }
      d.badgeOff();
      await d.wait(0.3);
      d.k = DEPLOY.length - 1;
      await d.until("#t-foot .btn.primary");
      await d.caption({ eb: "自动部署", main: "部署完成", sub: "同意 EULA、写配置、生成启动脚本，都做好了", pos: "right" });
      await d.wait(1);
      await d.fadeOut();
    } },

  // ---------- 启动和控制台 ----------
  { id: "03", title: "启动和控制台",
    boot: { hash: "#/task/task-1", ready: "#t-foot .btn.primary", seedLog: { [SURV]: ["[MineDeck] 服务器未运行，点击「启动」开服"] },
      setup: st => { keep(st, [SURV]); Object.assign(inst(st, SURV), { status: "stopped", uptimeSec: 0, onlineCount: 0 }); },
      routes: async (ctx, json, st, d) => {
        await ctx.route("**/api/tasks/**", r => json(r, deployTask(8)));
        await playersRoute(ctx, json, st, d);
      } },
    run: async d => {
      const eb = "控制台", st = d.mock.state;
      await d.chapter("03", "启动和控制台");
      await d.click("#t-foot .btn.primary");
      await d.until("#d-start:not([disabled])");
      await d.caption({ eb, main: "点「启动」", pos: "right" });
      await d.click("#d-start", { after: 0.1 });
      await d.caption({ eb, main: "日志会一行行进来", pos: "right" });
      for (const l of BOOT_LOG) { await d.log(SURV, l); await d.wait(0.24); }
      Object.assign(inst(st, SURV), { status: "running", uptimeSec: 12 });
      await d.wait(0.3);
      await d.caption({ eb, main: "看到 Done，就开好了", pos: "right" });
      d.mark("#console div|Done (", { dim: true, pad: 3, radius: 6 });
      await d.wait(2.4);
      d.unmark();
      await d.wait(0.2);
      await d.badge("几分钟后");
      inst(st, SURV).uptimeSec = 960; inst(st, SURV).onlineCount = 3;
      for (const l of JOIN_LOG) { await d.log(SURV, l); await d.wait(0.26); }
      await d.caption({ eb, main: "谁进了服，这里都看得到", pos: "right" });
      d.badgeOff();
      await d.wait(0.6);
      await d.cam("#cmd-in", { pad: 40, dur: 0.7, minW: 900, up: -60 });
      await d.caption({ eb: "发命令", main: "下面的框可以直接输命令", sub: "输到一半按 Tab，玩家名自动补全", pos: "right" });
      await d.wait(0.5);
      await d.type("#cmd-in", "op A", { gap: 0.13 });
      await d.wait(0.9);
      await d.press("Tab", { after: 0.8 });
      await d.press("Enter", { after: 0.3 });
      await d.log(SURV, "> op Alex");
      await d.log(SURV, "[21:47:03 INFO]: Made Alex a server operator");
      await d.camReset();
      await d.wait(1);
      await d.caption({ eb: "看日志", main: "日志多了，用过滤框", sub: "只看报错，或者只看某个玩家", pos: "right" });
      await d.type("#con-filter", "Alex");
      await d.wait(1.6);
      await d.press("Control+A", { after: 0.1 }); await d.press("Delete", { after: 0.4 });
      await d.caption({ eb: "看日志", main: "老整合包的日志乱码？", sub: "把编码换成 GBK 试试", pos: "right" });
      d.mark("#con-enc", { dim: true, pad: 5 });
      await d.wait(2.4);
      d.unmark();
      await d.fadeOut();
    } },

  // ---------- 让朋友连进来 ----------
  { id: "04", title: "让朋友连进来",
    boot: { hash: "#/tunnels", ready: "#tn-toggle", setup: st => { keep(st, [SURV]); st.tunnels = []; },
      routes: async (ctx, json, st) => {
        await ctx.route("**/api/tunnels", async r => {
          if (r.request().method() !== "POST") return r.fallback();
          st.tunnels.push({ id: "t1", name: "朋友联机", provider: "openfrp", running: false, boundInstance: SURV, autoStart: true, publicAddr: "", lastError: "", nextAddr: "cn-sz-bgp-1.openfrp.top:23456", addrDelay: 0 });
          await json(r, { id: "t1" });
        });
      } },
    run: async d => {
      await d.chapter("04", "让朋友连进来", "", { keep: true });
      await d.card(`<div class="wrap"><div class="eb"><i></i>在同一个局域网</div><div class="h">连着同一个路由器的朋友</div>
        <div class="row">
          <div class="tile" data-in="0.3">${svgPC}<b>你的电脑</b><span>开着服务器</span></div>
          <div class="arrow" data-in="0.8">—</div>
          <div class="tile" data-in="0.9">${svgRouter}<b>同一个路由器</b><span>不用内网穿透</span></div>
          <div class="arrow" data-in="1.3">—</div>
          <div class="tile" data-in="1.4">${svgPC}<b>朋友的电脑</b><span>游戏里添加服务器</span></div>
        </div>
        <div class="addr" data-in="1.9">地址填 192.168.x.x:25565</div>
        <div class="s" data-in="2.1">你电脑的内网 IP 加端口，IP 在 Windows 的网络设置里能看到</div></div>`);
      await d.wait(5.4);
      await d.card(`<div class="wrap"><div class="eb"><i></i>不在同一个网络</div><div class="h">用内网穿透，以 OpenFrp 为例</div>
        <ol>
          <li data-in="0.4"><i>1</i>到 OpenFrp 网站建一条 TCP 隧道</li>
          <li data-in="1.0"><i>2</i>本地端口填服务器的端口，这里是 25565</li>
          <li data-in="1.6"><i>3</i>复制用户密钥和隧道 ID，回到 MineDeck</li>
        </ol></div>`);
      await d.wait(5);
      await d.cardOff();
      const eb = "内网穿透";
      await d.caption({ eb, main: "点右上角「添加隧道」", pos: "right" });
      await d.click("#tn-toggle");
      await d.until("#tn-provider");
      await d.scrollTo("#tn-form", { pad: 56, dur: 0.6 });
      await d.caption({ eb, main: "选服务商，填密钥和隧道 ID", sub: "樱花穿透和自己的 frps，填法也差不多", pos: "right" });
      await d.type("#tn-name", "朋友联机", { clear: true, gap: 0.12 });
      await d.type("#tn-cred", "demo-3f9a7c21e84b", { gap: 0.04 });
      await d.type("#tn-proxy", "123456", { gap: 0.07 });
      await d.wait(0.4);
      await d.caption({ eb, main: "绑定服务器，隧道跟着一起启动", sub: "「跟随实例启动」默认开着，一开服隧道就连上", pos: "top" });
      await d.setSelect("#tn-bound", SURV);
      d.mark("#tn-bound", { pad: 4 });
      await d.wait(1.2);
      d.mark(".switch:has(#tn-auto)", { pad: 5 });
      await d.wait(1.6);
      d.unmark();
      await d.wait(0.3);
      await d.click("#tn-add");
      await d.until('[data-tstart="t1"]');
      await d.caption({ eb, main: "启动隧道", pos: "right" });
      await d.click('[data-tstart="t1"]');
      await d.until('[data-tun="t1"] [data-copy]', { max: 10 });
      await d.caption({ eb, main: "连上以后，这里是公网地址", sub: "点复制，发给朋友", pos: "right" });
      d.mark('[data-tun="t1"] *|openfrp.top:23456', { dim: true, pad: 5 });
      await d.wait(2);
      d.unmark();
      await d.click('[data-tun="t1"] [data-copy]');
      await d.wait(1.6);
      await d.fadeOut();
    } },

  // ---------- 改设置 ----------
  { id: "05", title: "改设置",
    boot: { hash: `#/inst/${enc(SURV)}/common`, ready: "#cp-search", seedLog: {}, setup: st => { keep(st, [SURV]); Object.assign(inst(st, SURV), { status: "stopped", uptimeSec: 0, onlineCount: 0 }); } },
    run: async d => {
      const eb = "常用设置";
      await d.chapter("05", "改设置");
      await d.caption({ eb, main: "常用的几十项都分好了组", sub: "每项有中文说明，也标了要不要重启", pos: "right" });
      const y = await d.page.evaluate(() => document.querySelector("#main").scrollTop);
      await d.scrollY(y, y + 360, 2.2);
      await d.wait(0.6);
      d.mark("#cp-list *|重启生效", { pad: 4 });
      await d.wait(1.6);
      d.unmark();
      await d.scrollY(y + 360, y, 0.8);
      await d.caption({ eb, main: "找不到就搜，比如「视距」", pos: "right" });
      await d.type("#cp-search", "视距", { gap: 0.2 });
      await d.wait(0.7);
      await d.type('[data-k="view-distance"]', "12", { clear: true, gap: 0.16 });
      await d.wait(0.9);
      await d.click("#cp-search");
      await d.press("Control+A", { after: 0.1 }); await d.press("Delete", { after: 0.5 });
      await d.until("#motd-pal");
      await d.scrollTo("#motd-in", { pad: 170 });
      await d.caption({ eb, main: "服务器介绍可以点颜色", sub: "下面能看到效果", pos: "right" });
      await d.click("#motd-in");
      await d.press("Control+A", { after: 0.1 }); await d.press("Delete", { after: 0.2 });
      await d.click("#motd-pal button:nth-of-type(7)", { after: 0.4 });
      await d.type(null, "朋友的生存服", { click: false, gap: 0.14 });
      await d.wait(1.4);
      await d.caption({ eb, main: "改完点保存", sub: "标着重启生效的，重启一下服务器" });
      await d.click('[data-sb="save"]');
      await d.wait(1.2);
      await d.caption({ eb: "全部配置", main: "其他键值都在「全部配置」", pos: "right" });
      await d.click('.tab[data-t="raw"]');
      await d.wait(1.6);
      await d.caption({ eb: "内存 / 启动", main: "内存和 JVM 参数在这里调", sub: "也可以套用 Aikar's Flags", pos: "right" });
      await d.click('.tab[data-t="jvm"]');
      await d.until("#jm-aikar");
      d.mark("#jm-aikar", { pad: 5 });
      await d.wait(2.2);
      d.unmark();
      await d.fadeOut();
    } },

  // ---------- 玩家和备份 ----------
  { id: "06", title: "玩家和备份",
    boot: { hash: `#/inst/${enc(SURV)}/players`, ready: '[data-add="whitelist-add"]', seedLog: {}, setup: st => keep(st, [SURV]),
      routes: async (ctx, json, st, d) => {
        await playersRoute(ctx, json, st, d);
        d.bk = [{ file: "2026-11-06_04-00_auto.zip", sizeMb: 412.3, time: "2026-11-06 04:00" }, { file: "2026-11-05_04-00_auto.zip", sizeMb: 398.1, time: "2026-11-05 04:00" }];
        d.pol = { crashRestart: false, schedules: [] };
        await ctx.route("**/api/instances/*/backups", async r => {
          if (r.request().method() === "GET") return json(r, d.bk);
          const b = r.request().postDataJSON() || {}, file = `2026-11-06_20-05_${b.label || "manual"}.zip`;
          d.bk.unshift({ file, sizeMb: 418.6, time: "2026-11-06 20:05" });
          await json(r, { file });
        });
        await ctx.route("**/api/instances/*/backups/restore", r => json(r, {}));
        await ctx.route("**/api/instances/*/policies", async r => { if (r.request().method() === "GET") return json(r, d.pol); d.pol = r.request().postDataJSON(); await json(r, d.pol); });
      } },
    run: async d => {
      await d.chapter("06", "玩家和备份");
      await d.caption({ eb: "玩家", main: "这里能看到谁在线", sub: "可以给 OP，也可以踢人", pos: "right" });
      await d.cam(".p-sec", { pad: 26, minW: 700, up: 90, dur: 0.7 });
      await d.wait(0.7);
      d.mark('[aria-label="OP Alex"]', { pad: 4 }); d.mark('[aria-label="踢 Alex"]', { pad: 4 });
      await d.wait(2);
      d.unmark();
      await d.camReset();
      await d.wait(0.7);
      await d.caption({ eb: "玩家", main: "白名单、管理员、封禁名单", sub: "输名字回车就加上了；服务器关着也能改", pos: "right" });
      await d.type('[data-add="whitelist-add"]', "Mia_23", { gap: 0.1 });
      await d.press("Enter", { after: 1.4 });
      await d.caption({ eb: "备份", main: "开着服也能备份", sub: "写个标签，回档时好找", pos: "right" });
      await d.click('.tab[data-t="backups"]');
      await d.until("#bk-create");
      await d.type("#bk-label", "打龙前", { gap: 0.2 });
      await d.click("#bk-create", { after: 0.8 });
      await d.until('[data-restore="2026-11-06_20-05_打龙前.zip"]');
      d.mark('[data-restore="2026-11-06_20-05_打龙前.zip"]', { pad: 4 });
      await d.wait(1.5);
      d.unmark();
      await d.caption({ eb: "备份", main: "要回档，先停服，再点还原", pos: "right" });
      await d.click("#d-stop", { after: 0.2 });
      Object.assign(inst(d.mock.state, SURV), { status: "stopped", uptimeSec: 0, onlineCount: 0 });
      await d.wait(0.6);
      await d.click('[data-restore="2026-11-06_20-05_打龙前.zip"]', { after: 0.4 });
      await d.until(".modal [data-x='ok']");
      await d.caption({ eb: "备份", main: "还原之前，会先备份现在的世界", pos: "right" });
      await d.wait(1.8);
      await d.click(".modal [data-x='ok']", { after: 0.5 });
      await d.wait(0.9);
      await d.caption({ eb: "定时任务", main: "崩溃了自动重启", sub: "隔 10、30、60 秒再试，连崩 3 次就停", pos: "right" });
      await d.click('.tab[data-t="tasks"]');
      await d.until("#pol-crash");
      await d.click("#pol-crash", { after: 0.5 });
      await d.wait(1.2);
      await d.caption({ eb: "定时任务", main: "还可以每天定时备份", sub: "定时重启、定时发命令也行", pos: "right" });
      await d.setSelect("#sc-type", "backup");
      d.mark("#sc-type", { pad: 4 });
      await d.wait(1);
      d.unmark();
      await d.click("#sc-add", { after: 0.5 });
      await d.click('[data-sb="save"]');
      await d.wait(1.2);
      await d.fadeOut();
    } },

  // ---------- 模组和整合包 ----------
  { id: "07", title: "模组和整合包",
    boot: { hash: `#/inst/${enc(SURV)}/res`, ready: "#res-q", seedLog: {}, setup: st => keep(st, [SURV]),
      routes: async (ctx, json, st, d) => {
        d.k = 0;
        await ctx.route("**/resources/search*", r => json(r, { hits: [{ project_id: "a3", title: "Chunky", description: "Pre-generates chunks, quickly and efficiently.", icon_url: "", downloads: 2100000 }] }));
        await ctx.route("**/resources/install", r => json(r, { file: "Chunky-Bukkit-1.4.28.jar" }));
        await ctx.route("**/api/import/modpack", r => json(r, { taskId: "task-2", pack: { core: "Forge", mc: "1.20.1", files: 214 } }));
        await ctx.route("**/api/tasks/**", r => json(r, packTask(d.k)));
      } },
    run: async d => {
      await d.chapter("07", "模组和整合包");
      await d.caption({ eb: "资源", main: "插件和模组在这里搜", sub: "搜的是 Modrinth，只列出和这台服务器对得上的", pos: "right" });
      await d.type("#res-q", "chunky", { gap: 0.11 });
      await d.click("#res-go", { after: 0.6 });
      await d.until("[data-rid]");
      await d.wait(0.9);
      await d.caption({ eb: "资源", main: "点安装，重启服务器后生效", pos: "right" });
      await d.click("[data-rid]", { after: 1.6 });
      await d.caption({ eb: "整合包", main: "想玩整合包，选「导入整合包」", pos: "right" });
      await d.click('.nav-item[data-nav="create"]');
      await d.until("#mode-import");
      await d.click("#mode-import");
      await d.until("#im-file");
      await d.caption({ eb: "整合包", main: "把 .mrpack 文件拖进来", sub: "CurseForge 的也能导，要先填 API Key", pos: "right" });
      const r = await d.rect("#im-file");
      await d.chip(PACK + "-1.4.mrpack", { x: 1010, y: 560 }, { x: r.x + 120, y: r.y + 2 });
      await (await d.page.$("#im-file")).setInputFiles({ name: PACK + "-1.4.mrpack", mimeType: "application/x-modrinth-modpack+zip", buffer: Buffer.from("PK\u0003\u0004") });
      await d.frames(2);
      await d.type("#im-name", PACK, { clear: true, gap: 0.08 });
      await d.wait(0.3);
      await d.scrollTo("#im-eula", { pad: 300 });
      await d.click("#im-eula");
      await d.caption({ eb: "整合包", main: "同意 EULA，点「导入并部署」" });
      await d.click("#im-go", { after: 0.2 });
      await d.until("#t-steps .task-step");
      await d.caption({ eb: "整合包", main: "核心、版本和加载器自动识别", sub: "只给客户端用的文件会跳过", pos: "right" });
      await d.badge("快进");
      for (let k = 1; k <= PACK_STEPS.length; k++) { await d.wait(0.9); d.k = k; }
      await d.until("#t-foot .btn.primary");
      d.badgeOff();
      await d.wait(1.2);
      await d.fadeOut();
    } },

  // ---------- AI：接入、报错、助手 ----------
  { id: "08", title: "AI 开服助手",
    boot: { hash: "#/settings/ai", ready: "#ai-preset", seedLog: { [PACK]: CRASH_LOG },
      // AI 的回答是真实时间推的事件流：正式录的时候一帧要几十毫秒，块间隔放大，看上去才是一段段写出来的
      setup: async (st, mock, d) => { keep(st, [SURV, PACK]); await post(mock, { asst: { reset: true, slow: d.preview ? 60 : 420 } }); } },
    run: async d => {
      await d.chapter("08", "AI 是可选的", "不配置，就不会连接任何 AI 服务");
      await d.caption({ eb: "接入 AI", main: "选一家服务商，填你自己的 Key", sub: "内置 7 个预设，也可以填自定义地址" });
      await d.mark("#ai-preset", { pad: 4 });
      await d.wait(1.2);
      await d.setSelect("#ai-preset", "deepseek");
      await d.wait(0.5);
      d.unmark();
      await d.type("#ai-key", "sk-demo-7c21e84b3f9a", { gap: 0.05 });
      await d.wait(0.4);
      await d.click('[data-sb="save"]', { after: 0.7 });
      await d.caption({ eb: "接入 AI", main: "测试一下连接", sub: "Key 加密保存在这台电脑上" });
      await d.click("#ai-test", { after: 0.4 });
      await d.until("#ai-test-sub|连接正常", { max: 6 });
      await d.wait(1.2);
      await d.scrollTo("#main *|每日额度", { pad: 70 });
      await d.caption({ eb: "接入 AI", main: "每个功能都能设每天的上限", sub: "按 token 算，用到上限就停" });
      await d.wait(1.6);
      await d.go(`#/inst/${enc(PACK)}/console`, ".diag-banner");
      await d.caption({ eb: "报错横幅", main: "开服失败，顶上直接说原因", sub: "缺前置、端口被占这些，不用 AI 也认得出", pos: "right" });
      d.mark(".diag-banner", { pad: 5 });
      await d.wait(3);
      d.unmark();
      await d.caption({ eb: "AI 助手", main: "接了 AI，还可以让它分析", pos: "right" });
      await d.click('.diag-banner [data-x="ai"]', { after: 0.4 });
      await d.until(".modal.ap-pv textarea");
      await d.type(".modal.ap-pv textarea", "整合包一开服就崩，帮我看看是哪个模组的问题", { gap: 0.05 });
      await d.wait(0.4);
      await d.click('.modal [data-x="ok"]', { after: 0.3 });
      await d.caption({ eb: "AI 助手", main: "它先查日志和配置，再回答", pos: "left" });
      await d.until("#ai-panel .ap-card.pending", { max: 30 });
      await d.until({ js: "(()=>{const b=document.querySelector('#ap-stop');return b&&b.hidden})()" }, { max: 30 });
      await d.scrollIntoPanel();
      await d.caption({ eb: "AI 助手", main: "要改东西，先弹卡片问你", sub: "你点了确认才执行", pos: "left" });
      d.mark('#ai-panel .ap-card.pending', { dim: true, pad: 5 });
      await d.wait(3);
      d.unmark();
      await d.fadeOut();
    } },

  // ---------- 游戏里的 AI 和音乐 ----------
  { id: "09", title: "游戏内 AI、伙伴和音乐共享",
    boot: { hash: `#/inst/${enc(PACK)}/gameai`, ready: "#gc-log .gc-rec", seedLog: {},
      setup: async (st, mock) => {
        keep(st, [SURV, PACK]);
        const log = (await (await fetch(mock.url + `api/instances/${enc(SURV)}/gameai/log`)).json()).data;
        await post(mock, { ai: AI, gc: { reset: true, inst: { [PACK]: { whitelistOn: true, log, settings: { enabled: true, allowed: ["Alex", "Steve"] } } } },
          cp: { reset: true, inst: { [PACK]: { mods: CP_INSTALLED, settings: { enabled: true, zones: CP_ZONES } } } },
          music: { reset: true, voiceTunnel: true, inst: { [PACK]: { bridge: "main", voiceHost: "frp-abc.natfrp.cloud:34567" } } } });
        await post(mock, { status: { [PACK]: "starting" } }); await post(mock, { status: { [PACK]: "running" } });
      } },
    run: async d => {
      await d.chapter("09", "游戏里的 AI 和音乐", "这几项也是可选的");
      await d.caption({ eb: "游戏内 AI", main: "玩家在聊天里 @AI 提问", sub: "默认全服都能看到回答", pos: "right" });
      await d.wait(1.4);
      await d.scrollTo("#gc-log", { pad: 110, dur: 1 });
      await d.wait(1.4);
      await d.caption({ eb: "游戏内 AI", main: "勾选的玩家，才能让它执行命令", sub: "只能执行白名单里的命令，op 这类不会执行", pos: "right" });
      d.mark("#gc-log .gc-rec", { pad: 3 });
      await d.wait(2.8);
      d.unmark();
      await d.go(`#/inst/${enc(PACK)}/companion`, "#cp-spawn:not([disabled])");
      await d.caption({ eb: "AI 伙伴", main: "在服务器里放一个 AI 伙伴", sub: "支持 NeoForge 1.21.1 和 Forge 1.20.1", pos: "right" });
      await d.wait(1);
      await d.click("#cp-spawn", { after: 0.3 });
      await d.until(".modal #cs-name");
      await d.type(".modal #cs-name", "小方", { clear: true, gap: 0.2 });
      await d.type(".modal #cs-owner", "Steve", { clear: true, gap: 0.08 });
      await d.click('.modal [data-x="ok"]', { after: 0.6 });
      await d.until({ js: "!document.querySelector('#modal-root').childElementCount" });
      await post(d.mock, { cp: { busy: true } });
      await d.wait(1);
      await d.until('#cp-list [data-comp="1"]', { max: 6 });
      await d.scrollTo("#cp-list", { pad: 120 });
      await d.caption({ eb: "AI 伙伴", main: "聊天里说「@小方 砍 10 个橡木原木」", sub: "会跟随、砍树、打怪，好友只需装一个伙伴模组", pos: "right" });
      d.mark('#cp-list [data-comp="1"]', { pad: 4 });
      await d.wait(3);
      d.unmark();
      await d.caption({ eb: "AI 伙伴", main: "可以划出不让它挖的区域", pos: "right" });
      await d.wait(1.6);
      await d.go(`#/inst/${enc(PACK)}/music`, "#mu-src");
      await d.caption({ eb: "音乐共享", main: "房主放的歌，全服都能听", sub: "每个人可以单独调小或静音", pos: "right" });
      d.mark("#mu-src", { pad: 5 });
      await d.wait(3);
      d.unmark();
      await d.wait(0.3);
      await d.fadeOut();
    } },

  // ---------- 其他小功能 ----------
  { id: "10", title: "其他小功能",
    boot: { hash: "#/settings/remote", ready: "#qr-box svg", seedLog: {}, setup: st => { keep(st, [SURV, PACK]); st.config.listenLan = true; st.lanActive = true; st.lanSet = true; } },
    run: async d => {
      await d.chapter("10", "其他小功能");
      await d.scrollTo("#qr-box", { pad: 130, dur: 0.6 });
      await d.caption({ eb: "远程访问", main: "局域网里，手机也能管", sub: "打开远程访问，设好密码，重启一次，扫码就能进", pos: "right" });
      d.mark("#qr-box", { pad: 6 });
      await d.wait(3);
      d.unmark();
      const phone = await d.phoneShot("#/instances", `.card[data-name="${SURV}"]`);
      await d.noCaption();
      await d.card(`<div class="wrap" style="right:480px"><div class="eb"><i></i>手机上</div><div class="h">看状态、发命令、开关服</div><div class="s">手机和电脑连同一个 Wi-Fi 就行</div></div>
        <div class="phone" data-in="0.2"><img src="${phone}" alt=""></div>`);
      await d.wait(3);
      await d.cardOff();
      await d.go("#/settings/startup", "#main");
      await d.caption({ eb: "启动与托盘", main: "关窗口时可以缩到托盘", sub: "服务器接着跑，也可以开机自动启动", pos: "right" });
      await d.wait(2);
      await d.noCaption();
      await d.card(`<div class="wrap" style="bottom:120px"><div class="eb"><i></i>系统托盘</div><div class="h">右键托盘图标</div><div class="s">打开面板，或者全部停止并退出</div></div>
        <div class="menu" data-in="0.4"><div class="on">打开面板</div><div>全部停止并退出</div></div>
        <div class="bar"><span>任务栏</span><img src="${d.icon}" alt=""></div>`);
      await d.wait(2.8);
      await d.cardOff();
      await d.go("#/settings/appearance", '[data-scheme="nord"]');
      await d.caption({ eb: "外观", main: "七套配色，深色浅色都有", sub: "还可以另选强调色" });
      await d.click('[data-scheme="nord"]', { after: 0.8 });
      await d.click('[data-scheme="catppuccin"]', { after: 0.8 });
      await d.click('[data-mode="dark"]', { after: 1 });
      await d.click('[data-scheme="craft"]', { after: 0.9 });
      await d.click('[data-mode="light"]', { after: 0.7 });
      await d.go("#/settings/java", "#main");
      await d.caption({ eb: "Java 运行时", main: "Java 不用自己装", sub: "本机有合适的就直接用，没有就下载一份", pos: "right" });
      await d.wait(1.6);
      await d.go("#/settings/storage", "#main");
      await d.caption({ eb: "存储位置", main: "服务器和备份可以放到别的盘", pos: "right" });
      await d.wait(1.4);
      await d.go("#/settings/about", "#main");
      await d.caption({ eb: "更新与关于", main: "新版本在这里检查和更新", pos: "right" });
      await d.wait(1.2);
      await d.fadeOut();
    } },

  // ---------- 片尾 ----------
  { id: "11", title: "片尾", boot: { hash: "#/instances", ready: "#main" },
    run: async d => {
      await d.card(`<div class="end"><img data-in="0" src="${d.icon}" alt=""><div class="w" data-in="0.25">MineDeck</div>
        <div class="l" data-in="0.5"><span>免费开源</span><i></i><span>Windows 10/11</span><i></i><span>v3.0.0</span></div>
        <div class="u" data-in="0.75">trasperioooooon.github.io/MineDeck</div><div class="g" data-in="0.95">GitHub 搜索 MineDeck</div></div>`);
      await d.wait(6);
      await d.fadeOut();
    } },
];
