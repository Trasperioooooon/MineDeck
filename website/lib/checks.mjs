// 构建检查（ARCH.md 第 9 节）：扫描 dist，防止 GitHub 资源或国外 CDN 漏到浏览器运行时。
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';

const GH = /(?:^|[/.])(github\.com|githubusercontent\.com|api\.github\.com)/;
const CDN = /fonts\.googleapis\.com|fonts\.gstatic\.com|cdn\.jsdelivr\.net|unpkg\.com|cdnjs\.cloudflare\.com/;

function* walk(dir) {
  for (const f of readdirSync(dir)) {
    const p = join(dir, f);
    if (statSync(p).isDirectory()) yield* walk(p); else yield p;
  }
}

export function checkDist(out) {
  const problems = [];
  for (const file of walk(out)) {
    if (!/\.(html|css|js)$/.test(file)) continue; // data/*.json、feed.xml 是给人和客户端看的公开数据，可以含 GitHub 备用地址
    const rel = relative(out, file);
    const s = readFileSync(file, 'utf8');
    if (CDN.test(s)) problems.push(`${rel}: 引用了国外 CDN`);
    for (const m of s.matchAll(/\b(src|srcset|poster|data-src)="([^"]*)"/g)) if (GH.test(m[2])) problems.push(`${rel}: ${m[1]} 指向 GitHub：${m[2]}`);
    for (const m of s.matchAll(/url\(([^)]*)\)/g)) if (GH.test(m[1])) problems.push(`${rel}: CSS url() 指向 GitHub`);
    for (const m of s.matchAll(/fetch\(\s*['"`]([^'"`]*)/g)) if (GH.test(m[1])) problems.push(`${rel}: fetch() 请求 GitHub`);
    if (rel.endsWith('.html')) {
      // 模板把对象或缺失的文案直接插进页面时会留下这些字样
      const text = s.replace(/<script[\s\S]*?<\/script>/g, '');
      for (const bad of ['[object Object]', 'undefined', 'NaN']) if (new RegExp(`(>|"|\\s)${bad.replace(/[[\]]/g, '\\$&')}(<|"|\\s)`).test(text)) problems.push(`${rel}: 页面里出现了“${bad}”`);
      for (const m of s.matchAll(/<a\b([^>]*)>/g)) {
        const h = m[1].match(/href="([^"]*)"/);
        if (h && /^https?:\/\/([\w-]+\.)?github(usercontent)?\.com/.test(h[1]) && !/\bdata-ext\b/.test(m[1])) problems.push(`${rel}: GitHub 链接没有经由 extLink()：${h[1]}`);
      }
    }
  }
  return problems;
}
