// 构建检查（ARCH.md 第 9 节）：扫描 dist，确保自动加载的资源只走同源相对地址或允许名单，防止第三方资源和国外 CDN 漏到浏览器运行时。
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';

const CDN = /fonts\.googleapis\.com|fonts\.gstatic\.com|cdn\.jsdelivr\.net|unpkg\.com|cdnjs\.cloudflare\.com/;

// 自动加载资源允许的外站来源（origin，如 https://example.com）。
// 目前构建产物里的脚本、样式表、图片、字体和静态索引全部使用同源相对地址，未依赖任何外站资源，名单为空。
export const ALLOWED_RESOURCE_ORIGINS = new Set([]);

const AUTO_LOAD_LINK_RELS = new Set([
  'stylesheet', 'preload', 'modulepreload', 'prefetch',
  'icon', 'apple-touch-icon', 'mask-icon', 'manifest',
  'preconnect', 'dns-prefetch',
]);

function* walk(dir) {
  for (const f of readdirSync(dir)) {
    const p = join(dir, f);
    if (statSync(p).isDirectory()) yield* walk(p); else yield p;
  }
}

function decodeHtmlEntities(s) {
  return String(s ?? '')
    .replace(/&#x([0-9a-f]+);/gi, (_, h) => String.fromCodePoint(parseInt(h, 16)))
    .replace(/&#(\d+);/g, (_, d) => String.fromCodePoint(parseInt(d, 10)))
    .replace(/&amp;/gi, '&')
    .replace(/&quot;/gi, '"')
    .replace(/&#39;|&apos;/gi, "'");
}

function cleanUrl(raw) {
  return String(raw ?? '').trim().replace(/^(['"])([\s\S]*)\1$/, '$2').trim();
}

function isAllowedResourceUrl(raw) {
  const u = cleanUrl(raw);
  if (!u || u.startsWith('#')) return true;
  if (/^[/\\]{2}/.test(u) || u.startsWith('\\')) return false;
  if (/^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(u)) {
    if (!/^https?:/i.test(u)) return false;
    try {
      const probe = u.replace(/\$\{[^}]*\}/g, 'x');
      const parsed = new URL(probe);
      return ALLOWED_RESOURCE_ORIGINS.has(parsed.origin.toLowerCase());
    } catch {
      return false;
    }
  }
  return true;
}

function getAttr(tagAttrs, name) {
  const re = new RegExp(`(?:^|\\s)${name}\\s*=\\s*(?:"([^"]*)"|'([^']*)'|([^\\s"'=<>` + '`' + `]+))`, 'i');
  const m = tagAttrs.match(re);
  if (!m) return null;
  return decodeHtmlEntities(m[1] ?? m[2] ?? m[3] ?? '');
}

function parseSrcset(val) {
  const urls = [];
  let s = String(val ?? '').trim();
  while (s.length) {
    s = s.replace(/^[\s,]+/, '');
    if (!s) break;
    const sp = s.search(/\s/);
    let url;
    if (sp < 0) {
      if (/^data:/i.test(s)) { url = s; s = ''; }
      else {
        const c = s.indexOf(',');
        if (c >= 0) { url = s.slice(0, c); s = s.slice(c + 1); }
        else { url = s; s = ''; }
      }
    } else {
      const token = s.slice(0, sp);
      const rest = s.slice(sp);
      if (!/^data:/i.test(token) && token.includes(',')) {
        const c = token.indexOf(',');
        url = token.slice(0, c);
        s = token.slice(c + 1) + rest;
      } else {
        url = token.replace(/,+$/, '');
        const nextComma = rest.indexOf(',');
        s = nextComma >= 0 ? rest.slice(nextComma + 1) : '';
      }
    }
    if (url) urls.push(url);
  }
  return urls;
}

function checkCssResources(cssText, rel, prefix, problems) {
  const stripped = cssText.replace(/\/\*[\s\S]*?\*\//g, '');
  for (const m of stripped.matchAll(/\burl\(\s*(?:"([^"]*)"|'([^']*)'|([^)'"\s][^)]*?))\s*\)/gi)) {
    const raw = (m[1] ?? m[2] ?? m[3] ?? '').trim();
    if (!isAllowedResourceUrl(raw)) problems.push(`${rel}: ${prefix}url() 使用了未允许的地址：${raw}`);
  }
  for (const m of stripped.matchAll(/@import\s+(?:url\(\s*)?(?:"([^"]*)"|'([^']*)')/gi)) {
    const raw = (m[1] ?? m[2] ?? '').trim();
    if (!isAllowedResourceUrl(raw)) problems.push(`${rel}: ${prefix}@import 使用了未允许的地址：${raw}`);
  }
}

function checkJsResources(jsText, rel, problems) {
  for (const m of jsText.matchAll(/\bfetch\s*\(\s*(?:new\s+(?:Request|URL)\s*\(\s*)?(["'`])((?:\\.|(?!\1)[^\\\r\n])*)\1?/g)) {
    const raw = m[2].trim();
    if (!isAllowedResourceUrl(raw)) problems.push(`${rel}: fetch() 使用了未允许的地址：${raw}`);
  }
}

export function checkDist(out) {
  const problems = [];
  for (const file of walk(out)) {
    if (!/\.(html|css|js)$/.test(file)) continue; // data/*.json、feed.xml 是给人和客户端看的公开数据，可以含 GitHub 备用地址
    const rel = relative(out, file).replace(/\\/g, '/');
    const s = readFileSync(file, 'utf8');
    if (CDN.test(s)) problems.push(`${rel}: 引用了国外 CDN`);
    if (rel.endsWith('.css')) {
      checkCssResources(s, rel, 'CSS ', problems);
      continue;
    }
    if (rel.endsWith('.js')) {
      checkJsResources(s, rel, problems);
      continue;
    }
    if (rel.endsWith('.html')) {
      for (const m of s.matchAll(/<script\b([^>]*)>/gi)) {
        const src = getAttr(m[1], 'src');
        if (src != null && !isAllowedResourceUrl(src)) {
          problems.push(`${rel}: <script src> 使用了未允许的地址：${src}`);
        }
      }
      for (const m of s.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/gi)) {
        const src = getAttr(m[1], 'src');
        if (src != null) continue;
        const type = (getAttr(m[1], 'type') || '').trim().toLowerCase();
        if (type === 'application/json' && getAttr(m[1], 'id') === 'fb-config') {
          try {
            const fb = JSON.parse(m[2]);
            for (const ch of fb.channels || []) {
              if (ch && ch.type === 'endpoint' && ch.url && !isAllowedResourceUrl(ch.url)) {
                problems.push(`${rel}: 反馈接口地址使用了未允许的地址：${ch.url}`);
              }
            }
          } catch { /* 无效 JSON 由页面自身测试暴露 */ }
        } else if (!type || /javascript|ecmascript|module/.test(type)) {
          checkJsResources(m[2], rel, problems);
        }
      }
      const noScript = s.replace(/<script\b([^>]*)>[\s\S]*?<\/script>/gi, '<script$1></script>');
      for (const m of noScript.matchAll(/<style\b[^>]*>([\s\S]*?)<\/style>/gi)) {
        checkCssResources(m[1], rel, '行内 <style> ', problems);
      }
      for (const m of noScript.matchAll(/<link\b([^>]*)>/gi)) {
        const relAttr = getAttr(m[1], 'rel') || '';
        const relTokens = relAttr.toLowerCase().split(/\s+/).filter(Boolean);
        if (!relTokens.some((t) => AUTO_LOAD_LINK_RELS.has(t))) continue;
        const href = getAttr(m[1], 'href');
        if (href != null && !isAllowedResourceUrl(href)) {
          problems.push(`${rel}: <link rel="${relAttr}" href> 使用了未允许的地址：${href}`);
        }
      }
      for (const m of noScript.matchAll(/<(img|source|iframe|video|audio|embed|track|input)\b([^>]*)>/gi)) {
        const tag = m[1].toLowerCase();
        const src = getAttr(m[2], 'src');
        if (src != null && !isAllowedResourceUrl(src)) {
          problems.push(`${rel}: <${tag} src> 使用了未允许的地址：${src}`);
        }
        const srcset = getAttr(m[2], 'srcset');
        if (srcset != null) {
          for (const u of parseSrcset(srcset)) {
            if (!isAllowedResourceUrl(u)) problems.push(`${rel}: <${tag} srcset> 使用了未允许的地址：${u}`);
          }
        }
        const poster = getAttr(m[2], 'poster');
        if (poster != null && !isAllowedResourceUrl(poster)) {
          problems.push(`${rel}: <${tag} poster> 使用了未允许的地址：${poster}`);
        }
      }
      for (const m of noScript.matchAll(/<object\b([^>]*)>/gi)) {
        const data = getAttr(m[1], 'data');
        if (data != null && !isAllowedResourceUrl(data)) {
          problems.push(`${rel}: <object data> 使用了未允许的地址：${data}`);
        }
      }
      for (const m of noScript.matchAll(/<[a-z][a-z0-9-]*\b([^>]*)>/gi)) {
        for (const attr of ['data-src', 'data-index-json', 'data-index-js']) {
          const val = getAttr(m[1], attr);
          if (val != null && !isAllowedResourceUrl(val)) {
            problems.push(`${rel}: ${attr} 使用了未允许的地址：${val}`);
          }
        }
        const styleVal = getAttr(m[1], 'style');
        if (styleVal) checkCssResources(styleVal, rel, '行内 style ', problems);
      }
      // 模板把对象或缺失的文案直接插进页面时会留下这些字样
      const text = s.replace(/<script[\s\S]*?<\/script>/g, '');
      for (const bad of ['[object Object]', 'undefined', 'NaN']) {
        if (new RegExp(`(>|"|\\s)${bad.replace(/[[\]]/g, '\\$&')}(<|"|\\s)`).test(text)) {
          problems.push(`${rel}: 页面里出现了“${bad}”`);
        }
      }
      for (const m of noScript.matchAll(/<a\b([^>]*)>/gi)) {
        const href = getAttr(m[1], 'href');
        if (href && /^https?:\/\/([\w-]+\.)?github(usercontent)?\.com/i.test(href) && !/\bdata-ext\b/.test(m[1])) {
          problems.push(`${rel}: GitHub 链接没有经由 extLink()：${href}`);
        }
      }
    }
  }
  return problems;
}
