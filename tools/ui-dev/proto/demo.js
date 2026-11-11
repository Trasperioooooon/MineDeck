// 演示里多演的几样。
// 仿真后端（../server.js）是给交互测试写的，很多操作只回一句「好」，数据并不跟着变：点了「启动」服务器还是停着，
// 建完服务器进度永远卡在第三步，接管进来的服务器点「进入」说不存在。评审的人知道是假的，无所谓；
// 放到官网上给没用过的人点，就像是坏了。这里在仿真后端前面补几段假流程（走 proto.js 的 before 钩子），
// 只动内存里的数据，不改 server.js：它要保持交互测试需要的样子。
//
//   启动 / 停止        状态真的变（启动中 → 运行中，停止中 → 已停止），控制台跟着出几行，运行时长开始走
//   控制台里发命令      回显出来；认得的几条（list、say、time、weather、gamerule……）给一句像样的回答，别的明说没有真的执行
//   从零创建            部署进度从第一步走到完成，「进入服务器」去的是刚建的那一台
//   接管、导入交接包     任务完成后列表里真的多出那一台
//   玩家                加白名单、设管理员、封禁、踢人以后，名单真的变
//   备份                立即备份多出一份，能删；还原要先停服，还原前自动再存一份
//   导入整合包          明说演示里不能导入（仿真后端没有这个接口，界面原来会冒出一句英文报错）
// 每台服务器的玩家和备份从 fiction.js 里抄一份来改；fiction.js 里没写的那几台，用和仿真后端一样的那份默认数据。
"use strict";
const { PLAYERS, BACKUPS } = require("./fiction");

const CORE_NAME = { vanilla: "Vanilla", paper: "Paper", purpur: "Purpur", leaves: "Leaves", folia: "Folia", fabric: "Fabric", neoforge: "NeoForge", forge: "Forge", mohist: "Mohist", banner: "Banner", velocity: "Velocity", waterfall: "Waterfall" };
const KIND = { velocity: "proxy", waterfall: "proxy", mohist: "hybrid", banner: "hybrid" };
const NO_IMPORT = "这里是演示，不能真的导入整合包。装好 MineDeck 以后，选一个整合包文件，它会自己认出核心和版本、把模组下载齐、装成一台能开的服务器。";

const ok = (res, data) => { res.writeHead(200, { "Content-Type": "application/json; charset=utf-8" }); res.end(JSON.stringify({ ok: true, data })); };
const err = (res, code, msg) => { res.writeHead(code, { "Content-Type": "application/json; charset=utf-8" }); res.end(JSON.stringify({ ok: false, error: msg })); };
const readBody = req => new Promise(r => { let b = ""; req.on("data", c => b += c); req.on("end", () => { try { r(JSON.parse(b || "{}")); } catch { r({}); } }); });
const clock = () => new Date().toTimeString().slice(0, 8);
const info = text => `[${clock()} INFO]: ${text}`;

// 这一份数据上的演示状态。挂在 state 上、不可枚举：换场景（proto.js 的 reset）时跟着清掉，/__mock/state 编成 JSON 时也不带它
function mem(state) {
  if (!state.__demo) Object.defineProperty(state, "__demo", { value: { cons: new Map(), timers: new Set(), started: new Map(), players: new Map(), backups: new Map(), created: null }, enumerable: false, configurable: true, writable: true });
  return state.__demo;
}
// 一台服务器的玩家名单、备份列表：第一次用到时抄一份，之后改的都是这份
const clone = x => JSON.parse(JSON.stringify(x));
const PLAYERS_DEFAULT = { online: ["Steve", "Alex", "Notch_CN"], whitelist: [{ name: "Steve" }, { name: "Alex" }], ops: [{ name: "Steve" }], banned: [{ name: "Griefer233" }] };
const BACKUPS_DEFAULT = [
  { file: "2026-10-26_04-00_auto.zip", sizeMb: 412.3, time: "2026-10-26 04:00" },
  { file: "2026-10-25_22-10_打龙前.zip", sizeMb: 405.8, time: "2026-10-25 22:10" },
  { file: "2026-10-25_04-00_auto.zip", sizeMb: 398.1, time: "2026-10-25 04:00" },
];
const roster = (d, name) => d.players.get(name) || d.players.set(name, clone(PLAYERS[name] || PLAYERS_DEFAULT)).get(name);
const shelf = (d, name) => d.backups.get(name) || d.backups.set(name, clone(BACKUPS[name] || BACKUPS_DEFAULT)).get(name);
// 新的一份备份：文件名同真程序（年月日-时分秒，带上标签），大小照着上一份
function newBackup(list, label) {
  const t = new Date(), z = n => String(n).padStart(2, "0");
  const day = [t.getFullYear(), z(t.getMonth() + 1), z(t.getDate())], hm = [z(t.getHours()), z(t.getMinutes())];
  const tag = String(label || "").trim().replace(/[\\/:*?"<>|\s]+/g, "_");
  return { file: `${day.join("")}-${hm.join("")}${z(t.getSeconds())}${tag ? "-" + tag : ""}.zip`, sizeMb: list[0] ? list[0].sizeMb : 96.4, time: `${day.join("-")} ${hm.join(":")}` };
}
function demoReset(state) {
  if (!state.__demo) return;
  for (const t of state.__demo.timers) clearTimeout(t);
  delete state.__demo;
}
const later = (d, ms, fn) => { const t = setTimeout(() => { d.timers.delete(t); fn(); }, ms); d.timers.add(t); };
// 往这台服务器开着的控制台里写一行
const say = (d, name, line) => { for (const r of d.cons.get(name) || []) r.write(`data: ${JSON.stringify(line)}\n\n`); };
// 仿真后端答完之后再做点事（它回的是成功才做）
const after = (res, fn) => { const end = res.end.bind(res); res.end = body => { try { if (JSON.parse(body).ok) fn(); } catch {} return end(body); }; };

const newInst = o => ({
  kind: KIND[o.core] || "game", build: "", xmsMb: 0, status: "stopped", motd: "", createdAt: new Date().toISOString(), consoleEncoding: "auto",
  onlineCount: 0, uptimeSec: 0, maxPlayers: KIND[o.core] === "proxy" ? 0 : 20, extraJvm: [], javaSource: "portable",
  javaPath: `D:\\MineDeck\\runtimes\\java-${o.javaMajor}\\bin\\java.exe`, adopted: false, ...o,
});

function startUp(d, i) {
  const proxy = i.kind === "proxy";
  i.status = "starting"; i.uptimeSec = 0; i.onlineCount = 0;
  say(d, i.name, `[MineDeck] 启动: java -Xmx${i.xmxMb}M ${[...(i.extraJvm || []), "-jar server.jar", proxy ? "" : "nogui"].filter(Boolean).join(" ")}`);
  const line = text => { if (i.status === "starting") say(d, i.name, info(text)); };
  later(d, 700, () => line(proxy ? `Booting up ${CORE_NAME[i.core] || i.core} ${i.mc}...` : `Starting minecraft server version ${i.mc}`));
  later(d, 1500, () => line(proxy ? `Listening on /0.0.0.0:${i.port}` : 'Preparing level "world"'));
  later(d, 2600, () => {
    if (i.status !== "starting") return;
    i.status = "running"; d.started.set(i.name, Date.now());
    say(d, i.name, info(proxy ? "Done (1.87s)!" : 'Done (2.412s)! For help, type "help"'));
  });
}
function shutDown(d, i) {
  if (i.status === "stopping" || i.status === "stopped") return;
  i.status = "stopping";
  say(d, i.name, info(i.kind === "proxy" ? "Shutting down the proxy..." : "Stopping the server"));
  if (i.kind !== "proxy") later(d, 500, () => say(d, i.name, info("Saving worlds")));
  later(d, 1600, () => {
    if (i.status !== "stopping") return;
    i.status = "stopped"; i.onlineCount = 0; i.uptimeSec = 0; d.started.delete(i.name);
    say(d, i.name, "[MineDeck] 服务器已正常关闭");
  });
}

// 控制台里敲的命令：回显，再给一句回答
function command(d, i, cmd) {
  say(d, i.name, "> " + cmd);
  const w = cmd.split(/\s+/), head = (w[0] || "").toLowerCase();
  if (head === "stop" || head === "end") return shutDown(d, i);
  const online = i.status === "running" ? roster(d, i.name).online : [];
  const TIME = { day: 1000, noon: 6000, night: 13000, midnight: 18000 };
  const answer =
    head === "list" ? `There are ${online.length} of a max of ${i.maxPlayers} players online: ${online.join(", ")}`
    : head === "say" ? `[Server] ${w.slice(1).join(" ")}`
    : head === "time" && w[1] === "set" && w[2] ? `Set the time to ${TIME[w[2]] ?? w[2]}`
    : head === "weather" && w[1] ? `Set the weather to ${w[1]}`
    : head === "gamerule" && w[2] ? `Gamerule ${w[1]} is now set to: ${w[2]}`
    : head === "whitelist" && w[1] === "add" && w[2] ? `Added ${w[2]} to the whitelist`
    : head === "op" && w[1] ? `Made ${w[1]} a server operator`
    : head === "seed" ? "Seed: [-4172144997902289642]"
    : "";
  later(d, 180, () => i.status !== "stopped" && say(d, i.name, answer ? info(answer) : `[MineDeck 演示] 这里没有真的服务器在跑，「${cmd}」不会真的执行。`));
}

// 刚建的那台服务器的部署任务：按过去了多久算走到第几步。仿真后端查任务时会把 taskList 里同编号的那一条盖在自己的回答上
function drawTask(state, d) {
  const c = d.created, i = c && state.instances.find(x => x.name === c.name);
  if (!i) return;
  const core = CORE_NAME[i.core] || i.core, proxy = i.kind === "proxy";
  const names = [`下载 Java ${i.javaMajor} 运行时`, `下载 ${core} ${i.mc} 核心`, proxy ? "生成配置" : "同意 EULA 并生成配置", "生成启动脚本"];
  const STEP = 1700, past = Date.now() - c.t0, at = Math.min(names.length, Math.floor(past / STEP)), ended = at >= names.length;
  const total = [46, 52, 0, 0][at] * 1048576 || 0, done = Math.round(total * ((past % STEP) / STEP));
  const log = [`[1/4] 准备 Java ${i.javaMajor}`, `[2/4] 下载 ${core} ${i.mc} 的服务端核心`, `[3/4] ${proxy ? "写入配置" : "写入 eula.txt 和 server.properties"}`, "[4/4] 生成启动脚本", `创建完成 ✔ 「${i.name}」可以启动了`];
  let t = state.taskList.find(x => x.id === "task-1");
  if (!t) state.taskList.push(t = { id: "task-1" });
  Object.assign(t, {
    title: `创建 ${core} 服务器`, steps: names.map((name, k) => ({ name, status: k < at ? "done" : k === at ? "running" : "pending" })),
    label: ended ? "" : at === 0 ? `下载 jdk-${i.javaMajor}-windows-x64.zip` : at === 1 ? `下载 ${i.core}-${i.mc}.jar` : "", done, total,
    error: "", warnings: [], log: log.slice(0, ended ? log.length : at + 1), ended, result: i.name,
  });
}

// 返回 true 表示这个请求这里已经答了；false 表示接着交给后面（多数情况只是顺手记一笔）
async function demo(req, res, state, u) {
  const p = u.pathname, m = req.method;
  if (!p.startsWith("/api/")) return false;
  const d = mem(state);

  if (p === "/api/import/modpack" && m === "POST") { err(res, 400, NO_IMPORT); return true; }

  // ---- 从零创建：记下刚建的是哪一台，之后查任务就按时间往前走 ----
  if (p === "/api/instances" && m === "POST") {
    const n = state.instances.length;
    after(res, () => { if (state.instances.length > n) { d.created = { name: state.instances[state.instances.length - 1].name, t0: Date.now() }; drawTask(state, d); } });
    return false;
  }
  if (m === "GET" && (p === "/api/tasks" || p === "/api/tasks/task-1")) { drawTask(state, d); return false; }
  // ---- 接管、导入交接包：仿真后端只记下请求，这里把那一台真的放进列表 ----
  if (p === "/api/adopt" && m === "POST") {
    const was = state.lastAdopt;
    after(res, () => { const b = state.lastAdopt; if (b && b !== was) state.instances.push(newInst({ name: b.name, core: b.core || "neoforge", mc: b.mc || "1.21.1", build: b.build || "", javaMajor: b.javaMajor || 21, xmxMb: b.xmxMb || 6144, port: 25570, dir: b.dir, adopted: true })); });
    return false;
  }
  if (p === "/api/transfer/import" && m === "POST") {
    const was = state.lastTransferImport;
    after(res, () => { const b = state.lastTransferImport; if (b && b !== was) state.instances.push(newInst({ name: b.name, core: "paper", mc: "1.20.1", build: "100", javaMajor: b.javaMajor || 17, xmxMb: b.xmxMb || 6144, port: b.port || 25570, dir: "D:\\MineDeck\\servers\\" + b.name })); });
    return false;
  }
  // 运行时长：演示里启动的服务器，时长从启动那一刻算
  if (p === "/api/instances" && m === "GET") { for (const [name, t0] of d.started) { const i = state.instances.find(x => x.name === name); if (i && i.status === "running") i.uptimeSec = Math.max(1, Math.round((Date.now() - t0) / 1000)); } return false; }

  // ---- 某一台服务器：启动、停止、发命令、控制台 ----
  const mm = p.match(/^\/api\/instances\/([^/]+)\/(console|start|stop|command|players|backups|backups\/restore)$/);
  if (!mm) return false;
  let name = "";
  try { name = decodeURIComponent(mm[1]); } catch {}
  const i = state.instances.find(x => x.name === name), sub = mm[2];
  if (!i) return false; // 交给仿真后端回 404
  if (sub === "console" && m === "GET") {
    // 只是记下这条连接，开头那几行还是后面出；连接一直留着，之后启停、发命令的输出就往这里写
    if (!d.cons.has(i.name)) d.cons.set(i.name, new Set());
    d.cons.get(i.name).add(res);
    req.on("close", () => { const s = d.cons.get(i.name); if (s) s.delete(res); });
    return false;
  }
  if (sub === "players") {
    const r = roster(d, i.name);
    if (m === "GET") { ok(res, { ...r, online: i.status === "running" ? r.online : [] }); return true; }
    if (m !== "POST") return false;
    const b = await readBody(req), who = String(b.player || "").trim(), same = x => (x.name || x).toLowerCase() === who.toLowerCase();
    if (!who) { err(res, 400, "缺少玩家名"); return true; }
    const add = key => { if (!r[key].some(same)) r[key].push({ name: who }); };
    const drop = key => { r[key] = r[key].filter(x => !same(x)); };
    const leave = () => { if (r.online.some(same)) { drop("online"); i.onlineCount = Math.max(0, i.onlineCount - 1); } };
    const act = { "whitelist-add": () => add("whitelist"), "whitelist-remove": () => drop("whitelist"), op: () => add("ops"), deop: () => drop("ops"),
      ban: () => { add("banned"); leave(); }, pardon: () => drop("banned"), kick: leave }[b.action];
    if (!act) { err(res, 400, "不认识的操作：" + b.action); return true; }
    act(); ok(res, "ok");
    return true;
  }
  if (sub === "backups" || sub === "backups/restore") {
    const list = shelf(d, i.name);
    if (sub === "backups" && m === "GET") { ok(res, list); return true; }
    if (sub === "backups" && m === "DELETE") { const f = u.searchParams.get("file"), at = list.findIndex(x => x.file === f); if (at >= 0) list.splice(at, 1); ok(res, "ok"); return true; }
    if (m !== "POST") return false;
    const b = await readBody(req);
    if (sub === "backups/restore") {
      if (i.status !== "stopped") { err(res, 400, "请先停止服务器再还原备份"); return true; }
      if (!list.some(x => x.file === b.file)) { err(res, 400, "备份不存在：" + b.file); return true; }
      list.unshift(newBackup(list, "还原前自动")); ok(res, "ok");
      return true;
    }
    if (i.status === "running") { say(d, i.name, "[MineDeck] 开始热备份：暂停自动保存..."); later(d, 600, () => say(d, i.name, "[MineDeck] 热备份完成，已恢复自动保存")); }
    const one = newBackup(list, b.label);
    list.unshift(one);
    list.length = Math.min(list.length, Math.max(1, +state.config.backupKeep || 10)); // 同真程序：只留最近的若干份
    ok(res, { file: one.file });
    return true;
  }
  if (m !== "POST") return false;
  if (sub === "start") {
    if (i.status !== "stopped") { err(res, 400, "服务器已经在运行了"); return true; }
    startUp(d, i); ok(res, "ok"); return true;
  }
  if (sub === "stop") {
    if (i.status === "stopped") { err(res, 400, "服务器未在运行"); return true; }
    shutDown(d, i); ok(res, "ok"); return true;
  }
  const cmd = String((await readBody(req)).cmd || "").trim().replace(/^\//, "");
  if (i.status === "stopped") { err(res, 400, "服务器未在运行"); return true; }
  if (cmd && state.__demo === d) command(d, i, cmd);
  ok(res, "ok");
  return true;
}

// 场景的 setup 用（proto.create）：摆出一台刚点了「创建」的服务器，部署进度从这一刻开始走
function demoCreate(state, o) {
  const d = mem(state);
  state.instances.push(newInst(o));
  d.created = { name: o.name, t0: Date.now() };
  drawTask(state, d);
}

module.exports = { demo, demoReset, demoCreate };
