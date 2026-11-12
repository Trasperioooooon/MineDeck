// v3.1.0 宣传片的小型静态服务：/ 和 /scene.* 是本目录的场景页，/cover.html 和 /cover/ 是封面和它的底图，/ui/ 是界面目录（字体），/out/ 是素材和截图。
// 单独运行可在浏览器里预览：node tools/promo/video-v3.1/serve.js [端口=27411]，然后打开
//   http://127.0.0.1:27411/?t=12.5        停在第 12.5 秒
//   http://127.0.0.1:27411/?play=1        按真实时间播放（只用来看，渲染不靠它）
//   http://127.0.0.1:27411/?t=12.5&debug  左上角显示时间和小节
"use strict";
const http = require("http");
const fs = require("fs");
const path = require("path");

const HERE = __dirname;
const REPO = path.resolve(HERE, "..", "..", "..");
const UI = path.join(REPO, "internal", "web", "ui");
const OUT = path.join(HERE, "out");
const TYPES = { ".html": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8", ".js": "application/javascript; charset=utf-8",
  ".json": "application/json; charset=utf-8", ".png": "image/png", ".woff2": "font/woff2", ".svg": "image/svg+xml" };

function start(port = 0) {
  const server = http.createServer((req, res) => {
    const p = decodeURIComponent(new URL(req.url, "http://x").pathname);
    let root, rel;
    if (p === "/" || p === "/index.html") [root, rel] = [HERE, "scene.html"];
    else if (/^\/scene\.(html|css|js)$/.test(p) || p === "/captions.js") [root, rel] = [HERE, p.slice(1)];
    else if (p === "/cover.html") [root, rel] = [HERE, "cover.html"];
    else if (p.startsWith("/cover/")) [root, rel] = [path.join(HERE, "cover"), p.slice(7)];
    else if (p.startsWith("/ui/")) [root, rel] = [UI, p.slice(4)];
    else if (p.startsWith("/out/")) [root, rel] = [OUT, p.slice(5)];
    else { res.writeHead(404); return res.end(); }
    const fp = path.resolve(root, rel);
    if (!fp.startsWith(root + path.sep) || !fs.existsSync(fp) || !fs.statSync(fp).isFile()) { res.writeHead(404); return res.end(); }
    res.writeHead(200, { "Content-Type": TYPES[path.extname(fp)] || "application/octet-stream", "Cache-Control": "no-store" });
    res.end(fs.readFileSync(fp));
  });
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, "127.0.0.1", () => {
      const port = server.address().port;
      resolve({ port, url: `http://127.0.0.1:${port}/`, close: () => new Promise(r => { server.closeAllConnections(); server.close(r); }) });
    });
  });
}

module.exports = { start };

if (require.main === module) {
  start(+(process.argv[2] || 27411)).then(s => console.log(`预览：${s.url}?t=0（Ctrl+C 退出）`)).catch(e => { console.error(e); process.exit(1); });
}
