// MineDeck 界面原型：在仿真后端（server.js）上跑真实的界面，外面套一个「场景目录」页。
// 用法：node proto.js [端口=27391]，然后用浏览器打开打印出来的地址；或者双击同目录的「打开界面原型.bat」。
//
// 目录页左边按页面列着各种状态（没有服务器、十二种核心各一台、向导的每一步、部署失败、隧道出错……），
// 点一个，右边嵌着的界面就切到那个状态。界面是真的（internal/web/ui/ 原样托管），数据是编的（proto/fiction.js），
// 所以能点、能填、能切标签，只是不会真的下载和开服。场景清单在 proto/scenes.js，加场景只改那一个文件。
//
// 和 server.js 的关系：这里不改仿真后端的行为，只用它的 before 钩子在前面加几样东西——
//   /__proto/…        目录页（index.html）、给访客看的导览页（tour.html），以及它们要的脚本和场景清单（data.js）
//   /__proto/app/…    界面本身。首页带 scene=编号 时先把数据摆成那个场景的样子，再塞进播放器（proto/player.js）
//   少数接口          场景指定的固定回答（proto.api）、演示里多演的几样和每台服务器自己的玩家、备份（proto/demo.js）、
//                     每台服务器自己的控制台输出（proto/fiction.js）
//
// 这个文件也会被打进静态版（proto-static.js），在浏览器里跑：那时没有真的 HTTP 服务器，
// 用的是 proto/static-runtime.js 里的替身，页面和脚本由浏览器自己从文件夹里读，走不到下面托管文件的那几段。
"use strict";
const fs = require("fs");
const path = require("path");
const { URL } = require("url");
const { start, newState, UI_DEFAULT, MOD_CATALOG, PLUGIN_CATALOG } = require("./server");
const { GROUPS, SCENES, base } = require("./proto/scenes");
const { TOUR } = require("./proto/tour");
const { CONSOLES } = require("./proto/fiction");
const { MODS, PLUGINS } = require("./proto/catalog");
const { demo, demoReset, demoCreate } = require("./proto/demo");

const PORT_DEFAULT = 27391; // 仿真后端单独跑用 27390，MineDeck 自己用 27333，三个互不相撞
const DIR = path.join(__dirname, "proto");
const TYPES = { ".html": "text/html", ".js": "application/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" };
const ALL = [...SCENES, ...TOUR]; // 目录页的场景和导览的步骤是同一种东西，按编号都能切过去

// 侧栏左下角的版本号：仿真后端里写死的是个老版本，原型里换成程序现在真正的版本号（internal/app/app.go 的 Version）
const VERSION = (() => {
  try { return (fs.readFileSync(path.join(__dirname, "..", "..", "internal", "app", "app.go"), "utf8").match(/const Version = "([^"]+)"/) || [])[1] || ""; } catch { return ""; }
})();

const json = (res, code, body) => { res.writeHead(code, { "Content-Type": "application/json; charset=utf-8", "Cache-Control": "no-store" }); res.end(JSON.stringify(body)); };
const okData = (res, data) => json(res, 200, { ok: true, data });
const send = (res, type, body) => { res.writeHead(200, { "Content-Type": type + (type.startsWith("image/png") ? "" : "; charset=utf-8"), "Cache-Control": "no-store" }); res.end(body); };

// 仿真后端回到刚启动的样子，再摆成原型的基础数据。state 这个对象本身不能换（server.js 的处理函数握着它），只能原地清空重填
function reset(state, ui) {
  if (state.asst) for (const s of state.asst.sessions || []) for (const r of s.subs || []) { try { r.end(); } catch {} }
  demoReset(state);
  for (const k of Object.keys(state)) delete state[k];
  Object.assign(state, newState(ui));
  Object.defineProperty(state, "__proto_api", { value: {}, enumerable: false, configurable: true, writable: true }); // 不可枚举：/__mock/state 把 state 编成 JSON 时不带它
  base(state);
}

// 把数据摆成某个场景的样子。编号对不上就停在基础数据上
function applyScene(state, id, ui = UI_DEFAULT) {
  const sc = ALL.find(s => s.id === id);
  reset(state, ui);
  if (sc && sc.setup) sc.setup(state, { api: (key, answer) => { state.__proto_api[key] = answer; }, create: o => demoCreate(state, o) });
  return !!sc;
}

// 目录页和导览页要的数据：场景清单、导览的步骤、界面在哪。setup 是函数，不下发；页面只要标题、说明、落点和要点的那几下
function protoData(extra = {}) {
  const strip = ({ setup, ...s }) => s;
  return { static: false, version: VERSION, app: "app/index.html", pages: { catalog: "./", tour: "tour.html" }, groups: GROUPS, scenes: SCENES.map(strip), tour: TOUR.map(strip), ...extra };
}

// 界面的首页，<head> 里多塞一行脚本（Node 版塞播放器，静态版塞 mock.js）。界面的其他文件一个字都不动
function appPage(ui, tag) {
  const html = fs.readFileSync(path.join(ui, "index.html"), "utf8");
  if (!html.includes("<head>")) throw new Error("界面的 index.html 里没找到 <head>，脚本塞不进去");
  return html.replace("<head>", "<head>\n" + tag);
}

function makeBefore(ui) {
  return async (req, res, state) => {
    const u = new URL(req.url, "http://x");
    const p = u.pathname, m = req.method;

    // ---- 目录页、导览页和它们要的东西 ----
    if (p === "/__proto") { res.writeHead(302, { Location: "/__proto/" }); res.end(); return true; } // 少了末尾的斜杠，页面里的相对地址会找错地方
    if (p === "/__proto/") { send(res, "text/html", fs.readFileSync(path.join(DIR, "index.html"))); return true; }
    if (p === "/__proto/data.js") { send(res, "application/javascript", "window.MD_PROTO = " + JSON.stringify(protoData()) + ";\n"); return true; }
    // 界面本身挂在 /__proto/app/ 下：和静态版的摆法一样，目录页、导览页里写的相对地址两边通用
    if (p.startsWith("/__proto/app/")) {
      const rest = p.slice("/__proto/app".length);
      if (m === "GET" && (rest === "/" || rest === "/index.html")) {
        if (u.searchParams.has("scene")) applyScene(state, u.searchParams.get("scene"), ui);
        send(res, "text/html", appPage(ui, '<script src="../player.js"></script>'));
        return true;
      }
      req.url = rest + u.search; // 界面的其他文件交给仿真后端照常托管
      return false;
    }
    if (p.startsWith("/__proto/")) {
      const fp = path.resolve(DIR, path.normalize(p.slice("/__proto/".length)));
      if (!fp.startsWith(DIR + path.sep) || !fs.existsSync(fp) || !fs.statSync(fp).isFile() || !TYPES[path.extname(fp)]) { res.writeHead(404); res.end("not found"); return true; }
      send(res, TYPES[path.extname(fp)], fs.readFileSync(fp));
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

    // ---- 演示里多演的几样：启停、发命令、建完服务器以后的进度（见 proto/demo.js）----
    if (await demo(req, res, state, u)) return true;

    // ---- 每台服务器自己的控制台输出（玩家和备份也是每台一份，因为要能改，放在 demo.js 里）----
    const mm = m === "GET" && p.match(/^\/api\/instances\/([^/]+)\/console$/);
    if (mm) {
      const name = decodeURIComponent(mm[1]);
      const i = (state.empty ? [] : state.instances).find(x => x.name === name);
      if (i && CONSOLES[name] && i.status !== "stopped") {
        res.writeHead(200, { "Content-Type": "text/event-stream", "Cache-Control": "no-cache", Connection: "keep-alive" });
        for (const l of CONSOLES[name]) res.write(`data: ${JSON.stringify(l)}\n\n`);
        return true; // 连接留着，同真的控制台
      }
    }
    return false; // 其余的交给仿真后端
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

module.exports = { startProto, applyScene, protoData, appPage };

// 用系统默认浏览器打开（双击 .bat 时用）
function openBrowser(url) {
  const { spawn } = require("child_process");
  const [cmd, args] = process.platform === "win32" ? ["cmd", ["/c", "start", "", url]] : process.platform === "darwin" ? ["open", [url]] : ["xdg-open", [url]];
  try { spawn(cmd, args, { stdio: "ignore", detached: true, windowsHide: true }).unref(); } catch {}
}

if (require.main === module) {
  const argv = process.argv.slice(2);
  const open = argv.includes("--open");
  const port = +(argv.find(a => /^\d+$/.test(a)) || PORT_DEFAULT);
  const addr = `http://127.0.0.1:${port}/__proto/`;
  startProto({ port })
    .then(m => {
      console.log(`界面原型已启动：${m.url}
给访客看的导览在 ${m.url}tour.html
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
