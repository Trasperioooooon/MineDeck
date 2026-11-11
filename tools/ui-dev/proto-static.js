// MineDeck 界面原型 · 静态版：把原型做成一个不需要任何服务器的文件夹。
// 用法：node proto-static.js [输出目录=proto-dist] [--ui=<界面目录>] [--version=<版本号>] [--no-catalog] [--open] [--serve[=端口]]
//   --open 生成后用默认浏览器打开 index.html（file://）；--serve 生成后起一个只发文件的静态服务器（默认 27392 端口），看放到网站上的样子
// 或者双击同目录的「生成并打开在线体验.bat」。
//
// 产物双击 index.html 就能看（file://），整个文件夹丢到随便哪个静态托管上也能看，放在网站的子目录下也行：
//   index.html     给访客看的导览（proto/tour.html），官网的「在线体验」用的就是它
//   catalog.html   评审用的场景目录（proto/index.html）；加 --no-catalog 不出这一页
//   stage.js       两张页面共用的「舞台」        data.js   场景清单和导览的步骤
//   mock.js        仿真后端 + 浏览器里的替身 + 播放器，跑在界面那一页里（见 proto/static-runtime.js 开头的说明）
//   app/           界面文件，从 internal/web/ui/ 原样拷过来；只有 index.html 动了两处：<head> 里多一行引 mock.js 的脚本，
//                  预加载字体的三行挪给 mock.js 去做（原因见下面 PRELOAD 的说明）
//
// 和 Node 版原型（proto.js）是同一套东西：同一份场景清单、同一个仿真后端、同一个舞台和播放器，
// 区别只是仿真后端跑在浏览器里，由 mock.js 接管界面的 fetch 和 EventSource，不发任何真的网络请求。
// 只用 Node 自带的模块，不用 npm install。官网的构建脚本直接调 buildStatic()。
"use strict";
const fs = require("fs");
const path = require("path");
const vm = require("vm");

const HERE = __dirname;
const ROOT = path.resolve(HERE, "..", "..");
const UI_DEFAULT = path.join(ROOT, "internal", "web", "ui");
const OUT_DEFAULT = path.join(HERE, "proto-dist");
const MARK = ".minedeck-proto-static"; // 产物里放一个记号：下次生成前要清空文件夹，有这个记号才敢删

const posix = f => "/" + path.relative(ROOT, f).split(path.sep).join("/"); // 模块在包里的名字：从仓库根算起的路径

// 从入口开始，把一路 require 到的本地模块都找出来（Node 自带的模块在浏览器里由替身顶上，不用打进去）
function collect(entry) {
  const mods = {};
  (function walk(file) {
    const id = posix(file);
    if (id in mods) return;
    const src = mods[id] = fs.readFileSync(file, "utf8");
    for (const m of src.matchAll(/\brequire\(\s*["'](\.\.?\/[^"']+)["']\s*\)/g)) {
      let f = path.resolve(path.dirname(file), m[1]);
      if (!f.endsWith(".js")) f += ".js";
      // 这是照着字面找的，注释里举例写的 require("./x") 也会被找到：文件不存在就跳过。真漏了模块，浏览器里 require 时会说缺哪个
      if (fs.existsSync(f)) walk(f);
    }
  })(entry);
  return mods;
}

// 仿真后端启动时要 server.properties 的全部键和默认值：在这里把界面的 props-catalog.js 求出来，浏览器里就不用 eval 了
function propsOf(ui) {
  const ctx = { window: {} };
  vm.runInNewContext(fs.readFileSync(path.join(ui, "props-catalog.js"), "utf8"), ctx);
  return (ctx.window.PROPS_CATALOG || []).map(p => ({ key: p.key, def: p.def }));
}

function versionOf() {
  try { return (fs.readFileSync(path.join(ROOT, "internal", "app", "app.go"), "utf8").match(/const Version = "([^"]+)"/) || [])[1] || ""; } catch { return ""; }
}

// 界面首页里有三行 <link rel="preload" as="font" crossorigin>，让字体早点开始下载。双击打开（file://）时浏览器会拦下这种请求，
// 在控制台里报跨域的错（字体本身照常能用，它走的是样式表里的 @font-face）。所以静态版的首页里把这几行拿掉，
// 改由 mock.js 在不是 file:// 的时候补回去：放在网站上照样提前下载，双击打开时控制台是干净的。
// （要是哪天界面首页里这几行换了写法、这里认不出来了，静态版的自检会在控制台报错那一项上查出来。）
const PRELOAD = /[ \t]*<link\b(?=[^>]*\brel=["']preload["'])[^>]*>\r?\n?/gi;
const attr = (tag, name) => (new RegExp(`\\s${name}=["']([^"']+)["']`, "i").exec(tag) || [])[1];
const preloadsOf = html => (html.match(PRELOAD) || []).map(tag => ({ href: attr(tag, "href"), as: attr(tag, "as"), type: attr(tag, "type"), cors: /\scrossorigin\b/i.test(tag) })).filter(x => x.href);

function bundle(ui, version, preloads) {
  const mods = collect(path.join(HERE, "proto.js"));
  const files = {
    "/internal/app/app.go": `const Version = ${JSON.stringify(version)}`, // proto.js 只从里面找这一行
    "/internal/web/ui/props-catalog.js": "", // 内容用不上（见 __PROPS），只要「有这个文件」
  };
  return [
    "/* MineDeck 界面原型 · 静态版的仿真后端。这是生成的文件，别手改：改 tools/ui-dev 下的源码，再运行 node proto-static.js。 */",
    "(function () {",
    '"use strict";',
    `const __ENTRY = ${JSON.stringify(posix(path.join(HERE, "proto.js")))};`,
    `const __FILES = ${JSON.stringify(files)};`,
    `const __PROPS = ${JSON.stringify(propsOf(ui))};`,
    `const __PRELOAD = ${JSON.stringify(preloads)};`,
    "const __MODULES = {",
    // 开头的 #! 行在 Node 里可以有，包进函数里就是语法错误，去掉
    ...Object.entries(mods).map(([id, src]) => `${JSON.stringify(id)}: function (module, exports, require, __dirname, __filename, Buffer, process) {\n${src.replace(/^#!.*/, "")}\n},`),
    "};",
    fs.readFileSync(path.join(HERE, "proto", "static-runtime.js"), "utf8"),
    "})();",
    fs.readFileSync(path.join(HERE, "proto", "player.js"), "utf8"),
  ].join("\n");
}

const sizeOf = dir => fs.readdirSync(dir, { withFileTypes: true }).reduce((n, e) => n + (e.isDirectory() ? sizeOf(path.join(dir, e.name)) : fs.statSync(path.join(dir, e.name)).size), 0);

// 生成静态版。out：输出目录；ui：界面目录；version：侧栏左下角显示的版本号；catalog: false 不出评审用的目录页（官网用）
function buildStatic({ out = OUT_DEFAULT, ui = UI_DEFAULT, version = versionOf(), catalog = true } = {}) {
  out = path.resolve(out); ui = path.resolve(ui);
  if (!fs.existsSync(path.join(ui, "index.html"))) throw new Error("界面目录里没有 index.html：" + ui);
  // 输出目录生成前要整个清空，所以不许它是仓库或界面目录本身，也不许是它们的上层；也不许在界面目录里面（会把自己拷进自己）
  const within = (child, parent) => { const r = path.relative(parent, child); return !r || (!r.startsWith("..") && !path.isAbsolute(r)); };
  if (within(ROOT, out) || within(ui, out) || within(out, ui) || out === HERE) throw new Error("输出目录不能放在这里：" + out + "\n它不能是仓库、界面目录或它们的上层，也不能在界面目录里面。");
  if (fs.existsSync(out) && fs.readdirSync(out).length && !fs.existsSync(path.join(out, MARK)))
    throw new Error(`输出目录里已经有别的东西，而且不是上次生成的产物，不敢清空：${out}\n换一个目录，或者先自己把它删掉。`);
  fs.rmSync(out, { recursive: true, force: true });
  fs.mkdirSync(out, { recursive: true });
  fs.writeFileSync(path.join(out, MARK), "这个文件夹是 tools/ui-dev/proto-static.js 生成的，重新生成时会整个清空。\n");

  // 界面：原样拷过来，只有首页多一行脚本
  const { protoData, appPage } = require("./proto");
  fs.cpSync(ui, path.join(out, "app"), { recursive: true });
  const page = appPage(ui, '<script src="../mock.js"></script>');
  fs.writeFileSync(path.join(out, "app", "index.html"), page.replace(PRELOAD, ""));
  fs.writeFileSync(path.join(out, "mock.js"), bundle(ui, version, preloadsOf(page)));

  // 外面的两张页面和它们要的东西
  fs.copyFileSync(path.join(HERE, "proto", "stage.js"), path.join(out, "stage.js"));
  fs.copyFileSync(path.join(HERE, "proto", "tour.html"), path.join(out, "index.html"));
  if (catalog) fs.copyFileSync(path.join(HERE, "proto", "index.html"), path.join(out, "catalog.html"));
  // bytes：点「开始体验」以后要加载多少（界面 + 仿真后端），导览页拿它告诉访客
  const bytes = sizeOf(path.join(out, "app")) + fs.statSync(path.join(out, "mock.js")).size;
  const data = protoData({ static: true, version, bytes, pages: { catalog: catalog ? "catalog.html" : "", tour: "index.html" }, ...(catalog ? {} : { groups: [], scenes: [] }) });
  fs.writeFileSync(path.join(out, "data.js"), "window.MD_PROTO = " + JSON.stringify(data) + ";\n");

  return { out, bytes, total: sizeOf(out), tour: data.tour.length, scenes: data.scenes.length };
}

// 一个最简单的静态文件服务器：只把文件原样发出去，没有任何接口。用来确认产物放到静态托管上也能用。
// prefix：挂在哪个路径下（自检用 /some/sub/，确认放在网站的子目录里也不坏）。返回 { url, close() }
function serveStatic(dir, { port = 0, prefix = "/" } = {}) {
  const http = require("http");
  const TYPES = { ".html": "text/html; charset=utf-8", ".js": "application/javascript; charset=utf-8", ".css": "text/css; charset=utf-8", ".txt": "text/plain; charset=utf-8", ".png": "image/png", ".svg": "image/svg+xml", ".woff2": "font/woff2" };
  dir = path.resolve(dir);
  prefix = ("/" + prefix + "/").replace(/\/+/g, "/"); // 前后都带斜杠，按整段路径比
  const server = http.createServer((req, res) => {
    let p;
    try { p = decodeURIComponent(new URL(req.url, "http://x").pathname); } catch { p = ""; }
    const f = p.startsWith(prefix) ? path.join(dir, p.slice(prefix.length), p.endsWith("/") ? "index.html" : "") : "";
    const rel = f ? path.relative(dir, f) : "..";
    if (!rel || rel.startsWith("..") || path.isAbsolute(rel) || !fs.existsSync(f) || !fs.statSync(f).isFile()) { res.writeHead(404, { "Content-Type": "text/plain; charset=utf-8" }); return res.end("not found"); }
    res.writeHead(200, { "Content-Type": TYPES[path.extname(f)] || "application/octet-stream", "Cache-Control": "no-store" });
    fs.createReadStream(f).pipe(res);
  });
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, "127.0.0.1", () => resolve({ url: `http://127.0.0.1:${server.address().port}${prefix}`, close: () => new Promise(r => { server.closeAllConnections(); server.close(r); }) }));
  });
}

module.exports = { buildStatic, serveStatic, OUT_DEFAULT };

if (require.main === module) {
  const argv = process.argv.slice(2);
  const opt = name => { const a = argv.find(x => x.startsWith(`--${name}=`)); return a ? a.slice(name.length + 3) : undefined; };
  const dir = argv.find(a => !a.startsWith("--"));
  const mb = n => (n / 1048576).toFixed(1) + " MB";
  try {
    const r = buildStatic({ out: dir, ui: opt("ui"), version: opt("version"), catalog: !argv.includes("--no-catalog") });
    const index = path.join(r.out, "index.html");
    console.log(`静态版已生成：${r.out}
整个文件夹 ${mb(r.total)}；导览 ${r.tour} 步${r.scenes ? `，评审用的目录页 ${r.scenes} 个场景` : ""}。
双击里面的 index.html 就能看，或者把整个文件夹放到任何静态托管上。`);
    const open = target => {
      const { spawn } = require("child_process");
      // Windows 上不经过 cmd：路径里有 & 这类字符时，cmd 会把它当成命令的一部分
      const [cmd, args] = process.platform === "win32" ? ["rundll32", ["url.dll,FileProtocolHandler", target]] : process.platform === "darwin" ? ["open", [target]] : ["xdg-open", [target]];
      try { spawn(cmd, args, { stdio: "ignore", detached: true, windowsHide: true }).unref(); } catch {}
    };
    const serve = argv.find(a => a === "--serve" || a.startsWith("--serve="));
    if (serve) {
      serveStatic(r.out, { port: +(serve.split("=")[1] || 27392) }).then(s => {
        console.log(`
静态服务器已启动：${s.url}（只发文件，没有任何接口。Ctrl+C 退出）`);
        if (argv.includes("--open")) open(s.url);
      }, e => { console.error(e.code === "EADDRINUSE" ? "端口被占用了，换一个：--serve=<端口>" : e); process.exit(1); });
    } else if (argv.includes("--open")) open(index);
  } catch (e) { console.error(String(e && e.message || e)); process.exit(1); }
}
