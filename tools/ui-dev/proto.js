// MineDeck 界面原型：在仿真后端（server.js）上跑真实的界面，外面套一个「场景目录」页。
// 用法：node proto.js [端口=27391]，然后用浏览器打开打印出来的地址；或者双击同目录的「打开界面原型.bat」。
//
// 目录页左边按页面列着各种状态（没有服务器、十二种核心各一台、向导的每一步、部署失败、隧道出错……），
// 点一个，右边嵌着的界面就切到那个状态。界面是真的（internal/web/ui/ 原样托管），数据是编的（proto/fiction.js），
// 所以能点、能填、能切标签，只是不会真的下载和开服。场景清单在 proto/scenes.js，加场景只改那一个文件。
//
// 和 server.js 的关系：这里不改仿真后端的行为，只用它的 before 钩子在前面加三样东西——
//   /__proto/…        目录页自己的页面和接口（场景清单、切换场景）
//   /?proto=1&host=1  给界面首页塞一小段脚本，假装在原生窗口里（标题栏右边才有最小化、最大化、关闭三个键）
//   少数接口          场景指定的固定回答（proto.api），以及每台服务器自己的玩家、备份、控制台（proto/fiction.js）
"use strict";
const fs = require("fs");
const path = require("path");
const { URL } = require("url");
const { start, newState, UI_DEFAULT, MOD_CATALOG, PLUGIN_CATALOG } = require("./server");
const { GROUPS, SCENES, base } = require("./proto/scenes");
const { PLAYERS, BACKUPS, CONSOLES } = require("./proto/fiction");
const { MODS, PLUGINS } = require("./proto/catalog");

const PORT_DEFAULT = 27391; // 仿真后端单独跑用 27390，MineDeck 自己用 27333，三个互不相撞
const DIR = path.join(__dirname, "proto");
const TYPES = { ".html": "text/html", ".js": "application/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" };

// 侧栏左下角的版本号：仿真后端里写死的是个老版本，原型里换成程序现在真正的版本号（internal/app/app.go 的 Version）
const VERSION = (() => {
  try { return (fs.readFileSync(path.join(__dirname, "..", "..", "internal", "app", "app.go"), "utf8").match(/const Version = "([^"]+)"/) || [])[1] || ""; } catch { return ""; }
})();

const json = (res, code, body) => { res.writeHead(code, { "Content-Type": "application/json; charset=utf-8", "Cache-Control": "no-store" }); res.end(JSON.stringify(body)); };
const okData = (res, data) => json(res, 200, { ok: true, data });

// 假装原生窗口：界面只要看到 hostWinClose 这几个函数就会画出窗口三键（见 app.js 的 initHostChrome）
const HOST_SHIM = `<script>(function(){var n=function(){};window.hostWinClose=n;window.hostWinMin=n;window.hostWinMaxToggle=n;window.hostWinDrag=n;window.hostCloseAck=n;window.hostWinHide=n;window.hostWinQuit=n;window.hostWinIsMax=function(){return Promise.resolve(false)};window.hostDblClickTime=function(){return Promise.resolve(500)};})();</script>`;

// 仿真后端回到刚启动的样子，再摆成原型的基础数据。state 这个对象本身不能换（server.js 的处理函数握着它），只能原地清空重填
function reset(state, ui) {
  if (state.asst) for (const s of state.asst.sessions || []) for (const r of s.subs || []) { try { r.end(); } catch {} }
  for (const k of Object.keys(state)) delete state[k];
  Object.assign(state, newState(ui));
  Object.defineProperty(state, "__proto_api", { value: {}, enumerable: false, configurable: true, writable: true }); // 不可枚举：/__mock/state 把 state 编成 JSON 时不带它
  base(state);
}

function makeBefore(ui) {
  return async (req, res, state) => {
    const u = new URL(req.url, "http://x");
    const p = u.pathname, m = req.method;

    // ---- 目录页 ----
    if (p === "/__proto" || p === "/__proto/") {
      res.writeHead(200, { "Content-Type": "text/html; charset=utf-8", "Cache-Control": "no-store" });
      res.end(fs.readFileSync(path.join(DIR, "index.html")));
      return true;
    }
    if (p === "/__proto/scenes" && m === "GET") {
      // setup 是函数，不下发；目录页只要标题、说明、落点和要点的那几下
      okData(res, { groups: GROUPS, scenes: SCENES.map(({ setup, ...s }) => s) });
      return true;
    }
    if (p === "/__proto/apply" && m === "POST") {
      const sc = SCENES.find(s => s.id === u.searchParams.get("id"));
      if (!sc) { json(res, 404, { ok: false, error: "没有这个场景" }); return true; }
      reset(state, ui);
      if (sc.setup) sc.setup(state, { api: (key, answer) => { state.__proto_api[key] = answer; } });
      okData(res, { id: sc.id });
      return true;
    }
    if (p.startsWith("/__proto/")) {
      const fp = path.resolve(DIR, path.normalize(p.slice("/__proto/".length)));
      if (!fp.startsWith(DIR + path.sep) || !fs.existsSync(fp) || !fs.statSync(fp).isFile() || !TYPES[path.extname(fp)]) { res.writeHead(404); res.end("not found"); return true; }
      res.writeHead(200, { "Content-Type": TYPES[path.extname(fp)] + "; charset=utf-8", "Cache-Control": "no-store" });
      res.end(fs.readFileSync(fp));
      return true;
    }

    // ---- 界面首页：假装在原生窗口里 ----
    if (p === "/" && m === "GET" && u.searchParams.get("host") === "1") {
      const html = fs.readFileSync(path.join(ui, "index.html"), "utf8").replace("<head>", "<head>" + HOST_SHIM);
      res.writeHead(200, { "Content-Type": "text/html; charset=utf-8", "Cache-Control": "no-store" });
      res.end(html);
      return true;
    }

    // ---- 版本号换成真的：响应还是仿真后端出，写回去之前改一个字段 ----
    if (VERSION && m === "GET" && (p === "/api/app" || p === "/api/update/check")) {
      const end = res.end.bind(res);
      res.end = body => {
        try { const j = JSON.parse(body); if (j && j.data) { if ("version" in j.data) j.data.version = VERSION; if ("current" in j.data) j.data.current = VERSION; body = JSON.stringify(j); } } catch {}
        return end(body);
      };
      return false;
    }

    // ---- 场景指定的固定回答 ----
    const fixed = state.__proto_api && state.__proto_api[`${m} ${p}`];
    if (fixed) {
      if (fixed.code >= 400) json(res, fixed.code, { ok: false, error: fixed.error || "出错了" });
      else okData(res, fixed.data === undefined ? {} : fixed.data);
      return true;
    }

    // ---- 每台服务器自己的玩家、备份、控制台 ----
    const mm = m === "GET" && p.match(/^\/api\/instances\/([^/]+)\/(players|backups|console)$/);
    if (mm) {
      const name = decodeURIComponent(mm[1]), sub = mm[2];
      const i = (state.empty ? [] : state.instances).find(x => x.name === name);
      if (!i) return false; // 交给仿真后端回 404
      if (sub === "players" && PLAYERS[name]) { okData(res, { ...PLAYERS[name], online: i.status === "running" ? PLAYERS[name].online : [] }); return true; }
      if (sub === "backups" && BACKUPS[name]) { okData(res, BACKUPS[name]); return true; }
      if (sub === "console" && CONSOLES[name] && i.status !== "stopped") {
        res.writeHead(200, { "Content-Type": "text/event-stream", "Cache-Control": "no-cache", Connection: "keep-alive" });
        for (const l of CONSOLES[name]) res.write(`data: ${JSON.stringify(l)}\n\n`);
        return true; // 连接留着，同真的控制台
      }
    }
    return false;
  };
}

// 热门清单的目录：仿真后端自带的那份开头四个留着（测试和前置检查的假数据认它们的编号），
// 后面「Hot Mod 005」这种占位的换成 proto/catalog.js 里像样的名字。只影响这个进程，测试用的仿真后端不受影响。
function dressCatalog(list, real) {
  const keep = list.slice(0, 4), have = new Set(keep.map(x => x.projectId));
  list.length = 0;
  list.push(...keep, ...real.filter(x => !have.has(x.projectId)));
  list.sort((a, b) => b.downloads - a.downloads);
}

async function startProto({ port = PORT_DEFAULT, ui = UI_DEFAULT } = {}) {
  ui = path.resolve(ui);
  dressCatalog(MOD_CATALOG, MODS);
  dressCatalog(PLUGIN_CATALOG, PLUGINS);
  const m = await start({ port, ui, before: makeBefore(ui) });
  reset(m.state, ui);
  return { ...m, url: m.url + "__proto/" };
}

module.exports = { startProto };

// 用系统默认浏览器打开（双击 .bat 时用）
function openBrowser(url) {
  const { spawn } = require("child_process");
  const [cmd, args] = process.platform === "win32" ? ["cmd", ["/c", "start", "", url]] : process.platform === "darwin" ? ["open", [url]] : ["xdg-open", [url]];
  try { spawn(cmd, args, { stdio: "ignore", detached: true }).unref(); } catch {}
}

if (require.main === module) {
  const argv = process.argv.slice(2);
  const open = argv.includes("--open");
  const port = +(argv.find(a => /^\d+$/.test(a)) || PORT_DEFAULT);
  const addr = `http://127.0.0.1:${port}/__proto/`;
  startProto({ port })
    .then(m => {
      console.log(`界面原型已启动：${m.url}
共 ${SCENES.length} 个场景。看完把这个窗口关掉（或按 Ctrl+C）就停了。`);
      if (open) openBrowser(m.url);
    })
    .catch(e => {
      if (e.code !== "EADDRINUSE") { console.error(e); process.exit(1); }
      // 端口占着，多半是原型已经开着一份：双击启动时直接打开那一份
      console.error(`端口 ${port} 已经被占用，可能原型已经开着了：${addr}
要换端口就运行 node proto.js <端口>`);
      if (open) openBrowser(addr);
      process.exit(open ? 0 : 1);
    });
}
