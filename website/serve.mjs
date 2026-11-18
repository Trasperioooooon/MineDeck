// 本地预览用的最小静态服务器（只用 Node 自带模块）：node serve.mjs [目录] [端口]
// 行为尽量接近 OSS 静态网站托管：目录请求返回 index.html，找不到时返回 404.html。
import { createServer } from 'node:http';
import { readFile, stat } from 'node:fs/promises';
import { join, resolve, extname, normalize } from 'node:path';
import { fileURLToPath } from 'node:url';
const root = resolve(fileURLToPath(new URL('.', import.meta.url)), process.argv[2] || 'dist');
const port = +(process.argv[3] || 8080);
const TYPES = { '.html': 'text/html; charset=utf-8', '.css': 'text/css; charset=utf-8', '.js': 'text/javascript; charset=utf-8', '.json': 'application/json; charset=utf-8', '.woff2': 'font/woff2', '.png': 'image/png', '.ico': 'image/x-icon', '.txt': 'text/plain; charset=utf-8', '.xml': 'application/xml; charset=utf-8' };
createServer(async (req, res) => {
  let p = decodeURIComponent(new URL(req.url, 'http://x').pathname);
  let f = join(root, normalize(p).replace(/^(\.\.[/\\])+/, ''));
  try { if ((await stat(f)).isDirectory()) f = join(f, 'index.html'); } catch {}
  try {
    const body = await readFile(f);
    res.writeHead(200, { 'Content-Type': TYPES[extname(f)] || 'application/octet-stream' }); res.end(body);
  } catch {
    try { res.writeHead(404, { 'Content-Type': TYPES['.html'] }); res.end(await readFile(join(root, '404.html'))); } catch { res.writeHead(404); res.end('404'); }
  }
}).listen(port, () => console.log(`http://localhost:${port}/  (${root})`));
